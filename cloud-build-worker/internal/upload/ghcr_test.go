package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestChecksumResultReadsFromCurrentOffsetAndRestoresReader(t *testing.T) {
	r := bytes.NewReader([]byte("prefixartifact-bytes"))
	if _, err := r.Seek(int64(len("prefix")), 0); err != nil {
		t.Fatalf("Seek returned error: %v", err)
	}

	got, err := checksumResult(r)
	if err != nil {
		t.Fatalf("checksumResult returned error: %v", err)
	}

	payload := []byte("artifact-bytes")
	sum := sha256.Sum256(payload)
	wantChecksum := "sha256:" + hex.EncodeToString(sum[:])
	if got.Size != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", got.Size, len(payload))
	}
	if got.Checksum != wantChecksum {
		t.Fatalf("checksum = %q, want %q", got.Checksum, wantChecksum)
	}

	offset, err := r.Seek(0, 1)
	if err != nil {
		t.Fatalf("Seek current returned error: %v", err)
	}
	if offset != int64(len("prefix")) {
		t.Fatalf("offset = %d, want %d", offset, len("prefix"))
	}
}

func TestGHCRUploaderReturnsSourceURLChecksumAndSize(t *testing.T) {
	payload := []byte("archive-bytes")
	r := bytes.NewReader(append([]byte("skip"), payload...))
	if _, err := r.Seek(int64(len("skip")), 0); err != nil {
		t.Fatalf("Seek returned error: %v", err)
	}

	uploader := NewGHCR(GHCRConfig{Owner: "example"})
	got, err := uploader.Upload(context.Background(), r, Options{Name: "owner/module"})
	if err != nil {
		t.Fatalf("Upload returned error: %v", err)
	}

	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	want := Result{
		URL:      "https://ghcr.io/v2/example/owner/module/blobs/sha256:" + digest,
		Size:     int64(len(payload)),
		Checksum: "sha256:" + digest,
	}
	if got != want {
		t.Fatalf("Upload returned %+v, want %+v", got, want)
	}
	if uploader.Type() != "ghcr" {
		t.Fatalf("Type = %q, want ghcr", uploader.Type())
	}

	offset, err := r.Seek(0, 1)
	if err != nil {
		t.Fatalf("Seek current returned error: %v", err)
	}
	if offset != int64(len("skip")) {
		t.Fatalf("offset = %d, want %d", offset, len("skip"))
	}
}
