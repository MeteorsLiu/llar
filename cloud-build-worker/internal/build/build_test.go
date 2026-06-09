package build

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/goplus/llar/cloud-build-worker/internal/artifact"
	"github.com/goplus/llar/cloud-build-worker/internal/upload"
)

func TestBuildReturnsCompletedArtifactBeforeJoiningLocalEntry(t *testing.T) {
	ctx := context.Background()
	key := artifact.Key{Module: "owner/mod", Version: "v1.0.0", MatrixStr: "amd64-linux"}
	ready := artifact.Artifact{
		Source:   artifact.Source{Type: "ghcr", URL: "https://example.test/blob"},
		Type:     "zip",
		Metadata: "-lmod",
		Checksum: "sha256:ready",
	}
	store := &fakeStore{artifacts: map[artifact.Key]artifact.Artifact{key: ready}}
	runner := &fakeRunner{}
	builds := New(Options{
		Artifacts: store,
		Uploader:  &fakeUploader{},
		Runner:    runner,
	})

	got, err := builds.Build(ctx, requestFor(key), nil)
	if err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if got.Artifact != ready {
		t.Fatalf("artifact = %+v, want %+v", got.Artifact, ready)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
}

func TestBuildSharesActiveEntryForSameArtifactKey(t *testing.T) {
	ctx := context.Background()
	key := artifact.Key{Module: "owner/mod", Version: "v1.0.0", MatrixStr: "amd64-linux"}
	store := &fakeStore{}
	runner := &fakeRunner{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		archive:  []byte("archive"),
		metadata: "-lmod",
	}
	builds := New(Options{
		Artifacts: store,
		Uploader:  &fakeUploader{result: upload.Result{URL: "https://example.test/blob", Checksum: "sha256:uploaded"}},
		Runner:    runner,
	})

	var wg sync.WaitGroup
	var first, second Result
	var firstErr, secondErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		first, firstErr = builds.Build(ctx, requestFor(key), nil)
	}()
	<-runner.started

	wg.Add(1)
	go func() {
		defer wg.Done()
		second, secondErr = builds.Build(ctx, requestFor(key), nil)
	}()

	runner.release <- struct{}{}
	wg.Wait()

	if firstErr != nil {
		t.Fatalf("first Build error: %v", firstErr)
	}
	if secondErr != nil {
		t.Fatalf("second Build error: %v", secondErr)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if first.Artifact != second.Artifact {
		t.Fatalf("artifacts differ: first %+v second %+v", first.Artifact, second.Artifact)
	}
	if first.Artifact.Source.URL != "https://example.test/blob" {
		t.Fatalf("artifact URL = %q", first.Artifact.Source.URL)
	}
}

func TestBuildWritesRawLogsToProvidedWriter(t *testing.T) {
	ctx := context.Background()
	key := artifact.Key{Module: "owner/mod", Version: "v1.0.0", MatrixStr: "amd64-linux"}
	store := &fakeStore{}
	runner := &fakeRunner{
		archive:  []byte("archive"),
		metadata: "-lmod",
		logs:     []byte("building\n"),
	}
	var logs bytes.Buffer
	builds := New(Options{
		Artifacts: store,
		Uploader:  &fakeUploader{result: upload.Result{URL: "https://example.test/blob", Checksum: "sha256:uploaded"}},
		Runner:    runner,
	})

	if _, err := builds.Build(ctx, requestFor(key), &logs); err != nil {
		t.Fatalf("Build returned error: %v", err)
	}
	if logs.String() != "building\n" {
		t.Fatalf("logs = %q, want building newline", logs.String())
	}
}

func TestBuildDoesNotCompleteWhenArtifactPutFails(t *testing.T) {
	ctx := context.Background()
	key := artifact.Key{Module: "owner/mod", Version: "v1.0.0", MatrixStr: "amd64-linux"}
	putErr := errors.New("put failed")
	store := &fakeStore{putErr: putErr}
	builds := New(Options{
		Artifacts: store,
		Uploader:  &fakeUploader{result: upload.Result{URL: "https://example.test/blob", Checksum: "sha256:uploaded"}},
		Runner:    &fakeRunner{archive: []byte("archive"), metadata: "-lmod"},
	})

	got, err := builds.Build(ctx, requestFor(key), nil)
	if !errors.Is(err, putErr) {
		t.Fatalf("Build error = %v, want %v", err, putErr)
	}
	if got != (Result{}) {
		t.Fatalf("result = %+v, want zero", got)
	}
	if _, ok, err := store.Get(ctx, key); err != nil || ok {
		t.Fatalf("stored artifact after failed Put: ok=%v err=%v", ok, err)
	}
}

func TestBuildContinuesAfterFirstWaiterCancels(t *testing.T) {
	key := artifact.Key{Module: "owner/mod", Version: "v1.0.0", MatrixStr: "amd64-linux"}
	store := &fakeStore{}
	runner := &fakeRunner{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		archive:  []byte("archive"),
		metadata: "-lmod",
	}
	builds := New(Options{
		Artifacts: store,
		Uploader:  &fakeUploader{result: upload.Result{URL: "https://example.test/blob", Checksum: "sha256:uploaded"}},
		Runner:    runner,
	})

	ctx, cancel := context.WithCancel(context.Background())
	var err error
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err = builds.Build(ctx, requestFor(key), nil)
	}()
	<-runner.started
	cancel()
	<-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("first Build error = %v, want context.Canceled", err)
	}

	runner.release <- struct{}{}
	got, err := builds.Build(context.Background(), requestFor(key), nil)
	if err != nil {
		t.Fatalf("second Build returned error: %v", err)
	}
	if got.Artifact.Source.URL != "https://example.test/blob" {
		t.Fatalf("artifact URL = %q", got.Artifact.Source.URL)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
}

func requestFor(key artifact.Key) Request {
	return Request{
		Target:    Target{Module: key.Module, Version: key.Version},
		MatrixStr: key.MatrixStr,
		Matrix:    Matrix{Require: map[string]string{"arch": "amd64", "os": "linux"}},
	}
}

type fakeStore struct {
	mu        sync.Mutex
	artifacts map[artifact.Key]artifact.Artifact
	putErr    error
}

func (s *fakeStore) Get(_ context.Context, key artifact.Key) (artifact.Artifact, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	got, ok := s.artifacts[key]
	return got, ok, nil
}

func (s *fakeStore) Put(_ context.Context, key artifact.Key, value artifact.Artifact) (artifact.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putErr != nil {
		return artifact.Artifact{}, s.putErr
	}
	if s.artifacts == nil {
		s.artifacts = map[artifact.Key]artifact.Artifact{}
	}
	s.artifacts[key] = value
	return value, nil
}

func (s *fakeStore) Delete(_ context.Context, key artifact.Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.artifacts, key)
	return nil
}

type fakeUploader struct {
	result upload.Result
	err    error
}

func (u *fakeUploader) Type() string {
	return "ghcr"
}

func (u *fakeUploader) Upload(context.Context, io.ReadSeeker, upload.Options) (upload.Result, error) {
	if u.err != nil {
		return upload.Result{}, u.err
	}
	return u.result, nil
}

type fakeRunner struct {
	mu       sync.Mutex
	calls    int
	started  chan struct{}
	release  chan struct{}
	archive  []byte
	metadata string
	logs     []byte
	err      error
}

func (r *fakeRunner) Run(_ context.Context, _ Request, log io.Writer) (RunResult, error) {
	r.mu.Lock()
	r.calls++
	if r.started != nil && r.calls == 1 {
		close(r.started)
	}
	r.mu.Unlock()

	if r.release != nil {
		select {
		case <-r.release:
		case <-time.After(2 * time.Second):
			return RunResult{}, errors.New("timed out waiting for release")
		}
	}
	if len(r.logs) > 0 && log != nil {
		if _, err := log.Write(r.logs); err != nil {
			return RunResult{}, err
		}
	}
	if r.err != nil {
		return RunResult{}, r.err
	}
	return RunResult{
		Archive:  bytes.NewReader(r.archive),
		Type:     "zip",
		Metadata: r.metadata,
	}, nil
}
