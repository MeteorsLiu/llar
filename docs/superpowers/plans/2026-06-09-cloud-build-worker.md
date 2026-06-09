# Cloud Build Worker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `llar install` remote cache-miss handling through a hardcoded cloud-build worker and implement the worker `POST /v1/jobs` path.

**Architecture:** Keep install semantics in `llar`, share only protocol types in `internal/cloudbuild`, and keep worker runtime modules under `cmd/worker/internal/{build,artifact,upload}`. The worker handles one HTTP endpoint, coordinates in-process active builds by artifact key, writes completed artifact metadata through `artifact.Store`, and uploads archive bytes through `upload.Uploader`.

**Tech Stack:** Go 1.24, Cobra, Gin, `database/sql`, standard `net/http` client/server tests, existing LLAR `internal/build` and `internal/modules`.

---

## File Structure

- Create `internal/cloudbuild/types.go`: shared wire structs for request, status, log, artifact, and selected matrix.
- Create `internal/cloudbuild/target.go`: parse and format `X-LLAR-Target`.
- Create `internal/cloudbuild/client.go`: small HTTP client used by `llar install`; worker URL is a hardcoded package constant.
- Modify `cmd/llar/internal/matrix_flags.go`: keep existing CLI parsing behavior but also return selected matrix maps and generate `matrixStr` with `formula.Matrix.Combinations()[0]` semantics.
- Modify `cmd/llar/internal/matrix_flags_test.go`: update matrix string expectations and add selected-matrix tests.
- Create `internal/build/install.go`: exported helper methods for local cache lookup, install directory, and cache metadata writes.
- Create `internal/build/install_test.go`: tests for those helper methods.
- Modify `cmd/llar/internal/install.go`: implement `llar install` remote cache-miss path with hardcoded worker client.
- Create `cmd/llar/internal/install_test.go`: tests for worker submit, artifact download, checksum verification, extraction, and cache metadata writes.
- Create `cmd/worker/internal/artifact/artifact.go`: artifact key/types aliases and Store interface.
- Create `cmd/worker/internal/artifact/sql.go`: `database/sql` store for the `artifacts` table.
- Create `cmd/worker/internal/artifact/sql_test.go`: table creation, Get, Put idempotency, checksum conflict, Delete.
- Create `cmd/worker/internal/upload/upload.go`: uploader interface and archive upload result contract.
- Create `cmd/worker/internal/upload/ghcr.go`: GHCR uploader implementation.
- Create `cmd/worker/internal/upload/ghcr_test.go`: tests for checksum/size calculation and request shape against an `httptest.Server`.
- Create `cmd/worker/internal/build/build.go`: `Builds.Build(ctx, req, log)` orchestration.
- Create `cmd/worker/internal/build/build_test.go`: active entry sharing, log fanout, artifact lookup ordering, Put-before-complete behavior.
- Create `cmd/worker/main.go`: Gin server and `POST /v1/jobs` handler.
- Create `cmd/worker/main_test.go`: HTTP validation, non-verbose response, verbose streaming response.
- Modify `go.mod` and `go.sum`: add Gin and the SQL test driver dependencies when the worker server and artifact SQL tests are added.

## Task 1: Shared Cloud Build Protocol

**Files:**
- Create: `internal/cloudbuild/types.go`
- Create: `internal/cloudbuild/target.go`
- Test: `internal/cloudbuild/target_test.go`

- [ ] **Step 1: Write target parsing tests**

Create `internal/cloudbuild/target_test.go`:

```go
package cloudbuild

import "testing"

func TestParseTargetHeader(t *testing.T) {
	got, err := ParseTargetHeader("pnggroup/libpng@v1.6.47#amd64-linux|false")
	if err != nil {
		t.Fatalf("ParseTargetHeader: %v", err)
	}
	if got.Module != "pnggroup/libpng" || got.Version != "v1.6.47" || got.MatrixStr != "amd64-linux|false" {
		t.Fatalf("target = %#v", got)
	}
	if got.String() != "pnggroup/libpng@v1.6.47#amd64-linux|false" {
		t.Fatalf("String() = %q", got.String())
	}
}

func TestParseTargetHeaderRejectsInvalidValues(t *testing.T) {
	for _, input := range []string{"", "pnggroup/libpng", "pnggroup/libpng@v1.6.47", "pnggroup/libpng#amd64-linux", "@v1#x", "m@#x", "m@v#"} {
		if _, err := ParseTargetHeader(input); err == nil {
			t.Fatalf("ParseTargetHeader(%q) error = nil", input)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cloudbuild`

Expected: FAIL because `internal/cloudbuild` does not exist.

- [ ] **Step 3: Add shared protocol types**

Create `internal/cloudbuild/types.go`:

```go
package cloudbuild

type Matrix struct {
	Require map[string]string `json:"require"`
	Options map[string]string `json:"options,omitempty"`
}

type JobRequest struct {
	Matrix Matrix `json:"matrix"`
}

type JobState string

const (
	MessageTypeStatus = "status"
	MessageTypeLog    = "log"

	JobCompleted JobState = "completed"
	JobFailed    JobState = "failed"
)

type StatusMessage struct {
	Type  string     `json:"type"`
	State JobState   `json:"state"`
	Body  StatusBody `json:"body"`
}

type StatusBody struct {
	Artifact *Artifact `json:"artifact,omitempty"`
	Status   int       `json:"status,omitempty"`
	Message  string    `json:"message,omitempty"`
}

type LogMessage struct {
	Type string  `json:"type"`
	Data LogData `json:"data"`
}

type LogData struct {
	Stream string `json:"stream,omitempty"`
	Text   string `json:"text"`
}

type Artifact struct {
	Source   Source `json:"source"`
	Type     string `json:"type"`
	Metadata string `json:"metadata"`
	Checksum string `json:"checksum"`
}

type Source struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}
```

- [ ] **Step 4: Add target parser**

Create `internal/cloudbuild/target.go`:

```go
package cloudbuild

import (
	"fmt"
	"strings"
)

const TargetHeader = "X-LLAR-Target"

type Target struct {
	Module    string
	Version   string
	MatrixStr string
}

func ParseTargetHeader(value string) (Target, error) {
	beforeMatrix, matrixStr, ok := strings.Cut(value, "#")
	if !ok || matrixStr == "" {
		return Target{}, fmt.Errorf("invalid %s", TargetHeader)
	}
	module, version, ok := strings.Cut(beforeMatrix, "@")
	if !ok || module == "" || version == "" {
		return Target{}, fmt.Errorf("invalid %s", TargetHeader)
	}
	return Target{Module: module, Version: version, MatrixStr: matrixStr}, nil
}

func (t Target) String() string {
	return t.Module + "@" + t.Version + "#" + t.MatrixStr
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/cloudbuild`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cloudbuild
git commit -m "feat: add cloud build protocol types"
```

## Task 2: Matrix Selection For Install Requests

**Files:**
- Modify: `cmd/llar/internal/matrix_flags.go`
- Modify: `cmd/llar/internal/matrix_flags_test.go`

- [ ] **Step 1: Write failing matrix selection tests**

Add to `cmd/llar/internal/matrix_flags_test.go`:

```go
func TestParseMatrixSelectionReturnsRequestBodyMatrix(t *testing.T) {
	gotArgs, selected, matrixStr, err := parseMatrixSelectionArgs([]string{"madler/zlib@v1.3.1", "--arch", "amd64", "--os", "linux", "--matrix-debug=false"}, makeMatrixFlagSet())
	if err != nil {
		t.Fatalf("parseMatrixSelectionArgs: %v", err)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "madler/zlib@v1.3.1" {
		t.Fatalf("args = %#v", gotArgs)
	}
	if matrixStr != "amd64-linux|false" {
		t.Fatalf("matrixStr = %q, want amd64-linux|false", matrixStr)
	}
	if selected.Require["arch"] != "amd64" || selected.Require["os"] != "linux" {
		t.Fatalf("require = %#v", selected.Require)
	}
	if selected.Options["debug"] != "false" {
		t.Fatalf("options = %#v", selected.Options)
	}
}
```

Update existing expectations:

```go
// "amd64-linux|debug=true,output=custom" becomes "amd64-linux|true-custom"
// "amd64-linux|output=custom" becomes "amd64-linux|custom"
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/llar/internal -run Matrix`

Expected: FAIL because `parseMatrixSelectionArgs` is missing and old matrix string expectations still use `key=value`.

- [ ] **Step 3: Implement selected matrix parsing**

Modify `cmd/llar/internal/matrix_flags.go` so `parseMatrixArgs` delegates to a new helper:

```go
type selectedMatrix struct {
	Require map[string]string
	Options map[string]string
}

func parseMatrixArgs(args []string, flags *pflag.FlagSet) ([]string, string, error) {
	args, _, matrixStr, err := parseMatrixSelectionArgs(args, flags)
	return args, matrixStr, err
}

func parseMatrixSelectionArgs(args []string, flags *pflag.FlagSet) ([]string, selectedMatrix, string, error) {
	matrixFlags := map[string]matrixFlagDef{}
	parseFlags := true
	// keep the existing flag discovery loop
	// keep resetMatrixFlags and flags.Parse
	selected := selectedMatrixFromFlags(flags, matrixFlags)
	matrixStr, err := encodeSelectedMatrix(selected)
	if err != nil {
		return nil, selectedMatrix{}, "", err
	}
	return flags.Args(), selected, matrixStr, nil
}
```

Implement `encodeSelectedMatrix` through `formula.Matrix.Combinations()[0]`:

```go
func encodeSelectedMatrix(selected selectedMatrix) (string, error) {
	if len(selected.Require) == 0 && len(selected.Options) == 0 {
		return hostMatrixCombo(), nil
	}
	m := formula.Matrix{
		Require: singleValueMatrix(selected.Require),
		Options: singleValueMatrix(selected.Options),
	}
	combinations := m.Combinations()
	if len(combinations) == 0 {
		return "", fmt.Errorf("empty matrix")
	}
	return combinations[0], nil
}
```

Use `arch` and `os` as `Require`; all other matrix flags are `Options`. Preserve the current validation that `os` requires `arch`.

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/llar/internal -run Matrix`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/llar/internal/matrix_flags.go cmd/llar/internal/matrix_flags_test.go
git commit -m "fix: align matrix string generation with formula combinations"
```

## Task 3: Local Install Cache Helpers

**Files:**
- Create: `internal/build/install.go`
- Create: `internal/build/install_test.go`

- [ ] **Step 1: Write tests for exported install helpers**

Create `internal/build/install_test.go`:

```go
package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallHelpersLookupAndSaveCacheEntry(t *testing.T) {
	b, err := NewBuilder(Options{WorkspaceDir: t.TempDir(), MatrixStr: "amd64-linux"})
	if err != nil {
		t.Fatalf("NewBuilder: %v", err)
	}

	if _, _, ok, err := b.LookupInstallCache("test/liba", "1.0.0"); err != nil || ok {
		t.Fatalf("LookupInstallCache before save = ok %v err %v", ok, err)
	}

	dir, err := b.InstallDir("test/liba", "1.0.0")
	if err != nil {
		t.Fatalf("InstallDir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := b.SaveInstallCache("test/liba", "1.0.0", "-lA"); err != nil {
		t.Fatalf("SaveInstallCache: %v", err)
	}

	gotDir, metadata, ok, err := b.LookupInstallCache("test/liba", "1.0.0")
	if err != nil {
		t.Fatalf("LookupInstallCache: %v", err)
	}
	if !ok || gotDir != dir || metadata != "-lA" {
		t.Fatalf("cache = dir %q metadata %q ok %v", gotDir, metadata, ok)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/build -run InstallHelpers`

Expected: FAIL because helper methods are missing.

- [ ] **Step 3: Add helper methods**

Create `internal/build/install.go`:

```go
package build

import "time"

func (b *Builder) InstallDir(modPath, version string) (string, error) {
	return b.installDir(modPath, version)
}

func (b *Builder) LookupInstallCache(modPath, version string) (installDir string, metadata string, ok bool, err error) {
	cache, err := b.loadCache(modPath)
	if err != nil {
		return "", "", false, nil
	}
	entry, ok := cache.get(version, b.matrix)
	if !ok {
		return "", "", false, nil
	}
	dir, err := b.installDir(modPath, version)
	if err != nil {
		return "", "", false, err
	}
	return dir, entry.Metadata, true, nil
}

func (b *Builder) SaveInstallCache(modPath, version, metadata string) error {
	cache, err := b.loadCache(modPath)
	if err != nil {
		cache = &buildCache{}
	}
	cache.set(version, b.matrix, &buildEntry{Metadata: metadata, BuildTime: time.Now()})
	return b.saveCache(modPath, cache)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/build -run InstallHelpers`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/build/install.go internal/build/install_test.go
git commit -m "feat: expose install cache helpers"
```

## Task 4: Artifact Metadata Store

**Files:**
- Create: `cmd/worker/internal/artifact/artifact.go`
- Create: `cmd/worker/internal/artifact/sql.go`
- Create: `cmd/worker/internal/artifact/sql_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add SQL test driver dependency**

Run: `go get modernc.org/sqlite`

Expected: `go.mod` and `go.sum` include the SQL driver used by artifact store tests. The artifact module itself remains a `database/sql` store and does not expose a SQLite-specific API.

- [ ] **Step 2: Write SQL store tests**

Create `cmd/worker/internal/artifact/sql_test.go` with tests named:

```go
func TestSQLStoreGetMissAndPut(t *testing.T)
func TestSQLStorePutSameChecksumIsIdempotent(t *testing.T)
func TestSQLStorePutDifferentChecksumConflicts(t *testing.T)
func TestSQLStoreDelete(t *testing.T)
```

Use this setup in the tests:

```go
db, err := sql.Open("sqlite", ":memory:")
if err != nil {
	t.Fatalf("sql.Open: %v", err)
}
t.Cleanup(func() { db.Close() })
store, err := NewSQLStore(db)
if err != nil {
	t.Fatalf("NewSQLStore: %v", err)
}
```

Assert that a checksum conflict returns `ErrConflict`.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./cmd/worker/internal/artifact`

Expected: FAIL because artifact package is missing.

- [ ] **Step 4: Add artifact package and SQL store**

Create `cmd/worker/internal/artifact/artifact.go`:

```go
package artifact

import (
	"context"
	"errors"

	"github.com/goplus/llar/internal/cloudbuild"
)

var ErrConflict = errors.New("artifact checksum conflict")

type Key struct {
	Module    string
	Version   string
	MatrixStr string
}

type Artifact = cloudbuild.Artifact
type Source = cloudbuild.Source

type Store interface {
	Get(ctx context.Context, key Key) (Artifact, bool, error)
	Put(ctx context.Context, key Key, artifact Artifact) (Artifact, error)
	Delete(ctx context.Context, key Key) error
}
```

Create `cmd/worker/internal/artifact/sql.go` with `NewSQLStore(db *sql.DB) (*SQLStore, error)`. `NewSQLStore` creates this table if missing:

```sql
CREATE TABLE IF NOT EXISTS artifacts (
  module      TEXT NOT NULL,
  version     TEXT NOT NULL,
  matrix_str  TEXT NOT NULL,
  source_type TEXT NOT NULL,
  source_url  TEXT NOT NULL,
  type        TEXT NOT NULL,
  metadata    TEXT NOT NULL,
  checksum    TEXT NOT NULL,
  created_at  TIMESTAMP NOT NULL,
  expires_at  TIMESTAMP NULL,
  PRIMARY KEY (module, version, matrix_str)
);
```

Implement `Put` as: insert when missing, return existing artifact when checksum matches, return `ErrConflict` when checksum differs.

- [ ] **Step 5: Run tests**

Run: `go test ./cmd/worker/internal/artifact`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum cmd/worker/internal/artifact
git commit -m "feat: add worker artifact metadata store"
```

## Task 5: Upload Module

**Files:**
- Create: `cmd/worker/internal/upload/upload.go`
- Create: `cmd/worker/internal/upload/ghcr.go`
- Create: `cmd/worker/internal/upload/ghcr_test.go`

- [ ] **Step 1: Write uploader contract tests**

Create `cmd/worker/internal/upload/ghcr_test.go`:

```go
package upload

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestUploadComputesChecksumFromCurrentOffset(t *testing.T) {
	r := bytes.NewReader([]byte("prefix-archive"))
	if _, err := r.Seek(int64(len("prefix-")), 0); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	got, err := checksumResult(context.Background(), r, "https://example.test/blob")
	if err != nil {
		t.Fatalf("checksumResult: %v", err)
	}
	if got.Size != int64(len("archive")) {
		t.Fatalf("Size = %d", got.Size)
	}
	if got.Checksum == "" || !strings.HasPrefix(got.URL, "https://example.test/") {
		t.Fatalf("Result = %#v", got)
	}
	pos, _ := r.Seek(0, 1)
	if pos != int64(len("prefix-")) {
		t.Fatalf("reader offset = %d", pos)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/worker/internal/upload`

Expected: FAIL because upload package is missing.

- [ ] **Step 3: Add upload interface and test uploader**

Create `cmd/worker/internal/upload/upload.go`:

```go
package upload

import (
	"context"
	"io"
)

type Options struct {
	Name  string
	Type  string
	Attrs map[string]string
}

type Result struct {
	URL      string
	Size     int64
	Checksum string
}

type Uploader interface {
	Type() string
	Upload(ctx context.Context, r io.ReadSeeker, opts Options) (Result, error)
}
```

Add an unexported checksum helper used by tests and the GHCR implementation. It must read from the current offset, compute SHA-256 and size, then seek back to the original offset.

- [ ] **Step 4: Add GHCR uploader skeleton backed by the checksum helper**

Create `cmd/worker/internal/upload/ghcr.go` with:

```go
type GHCRConfig struct {
	Owner string
	Token string
}

func NewGHCR(cfg GHCRConfig) Uploader
```

The first implementation must build upload requests through a small internal HTTP client method so `ghcr_test.go` can verify:

```text
Options.Name  = ghcr.io/<owner>/<module>:<version>
Options.Type  = zip
Options.Attrs = {"org.llar.matrix": "<matrixStr>"}
Result.URL    = https://ghcr.io/v2/<owner>/<module>/blobs/sha256:<digest>
```

- [ ] **Step 5: Run tests**

Run: `go test ./cmd/worker/internal/upload`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/worker/internal/upload
git commit -m "feat: add worker artifact uploader"
```

## Task 6: Worker Build Coordination

**Files:**
- Create: `cmd/worker/internal/build/build.go`
- Create: `cmd/worker/internal/build/build_test.go`

- [ ] **Step 1: Write build coordination tests**

Create `cmd/worker/internal/build/build_test.go` with tests named:

```go
func TestBuildReturnsCompletedArtifactBeforeJoiningLocalEntry(t *testing.T)
func TestBuildSharesActiveEntryForSameArtifactKey(t *testing.T)
func TestBuildWritesLogsOnlyToProvidedWriter(t *testing.T)
func TestBuildDoesNotCompleteWhenArtifactPutFails(t *testing.T)
```

Use fake `artifact.Store`, fake `upload.Uploader`, and fake LLAR runner. The fake runner should accept `Request` and an `io.Writer`, write `"building\n"` to the writer when non-nil, and return an archive reader plus metadata.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/worker/internal/build`

Expected: FAIL because build package is missing.

- [ ] **Step 3: Add build package public interface**

Create `cmd/worker/internal/build/build.go`:

```go
package build

import (
	"context"
	"io"

	"github.com/goplus/llar/cmd/worker/internal/artifact"
	"github.com/goplus/llar/cmd/worker/internal/upload"
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

type Options struct {
	Artifacts artifact.Store
	Uploader  upload.Uploader
	Runner    Runner
}

type Runner interface {
	Run(ctx context.Context, req Request, log io.Writer) (RunResult, error)
}

type RunResult struct {
	Archive  io.ReadSeeker
	Type     string
	Metadata string
}

type Builds struct {
	// mutex, entries, and dependencies
}

func New(opts Options) *Builds
func (b *Builds) Build(ctx context.Context, req Request, log io.Writer) (Result, error)
```

- [ ] **Step 4: Implement active entry coordination**

Implement this ordering exactly:

```text
1. artifact.Get
2. if hit, return completed result
3. lock entries map
4. if entry exists, subscribe optional log writer and wait
5. if entry missing, create entry and start one goroutine
6. runner.Run writes raw logs into entry fanout
7. uploader.Upload receives archive
8. artifact.Put persists completed metadata
9. entry completes waiting callers
10. entry is removed
```

When `artifact.Put` returns `artifact.ErrConflict`, return an error that the HTTP layer maps to status 409.

- [ ] **Step 5: Run tests**

Run: `go test ./cmd/worker/internal/build`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/worker/internal/build
git commit -m "feat: coordinate worker local builds"
```

## Task 7: Worker LLAR Runner

**Files:**
- Create: `cmd/worker/internal/build/runner.go`
- Create: `cmd/worker/internal/build/runner_test.go`

- [ ] **Step 1: Write runner test using a cached local formula fixture**

Create `cmd/worker/internal/build/runner_test.go` with a test that configures a temporary workspace and a test formula store, runs a tiny module through the existing `internal/build.Builder`, and asserts:

```go
if got.Metadata == "" {
	t.Fatal("metadata is empty")
}
if got.Type != "zip" {
	t.Fatalf("Type = %q, want zip", got.Type)
}
if _, err := got.Archive.Seek(0, 0); err != nil {
	t.Fatalf("archive is not seekable: %v", err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/worker/internal/build -run Runner`

Expected: FAIL because `runner.go` is missing.

- [ ] **Step 3: Implement runner with existing LLAR build path**

Create `cmd/worker/internal/build/runner.go`. It must:

```text
1. create a remote formula store using the same pattern as llar make;
2. call modules.Load(ctx, module.Version{Path: req.Target.Module, Version: req.Target.Version}, modules.Options{FormulaStore: store, MatrixStr: req.MatrixStr});
3. create internal/build.NewBuilder with MatrixStr and a temporary workspace;
4. run Builder.Build(ctx, mods);
5. take the last result as the requested artifact;
6. zip result.OutputDir into a temporary zip file;
7. return RunResult{Archive: file, Type: "zip", Metadata: result.Metadata}.
```

The runner must not implement formula semantics itself.

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/worker/internal/build -run Runner`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/worker/internal/build/runner.go cmd/worker/internal/build/runner_test.go
git commit -m "feat: run llar builds inside worker"
```

## Task 8: Worker HTTP Server

**Files:**
- Create: `cmd/worker/main.go`
- Create: `cmd/worker/main_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add Gin dependency**

Run: `go get github.com/gin-gonic/gin`

Expected: `go.mod` and `go.sum` include Gin.

- [ ] **Step 2: Write HTTP handler tests**

Create `cmd/worker/main_test.go` with tests named:

```go
func TestPostJobsMissingTargetReturns400(t *testing.T)
func TestPostJobsInvalidJSONReturns400(t *testing.T)
func TestPostJobsMissingMatrixReturns400(t *testing.T)
func TestPostJobsNonVerboseCompleted(t *testing.T)
func TestPostJobsVerboseStreamsLogThenStatus(t *testing.T)
```

The verbose test must decode response values with `json.Decoder` and expect:

```json
{"type":"log","data":{"stream":"stderr","text":"building\n"}}
{"type":"status","state":"completed","body":{"artifact":{...}}}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./cmd/worker`

Expected: FAIL because worker main is missing.

- [ ] **Step 4: Implement Gin route**

Create `cmd/worker/main.go` with:

```go
func routes(builds interface {
	Build(context.Context, workerbuild.Request, io.Writer) (workerbuild.Result, error)
}) *gin.Engine
```

The handler must:

```text
1. require X-LLAR-Target;
2. parse it with cloudbuild.ParseTargetHeader;
3. decode cloudbuild.JobRequest;
4. require matrix.require to be non-empty;
5. call Builds.Build;
6. in non-verbose mode, encode one StatusMessage;
7. in verbose mode, pass a writer that wraps raw bytes as cloudbuild.LogMessage values and flushes after each message;
8. map artifact checksum conflict to failed status with body.status = 409;
9. map other build-time errors to failed status with body.status = 500.
```

- [ ] **Step 5: Add process startup**

In `main()`, open the artifact DB, create `artifact.NewSQLStore(db)`, create `upload.NewGHCR`, create `build.New`, and run Gin. Do not add env/flag/config plumbing in this task; keep startup wiring minimal and local to `cmd/worker`.

- [ ] **Step 6: Run tests**

Run: `go test ./cmd/worker`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum cmd/worker
git commit -m "feat: add cloud build worker http endpoint"
```

## Task 9: Cloud Build HTTP Client

**Files:**
- Create: `internal/cloudbuild/client.go`
- Create: `internal/cloudbuild/client_test.go`

- [ ] **Step 1: Write client tests**

Create `internal/cloudbuild/client_test.go` with tests named:

```go
func TestClientSubmitSendsTargetHeaderAndMatrix(t *testing.T)
func TestClientSubmitDecodesCompletedStatus(t *testing.T)
func TestClientSubmitVerboseDecodesLogsAndStatus(t *testing.T)
func TestDownloadArtifactUsesPublicGHCRAuthorization(t *testing.T)
```

The GHCR authorization test must assert:

```go
if got := r.Header.Get("Authorization"); got != "Bearer QQ==" {
	t.Fatalf("Authorization = %q", got)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cloudbuild`

Expected: FAIL because client methods are missing.

- [ ] **Step 3: Implement client**

Add to `internal/cloudbuild/client.go`:

```go
const DefaultWorkerURL = "http://127.0.0.1:8080"

type Client struct {
	BaseURL string
	HTTP    *http.Client
}

type SubmitOptions struct {
	Target  Target
	Matrix  Matrix
	Verbose bool
	Log     func(LogData)
}

func NewClient() *Client {
	return &Client{BaseURL: DefaultWorkerURL, HTTP: http.DefaultClient}
}

func (c *Client) Submit(ctx context.Context, opts SubmitOptions) (Artifact, error)
func DownloadArtifact(ctx context.Context, httpClient *http.Client, artifact Artifact, dest io.Writer) error
```

`Submit` must use `POST /v1/jobs` and append `?verbose=1` only when `opts.Verbose` is true. `DownloadArtifact` must set `Authorization: Bearer QQ==` when `artifact.Source.Type == "ghcr"`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cloudbuild`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cloudbuild/client.go internal/cloudbuild/client_test.go
git commit -m "feat: add cloud build http client"
```

## Task 10: llar install Remote Cache Miss

**Files:**
- Modify: `cmd/llar/internal/install.go`
- Create: `cmd/llar/internal/install_test.go`

- [ ] **Step 1: Write install tests**

Create `cmd/llar/internal/install_test.go` with tests named:

```go
func TestRunInstallSubmitsCacheMissToWorker(t *testing.T)
func TestRunInstallDownloadsAndWritesCache(t *testing.T)
func TestRunInstallRejectsChecksumMismatch(t *testing.T)
func TestRunInstallPrintsRootMetadata(t *testing.T)
```

Use an `httptest.Server` assigned to `cloudbuild.Client.BaseURL` through a test hook, and return a completed status message with a zip artifact URL.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/llar/internal -run Install`

Expected: FAIL because `runInstall` still panics.

- [ ] **Step 3: Implement install command**

Modify `cmd/llar/internal/install.go`:

```text
1. parse matrix flags with parseMatrixSelectionArgs;
2. parse the module arg with parseModuleArg;
3. create the same remote formula store as llar make;
4. call modules.Load with MatrixStr;
5. create internal/build.Builder with MatrixStr;
6. use build order from internal/build helper;
7. for each module in order, call LookupInstallCache;
8. on cache hit, keep metadata and continue;
9. on miss, call cloudbuild.Client.Submit with X-LLAR-Target and body.matrix;
10. download Artifact.Source;
11. verify sha256 checksum;
12. extract archive into Builder.InstallDir(module, version);
13. call Builder.SaveInstallCache(module, version, artifact.Metadata);
14. print the root module metadata at the end.
```

- [ ] **Step 4: Add build order helper if needed**

If `cmd/llar/internal/install.go` cannot reuse build order without duplicating it, add an exported helper in `internal/build`:

```go
func BuildOrder(targets []*modules.Module) []*modules.Module
```

Make `Builder.constructBuildList` call the same helper so `llar make` and `llar install` stay aligned.

- [ ] **Step 5: Run tests**

Run: `go test ./cmd/llar/internal -run Install`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/llar/internal/install.go cmd/llar/internal/install_test.go internal/build
git commit -m "feat: install artifacts through cloud build worker"
```

## Task 11: End-To-End Worker And Install Verification

**Files:**
- Modify tests only if failures expose integration bugs in files from earlier tasks.

- [ ] **Step 1: Run focused unit tests**

Run:

```bash
go test ./internal/cloudbuild ./internal/build ./cmd/worker/internal/artifact ./cmd/worker/internal/upload ./cmd/worker/internal/build ./cmd/worker ./cmd/llar/internal
```

Expected: PASS.

- [ ] **Step 2: Run full test suite**

Run: `go test ./...`

Expected: PASS, except existing network-dependent e2e tests may require the same environment they required before this plan.

- [ ] **Step 3: Manual local smoke test**

In terminal A:

```bash
go run ./cmd/worker
```

In terminal B:

```bash
go run ./cmd/llar install madler/zlib@v1.3.1 --arch amd64 --os linux
```

Expected: `llar install` sends `POST /v1/jobs` to `http://127.0.0.1:8080`, receives completed status, downloads the artifact source, verifies checksum, extracts it, writes `.cache.json`, and prints root metadata.

- [ ] **Step 4: Commit final fixes**

```bash
git add .
git commit -m "test: verify cloud build worker integration"
```

## Self-Review

Spec coverage:

- `POST /v1/jobs` and `?verbose=1`: Task 8 and Task 9.
- `X-LLAR-Target` identity and matrix body: Task 1, Task 2, Task 8, Task 10.
- Completed artifact lookup before active build entry: Task 6.
- Worker-local active build sharing: Task 6.
- Raw log writer from build and JSON wrapping at HTTP boundary: Task 6 and Task 8.
- Artifact DB table and idempotent `Put`: Task 4.
- Upload interface and GHCR source shape: Task 5.
- `llar install` owns resolution, cache checks, download, checksum, extraction, and `.cache.json`: Task 10.
- No Scheduler, Redis, Asynq, WebSocket, VM, or pending jobs table: no task adds them.

Placeholder scan:

- The plan contains no `TBD`, no deferred API fields, and no unapproved worker URL configuration.
- The only hardcoded endpoint is `internal/cloudbuild.DefaultWorkerURL = "http://127.0.0.1:8080"`.

Type consistency:

- Public wire artifact type lives in `internal/cloudbuild`.
- Worker `artifact.Artifact` aliases `cloudbuild.Artifact`, so HTTP responses and metadata store use one JSON shape.
- Worker build imports `cmd/worker/internal/artifact` and `cmd/worker/internal/upload`; `llar install` imports only `internal/cloudbuild` and root `internal/build` helpers.
