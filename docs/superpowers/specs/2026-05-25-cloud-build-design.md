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
- **Edge Worker**: A provider-started `llar-edge-worker` process that executes
  one build job. It starts with Scheduler connection credentials, opens an
  outbound control-plane WebSocket to the Scheduler, receives the job
  assignment, prepares dependency artifacts in its workspace, coordinates
  `llar make` as the build command, uploads the artifact, and streams logs/status
  back to the Scheduler.
- **Object Storage**: Stores artifact archives. Clients and workers download
  archives from object storage URLs; workers upload completed archives.

## Command Semantics

`llar make` is the build command.

- It can still be used locally.
- The Edge Worker invokes `llar make` in a controlled workspace.
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

`ArtifactKey.String()` uses the internal form:

```text
<module>@<version>#<matrixStr>
```

This string is used for Redis artifact state keys and Asynq task IDs. It is an
internal representation; the public API still accepts structured target and
matrix fields.

## Redis and Asynq Dedupe

Redis is used for build-state dedupe, not as an artifact metadata cache. It does
not store artifact URL, type, metadata, or checksum. The artifact database
remains the source of truth for completed artifact metadata.

For each artifact key, the Scheduler maintains a Redis state key:

```text
artifact:state:<ArtifactKey.String()> = building(jobID) | completed
```

State meaning:

```text
building
  A job is already building this artifact key. Repeated submit requests return
  pending + jobID without creating another job.

completed
  This artifact key was recently completed. Submit requests query the artifact
  database and return ready + artifact when the database record exists.

missing Redis key
  Redis has no current state for this artifact key. The Scheduler queries the
  artifact database. If the artifact exists, it restores completed state in
  Redis and returns ready. If it does not exist, it creates a new job.
```

The `building` state uses a short TTL tied to the maximum build/attempt timeout
so crashed workers cannot block a key forever. The `completed` state uses a long
TTL and may rely on Redis eviction policy to let cold artifact keys disappear.

Completed Redis state is written only after the Scheduler has successfully
recorded the artifact in the artifact database. If a completed Redis state is
found but the artifact database has no matching artifact, the Scheduler treats
the Redis state as stale, deletes it, and continues through normal job creation.

Asynq is used as the lightweight Redis-backed queue for worker provisioning
tasks. Provision tasks use `ArtifactKey.String()` as their Asynq task ID, so the
queue also dedupes repeated provisioning for the same artifact key. Asynq task
state is not job state: a successful Asynq task only means a worker was
provisioned, not that the build completed.

## Artifact Store Abstraction

The design does not choose a concrete object storage backend or upload
mechanism. S3-compatible storage, GitHub Releases, GitHub Packages, local
storage, and future backends are implementation choices behind an internal
artifact store abstraction.

The public API only exposes completed artifacts as `Artifact{URL, Type,
Metadata, Checksum}`. Clients download from `Artifact.URL`; they do not know how
the artifact was uploaded or which backend produced the URL.

The Scheduler uses the artifact store abstraction for artifact lookup and for
publishing completed artifact metadata. The Edge Worker uses provider-specific
upload capability supplied through that abstraction. The upload path may be a
direct object-storage upload, a Scheduler-mediated upload, a GitHub Release
asset upload, or another backend-specific mechanism.

Those storage details must not leak into the public job protocol. For cloud
build orchestration, the only stable contract is:

```text
artifact key -> maybe completed Artifact
completed build output -> completed Artifact
```

`Artifact.URL` must be usable by clients when `POST /v1/jobs` returns
`status=ready` or when a job WebSocket reports `completed`. URL lifetime,
refresh, redirect, upload method, and backend-specific headers are artifact
store implementation details.

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

## Edge Worker Runtime

`llar-edge-worker` is a separate program from the user-facing `llar` CLI. It
owns Scheduler connectivity and cloud coordination. `llar make` remains a
protocol-free local build command and must not know about Scheduler URLs,
worker tokens, job IDs, WebSockets, provider selection, or artifact publishing.

Each `llar-edge-worker` process executes one target.

```text
1. Start with Scheduler URL and worker token from the worker provider.
2. Open the outbound worker WebSocket to the Scheduler.
3. Receive `WorkerJobMessage` from the Scheduler.
4. Prepare an isolated workspace.
5. Resolve the target enough to identify direct dependency artifacts required by
   this build.
6. For each required dependency artifact:
   a. Require its artifact to exist in the artifact store.
   b. Download it into the worker workspace installDir.
   c. Write dependency cache metadata using the existing build cache format.
7. Execute `llar make -v <module>@<version>` with the assigned matrix and
   workspace.
8. Stream child process stdout/stderr to the Scheduler as worker log messages.
9. If `llar make` exits non-zero, report failed with the mapped status/message.
10. If `llar make` succeeds, locate the target installDir in the workspace.
11. Package installDir contents as an artifact archive.
12. Upload the archive through the artifact store abstraction.
13. Report completed with Artifact{URL, Type, Metadata, Checksum}.
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
- Do not put Scheduler protocol, worker token handling, WebSocket handling, or
  artifact publishing into `llar make`.
- Do not add `sourceHash`, `formulaHash`, or lock-file based reproducibility in
  this design.
- Do not make Edge Workers public API servers. Workers are internal build
  executors that call back to the Scheduler over outbound connections.

## Open Questions

- Exact worker provider priority and fallback policy: GitHub Actions first,
  Kubernetes capacity, or other providers.
- Exact Kubernetes resource model: Job vs Pod, timeout, and cleanup.
- Worker token format, lifetime, rotation, and retry behavior.
- Worker authentication with the Scheduler and object storage.
- Client authentication with the Scheduler.
- Artifact retention, eviction, and metadata persistence.
- Whether future install concurrency should use DAG layers or a bounded worker
  pool on the client side.
