# Cloud Build Worker Implementation Plan

## Source Of Truth

Normative spec:

- `docs/superpowers/specs/2026-05-25-cloud-build-design.md`

Supplementary architecture narrative:

- `docs/superpowers/specs/2026-06-08-cloud-build-worker-architecture.md`

If this plan conflicts with the spec, the spec wins.

## Goal

Implement cloud-backed `llar install` cache-miss handling.

The implementation must preserve the existing LLAR local semantics:

```text
local cache hit
  use the existing LLAR local cache

local cache miss
  ask cloud build for an artifact
  install the returned artifact through the existing Builder flow
```

`llar make` remains the build command. Cloud build does not redefine formula semantics, module loading semantics, matrix propagation, local install directories, or cache metadata.

## Architecture Boundaries

### Roles

- `llar install` resolves modules, computes dependency build order, checks the local cache, submits cache misses, downloads completed artifacts, verifies checksums, writes install directories and `.cache.json`, and prints the root metadata.
- `nginx` hash-routes `POST /v1/jobs` by `X-LLAR-Target`.
- `cloud-build-worker` owns HTTP handling, worker-local active build entries, build execution, verbose log fanout, artifact upload, and completed artifact metadata persistence.
- Artifact DB stores completed artifact metadata.
- Artifact Store stores archive bytes. GHCR is the default backend.

### Explicit Non-Goals

- No Scheduler.
- No dispatcher.
- No queue, Redis, Asynq, Redis building key, or pending jobs table.
- No client WebSocket or agent WebSocket.
- No VM provisioning module.
- No shared `cloudbuild` protocol package.
- No `BuildRemote`.
- No exported install helper APIs such as `LookupInstallCache`, `SaveInstallCache`, or `BuildOrder`.
- No fake `llar` runner in worker tests.

## Target Layout

Worker project:

```text
cloud-build-worker/
  go.mod
  cmd/worker/main.go
  cmd/worker/main_test.go
  internal/build/build.go
  internal/build/build_test.go
  internal/build/runner.go
  internal/build/runner_test.go
  internal/artifact/artifact.go
  internal/artifact/sql.go
  internal/artifact/sql_test.go
  internal/upload/upload.go
  internal/upload/ghcr.go
  internal/upload/ghcr_test.go
```

LLAR project changes:

```text
cmd/llar/internal/matrix_flags.go
cmd/llar/internal/matrix_flags_test.go
cmd/llar/internal/install.go
internal/remote/client.go
internal/remote/client_test.go
internal/build/build.go
internal/build/build_test.go
```

Do not add LLAR-side files unless they are required by the existing Builder flow.

## Task 0: Remove The Old Worker Prototype

Files:

- `cloud-build-worker/`

Work:

- Delete the old `cloud-build-worker/` implementation.
- Keep docs, specs, assets, and unrelated LLAR code.

Validation:

```bash
git status --short
```

Acceptance:

- Old worker source files are deleted.
- The next worker implementation starts from an empty `cloud-build-worker/` tree.

## Task 1: Preserve Matrix Identity For Cloud Requests

Spec mapping:

- Request Identity
- llar Client Behavior

Files:

- `cmd/llar/internal/matrix_flags.go`
- `cmd/llar/internal/matrix_flags_test.go`

Work:

- Keep `matrixStr` derived from `formula.Matrix.Combinations()[0]`.
- Expose the selected matrix values needed by `llar install`:
  - `require`
  - `options`
- Do not include `defaultOptions` in the cloud request.
- Do not reconstruct `formula.Matrix` from `matrixStr`.

Validation:

```bash
go test ./cmd/llar/internal
```

Acceptance:

- Existing matrix flag behavior remains unchanged.
- `matrixStr` matches the existing local cache key format.
- Tests cover selected `require` and `options` values.

## Task 2: Add Worker Public API Shell

Spec mapping:

- Public API
- Request Identity
- Failure Boundaries
- `cmd/worker`

Files:

- `cloud-build-worker/go.mod`
- `cloud-build-worker/cmd/worker/main.go`
- `cloud-build-worker/cmd/worker/main_test.go`

Work:

- Create the worker as a separate Go project.
- Add `POST /v1/jobs`.
- Support `POST /v1/jobs?verbose=1`.
- Parse `X-LLAR-Target`:

```http
X-LLAR-Target: <module>@<version>#<matrixStr>
```

- Decode request body:

```go
type JobRequest struct {
	Matrix Matrix `json:"matrix"`
}

type Matrix struct {
	Require map[string]string `json:"require"`
	Options map[string]string `json:"options,omitempty"`
}
```

- Keep HTTP wire structs local to `cmd/worker`.
- Do not validate `matrixStr` by recomputing it from `body.matrix`.
- Do not start build execution when request parsing fails.

Validation:

```bash
cd cloud-build-worker
go test ./cmd/worker
```

Acceptance:

- Missing `X-LLAR-Target` returns HTTP 400.
- Invalid `X-LLAR-Target` returns HTTP 400.
- Invalid JSON body returns HTTP 400.
- Missing `matrix` returns HTTP 400.
- `verbose` changes only response streaming.
- No old protocol fields appear in worker responses.

## Task 3: Implement The Approved Response Protocol

Spec mapping:

- Non-Verbose Response
- Verbose Response

Files:

- `cloud-build-worker/cmd/worker/main.go`
- `cloud-build-worker/cmd/worker/main_test.go`

Work:

- Use the approved envelope:

```go
type Message struct {
	Type string          `json:"type"`
	Body json.RawMessage `json:"body"`
}
```

- Non-verbose response is one terminal JSON message:
  - `type=completed`, `body.artifact`
  - `type=failed`, `body.message`
- Verbose response is a JSON value stream:
  - zero or more `type=log` messages
  - one terminal `type=completed` or `type=failed` message
- Log message body:

```go
type LogBody struct {
	Stream string `json:"stream,omitempty"`
	Text   string `json:"text"`
}
```

Validation:

```bash
cd cloud-build-worker
go test ./cmd/worker
```

Acceptance:

- No response uses `type=status`.
- No response uses `state`.
- Log messages use `body`, not `data`.
- Failed messages contain only `body.message`.
- Verbose output can be consumed by `json.Decoder`.

## Task 4: Implement Artifact Metadata Module

Spec mapping:

- `internal/artifact`

Files:

- `cloud-build-worker/internal/artifact/artifact.go`
- `cloud-build-worker/internal/artifact/sql.go`
- `cloud-build-worker/internal/artifact/sql_test.go`

Work:

- Implement:

```go
package artifact

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

- Implement the approved artifacts table:

```sql
CREATE TABLE artifacts (
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

- Make `Put` atomic and idempotent:
  - missing key inserts and returns the artifact
  - same key plus same checksum returns the stored artifact
  - same key plus different checksum returns conflict

Validation:

```bash
cd cloud-build-worker
go test ./internal/artifact
```

Acceptance:

- `artifact` does not upload, download, unpack, calculate checksums, manage local build entries, or handle HTTP.
- Artifact table primary key is `(module, version, matrix_str)`.
- `Put` is idempotent for the same checksum.
- Checksum conflict is observable by callers.

## Task 5: Implement Upload Module

Spec mapping:

- `internal/upload`
- Artifact Store

Files:

- `cloud-build-worker/internal/upload/upload.go`
- `cloud-build-worker/internal/upload/ghcr.go`
- `cloud-build-worker/internal/upload/ghcr_test.go`

Work:

- Implement:

```go
package upload

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

type GHCRConfig struct {
	Owner string
	Token string
}

func NewGHCR(cfg GHCRConfig) Uploader
```

- `Upload` reads from the current reader offset to EOF.
- `Upload` computes SHA-256 and size.
- `Upload` seeks back to the original offset before uploading the same bytes.
- `Upload` returns `Result{URL, Size, Checksum}`.
- `Upload` does not query or mutate the artifact DB.
- `Upload` does not manage build state.
- GHCR uses `org.llar.matrix` as the matrix annotation.
- Public GHCR blob downloads use `Authorization: Bearer QQ==`.

Validation:

```bash
cd cloud-build-worker
go test ./internal/upload
```

Acceptance:

- `Uploader.Type()` returns the artifact source type, for example `ghcr`.
- `Options.Type` is the archive type, for example `zip`.
- GHCR result URL follows the blob URL shape from the spec.
- Publish credentials are runtime configuration, not public API input.

## Task 6: Implement Worker Build Module

Spec mapping:

- Missing Artifact
- Multiple Workers
- `internal/build`
- Failure Boundaries

Files:

- `cloud-build-worker/internal/build/build.go`
- `cloud-build-worker/internal/build/build_test.go`

Work:

- Implement:

```go
package build

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

type Builds struct {
	// internal state
}

func New(opts Options) *Builds
func (b *Builds) Build(ctx context.Context, req Request, log io.Writer) (Result, error)
```

- Check completed artifact metadata before local active entry lookup.
- Maintain worker-local `ArtifactKey -> build entry` coordination.
- Join an existing local entry for the same artifact key.
- Start a new local build when artifact metadata is missing and no local entry exists.
- Fan out raw verbose logs to waiting requests.
- Keep a bounded in-memory log ring for verbose reconnect.
- Invoke the LLAR build path.
- Call `upload` for archive bytes.
- Compose `artifact.Artifact` from upload result, uploader type, archive type, and LLAR metadata.
- Call `artifact.Store.Put` before returning completed.
- Remove local entries after terminal completion.

Validation:

```bash
cd cloud-build-worker
go test ./internal/build
```

Acceptance:

- Artifact DB hit returns without starting a build.
- Local active entry is checked only after artifact metadata miss.
- Multiple requests for the same key share the same local build entry.
- A request arriving after `artifact.Store.Put` but before entry removal returns from artifact metadata.
- `log == nil` waits for final `Result` or `error`.
- `log != nil` receives raw log text while waiting.
- Failed log writer removes only that subscriber.
- `internal/build` does not depend on Gin or HTTP.
- `internal/build` does not JSON-encode log or terminal messages.
- Build-time failures return `error` for the caller to encode as failed terminal messages.

## Task 7: Implement Real LLAR Runner

Spec mapping:

- `internal/build` runner behavior

Files:

- `cloud-build-worker/internal/build/runner.go`
- `cloud-build-worker/internal/build/runner_test.go`

Work:

- Execute:

```text
llar make -v -o <tmp>/artifact.zip <module>@<version> <matrix flags>
```

- Use the generated archive as upload input.
- Take final LLAR metadata from the last non-empty stdout line.
- Treat earlier stdout and stderr as raw verbose log text.
- Keep runner details inside `internal/build`.

Validation:

```bash
cd cloud-build-worker
go test ./internal/build
```

Acceptance:

- Runner calls a real `llar make` command path.
- Runner test uses a real LLAR package scenario, for example `madler/zlib`.
- Runner returns archive input suitable for upload.
- Runner returns final LLAR metadata.
- Runner exposes raw verbose logs.
- Failed `llar make` returns an error.

## Task 8: Wire HTTP To Build

Spec mapping:

- `cmd/worker`
- Public API
- Failure Boundaries

Files:

- `cloud-build-worker/cmd/worker/main.go`
- `cloud-build-worker/cmd/worker/main_test.go`

Work:

- `cmd/worker` starts Gin and registers `POST /v1/jobs`.
- Parse HTTP input and convert it into `build.Request`.
- Pass `nil` log writer for non-verbose requests.
- Pass a raw log writer for verbose requests.
- Wrap raw logs into JSON `log` messages only in `cmd/worker`.
- Write the terminal `completed` or `failed` message only in `cmd/worker`.
- If `artifact.Store.Get` fails before build execution can proceed, return HTTP 500.
- Encode build-time failures as failed terminal messages.

Validation:

```bash
cd cloud-build-worker
go test ./...
```

Acceptance:

- `cmd/worker` owns HTTP parsing and JSON response encoding.
- `internal/build` owns build coordination.
- Request parsing errors happen before build execution.
- Completed message is sent only after `artifact.Store.Put` succeeds.
- Build failure, upload failure, `artifact.Store.Put` DB failure, and checksum conflict are failed terminal messages.

## Task 9: Add LLAR Remote Client Module

Spec mapping:

- llar Client Behavior
- llar-Side Interfaces

Files:

- `internal/remote/client.go`
- `internal/remote/client_test.go`
- `cmd/llar/internal/install.go`

Work:

- Add `internal/remote.Client` for the llar-side cloud build HTTP client.
- Hardcode the worker URL for the first implementation.
- Implement:

```go
package remote

type Client struct {
	BaseURL string
	HTTP    *http.Client
	Verbose bool
}

type Submitter interface {
	Submit(ctx context.Context, target module.Version, matrixStr string, matrix Matrix, log io.Writer) (Artifact, error)
}
```

- `Submit` maps to `POST /v1/jobs` or `POST /v1/jobs?verbose=1`.
- Send `X-LLAR-Target` and `body.matrix`.
- Decode one terminal message in non-verbose mode.
- In verbose mode, use `json.Decoder` to read log messages followed by one terminal message.
- Write `log.body.text` to the provided writer.
- Return `artifact.Artifact` on completed.
- Return error on failed.

Validation:

```bash
go test ./internal/remote ./cmd/llar/internal
```

Acceptance:

- No shared worker protocol package is introduced.
- Worker keeps its own HTTP wire structs.
- `internal/remote` owns the llar-side HTTP request and response structs.
- Failed body is decoded from `body.message`.
- Verbose logs do not affect artifact identity.

## Task 10: Reuse Builder Cache-Miss Semantics

Spec mapping:

- llar Install Remote Mode
- Completed Artifact
- llar Client Behavior

Files:

- `internal/build/build.go`
- `internal/build/build_test.go`
- `cmd/llar/internal/install.go`
- `internal/remote/client.go`

Work:

- Keep existing Builder ownership:
  - module resolution
  - dependency order
  - local cache lookup
  - install directory calculation
  - `.cache.json`
  - final result selection
- Change only the cache-miss action:
  - call remote client
  - receive completed `Artifact`
  - download `Artifact.Source`
  - verify checksum
  - extract into Builder's install directory
  - save `Artifact.Metadata` in Builder's `.cache.json`
  - return normal `Result{Metadata, OutputDir}`

Validation:

```bash
go test ./internal/build ./cmd/llar/internal
```

Acceptance:

- Existing local cache hit behavior is unchanged.
- Existing local build behavior remains available.
- Remote artifact install produces the same local cache shape expected by later `llar make`.
- Dependencies are submitted in dependency build order.
- Worker does not recursively submit dependency jobs.

## Task 11: Verify Multi-Request And Failure Behavior

Spec mapping:

- Missing Artifact
- Multiple Workers
- Failure Boundaries

Files:

- Add tests in the owning packages only.

Work:

- Test multiple same-key requests on one worker.
- Test verbose reconnect behavior against the bounded log ring.
- Test worker fallback behavior at the logic level:
  - artifact DB hit returns completed
  - artifact DB miss starts a new build
- Test client disconnect behavior:
  - waiting request stops waiting
  - shared local build may continue
  - failed verbose writer does not fail the build

Validation:

```bash
go test ./...
cd cloud-build-worker
go test ./...
```

Acceptance:

- Duplicate builds across workers are accepted only as failure-window behavior.
- `artifact.Store.Put` remains the final consistency boundary.
- Completed artifact metadata is persisted before completed message delivery.
- Artifact bytes never flow through the artifact DB.

## Final Checklist

- [ ] `cloud-build-worker/` is a fresh worker project.
- [ ] No old prototype code remains.
- [ ] Public API is only `POST /v1/jobs` and `POST /v1/jobs?verbose=1`.
- [ ] `X-LLAR-Target` is the routing and artifact identity.
- [ ] Worker does not recompute `matrixStr` from request body matrix.
- [ ] Response protocol is `type + body`.
- [ ] Failed body contains only `message`.
- [ ] Log message body contains `stream` and `text`.
- [ ] `cmd/worker` owns HTTP and JSON encoding.
- [ ] `internal/build` owns local active build entries and raw log fanout.
- [ ] `internal/artifact` owns completed artifact metadata only.
- [ ] `internal/upload` owns artifact byte upload only.
- [ ] Worker executes real `llar make`.
- [ ] `llar install` reuses existing Builder install semantics.
- [ ] No shared `cloudbuild` package exists.
- [ ] No `BuildRemote` exists.
- [ ] No exported install helper APIs were introduced without approval.
