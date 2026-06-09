package upload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

type GHCRConfig struct {
	Owner string
	Token string
}

func NewGHCR(cfg GHCRConfig) Uploader {
	return ghcrUploader{cfg: cfg}
}

type ghcrUploader struct {
	cfg GHCRConfig
}

func (ghcrUploader) Type() string {
	return "ghcr"
}

func (u ghcrUploader) Upload(ctx context.Context, r io.ReadSeeker, opts Options) (Result, error) {
	offset, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return Result{}, err
	}

	result, err := checksumResult(r)
	if err != nil {
		return Result{}, err
	}
	_, copyErr := io.Copy(io.Discard, ctxReader{ctx: ctx, r: r})
	_, restoreErr := r.Seek(offset, io.SeekStart)
	if copyErr != nil {
		return Result{}, copyErr
	}
	if restoreErr != nil {
		return Result{}, restoreErr
	}
	digest := result.Checksum[len("sha256:"):]
	result.URL = fmt.Sprintf("https://ghcr.io/v2/%s/%s/blobs/sha256:%s", u.cfg.Owner, opts.Name, digest)
	return result, nil
}

func checksumResult(r io.ReadSeeker) (Result, error) {
	offset, err := r.Seek(0, io.SeekCurrent)
	if err != nil {
		return Result{}, err
	}

	h := sha256.New()
	size, err := io.Copy(h, r)
	_, restoreErr := r.Seek(offset, io.SeekStart)
	if err != nil {
		return Result{}, err
	}
	if restoreErr != nil {
		return Result{}, restoreErr
	}
	return Result{
		Size:     size,
		Checksum: "sha256:" + hex.EncodeToString(h.Sum(nil)),
	}, nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (r ctxReader) Read(p []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.r.Read(p)
	}
}
