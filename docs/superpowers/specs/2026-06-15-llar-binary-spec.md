# LLAR Binary Artifact

## Archive

```text
artifact.tar.gz
  .llar/
    metadata.json
  ...
```

The default archive type is `tar.gz`. The archive root is the artifact install directory. `.llar/` is reserved for LLAR files. Other files are package payload.

C/C++ example:

```text
artifact.tar.gz
  .llar/
    metadata.json
  include/
    zlib.h
    zconf.h
  lib/
    libz.a
    pkgconfig/
      zlib.pc
```

## `.llar/metadata.json`

```json
{
  "metadata": "-I{{.InstallDir}}/include -L{{.InstallDir}}/lib -lz",
  "deps": [
    "madler/zlib@v1.3.1"
  ]
}
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `metadata` | string | yes | LLAR build metadata, same semantic value as `formula.BuildResult.Metadata()`. |
| `deps` | string[] | no | Artifact dependencies in `module@version` form. |

No `version` field is defined.

## Path Placeholder

Use `{{.InstallDir}}` for this artifact's install directory inside metadata.

```text
/tmp/llard/work/madler/zlib/install/include
{{.InstallDir}}/include
```

`llar install` expands it to the local install directory before writing `.cache.json`.

## Rules

Producer:

- archive the install directory as the archive root;
- include `.llar/metadata.json`;
- replace this artifact's build-time install directory with `{{.InstallDir}}`.

Installer:

- verify and extract the archive;
- read `.llar/metadata.json`;
- expand `{{.InstallDir}}`;
- write metadata into `.cache.json`.

## Invalid Artifacts

| Case | Reason |
| --- | --- |
| missing `.llar/metadata.json` | no LLAR metadata |
| invalid `.llar/metadata.json` | cannot decode metadata |
| missing `metadata` | cannot determine this artifact's usage information, such as include paths and libraries, for the selected matrix |
