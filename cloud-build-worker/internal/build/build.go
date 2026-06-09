package build

import (
	"context"
	"io"
	"sync"

	"github.com/goplus/llar/cloud-build-worker/internal/artifact"
	"github.com/goplus/llar/cloud-build-worker/internal/upload"
)

type Target struct {
	Module  string
	Version string
}

type Matrix struct {
	Require map[string]string `json:"require"`
	Options map[string]string `json:"options,omitempty"`
}

type Request struct {
	Target    Target
	MatrixStr string
	Matrix    Matrix
}

type Result struct {
	Artifact artifact.Artifact
}

type Runner interface {
	Run(ctx context.Context, req Request, log io.Writer) (RunResult, error)
}

type RunResult struct {
	Archive  io.ReadSeeker
	Type     string
	Metadata string
}

type Options struct {
	Artifacts artifact.Store
	Uploader  upload.Uploader
	Runner    Runner
}

type Builds struct {
	artifacts artifact.Store
	uploader  upload.Uploader
	runner    Runner

	mu      sync.Mutex
	entries map[artifact.Key]*entry
}

func New(opts Options) *Builds {
	return &Builds{
		artifacts: opts.Artifacts,
		uploader:  opts.Uploader,
		runner:    opts.Runner,
		entries:   map[artifact.Key]*entry{},
	}
}

func (b *Builds) Build(ctx context.Context, req Request, log io.Writer) (Result, error) {
	key := artifact.Key{
		Module:    req.Target.Module,
		Version:   req.Target.Version,
		MatrixStr: req.MatrixStr,
	}

	if stored, ok, err := b.artifacts.Get(ctx, key); err != nil {
		return Result{}, err
	} else if ok {
		return Result{Artifact: stored}, nil
	}

	b.mu.Lock()
	e, ok := b.entries[key]
	if !ok {
		e = newEntry()
		b.entries[key] = e
	}
	e.addLogWriter(log)
	if !ok {
		go b.run(context.WithoutCancel(ctx), key, req, e)
	}
	b.mu.Unlock()

	result, err := e.wait(ctx)
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

func (b *Builds) run(ctx context.Context, key artifact.Key, req Request, e *entry) {
	runResult, err := b.runner.Run(ctx, req, e)
	if err != nil {
		e.complete(Result{}, err)
		b.deleteEntry(key, e)
		return
	}

	uploaded, err := b.uploader.Upload(ctx, runResult.Archive, upload.Options{
		Name: key.Module + ":" + key.Version,
		Type: runResult.Type,
		Attrs: map[string]string{
			"org.llar.matrix": key.MatrixStr,
		},
	})
	if err != nil {
		e.complete(Result{}, err)
		b.deleteEntry(key, e)
		return
	}

	value := artifact.Artifact{
		Source: artifact.Source{
			Type: b.uploader.Type(),
			URL:  uploaded.URL,
		},
		Type:     runResult.Type,
		Metadata: runResult.Metadata,
		Checksum: uploaded.Checksum,
	}
	stored, err := b.artifacts.Put(ctx, key, value)
	if err != nil {
		e.complete(Result{}, err)
		b.deleteEntry(key, e)
		return
	}
	e.complete(Result{Artifact: stored}, nil)
	b.deleteEntry(key, e)
}

func (b *Builds) deleteEntry(key artifact.Key, e *entry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.entries[key] == e {
		delete(b.entries, key)
	}
}

type entry struct {
	mu      sync.Mutex
	done    chan struct{}
	result  Result
	err     error
	writers []io.Writer
}

func newEntry() *entry {
	return &entry{done: make(chan struct{})}
}

func (e *entry) addLogWriter(w io.Writer) {
	if w == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.writers = append(e.writers, w)
}

func (e *entry) Write(p []byte) (int, error) {
	e.mu.Lock()
	writers := append([]io.Writer(nil), e.writers...)
	e.mu.Unlock()

	for _, w := range writers {
		if _, err := w.Write(p); err != nil {
			e.removeLogWriter(w)
		}
	}
	return len(p), nil
}

func (e *entry) removeLogWriter(w io.Writer) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, existing := range e.writers {
		if existing == w {
			e.writers = append(e.writers[:i], e.writers[i+1:]...)
			return
		}
	}
}

func (e *entry) complete(result Result, err error) {
	e.mu.Lock()
	e.result = result
	e.err = err
	e.mu.Unlock()
	close(e.done)
}

func (e *entry) wait(ctx context.Context) (Result, error) {
	select {
	case <-e.done:
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.result, e.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}
