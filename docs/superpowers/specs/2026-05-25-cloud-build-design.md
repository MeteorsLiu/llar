# Cloud Build Design

## Goal

`llar install` should obtain build artifacts from cloud build infrastructure
instead of building on the user's machine. `llar make` remains the local build
command and is also the build capability used by remote workers.

This design is intentionally scoped to cloud artifact orchestration. It must
not redefine formula semantics, module loading semantics, matrix propagation,
or `build.Builder` behavior.

## Roles

The cloud build system has four roles:

```text
Client -> Scheduler -> Edge Worker -> Object Storage
```

- **Client**: `llar install`. Resolves the requested module locally, computes
  the existing build order, submits each module build to the Scheduler, downloads
  artifacts, writes the local build cache, and prints the root metadata.
- **Scheduler**: Owns the public API, artifact lookup, active-job dedupe,
  queueing, worker provisioning, job state, and client WebSocket fanout. It can
  provision workers through GitHub Actions, Kubernetes, or a fallback provider,
  but it does not execute builds and does not initiate network connections to
  workers.
- **Edge Worker**: A provider-started container or runner that executes one
  build job. It starts with Scheduler connection credentials, opens an outbound
  control-plane WebSocket to the Scheduler, receives the job assignment, reuses
  the existing `modules.Load` and `build.Builder` flow, prepares dependency
  artifacts in its workspace, builds the target, uploads the artifact, and
  streams logs/status back to the Scheduler.
- **Object Storage**: Stores artifact archives. Clients and workers download
  archives from object storage URLs; workers upload completed archives.

## Command Semantics

`llar make` is the build command.

- It can still be used locally.
- The Edge Worker uses the same build path or equivalent shared code.
- Existing build cache, workspace layout, and result selection are preserved.

`llar install` is the cloud artifact command.

- It does not build locally.
- It resolves dependencies locally and submits cloud build jobs in build order.
- It first checks the local build cache and skips remote work for local hits.
- It downloads each remote artifact into the existing workspace layout and writes
  the existing build cache metadata so later `llar make` sees a normal cache hit.
- It prints the root target metadata, matching `llar make`.
- With `-v`, it streams remote worker logs while waiting for pending jobs.

## Matrix Handling

The cloud API receives a structured matrix selection, not the internal matrix
string.

```go
type MatrixSelection struct {
	Require map[string]string `json:"require"`
	Options map[string]string `json:"options,omitempty"`
}
```

`MatrixSelection` must be complete when sent to the Scheduler. The client is
responsible for filling defaults, including the host matrix when the user did
not pass matrix flags. The Scheduler validates the matrix but does not infer
missing values from its own host.

Internally, `MatrixSelection` is converted to the existing `formula.Matrix`
shape by wrapping each selected value in a single-element slice. The internal
`MatrixStr` is:

```text
formula.Matrix.Combinations()[0]
```

`MatrixStr` is used for artifact keys, active-job dedupe, `modules.Options`,
`build.Options`, workspace install directories, and build cache keys. The API
does not expose `MatrixStr`.

Cloud build does not redefine matrix behavior. It only converts the API matrix
selection into the existing `MatrixStr` and passes that into the existing
`modules` and `build` APIs.

## Public API

### Submit Job

```text
POST /v1/jobs
```

Request:

```go
type SubmitJobRequest struct {
	Target Target          `json:"target"`
	Matrix MatrixSelection `json:"matrix"`
}

type Target struct {
	Module  string `json:"module"`
	Version string `json:"version"`
}
```

`Target.Version` is required. If the user omits a version in the CLI, the
client resolves it before submitting the request.

Response:

```go
type SubmitJobResponse struct {
	Target   Target    `json:"target"`
	Status   string    `json:"status"` // ready | pending
	JobID    string    `json:"jobID,omitempty"`
	Artifact *Artifact `json:"artifact,omitempty"`
}

type Artifact struct {
	URL      string `json:"url"`
	Type     string `json:"type"`     // zip | tar.gz | tar.zst
	Metadata string `json:"metadata"`
	Checksum string `json:"checksum"` // SHA-256 of archive bytes
}
```

Response constraints:

```text
status=ready   -> artifact required
status=pending -> jobID required
```

Scheduler behavior:

```text
artifact exists:
  return ready + artifact

artifact missing, active job exists:
  return pending + existing jobID

artifact missing, no active job:
  create JobRecord, enqueue it, provision an Edge Worker, return pending + new jobID
```

The internal artifact key is:

```go
type ArtifactKey struct {
	Module    string
	Version   string
	MatrixStr string
}
```

### Client Job WebSocket

```text
GET /v1/jobs/{jobID}/ws?verbose=true|false
```

The connection is bound to a single job. The Scheduler broadcasts the job's
terminal status to all subscribers. When `verbose=true`, it also forwards worker
logs.

The Scheduler sends two message shapes.

```go
type StatusMessage struct {
	Type  string     `json:"type"`  // status
	State JobState   `json:"state"` // completed | failed
	Body  StatusBody `json:"body"`
}

type JobState string

const (
	JobCompleted JobState = "completed"
	JobFailed    JobState = "failed"
)

type StatusBody struct {
	Artifact *Artifact `json:"artifact,omitempty"` // completed
	Status   int       `json:"status,omitempty"`   // failed, HTTP-style status
	Message  string    `json:"message,omitempty"`  // failed
}
```

```go
type LogMessage struct {
	Type string  `json:"type"` // log
	Data LogData `json:"data"`
}

type LogData struct {
	Stream string `json:"stream,omitempty"` // stdout | stderr
	Text   string `json:"text"`             // UTF-8, ANSI preserved
}
```

The WebSocket does not send progress. Without verbose logs, the client waits
for `completed` or `failed`. After sending a terminal status, the Scheduler
closes the connection.

### HTTP Errors

Synchronous API errors use HTTP status codes and a simple body:

```go
type ErrorResponse struct {
	Message string `json:"message"`
}
```

Initial status set:

```text
400 Bad Request
  malformed JSON, missing target/matrix fields, empty matrix values

404 Not Found
  unknown job, missing artifact object

409 Conflict
  inconsistent artifact/job state for an artifact key

424 Failed Dependency
  worker cannot find a required dependency artifact

500 Internal Server Error
  unexpected scheduler or worker error
```

Asynchronous job failures are sent over the client WebSocket as
`StatusMessage{State: failed}` with `body.status` and `body.message`. The
status value uses HTTP status code semantics.

## Worker Control Plane

The Scheduler owns the queue and provisions Edge Workers. The Edge Worker is a
one-job build executor.

Workers are not public HTTP/WebSocket servers. The network direction is always:

```text
Edge Worker -> Scheduler
```

When the Scheduler decides to run a queued job, it creates a worker token scoped
to that job and starts a worker through the selected provider:

```text
GitHub Actions workflow_dispatch: scheduler URL + worker token as inputs
Kubernetes Job/Pod: scheduler URL + worker token as environment variables
Fallback provider: equivalent one-job launch metadata
```

The worker starts, opens an authenticated outbound WebSocket to the Scheduler,
and waits for the job assignment:

```text
GET /v1/workers/ws
```

This is an internal control-plane endpoint. The exact URL can change, but the
direction must not: workers connect to the Scheduler, not the other way around.

After authenticating the worker token, the Scheduler binds the connection to the
queued job and sends one job message:

```go
type WorkerJobMessage struct {
	Type      string          `json:"type"` // job
	JobID     string          `json:"jobID"`
	Target    Target          `json:"target"`
	Matrix    MatrixSelection `json:"matrix"`
	MatrixStr string          `json:"matrixStr"`
}
```

Worker-to-Scheduler log/status messages include `jobID` because worker
connections are internal control-plane connections, and future worker providers
may include additional provider-level routing outside this protocol.

```go
type WorkerLogMessage struct {
	Type  string  `json:"type"` // log
	JobID string  `json:"jobID"`
	Data  LogData `json:"data"`
}

type WorkerStatusMessage struct {
	Type  string     `json:"type"` // status
	JobID string     `json:"jobID"`
	State JobState   `json:"state"` // completed | failed
	Body  StatusBody `json:"body"`
}
```

The Scheduler stores worker logs and forwards them only to verbose client
subscribers. Worker terminal status updates the job record, updates the artifact
metadata on success, fans out terminal status to clients, closes the worker
connection, and triggers provider cleanup when needed.

If a provisioned worker never connects or disconnects before a terminal status,
the Scheduler keeps the original `jobID`, marks the attempt failed internally,
and may provision a replacement worker for the same job record. Client
subscribers continue waiting on the same client WebSocket until the job reaches
`completed` or `failed`.

## Client Install Flow

`llar install` is responsible for dependency scheduling.

```text
1. Parse CLI target and matrix flags.
2. Resolve any omitted root version locally. SubmitJobRequest requires a version.
3. Convert matrix flags into a complete MatrixSelection.
4. Run modules.Load(root, MatrixStr) locally.
5. Use the same postorder build ordering as the existing build path.
6. For each module in build order:
   a. Check local build cache using existing build cache helpers.
   b. If local cache hit, skip remote work.
   c. Submit POST /v1/jobs for that module.
   d. If ready, download the artifact.
   e. If pending, open /v1/jobs/{jobID}/ws and wait for completed/failed.
   f. Verify artifact checksum.
   g. Extract artifact into the existing installDir for module/version/MatrixStr.
   h. Write cache metadata through the same helpers/format used by build.Builder.
7. Print the root module metadata.
```

The client does not submit dependency metadata or BuildList to the Scheduler.
It schedules modules one at a time in build order. Future concurrency can submit
independent modules concurrently without changing the public job API.

## Edge Worker Build Flow

Each Edge Worker job builds one target.

```text
1. Start with Scheduler URL and worker token from the worker provider.
2. Open the outbound worker WebSocket to the Scheduler.
3. Receive `WorkerJobMessage` from the Scheduler.
4. Run modules.Load(target, MatrixStr) using the service-side latest llarhub.
5. Find the main target in the returned build list.
6. For each dependency in main.Deps:
   a. Require its artifact to exist in the artifact store.
   b. Download it into the worker workspace installDir.
   c. Write dependency cache metadata using the existing build cache format.
7. Run the existing build path:
   build.NewBuilder(build.Options{MatrixStr: MatrixStr, WorkspaceDir: workerWorkspace})
   builder.Build(ctx, mods)
8. Select the main result exactly as llar make does:
   main := results[len(results)-1]
9. Package main.OutputDir contents as an artifact archive.
10. Upload the archive to object storage.
11. Report completed with Artifact{URL, Type, Metadata: main.Metadata, Checksum}.
```

The artifact archive contains the install directory contents only:

```text
include/...
lib/...
```

It does not contain a wrapper directory and does not contain `.cache.json`.
Cache metadata is materialized by the client or worker when the artifact is
installed into a workspace.

If a dependency artifact is missing, the worker reports failed with status
`424`. Official clients should not hit this path when they submit modules in
build order, but the worker still validates dependencies to handle manual API
calls, artifact expiry, and race conditions.

## Non-Goals

- Do not change formula APIs.
- Do not change `modules.Load` semantics.
- Do not change `build.Builder` ordering, cache-hit behavior, or result
  selection.
- Do not add `sourceHash`, `formulaHash`, or lock-file based reproducibility in
  this design.
- Do not make Edge Workers public API servers. Workers are internal build
  executors that call back to the Scheduler over outbound connections.

## Open Questions

- Exact worker provider priority and fallback policy: GitHub Actions first,
  Kubernetes capacity, or other providers.
- Exact Kubernetes resource model: Job vs Pod, timeout, and cleanup.
- Worker token format, lifetime, rotation, and retry behavior.
- Object storage upload mechanism and URL lifetime.
- Worker authentication with the Scheduler and object storage.
- Client authentication with the Scheduler.
- Artifact retention, eviction, and metadata persistence.
- Whether future install concurrency should use DAG layers or a bounded worker
  pool on the client side.
