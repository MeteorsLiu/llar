# llard Cloud Build Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild cloud install around `llard`, `GET /v1/artifacts/<module>@<version>?<matrix query>`, and command JSON line responses.

**Architecture:** `llard/` is a new standalone Go project. The main LLAR CLI keeps install semantics and uses `internal/remote` only as a protocol client. `llard` owns HTTP handling, local in-progress build entries, artifact metadata, build execution, and upload boundaries.

**Tech Stack:** Go 1.24, `net/http`, LLAR CLI/build packages, command JSON line protocol, tar/zip archive readers.

---

## Source Of Truth

- Normative design: `docs/superpowers/specs/2026-05-25-cloud-build-design.md`
- Architecture narrative: `docs/superpowers/specs/2026-06-08-cloud-build-worker-architecture.md`
- Archive layout: `docs/superpowers/specs/2026-06-15-llar-binary-spec.md`
- GHCR layout: `docs/superpowers/specs/2026-06-15-ghcr-artifact-store.md`

If this plan conflicts with the normative design, update the plan before coding.

## Target Layout

```text
llard/
  go.mod
  cmd/llard/main.go
  cmd/llard/main_test.go
  internal/artifact/artifact.go
  internal/artifact/memory.go
  internal/artifact/memory_test.go
  internal/upload/upload.go
  internal/upload/file.go
  internal/upload/file_test.go
  internal/build/build.go
  internal/build/build_test.go

internal/remote/
  client.go
  client_test.go

cmd/llar/internal/install.go
cmd/llar/internal/install_test.go
cmd/llar/internal/matrix_flags.go
cmd/llar/internal/matrix_flags_test.go
```

`llard/` is independent so its `internal/build` does not conflict with LLAR's existing `internal/build`.

## Task 0: Keep Old Worker Deleted

**Files:**
- Delete: `cloud-build-worker/`

- [ ] **Step 1: Verify old worker source is gone**

Run:

```bash
rg --files cloud-build-worker
```

Expected: command exits non-zero because the directory does not exist.

- [ ] **Step 2: Verify no code imports the old worker**

Run:

```bash
rg -n "cloud-build-worker|cmd/worker|/v1/jobs|X-LLAR-Target" --glob '*.go'
```

Expected: no production code depends on old worker names or protocol.

## Task 1: Rebuild LLAR Remote Client For cmdjsonl

**Files:**
- Create: `internal/remote/client.go`
- Create: `internal/remote/client_test.go`

- [ ] **Step 1: Write failing tests for request shape and terminal artifact**

Test behavior:

```go
func TestClientSubmitArtifactSet(t *testing.T)
```

The test server must assert:

```text
method = GET
path   = /v1/artifacts/pnggroup/libpng@v1.6.47
query  = arch=amd64&debug=false&os=linux
```

The response is:

```text
info {"stream":"stderr","text":"checking\n"}
artifact {"artifacts":[{"target":"madler/zlib@v1.3.1","artifact":{"source":{"type":"http","url":"https://example.invalid/zlib.tar.gz"},"type":"tar.gz","metadata":"-lz","checksum":"abc"}},{"target":"pnggroup/libpng@v1.6.47","artifact":{"source":{"type":"http","url":"https://example.invalid/libpng.tar.gz"},"type":"tar.gz","metadata":"-lpng","checksum":"def"}}]}
```

Expected result:

```text
Submit returns two TargetArtifact values in response order.
info text is copied to the provided writer.
```

- [ ] **Step 2: Run test and verify RED**

Run:

```bash
go test ./internal/remote
```

Expected: fails because `internal/remote` does not exist.

- [ ] **Step 3: Implement minimal client**

Implement:

```go
package remote

type Matrix map[string]string

type Source struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type Artifact struct {
	Source   Source `json:"source"`
	Type     string `json:"type"`
	Metadata string `json:"metadata"`
	Checksum string `json:"checksum"`
}

type TargetArtifact struct {
	Target   string   `json:"target"`
	Artifact Artifact `json:"artifact"`
}

type Submitter interface {
	Submit(ctx context.Context, target module.Version, matrix Matrix, log io.Writer) ([]TargetArtifact, error)
}
```

`Client.Submit` maps to `GET /v1/artifacts/<module>@<version>?<matrix query>`. It reads one command JSON line at a time, splits at the first space, writes `info.text` to `log`, returns `artifact.artifacts`, and returns `error.message` as an error.

- [ ] **Step 4: Verify GREEN**

Run:

```bash
go test ./internal/remote
```

Expected: pass.

## Task 2: Change `llar install` To One Remote Request

**Files:**
- Modify: `cmd/llar/internal/install.go`
- Create/modify: `cmd/llar/internal/install_test.go`

- [ ] **Step 1: Write failing CLI test**

Test behavior:

```go
func TestInstallSubmitsOneRootRequest(t *testing.T)
```

The fake remote client records one call:

```text
target = pnggroup/libpng@v1.6.47
matrix = {"arch":"amd64","os":"linux","debug":"false"}
```

The fake client returns dependency artifact first, root artifact last. The test asserts `llar install` does not call `modules.Load` and installs returned artifacts in order.

- [ ] **Step 2: Run test and verify RED**

Run:

```bash
go test ./cmd/llar/internal
```

Expected: fails because current `install` still loads modules and uses old builder remote mode.

- [ ] **Step 3: Implement direct install flow**

Change `runInstall`:

```text
parse module@version
parse matrix flags into query values
call remote.Submit once
for each returned TargetArtifact:
  download Artifact.Source
  verify checksum
  extract archive into installDir(target, matrixStr)
  write .cache.json with Artifact.Metadata
print root metadata
```

Keep install directory and `.cache.json` layout identical to `internal/build`.

- [ ] **Step 4: Verify GREEN**

Run:

```bash
go test ./cmd/llar/internal
```

Expected: pass.

## Task 3: Create llard Artifact And Upload Boundaries

**Files:**
- Create: `llard/go.mod`
- Create: `llard/internal/artifact/artifact.go`
- Create: `llard/internal/artifact/memory.go`
- Create: `llard/internal/artifact/memory_test.go`
- Create: `llard/internal/upload/upload.go`
- Create: `llard/internal/upload/file.go`
- Create: `llard/internal/upload/file_test.go`

- [ ] **Step 1: Write failing artifact store tests**

Tests:

```go
func TestMemoryStorePutReturnsExistingArtifact(t *testing.T)
func TestMemoryStoreGetMiss(t *testing.T)
```

Expected behavior: `Put` returns the first stored artifact for the same key, even when the second candidate differs.

- [ ] **Step 2: Implement artifact store types**

Implement `Key`, `Source`, `Artifact`, `Store`, and an in-memory store for tests and local development.

- [ ] **Step 3: Write failing upload tests**

Tests:

```go
func TestFileUploaderComputesChecksumAndURL(t *testing.T)
```

Expected behavior: upload copies bytes, computes SHA-256, returns `Source{Type:"http", URL:...}`.

- [ ] **Step 4: Implement upload interface and file uploader**

`file` uploader is the first local backend. GHCR can replace it behind the same interface.

- [ ] **Step 5: Verify GREEN**

Run:

```bash
cd llard
go test ./internal/artifact ./internal/upload
```

Expected: pass.

## Task 4: Create llard Build Coordinator

**Files:**
- Create: `llard/internal/build/build.go`
- Create: `llard/internal/build/build_test.go`

- [ ] **Step 1: Write failing tests for completed lookup and entry sharing**

Tests:

```go
func TestBuildReturnsCompletedArtifactBeforeEntryLookup(t *testing.T)
func TestBuildJoinsExistingEntry(t *testing.T)
func TestBuildUsesArtifactReturnedByPut(t *testing.T)
```

Expected behavior:

```text
completed DB hit returns immediately
same key concurrent requests share one runner call
Put race returns stored artifact and build result uses stored artifact
```

- [ ] **Step 2: Implement `Builds.Build` with in-memory entries**

Implement:

```go
func (b *Builds) Build(ctx context.Context, req Request, info io.Writer) (Result, error)
```

No HTTP or JSON encoding in this package.

- [ ] **Step 3: Verify GREEN**

Run:

```bash
cd llard
go test ./internal/build
```

Expected: pass.

## Task 5: Add llard HTTP Endpoint

**Files:**
- Create: `llard/cmd/llard/main.go`
- Create: `llard/cmd/llard/main_test.go`

- [ ] **Step 1: Write failing HTTP tests**

Tests:

```go
func TestGetArtifactsParsesTargetAndMatrixQuery(t *testing.T)
func TestGetArtifactsWritesCmdJSONLArtifact(t *testing.T)
func TestGetArtifactsWritesCmdJSONLError(t *testing.T)
```

Expected response header:

```http
Content-Type: application/x-cmdjsonl
```

Expected terminal lines:

```text
artifact {"artifacts":[...]}
error {"message":"llar make failed"}
```

- [ ] **Step 2: Implement HTTP glue**

Use `net/http` for the first implementation. The handler parses the request URI, calls `internal/build`, wraps raw info text into `info` lines, and writes exactly one terminal `artifact` or `error` line.

- [ ] **Step 3: Verify GREEN**

Run:

```bash
cd llard
go test ./cmd/llard
```

Expected: pass.

## Task 6: Wire llard Runner To Real `llar make`

**Files:**
- Modify: `llard/internal/build/build.go`
- Modify: `llard/internal/build/build_test.go`

- [ ] **Step 1: Write failing runner command test**

Test behavior:

```go
func TestRunnerInvokesLlarMakeWithOutputArchive(t *testing.T)
```

Expected command shape:

```text
llar make -v -o <tmp>/artifact.zip <module>@<version> --arch amd64 --os linux
```

- [ ] **Step 2: Implement command runner seam**

Implement a small runner interface so tests can assert command arguments without fake `llar` binaries.

- [ ] **Step 3: Verify GREEN**

Run:

```bash
cd llard
go test ./internal/build
```

Expected: pass.

## Final Verification

Run:

```bash
go test ./cmd/llar/internal ./internal/build ./internal/remote
cd llard && go test ./...
```

Expected: all tests pass.
