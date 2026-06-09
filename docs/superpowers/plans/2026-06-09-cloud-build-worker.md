# Cloud Build Worker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement `llar install` remote cache-miss handling and a separate cloud-build worker project that serves `POST /v1/jobs`.

**Architecture:** The worker is a separate Go project under `cloud-build-worker/`. It owns its HTTP endpoint and its internal `build`, `artifact`, and `upload` modules. The llar project does not get a shared `cloudbuild` package; `llar install` uses small unexported HTTP request/response structs local to the install implementation.

**Tech Stack:** Go 1.24, Cobra in the llar CLI, Gin in the worker project, `database/sql` in the worker artifact module, standard `net/http` client/server tests, and the existing `llar make` command as the worker build executor.

---

## File Structure

Worker project:

- Create `cloud-build-worker/go.mod`: independent worker module.
- Create `cloud-build-worker/cmd/worker/main.go`: Gin startup and `POST /v1/jobs` HTTP glue.
- Create `cloud-build-worker/cmd/worker/main_test.go`: HTTP validation and response-stream tests.
- Create `cloud-build-worker/internal/build/build.go`: worker-local active build coordination and `Builds.Build(ctx, req, log)`.
- Create `cloud-build-worker/internal/build/build_test.go`: active-entry sharing, log fanout, artifact lookup ordering, and Put-before-complete tests.
- Create `cloud-build-worker/internal/build/runner.go`: executes `llar make -v -o <archive>` and captures stdout metadata plus stderr logs.
- Create `cloud-build-worker/internal/build/runner_test.go`: runner tests with a fake `llar` executable.
- Create `cloud-build-worker/internal/artifact/artifact.go`: artifact key, artifact structs, and Store interface.
- Create `cloud-build-worker/internal/artifact/sql.go`: `database/sql` implementation of the `artifacts` table.
- Create `cloud-build-worker/internal/artifact/sql_test.go`: Get, Put, idempotency, conflict, Delete tests.
- Create `cloud-build-worker/internal/upload/upload.go`: upload interface.
- Create `cloud-build-worker/internal/upload/ghcr.go`: GHCR uploader.
- Create `cloud-build-worker/internal/upload/ghcr_test.go`: checksum/size and GHCR request-shape tests.

llar project:

- Modify `cmd/llar/internal/matrix_flags.go`: return selected matrix maps for `llar install` and keep matrixStr aligned with `formula.Matrix.Combinations()[0]`.
- Modify `cmd/llar/internal/matrix_flags_test.go`: matrix selection tests.
- Create `internal/build/install.go`: exported helpers for local cache lookup, install directory, and cache metadata writes.
- Create `internal/build/install_test.go`: helper tests.
- Modify `cmd/llar/internal/install.go`: implement local cache miss -> worker `POST /v1/jobs`; worker URL is hardcoded for now.
- Create `cmd/llar/internal/install_test.go`: install HTTP request, artifact download, checksum, extraction, and cache metadata tests.

## Task 1: Worker Project Skeleton And HTTP Wire Types

**Files:**
- Create: `cloud-build-worker/go.mod`
- Create: `cloud-build-worker/cmd/worker/main.go`
- Create: `cloud-build-worker/cmd/worker/main_test.go`
- Create: `cloud-build-worker/internal/artifact/artifact.go`

- [ ] **Step 1: Write worker HTTP parsing tests**

Create `cloud-build-worker/cmd/worker/main_test.go` with:

```go
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostJobsMissingTargetReturns400(t *testing.T) {
	r := routes(fakeBuilds{})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader(`{"matrix":{"require":{"arch":"amd64","os":"linux"}}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPostJobsInvalidTargetReturns400(t *testing.T) {
	r := routes(fakeBuilds{})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader(`{"matrix":{"require":{"arch":"amd64","os":"linux"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(targetHeader, "pnggroup/libpng")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
```

Use a small `fakeBuilds` test double in the same test file; later tasks can extend it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cloud-build-worker && go test ./cmd/worker`

Expected: FAIL because the worker project does not exist.

- [ ] **Step 3: Create worker module**

Create `cloud-build-worker/go.mod`:

```go
module github.com/goplus/llar/cloud-build-worker

go 1.24.0

require github.com/gin-gonic/gin v1.10.1
```

- [ ] **Step 4: Add artifact structs**

Create `cloud-build-worker/internal/artifact/artifact.go`:

```go
package artifact

import (
	"context"
	"errors"
)

var ErrConflict = errors.New("artifact checksum conflict")

type Key struct {
	Module    string
	Version   string
	MatrixStr string
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

type Store interface {
	Get(ctx context.Context, key Key) (Artifact, bool, error)
	Put(ctx context.Context, key Key, artifact Artifact) (Artifact, error)
	Delete(ctx context.Context, key Key) error
}
```

- [ ] **Step 5: Add minimal HTTP glue and wire structs**

Create `cloud-build-worker/cmd/worker/main.go` with local wire types:

```go
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/goplus/llar/cloud-build-worker/internal/artifact"
)

const targetHeader = "X-LLAR-Target"

type matrix struct {
	Require map[string]string `json:"require"`
	Options map[string]string `json:"options,omitempty"`
}

type jobRequest struct {
	Matrix matrix `json:"matrix"`
}

type target struct {
	Module    string
	Version   string
	MatrixStr string
}

type statusMessage struct {
	Type  string     `json:"type"`
	State string     `json:"state"`
	Body  statusBody `json:"body"`
}

type statusBody struct {
	Artifact *artifact.Artifact `json:"artifact,omitempty"`
	Status   int                `json:"status,omitempty"`
	Message  string             `json:"message,omitempty"`
}

type buildRequest struct {
	Target    target
	MatrixStr string
	Matrix    matrix
}

type buildResult struct {
	Artifact artifact.Artifact
}

type builds interface {
	Build(context.Context, buildRequest, io.Writer) (buildResult, error)
}

func parseTargetHeader(value string) (target, error) {
	beforeMatrix, matrixStr, ok := strings.Cut(value, "#")
	if !ok || matrixStr == "" {
		return target{}, fmt.Errorf("invalid %s", targetHeader)
	}
	module, version, ok := strings.Cut(beforeMatrix, "@")
	if !ok || module == "" || version == "" {
		return target{}, fmt.Errorf("invalid %s", targetHeader)
	}
	return target{Module: module, Version: version, MatrixStr: matrixStr}, nil
}

func routes(builds builds) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/jobs", func(c *gin.Context) {
		t, err := parseTargetHeader(c.GetHeader(targetHeader))
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		var body jobRequest
		if err := c.ShouldBindJSON(&body); err != nil || len(body.Matrix.Require) == 0 {
			c.Status(http.StatusBadRequest)
			return
		}
		result, err := builds.Build(c.Request.Context(), buildRequest{Target: t, MatrixStr: t.MatrixStr, Matrix: body.Matrix}, nil)
		if err != nil {
			c.JSON(http.StatusOK, statusMessage{Type: "status", State: "failed", Body: statusBody{Status: 500, Message: err.Error()}})
			return
		}
		c.JSON(http.StatusOK, statusMessage{Type: "status", State: "completed", Body: statusBody{Artifact: &result.Artifact}})
	})
	return r
}
```

- [ ] **Step 6: Run tests**

Run: `cd cloud-build-worker && go test ./cmd/worker`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add cloud-build-worker
git commit -m "feat: add cloud build worker project skeleton"
```

## Task 2: Worker Artifact Store

**Files:**
- Create: `cloud-build-worker/internal/artifact/sql.go`
- Create: `cloud-build-worker/internal/artifact/sql_test.go`
- Modify: `cloud-build-worker/go.mod`
- Modify: `cloud-build-worker/go.sum`

- [ ] **Step 1: Add SQL test driver**

Run: `cd cloud-build-worker && go get modernc.org/sqlite`

Expected: worker `go.mod` and `go.sum` include the SQL test driver.

- [ ] **Step 2: Write SQL store tests**

Create `cloud-build-worker/internal/artifact/sql_test.go` with tests:

```go
func TestSQLStoreGetMissAndPut(t *testing.T)
func TestSQLStorePutSameChecksumIsIdempotent(t *testing.T)
func TestSQLStorePutDifferentChecksumConflicts(t *testing.T)
func TestSQLStoreDelete(t *testing.T)
```

Use `sql.Open("sqlite", ":memory:")`, call `NewSQLStore(db)`, and assert the primary key is `module`, `version`, `matrix_str`. Conflict must return `ErrConflict`.

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd cloud-build-worker && go test ./internal/artifact`

Expected: FAIL because `NewSQLStore` is missing.

- [ ] **Step 4: Implement SQL store**

Create `cloud-build-worker/internal/artifact/sql.go` with `NewSQLStore(db *sql.DB) (*SQLStore, error)`. It creates:

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

`Put` behavior:

```text
missing key -> insert and return inserted artifact
same checksum -> return existing artifact
different checksum -> return ErrConflict
```

- [ ] **Step 5: Run tests**

Run: `cd cloud-build-worker && go test ./internal/artifact`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cloud-build-worker/internal/artifact cloud-build-worker/go.mod cloud-build-worker/go.sum
git commit -m "feat: add worker artifact metadata store"
```

## Task 3: Worker Upload Module

**Files:**
- Create: `cloud-build-worker/internal/upload/upload.go`
- Create: `cloud-build-worker/internal/upload/ghcr.go`
- Create: `cloud-build-worker/internal/upload/ghcr_test.go`

- [ ] **Step 1: Write upload tests**

Create `cloud-build-worker/internal/upload/ghcr_test.go` with tests:

```go
func TestChecksumResultReadsFromCurrentOffsetAndRestoresReader(t *testing.T)
func TestGHCRUploaderReturnsSourceURLChecksumAndSize(t *testing.T)
```

The checksum test uses a `bytes.Reader`, seeks past a prefix, calls the unexported checksum helper, and verifies size/checksum and reader offset restoration.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cloud-build-worker && go test ./internal/upload`

Expected: FAIL because upload package is missing.

- [ ] **Step 3: Add upload interface**

Create `cloud-build-worker/internal/upload/upload.go`:

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

- [ ] **Step 4: Add GHCR uploader**

Create `cloud-build-worker/internal/upload/ghcr.go` with:

```go
type GHCRConfig struct {
	Owner string
	Token string
}

func NewGHCR(cfg GHCRConfig) Uploader
```

The uploader computes SHA-256 and size from the current reader offset, restores the offset, uploads the same bytes, and returns:

```text
Type() = "ghcr"
Result.URL = https://ghcr.io/v2/<owner>/<module>/blobs/sha256:<digest>
```

- [ ] **Step 5: Run tests**

Run: `cd cloud-build-worker && go test ./internal/upload`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cloud-build-worker/internal/upload
git commit -m "feat: add worker artifact uploader"
```

## Task 4: Worker Build Coordination

**Files:**
- Create: `cloud-build-worker/internal/build/build.go`
- Create: `cloud-build-worker/internal/build/build_test.go`

- [ ] **Step 1: Write build coordination tests**

Create `cloud-build-worker/internal/build/build_test.go` with:

```go
func TestBuildReturnsCompletedArtifactBeforeJoiningLocalEntry(t *testing.T)
func TestBuildSharesActiveEntryForSameArtifactKey(t *testing.T)
func TestBuildWritesRawLogsToProvidedWriter(t *testing.T)
func TestBuildDoesNotCompleteWhenArtifactPutFails(t *testing.T)
```

Use fake `artifact.Store`, fake `upload.Uploader`, and fake `Runner`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cloud-build-worker && go test ./internal/build`

Expected: FAIL because build package is missing.

- [ ] **Step 3: Add build interface**

Create `cloud-build-worker/internal/build/build.go`:

```go
package build

import (
	"context"
	"io"

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
	// internal state
}

func New(opts Options) *Builds
func (b *Builds) Build(ctx context.Context, req Request, log io.Writer) (Result, error)
```

- [ ] **Step 4: Implement coordination**

Implement this order:

```text
1. artifact.Get
2. if hit, return completed
3. lock local entries
4. join existing entry or create a new one
5. runner.Run writes raw logs to the entry fanout
6. upload archive
7. artifact.Put
8. complete waiting callers
9. remove entry
```

After `artifact.Put` succeeds, new requests must hit `artifact.Get` and return completed even if the old local entry has not yet been removed.

- [ ] **Step 5: Run tests**

Run: `cd cloud-build-worker && go test ./internal/build`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cloud-build-worker/internal/build
git commit -m "feat: coordinate worker local builds"
```

## Task 5: Worker LLAR Runner

**Files:**
- Create: `cloud-build-worker/internal/build/runner.go`
- Create: `cloud-build-worker/internal/build/runner_test.go`

- [x] **Step 1: Write runner tests**

Create `cloud-build-worker/internal/build/runner_test.go` with the real repository `llar` binary on `PATH`. The runner test uses `madler/zlib@v1.3.1` and verifies the worker invokes:

```text
llar make -v -o <archive> <module>@<version>
```

The test asserts `RunResult.Type == "zip"`, metadata is the final LLAR metadata line, verbose output reaches the provided log writer, and `Archive` is seekable.

- [x] **Step 2: Run tests to verify they fail**

Run: `cd cloud-build-worker && go test ./internal/build -run Runner`

Expected: FAIL because runner is missing.

- [x] **Step 3: Implement subprocess runner**

Create `cloud-build-worker/internal/build/runner.go`. It executes:

```text
llar make -v -o <tmp>/artifact.zip <module>@<version>
```

It sets matrix flags from `Request.Matrix` as CLI flags:

```text
require arch/os -> --arch / --os
options -> --matrix-<key> <value>
```

It sends stderr to the optional raw log writer, treats the final stdout line as LLAR metadata, forwards earlier verbose stdout as raw log text, opens the generated archive as `io.ReadSeeker`, and returns `RunResult{Archive, Type: "zip", Metadata}`.

- [x] **Step 4: Run tests**

Run: `cd cloud-build-worker && go test ./internal/build -run Runner`

Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add cloud-build-worker/internal/build/runner.go cloud-build-worker/internal/build/runner_test.go
git commit -m "feat: execute llar make from worker"
```

## Task 6: Worker HTTP Endpoint

**Files:**
- Modify: `cloud-build-worker/cmd/worker/main.go`
- Modify: `cloud-build-worker/cmd/worker/main_test.go`

- [ ] **Step 1: Expand HTTP tests**

Add tests:

```go
func TestPostJobsInvalidJSONReturns400(t *testing.T)
func TestPostJobsMissingMatrixReturns400(t *testing.T)
func TestPostJobsNonVerboseCompleted(t *testing.T)
func TestPostJobsVerboseStreamsLogThenStatus(t *testing.T)
func TestPostJobsConflictReturnsFailedStatus409(t *testing.T)
```

The verbose test decodes multiple JSON values with `json.Decoder`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd cloud-build-worker && go test ./cmd/worker`

Expected: FAIL for the new behavior.

- [ ] **Step 3: Wire real build package**

Update `main.go` so HTTP glue calls `internal/build.Builds`. Keep JSON encoding at HTTP boundary:

```text
verbose=0 -> one status JSON value
verbose=1 -> zero or more log JSON values, then one status JSON value
```

Build errors return status messages, not HTTP 500, except request parsing errors.

- [ ] **Step 4: Run tests**

Run: `cd cloud-build-worker && go test ./cmd/worker`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cloud-build-worker/cmd/worker
git commit -m "feat: serve worker job endpoint"
```

## Task 7: llar Matrix Selection For Install

**Files:**
- Modify: `cmd/llar/internal/matrix_flags.go`
- Modify: `cmd/llar/internal/matrix_flags_test.go`

- [ ] **Step 1: Write selected matrix tests**

Add tests for `parseMatrixSelectionArgs` that assert:

```text
--arch amd64 --os linux --matrix-debug=false
matrixStr = amd64-linux|false
body matrix require = {"arch":"amd64","os":"linux"}
body matrix options = {"debug":"false"}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/llar/internal -run Matrix`

Expected: FAIL because selected matrix output is missing and existing option matrix string uses `key=value`.

- [ ] **Step 3: Implement selected matrix output**

Add an unexported `selectedMatrix` type in `cmd/llar/internal/matrix_flags.go`. `parseMatrixArgs` remains available for `llar make`; `parseMatrixSelectionArgs` returns args, selected matrix, and matrixStr. Generate matrixStr through `formula.Matrix.Combinations()[0]`, with `arch` and `os` in Require and all other matrix flags in Options.

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/llar/internal -run Matrix`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/llar/internal/matrix_flags.go cmd/llar/internal/matrix_flags_test.go
git commit -m "fix: align install matrix selection with formula combinations"
```

## Task 8: llar Local Install Cache Helpers

**Files:**
- Create: `internal/build/install.go`
- Create: `internal/build/install_test.go`

- [ ] **Step 1: Write helper tests**

Create tests for:

```go
func TestInstallHelpersLookupAndSaveCacheEntry(t *testing.T)
func TestInstallDirUsesExistingBuilderLayout(t *testing.T)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/build -run Install`

Expected: FAIL because helpers are missing.

- [ ] **Step 3: Add helpers**

Create `internal/build/install.go` exposing:

```go
func (b *Builder) InstallDir(modPath, version string) (string, error)
func (b *Builder) LookupInstallCache(modPath, version string) (installDir string, metadata string, ok bool, err error)
func (b *Builder) SaveInstallCache(modPath, version, metadata string) error
func BuildOrder(targets []*modules.Module) []*modules.Module
```

`BuildOrder` must use the same ordering as `Builder.constructBuildList`; update `constructBuildList` to delegate to it.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/build -run Install`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/build/install.go internal/build/install_test.go internal/build/build.go
git commit -m "feat: expose install cache helpers"
```

## Task 9: llar install Worker Client

**Files:**
- Modify: `cmd/llar/internal/install.go`
- Create: `cmd/llar/internal/install_test.go`

- [ ] **Step 1: Write install tests**

Create tests:

```go
func TestRunInstallSubmitsCacheMissToWorker(t *testing.T)
func TestRunInstallDownloadsAndWritesCache(t *testing.T)
func TestRunInstallRejectsChecksumMismatch(t *testing.T)
func TestRunInstallPrintsRootMetadata(t *testing.T)
```

Tests use `httptest.Server` through a package-level test hook for the hardcoded worker base URL.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/llar/internal -run Install`

Expected: FAIL because `runInstall` currently panics.

- [ ] **Step 3: Implement install**

In `cmd/llar/internal/install.go`, define unexported request/response structs matching the worker API. Do not create a shared protocol package.

Flow:

```text
1. parse matrix selection
2. parse module arg
3. resolve modules with existing modules.Load
4. build dependency order with internal/build.BuildOrder
5. local cache hit -> use local result
6. local cache miss -> POST hardcoded worker /v1/jobs
7. decode status response
8. download Artifact.Source directly
9. for ghcr source, set Authorization: Bearer QQ==
10. verify sha256
11. extract archive into install dir
12. write .cache.json metadata
13. print root metadata
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/llar/internal -run Install`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/llar/internal/install.go cmd/llar/internal/install_test.go
git commit -m "feat: install through cloud build worker"
```

## Task 10: Verification

**Files:**
- Modify only files from earlier tasks if verification finds implementation bugs.

- [ ] **Step 1: Run worker tests**

Run:

```bash
cd cloud-build-worker && go test ./...
```

Expected: PASS.

- [ ] **Step 2: Run llar focused tests**

Run:

```bash
go test ./cmd/llar/internal ./internal/build
```

Expected: PASS.

- [ ] **Step 3: Run full llar tests**

Run:

```bash
go test ./...
```

Expected: PASS, except pre-existing network-dependent tests may require the same environment as before.

- [ ] **Step 4: Commit final fixes**

```bash
git add cloud-build-worker cmd/llar/internal internal/build
git commit -m "test: verify cloud build worker integration"
```

## Self-Review

Spec coverage:

- Separate worker owns `cmd/worker`, `internal/build`, `internal/artifact`, and `internal/upload`.
- No `internal/cloudbuild` shared package is created.
- Worker `POST /v1/jobs` parses `X-LLAR-Target`, matrix body, and `verbose`.
- Worker `build` owns active entry coordination and raw log fanout.
- Worker `artifact` owns artifact metadata and `artifacts` table.
- Worker `upload` owns artifact byte upload.
- `llar install` owns module resolution, dependency order, local cache, artifact download, checksum, extraction, `.cache.json`, and final metadata.
- Worker URL is hardcoded in `llar install` for now.

Out of scope:

- Scheduler, Redis, Asynq, VM, agent protocol, WebSocket APIs, persistent pending jobs, and distributed locks.
