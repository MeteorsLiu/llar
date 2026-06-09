package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])

	var sawStart bool
	var sawComplete bool
	var tokenRequests int
	var startAttempts int
	var completeAttempts int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/token":
			tokenRequests++
			if got := r.URL.Query().Get("service"); got != "ghcr.io" {
				t.Fatalf("token service = %q, want ghcr.io", got)
			}
			if got := r.URL.Query().Get("scope"); got != "repository:example/owner/module:push,pull" {
				t.Fatalf("token scope = %q, want repository:example/owner/module:push,pull", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"registry-token"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v2/example/owner/module/blobs/uploads/":
			startAttempts++
			if r.Header.Get("Authorization") == "Bearer token" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+server.URL+`/token",service="ghcr.io",scope="repository:example/owner/module:push,pull"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			sawStart = true
			if got := r.Header.Get("Authorization"); got != "Bearer registry-token" {
				t.Fatalf("start Authorization = %q, want Bearer registry-token", got)
			}
			w.Header().Set("Location", "/v2/example/owner/module/blobs/uploads/session")
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodPut && r.URL.Path == "/v2/example/owner/module/blobs/uploads/session":
			completeAttempts++
			if r.Header.Get("Authorization") == "Bearer token" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+server.URL+`/token",service="ghcr.io",scope="repository:example/owner/module:push,pull"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			sawComplete = true
			if got := r.Header.Get("Authorization"); got != "Bearer registry-token" {
				t.Fatalf("complete Authorization = %q, want Bearer registry-token", got)
			}
			if got := r.Header.Get("Content-Type"); got != "application/octet-stream" {
				t.Fatalf("Content-Type = %q, want application/octet-stream", got)
			}
			queryDigest, err := url.QueryUnescape(r.URL.Query().Get("digest"))
			if err != nil {
				t.Fatalf("digest query unescape: %v", err)
			}
			if queryDigest != "sha256:"+digest {
				t.Fatalf("digest query = %q, want sha256:%s", queryDigest, digest)
			}
			gotBody, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll body: %v", err)
			}
			if !bytes.Equal(gotBody, payload) {
				t.Fatalf("uploaded body = %q, want %q", gotBody, payload)
			}
			w.WriteHeader(http.StatusCreated)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	r := bytes.NewReader(append([]byte("skip"), payload...))
	if _, err := r.Seek(int64(len("skip")), 0); err != nil {
		t.Fatalf("Seek returned error: %v", err)
	}

	uploader := ghcrUploader{
		cfg:    GHCRConfig{Owner: "example", Token: "token"},
		client: server.Client(),
		base:   server.URL,
	}
	got, err := uploader.Upload(context.Background(), r, Options{Name: "owner/module"})
	if err != nil {
		t.Fatalf("Upload returned error: %v", err)
	}

	want := Result{
		URL:      server.URL + "/v2/example/owner/module/blobs/sha256:" + digest,
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
	if !sawStart {
		t.Fatal("upload start request was not sent")
	}
	if !sawComplete {
		t.Fatal("upload complete request was not sent")
	}
	if tokenRequests != 2 {
		t.Fatalf("token requests = %d, want 2", tokenRequests)
	}
	if startAttempts != 2 {
		t.Fatalf("start attempts = %d, want 2", startAttempts)
	}
	if completeAttempts != 2 {
		t.Fatalf("complete attempts = %d, want 2", completeAttempts)
	}
}
