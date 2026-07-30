# LLAR Formula Guide

## Contents

- [Module files](#module-files)
- [Formula classfile](#formula-classfile)
- [Minimal formula](#minimal-formula)
- [Top-level API](#top-level-api)
- [Hook lifecycle](#hook-lifecycle)
- [Dependencies](#dependencies)
- [Build matrix](#build-matrix)
- [Context and results](#context-and-results)
- [CMake](#cmake)
- [Autotools](#autotools)
- [Shell commands](#shell-commands)
- [Metadata](#metadata)
- [Installed-output tests](#installed-output-tests)
- [Version comparator](#version-comparator)
- [Complete example](#complete-example)
- [Review checklist](#review-checklist)

## Module Files

An LLAR module uses the upstream repository id `<owner>/<repo>`.

```text
owner/repo/
  versions.json
  Repo_cmp.gox          # optional
  v1.2.3/
    Repo_llar.gox
  v2.0.0/
    Repo_llar.gox
```

`versions.json` contains the module id and exact-version direct dependency
fallbacks:

```json
{
  "path": "example/libalpha",
  "deps": {
    "v1.2.3": [
      {"path": "example/libbeta", "version": "v2.3.4"}
    ]
  }
}
```

- `path` matches formula `id`.
- Each `deps` key is an exact requested source version.
- Each dependency is direct.
- Use an empty object when there are no fallback dependencies.

LLAR scans files ending in `_llar.gox`. It selects the greatest literal
`fromVer` not newer than the requested version under the module comparator.
The directory name does not select the formula.

Use a valid Go identifier before the first underscore in formula and comparator
filenames, for example `Libalpha_llar.gox` and `Libalpha_cmp.gox`.

## Formula Classfile

`_llar.gox` is an XGo project class backed by `formula.ModuleF`. The generated
class embeds `ModuleF`. Top-level executable statements become `MainEntry`; the
generated `Main` calls `formula.XGot_ModuleF_Main`.

Do not declare or call the generated class, `MainEntry`, `Main`, or
`XGot_ModuleF_Main`.

`cmake` and `autotools` are automatic imports. Add Go imports explicitly:

```gox
import "slices"
```

Place imports, types, constants, class fields, and helper declarations before
the first executable statement such as `id`. A receiverless top-level function
becomes a method of the generated formula class.

Use the XGo surface names:

- `ctx.outputDir` calls the zero-argument output-directory overload.
- `ctx.outputDir(dep)` calls the dependency overload.
- `os.readFile` resolves to exported Go function `os.ReadFile`.
- Standalone calls may omit parentheses.
- Calls used as expressions keep parentheses.
- Do not call generated overload names such as `OutputDir__0`.

## Minimal Formula

```gox
id "example/libalpha"

fromVer "v1.2.3"

onBuild ctx => {
    installDir := ctx.outputDir

    a := autotools.new(ctx.SourceDir, ctx.SourceDir+"/_build", installDir)
    a.configure
    a.build
    a.install

    ctx.setMetadata "-lalpha"
}
```

Every formula sets:

- `id` equal to `versions.json.path`;
- a non-empty string-literal `fromVer`;
- `onBuild ctx => { ... }`.

## Top-Level API

| DSL | Go API | Purpose |
| --- | --- | --- |
| `id "owner/repo"` | `(*ModuleF).Id(string)` | Set the module id. |
| `fromVer "version"` | `(*ModuleF).FromVer(string)` | Set the first version served by the formula. |
| `target.require["key"]` | `ModuleF.Target().Require()` | Read selected propagated dimensions. |
| `target.options["key"]` | `ModuleF.Target().Options()` | Read selected package options. |
| `defaults {"key": "value"}` | `(*ModuleF).Defaults(map[string]string)` | Set option defaults. |
| `filter => { ... }` | `(*ModuleF).Filter(func() bool)` | Reject unsupported selections. |
| `onRequire (proj, deps) => { ... }` | `(*ModuleF).OnRequire(func(*Project, *ModuleDeps))` | Discover direct dependencies. |
| `onBuild ctx => { ... }` | `(*ModuleF).OnBuild(func(*Context))` | Build and install. |
| `onTest ctx => { ... }` | `(*ModuleF).OnTest(func(*Context))` | Test installed output. |

`defaults`, `filter`, `onRequire`, and `onTest` are optional.

There is no top-level `matrix` declaration. There is no
`ctx.currentMatrix()` method.

## Hook Lifecycle

For one requested module/version/matrix, LLAR:

1. Selects the formula by comparator and `fromVer`.
2. Applies requested Require values, defaults, and option overrides to
   `target`.
3. Runs `filter`.
4. Runs `onRequire` and reconciles dependencies with `versions.json`.
5. Resolves the graph and builds dependencies before dependents.
6. Reuses a cached result or runs `onBuild` and caches the install directory
   plus metadata.
7. Under `llar test`, runs the root module's `onTest` after a fresh build or a
   cache hit.

LLAR recovers panics from `filter`, `onRequire`, `onBuild`, and `onTest` and
returns contextual errors.

Use `!` for a required error-returning operation:

```gox
data := proj.readFile("CMakeLists.txt")!
```

Handle an error explicitly when failure is an accepted source condition:

```gox
data, err := proj.readFile("optional.lock")
if err == nil {
    _ = data
}
```

Current CMake and Autotools `configure`, `build`, and `install` methods return
no error and panic on command failure:

```gox
c.configure
c.build
c.install
```

Do not assign their results or add manual error checks.

## Dependencies

`onRequire` receives:

| Surface | Type | Purpose |
| --- | --- | --- |
| `proj` | `*Project` | Selected upstream source. |
| `proj.readFile(path)` | `([]byte, error)` | Read an upstream source file. |
| `deps` | `*ModuleDeps` | Direct dependency collector. |
| `deps.require(path, version)` | `func(string, string)` | Add a direct dependency. |
| `deps.deps()` | `[]module.Version` | Read dependencies collected so far. |

Static direct dependency:

```gox
onRequire (proj, deps) => {
    deps.require "example/libbeta", "v2.3.4"
}
```

Source-derived dependency:

```gox
onRequire (proj, deps) => {
    data, err := proj.readFile("dependency.lock")
    if err != nil {
        return
    }

    version := parseDependencyVersion(data)
    if version != "" {
        deps.require "owner/dependency", version
    }
}
```

Declare direct dependencies only. Translate upstream dependency names to LLAR
module ids explicitly.

Dependency fallback rules:

- If `onRequire` is absent or yields no usable dependency, use
  `versions.json.deps[requestedVersion]`.
- If `onRequire` supplies an empty dependency version, fill it only from the
  same dependency path under that exact requested version.
- Omit an empty-version dependency when no matching fallback exists.
- When `onRequire` produces resolved dependencies, do not append other static
  fallback entries.

Use resolved dependency roots during build:

```gox
a := autotools.new(ctx.SourceDir, ctx.SourceDir+"/_build", ctx.outputDir)
for _, dep := range ctx.Proj.Deps {
    depDir := ctx.outputDir(dep)
    a.use depDir
}
```

`ctx.outputDir(dep)` records a lookup error and panics.

## Build Matrix

| Group | Meaning | Examples |
| --- | --- | --- |
| `target.require` | Environment dimensions propagated through dependency resolution. | `os`, `arch`, libc, ABI, toolchain |
| `target.options` | Choices owned by the current package. | `shared`, `debug`, `beta`, `tests` |

Values are `[]string`. Test membership with `slices.contains`:

```gox
import "slices"

defaults {
    "shared": "OFF",
    "beta": "OFF",
}

filter => {
    if slices.contains(target.require["os"], "unsupported-os") &&
        slices.contains(target.require["arch"], "unsupported-arch") {
        return false
    }
    return true
}

onRequire (proj, deps) => {
    if slices.contains(target.options["beta"], "ON") {
        deps.require "example/libbeta", "v2.3.4"
    }
}
```

Matrix rules:

- Read a key only when it affects dependencies, build flags, tests, or
  support.
- `defaults` applies only to Options. It is not a legal-value schema.
- Required values propagate to dependencies. Package Options do not.
- Use `filter` for unsupported selections.
- Keep options independent.
- Combine dependent choices into one option. Prefer
  `backend=[native,provider-static,provider-shared]` over separate `backend`
  and `provider-linkage` keys that create meaningless combinations.

CLI forms:

```text
--os linux
--require os=linux
--option shared=ON
```

Without matrix flags, the CLI supplies host `os` and `arch`. When matrix flags
are present, supply every required dimension explicitly.

## Context And Results

| Surface | Type | Purpose |
| --- | --- | --- |
| `ctx.SourceDir` | `string` | Selected source checkout. |
| `ctx.Proj.Deps` | `[]module.Version` | Resolved build dependencies. |
| `ctx.outputDir` | `string` | Current install directory. |
| `ctx.outputDir(dep)` | `string` | Dependency install directory. |
| `ctx.setMetadata(value)` | `func(string)` | Replace current metadata. |
| `ctx.buildResult(dep)` | `(BuildResult, bool)` | Read a dependency build result. |
| `result.metadata()` | `string` | Read dependency metadata. |

Dependency metadata example:

```gox
if len(ctx.Proj.Deps) > 0 {
    dep := ctx.Proj.Deps[0]
    result, ok := ctx.buildResult(dep)
    if ok {
        ctx.setMetadata result.metadata() + " -lcurrent"
    }
}
```

## CMake

```gox
c := cmake.new(ctx.SourceDir, ctx.SourceDir+"/_build", ctx.outputDir)
c.buildType "BUILD_TYPE"

for _, dep := range ctx.Proj.Deps {
    c.use ctx.outputDir(dep)
}

c.configure
c.build
c.install
```

| Call | Purpose |
| --- | --- |
| `cmake.new(source, build, install)` | Create a wrapper. |
| `c.source(dir)` | Override source directory. |
| `c.generator(name)` | Select generator. |
| `c.buildType(name)` | Set `CMAKE_BUILD_TYPE`. |
| `c.toolchain(path)` | Set `CMAKE_TOOLCHAIN_FILE`. |
| `c.define(key, value)` | Add STRING cache definition. |
| `c.defineBool(key, value)` | Add BOOL cache definition. |
| `c.use(root)` | Add dependency prefix. |
| `c.configure(args...)` | Run CMake configure. |
| `c.build(args...)` | Run CMake build. |
| `c.install(args...)` | Run CMake install. |
| `c.outputDir` | Return install dir, or build dir without an install dir. |

Do not add policy flags, force a generator, disable features, or choose
static/shared settings without source evidence.

## Autotools

```gox
a := autotools.new(ctx.SourceDir, ctx.SourceDir+"/_build", ctx.outputDir)

for _, dep := range ctx.Proj.Deps {
    a.use ctx.outputDir(dep)
}

a.configure
a.build
a.install
```

| Call | Purpose |
| --- | --- |
| `autotools.new(source, build, install)` | Create a wrapper. |
| `a.source(dir)` | Override source directory. |
| `a.use(root)` | Add dependency prefix. |
| `a.configure(args...)` | Run source `configure` in build dir and prepend `--prefix`. |
| `a.build(args...)` | Run `make`. |
| `a.install(args...)` | Run `make install`. |
| `a.outputDir` | Return install dir, or build dir without an install dir. |

Both `use` methods add existing dependency include, library, pkg-config, and
CMake search paths to the build environment.

## Shell Commands

`formula.ModuleF` embeds `gsh.App`:

| Surface | Purpose |
| --- | --- |
| `exec commandLine` | Run a command line. |
| `exec name, args...` | Run a command with explicit arguments. |
| `capout => { ... }` | Capture stdout. |
| `output` | Last captured stdout. |
| `lastErr` | Last command error. |
| `exitCode` | Last command exit code. |

Fail a hook on non-zero command status:

```gox
exec testBinary
if lastErr != nil {
    panic lastErr
}
```

## Metadata

Metadata describes how a consumer uses the installed package. Set the final
value once with `ctx.setMetadata`.

Direct linker metadata:

```gox
ctx.setMetadata "-lalpha"
```

Package-config metadata:

```gox
installDir := ctx.outputDir
c.use installDir
capout => {
    exec "pkg-config", "--libs", "libalpha"
}
if lastErr != nil {
    panic lastErr
}
ctx.setMetadata output
```

Derive metadata from installed artifacts or a verified package-config file.

## Installed-Output Tests

`onTest` runs for the requested root after a fresh build or a cache hit. It does
not change cached metadata.

```gox
onTest ctx => {
    installDir := ctx.outputDir
    testSrc := ctx.SourceDir + "/tests"
    testBuild := ctx.SourceDir + "/_testbuild"

    tc := cmake.new(testSrc, testBuild, testBuild+"/_out")
    tc.buildType "BUILD_TYPE"
    tc.use installDir
    tc.configure
    tc.build

    exec testBuild + "/consumer_check"
    if lastErr != nil {
        panic lastErr
    }
}
```

Use a separate test build directory. Depend only on source files, installed
artifacts, and declared dependency outputs. Do not depend on the `onBuild`
scratch directory because `onBuild` is skipped on cache hits.

## Version Comparator

Without `_cmp.gox`, LLAR uses GNU version comparison. Add one comparator only
when actual source tags require another order.

`_cmp.gox` uses `cmp.CmpApp`. `semver` and `gnu` are automatic imports.

```gox
compareVer (a, b) => {
    return semver.Compare(a.Version, b.Version)
}
```

Use semver comparison only for compatible semantic-version tag spelling.
Validate ordering against actual tags and all `fromVer` values.

## Complete Example

```gox
id "example/libalpha"

fromVer "v1.2.3"

onRequire (proj, deps) => {
    deps.require "example/libbeta", "v2.3.4"
}

onBuild ctx => {
    installDir := ctx.outputDir
    a := autotools.new(ctx.SourceDir, ctx.SourceDir+"/_build", installDir)

    for _, dep := range ctx.Proj.Deps {
        a.use ctx.outputDir(dep)
    }

    a.configure
    a.build
    a.install

    ctx.setMetadata "-lalpha"
}
```

`example/libalpha` and `example/libbeta` are fictional. Replace every module
id, version, option, flag, target name, and metadata value with verified values
for the selected source.

## Review Checklist

- [ ] `versions.json.path` and formula `id` match.
- [ ] `fromVer` is a non-empty string literal.
- [ ] Filename prefix is a valid Go identifier.
- [ ] Imports and helper declarations precede `id`.
- [ ] Hooks use current signatures.
- [ ] Dependencies are direct and enabled.
- [ ] Empty dependency versions have an exact fallback.
- [ ] Require and Options keys have the correct ownership.
- [ ] Options are independent and defaults are valid.
- [ ] Filter rejects only unsupported selections.
- [ ] CMake/Autotools calls use the panic-based API.
- [ ] Shell failures check `lastErr`.
- [ ] Metadata matches installed output.
- [ ] `onTest` is independent of the `onBuild` scratch directory.
- [ ] Exact versions and affected matrix selections pass `llar test`.
