## GET artifact

### Request

```http
GET /v1/artifacts/<module>@<version>?<matrix query>
```

- `<module>` — package module identifier (e.g. `madler/zlib`).
- `<version>` — version string (e.g. `v1.3.1`).
- `<matrix query>` — build matrix parameters (e.g. `arch=amd64&os=linux&debug=false`), used to select a specific build variant of the artifact.

Examples:

```http
GET /v1/artifacts/madler/zlib@v1.3.1?arch=amd64&os=linux
GET /v1/artifacts/pnggroup/libpng@v1.6.47?arch=amd64&os=linux&debug=false
```

### Response

- Content type: `application/x-cmdjsonl`. See [github.com/qiniu/x/cmdjsonl](https://pkg.go.dev/github.com/qiniu/x/cmdjsonl) for the line format (`Command <space> JSON object`, newline-delimited).

The response is a stream of commands, each on its own line, describing the progress and result of resolving and building the requested artifact. Three commands are currently supported: `info`, `error`, and `artifact`.

#### `info`

Reports human-readable progress information about the build process. Carries a single string payload.

```
info "fetching source for madler/zlib@v1.3.1"
info "configuring build (arch=amd64, os=linux)"
info "compiling..."
```

- Payload: a JSON string containing a free-form, human-readable message.
- Clients may display these messages to the user as progress updates but should not depend on their content for control flow.
- Multiple `info` lines may be emitted over the course of a single response.

#### `error`

Reports an error encountered while resolving or building the artifact. Carries a single string payload.

```
error "module madler/zlib@v1.3.1 not found"
error "build failed: missing dependency pnggroup/libpng"
```

- Payload: a JSON string containing a human-readable error message.
- An `error` line indicates the request has failed. No `artifact` line should be expected for the failing module after this point.
- A response may still contain `artifact` lines for dependencies that were successfully resolved before the error occurred.

#### `artifact`

Reports a successfully resolved/built artifact, including its location and (optionally) its dependency graph.

```json
artifact {"id": "<module-id>", "type": "zip | tar.gz | ...", "url": "<artifact-binary-download-url>", "deps": ["<dep-module-id1>", "<dep-module-id2>", ...]}
```

Fields:

| Field  | Type     | Required | Description |
|--------|----------|----------|--------------|
| `id`   | string   | yes | The module ID of this artifact, in the form `<module>@<version>?<matrix query>` (e.g. `madler/zlib@v1.3.1?arch=amd64&os=linux`). This is the same identifier format used in the request path/query. |
| `type` | string   | yes | The archive format of the artifact binary, e.g. `"zip"`, `"tar.gz"`. |
| `url`  | string   | yes | A download URL for the artifact binary, in the format/archive given by `type`. |
| `deps` | string[] | no  | A list of module IDs (same `<module>@<version>?<matrix query>` format as `id`) that this artifact depends on. If omitted or empty, the artifact has no dependencies. Each dependency should itself appear as a separate `artifact` line in the response (possibly with its own `deps`). |

Notes:

- A single response may contain multiple `artifact` lines: one for the requested module, and one for each (transitive) dependency in its build graph.
- There is no fixed ordering requirement between `artifact` lines, but dependencies are typically emitted before or alongside the modules that depend on them.
- `id` and entries in `deps` use the **module-id** format `<module>@<version>?<matrix query>`, exactly as used to address artifacts via `GET /v1/artifacts/<module>@<version>?<matrix query>`. This allows clients to recursively resolve any dependency by issuing the same kind of request against its `id`.
- `deps` is optional. Artifacts with no dependencies may omit the field entirely.

### Example response streams

**Artifact with no dependencies:**

```
info "resolving madler/zlib@v1.3.1 (arch=amd64, os=linux)"
info "no dependencies required"
artifact {"id": "madler/zlib@v1.3.1?arch=amd64&os=linux", "type": "tar.gz", "url": "https://cdn.example.com/artifacts/zlib-1.3.1-amd64-linux.tar.gz"}
```

**Artifact with dependencies:**

```
info "resolving pnggroup/libpng@v1.6.47 (arch=amd64, os=linux, debug=false)"
info "dependency required: madler/zlib"
artifact {"id": "madler/zlib@v1.3.1?arch=amd64&os=linux&debug=false", "type": "tar.gz", "url": "https://cdn.example.com/artifacts/zlib-1.3.1-amd64-linux.tar.gz"}
info "compiling libpng..."
artifact {"id": "pnggroup/libpng@v1.6.47?arch=amd64&os=linux&debug=false", "type": "tar.gz", "url": "https://cdn.example.com/artifacts/libpng-1.6.47-amd64-linux-debug-false.tar.gz", "deps": ["madler/zlib@v1.3.1?arch=amd64&os=linux&debug=false"]}
```

**Error response:**

```
info "resolving unknown/foo@v1.0.0"
error "module unknown/foo@v1.0.0 not found"
```
