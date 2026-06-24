# GHCR Artifact Store

GHCR is the default Artifact Store backend for LLAR binary artifacts.

## Storage Shape

```text
package = ghcr.io/<owner>/<module>
tag     = <version>
matrix  = OCI index manifest annotation org.llar.matrix = <MatrixStr>
blob    = artifact archive layer
```

One GHCR package stores one LLAR module. One tag stores one module version. The tag points to an OCI image index. Each matrix variant is one manifest entry in that index.

## OCI Layout

```text
ghcr.io/<owner>/<module>:<version>
  OCI image index
    manifest entry
      annotations:
        org.llar.matrix = <MatrixStr>
      platform:
        os           = <os>
        architecture = <arch>
      image manifest
        layer
          media type = application/vnd.oci.image.layer.v1.tar+gzip
          content    = artifact.tar.gz
```

`platform.os` and `platform.architecture` should be filled when the selected matrix has those values. LLAR artifact matching uses `org.llar.matrix`.

## Blob URL

The uploader publishes the archive blob, writes or updates the version index, and returns the final blob URL:

```text
https://ghcr.io/v2/<owner>/<module>/blobs/sha256:<digest>
```

`llar install` detects GHCR downloads from the URL host. Public GHCR blob downloads use:

```http
Authorization: Bearer Qq==
```

## Example

For `madler/zlib@v1.3.1`:

```text
package = ghcr.io/llar-artifacts/madler/zlib
tag     = v1.3.1
matrix  = org.llar.matrix = amd64-linux
blob    = artifact.tar.gz
```

OCI layout:

```text
ghcr.io/llar-artifacts/madler/zlib:v1.3.1
  OCI image index
    manifest entry
      annotations:
        org.llar.matrix = amd64-linux
      platform:
        os           = linux
        architecture = amd64
      image manifest
        layer
          media type = application/vnd.oci.image.layer.v1.tar+gzip
          content    = artifact.tar.gz
          digest     = sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
    manifest entry
      annotations:
        org.llar.matrix = arm64-darwin
      platform:
        os           = darwin
        architecture = arm64
      image manifest
        layer
          media type = application/vnd.oci.image.layer.v1.tar+gzip
          content    = artifact.tar.gz
          digest     = sha256:486ea46224d1bb4fb680f34f7c9ad96a8f24ec88be73ea8e5a6c65260e9cb8a7
```

Returned artifact URL for `amd64-linux`:

```text
https://ghcr.io/v2/llar-artifacts/madler/zlib/blobs/sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
```
