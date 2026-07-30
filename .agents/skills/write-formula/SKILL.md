---
name: write-formula
description: Create, migrate, debug, and validate current LLAR build formulas, versions.json dependency tables, and optional _cmp.gox comparators. Use when adding or updating a package in llarhub, porting a formula to a new LLAR Formula API, declaring dependencies or matrix options, choosing CMake or Autotools settings, adding onTest verification, or diagnosing a .gox formula failure.
---

# Write an LLAR Formula

Write the smallest formula that implements the target package's verified build
and consumer requirements. Treat the target package and the LLAR revision used
by the project as the sources of truth.

## XGo Prerequisite

Before editing any `.gox` file, read and follow the `xgo-classfile` skill when
it is available. Otherwise read
[XGo Classfiles for Formula Authors](references/xgo-classfile.md) completely.

Keep the boundaries separate:

- XGo defines syntax and classfile-to-Go generation.
- LLAR's registered base classes define the Formula DSL.
- The target package defines build flags, dependencies, installed artifacts,
  and consumer metadata.

Do not copy XGo syntax into the Formula API or infer an LLAR method from a
generated `XGot_`, `Gopt_`, `Gops_`, `Gopx_`, or overload symbol.

## Source Of Truth

Before writing a formula:

1. Identify the LLAR revision used by local validation and CI.
2. Inspect these files from that revision:
   - `formula/classfile.go`
   - `formula/project.go`
   - `internal/ixgo/classfile.go`
   - `x/cmake/cmake.go`
   - `x/autotools/autotools.go`
3. Inspect the target package at the exact upstream tag. Read its build files,
   dependency declarations, install rules, and consumer configuration.
4. Inspect nearby formulas only for repository conventions and current DSL
   spelling. Do not inherit their package-specific flags or metadata.

Use the current upstream LLAR source when no revision is pinned:

- [Formula class](https://github.com/goplus/llar/blob/main/formula/classfile.go)
- [Formula context](https://github.com/goplus/llar/blob/main/formula/project.go)
- [Classfile registration](https://github.com/goplus/llar/blob/main/internal/ixgo/classfile.go)
- [CMake wrapper](https://github.com/goplus/llar/blob/main/x/cmake/cmake.go)
- [Autotools wrapper](https://github.com/goplus/llar/blob/main/x/autotools/autotools.go)

Do not add fallback flags, optional dependency paths, compatibility branches,
or defensive error handling until source or a reproduced build proves they are
needed.

## Workflow

1. Confirm the module path and exact upstream tag.
2. Determine the upstream build system and the supported static/shared,
   platform, and optional dependency settings.
3. Determine the installed headers, libraries, tools, and correct consumer
   metadata.
4. Decide whether dependencies are static in `versions.json`, discovered by
   `onRequire`, selected by the matrix, or a combination of these.
5. Update only the required `versions.json`, `_llar.gox`, and optional
   `_cmp.gox` files.
6. Run the formula through the same LLAR revision and command path used by CI.
7. Inspect installed output and test actual consumer behavior; a successful
   configure/build command alone is insufficient.

## Repository Layout

```text
owner/repo/
  versions.json
  Repo_cmp.gox                 # optional custom version ordering
  from-version/Repo_llar.gox   # one or more formula thresholds
```

Formula filenames must end in `_llar.gox`; comparator filenames must end in
`_cmp.gox`. Keep the filename prefix before the first underscore a valid Go
identifier because LLAR uses it to find the generated class.

The directory name is organizational. LLAR scans `_llar.gox` files and selects
the greatest `fromVer` not newer than the requested version. `fromVer` must be
a non-empty string literal so LLAR can read it from the AST before loading the
classfile.

## Current Formula Shape

Use the current one-context build hook. Do not retain the old
`onBuild (ctx, proj, out)` or `onTest (ctx, proj, out)` signatures.

```gox
id "madler/zlib"

fromVer "1.0.0"

onBuild ctx => {
	installDir := ctx.outputDir

	a := autotools.new(ctx.SourceDir, ctx.SourceDir+"/_build", installDir)
	a.configure "--static"
	a.build
	a.install

	ctx.setMetadata "-lz"
}
```

`_llar.gox` is a registered project class backed by `formula.ModuleF`.
`autotools` and `cmake` are imported automatically. Import standard-library or
other packages explicitly.

## Top-Level DSL

| DSL | Current Go API | Purpose |
| --- | --- | --- |
| `id "owner/repo"` | `ModuleF.Id(string)` | Set the module path served by the formula. |
| `fromVer "tag"` | `ModuleF.FromVer(string)` | Set the oldest version served by this formula. |
| `defaults {"key": "value"}` | `ModuleF.Defaults(map[string]string)` | Set default package option selections. |
| `filter => { ... }` | `ModuleF.Filter(func() bool)` | Reject an unsupported effective matrix. |
| `onRequire (proj, deps) => { ... }` | `ModuleF.OnRequire(func(*Project, *ModuleDeps))` | Discover direct dependencies. |
| `onBuild ctx => { ... }` | `ModuleF.OnBuild(func(*Context))` | Build and install the module. |
| `onTest ctx => { ... }` | `ModuleF.OnTest(func(*Context))` | Verify the root module's installed artifacts. |

Every build formula must set a literal `fromVer` and register `onBuild`.
`defaults`, `filter`, `onRequire`, and `onTest` are optional. Keep `id` aligned
with the module path in `versions.json`.

There is no top-level `matrix` declaration and no `ctx.currentMatrix()` API.

## Hook Data

### `onRequire`

`proj.readFile(path)` reads the target package's upstream source at the
selected tag. Use it when dependencies must be derived from checked-in build
or lock files. `deps.require(path, version)` declares a direct dependency.

A required read may use XGo's panic-on-error operator because LLAR recovers
hook panics and reports them as `onRequire` errors:

```gox
onRequire (proj, deps) => {
	data := proj.readFile("build-config.txt")!
	_ = data
	deps.require "owner/dependency", "v1.2.3"
}
```

Handle an error explicitly only when absence or failure is verified to be an
accepted package behavior.

### `onBuild` and `onTest`

| DSL | Current Go API | Purpose |
| --- | --- | --- |
| `ctx.SourceDir` | `Context.SourceDir` | Upstream source checkout. |
| `ctx.Proj.Deps` | `Context.Proj.Deps` | Resolved build dependencies. |
| `ctx.outputDir` | `Context.OutputDir__0()` | Current module install directory. |
| `ctx.outputDir(dep)` | `Context.OutputDir__1(module.Version)` | Dependency install directory. |
| `ctx.buildResult(dep)` | `Context.BuildResult(module.Version)` | Dependency result and presence boolean. |
| `ctx.setMetadata(value)` | `Context.SetMetadata(string)` | Set current module consumer metadata. |
| `result.metadata()` | `BuildResult.Metadata()` | Read dependency metadata. |

`ctx.outputDir(dep)` records lookup failure and panics. LLAR recovers the panic
at the hook boundary. Do not wrap it in repeated manual error plumbing.

`onTest` runs only for the requested root under `llar test`. It also runs on a
build cache hit, when `onBuild` and its scratch directory may not have run.
Build tests in their own directory and depend only on `ctx.SourceDir`, installed
artifacts under `ctx.outputDir`, and declared dependencies.

## Failure Model

LLAR recovers panics from `filter`, `onRequire`, `onBuild`, and `onTest` and
returns contextual errors. Current CMake and Autotools
`configure`/`build`/`install` methods panic on failure and return no error.

Write direct calls:

```gox
c.configure
c.build
c.install
```

Do not assign their results, add `if err != nil`, or manually panic after each
builder call.

The embedded `gsh.App` shell surface is different: `exec` returns an error and
stores it in `lastErr`. Check it when command failure must fail the hook:

```gox
exec testBinary
if lastErr != nil {
	panic lastErr
}
```

For captured output, verify the command before using `output`:

```gox
capout => {
	exec "pkg-config", "--libs", "libname"
}
if lastErr != nil {
	panic lastErr
}
ctx.setMetadata output
```

## Dependencies And `versions.json`

Every module requires `versions.json`, even when `onRequire` is present:

```json
{
	"path": "owner/repo",
	"deps": {
		"v1.2.3": [
			{"path": "owner/dependency", "version": "v4.5.6"}
		]
	}
}
```

Dependency resolution uses the exact requested version as the `deps` key.

- If `onRequire` yields no dependencies, LLAR uses that version's
  `versions.json` entry.
- If `onRequire` declares an empty dependency version, LLAR fills it only when
  the same dependency path exists in that version's table; otherwise LLAR
  omits it.
- When `onRequire` produces resolved dependencies, LLAR does not append the
  rest of the fallback table automatically.
- In `onBuild`, iterate `ctx.Proj.Deps`, resolve each root with
  `ctx.outputDir(dep)`, and pass only required roots to the build tool.

Do not declare a dependency just because the upstream supports it optionally.
Confirm that the selected build configuration enables and consumes it.

## Matrix

Read selected values through `target`:

| DSL | Meaning |
| --- | --- |
| `target.require["key"]` | Required build dimension supplied by the client, as `[]string`. |
| `target.options["key"]` | Package option after defaults and client overrides, as `[]string`. |
| `defaults {"key": "value"}` | Default an option when the client does not override it. |
| `filter => { ... }` | Accept or reject the effective selection before dependency resolution. |

Use literal keys and compare the slice values explicitly, commonly with
`slices.contains`. Required dimensions use `--<key> value` or
`--require key=value`; package options use `--option key=value`.

When no matrix flags are passed, the CLI supplies host `os` and `arch`. Once
any matrix flag is passed, only the provided dimensions are present, so include
every required dimension in validation commands.

## Build Tools

Both wrappers accept `(sourceDir, buildDir, installDir)` and expose
`source`, `use`, and `outputDir`.

Autotools:

```gox
a := autotools.new(sourceDir, buildDir, installDir)
a.source(sourceDir)
a.use(dependencyRoot)
a.configure "--enable-feature"
a.build
a.install
```

`configure` prepends `--prefix=<installDir>` and runs the source configure
script from the build directory.

CMake:

```gox
c := cmake.new(sourceDir, buildDir, installDir)
c.source(sourceDir)
c.generator "Ninja"
c.buildType "Release"
c.toolchain toolchainFile
c.define "KEY", "VALUE"
c.defineBool "FEATURE", true
c.use dependencyRoot
c.configure
c.build
c.install
```

`use` adds existing dependency include, library, CMake, and pkg-config paths to
the build environment. Inspect the wrapper source before relying on exact path
or flag behavior.

Choose the wrapper from the target package's supported build entrypoints. Do
not add generic CMake policy flags or switch build systems without a reproduced
need.

## Consumer Metadata

`ctx.setMetadata` overwrites the current build result metadata. Derive the
value from the installed artifact and actual consumer workflow, for example a
verified linker flag or captured pkg-config output. Do not guess it from the
repository name.

Use `ctx.buildResult(dep)` only when the current package's consumer metadata
must intentionally include dependency metadata.

## Version Comparator

LLAR uses GNU version comparison when no `_cmp.gox` file exists. Add one only
when verified upstream tags require a different order. `_cmp.gox` is backed by
`cmp.CmpApp`; `semver` and `gnu` are imported automatically.

```gox
compareVer (a, b) => {
	return semver.Compare(a.Version, b.Version)
}
```

Keep only one comparator file per module and validate it against real upstream
tags, including the `fromVer` thresholds used by the formulas.

## Validation

Run from the llarhub repository root with the LLAR revision used by CI:

```sh
llar test --verbose ./owner/repo@exact-upstream-tag \
  --os "$(go env GOOS)" --arch "$(go env GOARCH)"
```

Add every non-default `--option key=value` required by the case. Exercise each
supported option and platform selection affected by the formula.

Verify:

1. The exact upstream tag exists and selects the intended `fromVer` formula.
2. Dependency discovery and pinned versions match upstream source.
3. The expected headers, libraries, and tools exist under `ctx.outputDir`.
4. Consumer metadata works in a real compile/link or package consumer.
5. Unsupported matrices fail through `filter`.
6. `onTest`, when present, passes after both a fresh build and a cache hit.
7. The repository's formula CI passes on every supported runner.

When validation fails, preserve the first concrete diagnostic, confirm the
failing source or execution path, and report the evidence before changing
behavior.
