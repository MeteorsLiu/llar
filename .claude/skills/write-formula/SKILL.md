---
name: write-formula
description: Write or update LLAR build formulas (.gox) and versions.json entries for library packages. Use when adding a package to llarhub, changing a build recipe, declaring formula dependencies or matrix dimensions, adding build tests, or fixing an existing LLAR formula.
---

# Write an LLAR Build Formula

Create or update the requested formula using the current LLAR Formula DSL.
Inspect the target package before choosing build flags, dependencies, output
metadata, or tests.

Do not copy a build recipe from another package. Similar build systems do not
imply compatible flags, targets, installed files, or metadata.

## Workflow

1. Before creating or editing any `.gox` file, read
   [XGo Classfile Guide](references/xgo-classfile.md) completely.
2. Inspect the package source at the requested upstream tag.
3. Identify its build system, dependencies, supported configuration, installed
   files, and consumer metadata from its own source files.
4. Inspect the current LLAR API and existing repository formulas.
5. Update only the required `versions.json`, `_llar.gox`, and optional
   `_cmp.gox` files.
6. Validate the formula with `llar make` using the exact upstream tag.

## References

Read source instead of inferring an API or copying a recipe:

- [XGo classfile syntax used by formulas](references/xgo-classfile.md)
- [Formula classfile API](https://github.com/goplus/llar/blob/main/formula/classfile.go)
- [Project and Context API](https://github.com/goplus/llar/blob/main/formula/project.go)
- [Autotools wrapper](https://github.com/goplus/llar/blob/main/x/autotools/autotools.go)
- [CMake wrapper](https://github.com/goplus/llar/blob/main/x/cmake/cmake.go)
- [LLAR formula fixtures](https://github.com/goplus/llar/tree/main/internal/build/testdata/formulas)
- [Existing formula in this repository](../../../madler/zlib/1.0.0/Zlib_llar.gox)
- [Existing versions file in this repository](../../../madler/zlib/versions.json)

Use an existing formula only to learn the LLAR DSL. Derive package-specific
flags and behavior from the target package itself.

## Repository Layout

```text
owner/repo/
  versions.json
  Repo_cmp.gox                 # optional
  from-version/Repo_llar.gox
```

The directory name is conventional. Formula selection uses the `fromVer`
string literal inside each `_llar.gox` file. When several formulas match, LLAR
selects the greatest `fromVer` not newer than the requested version.

Formula filenames must end in `_llar.gox`. Comparator filenames must end in
`_cmp.gox`.

## Formula DSL

A `_llar.gox` file is an XGo classfile backed by `formula.ModuleF`.

### Top-level declarations

| DSL | Go API | Responsibility |
| --- | --- | --- |
| `id "<owner>/<repo>"` | `ModuleF.Id(string)` | Set the module path. |
| `fromVer "<tag>"` | `ModuleF.FromVer(string)` | Set the oldest version served by the formula. The argument must be a string literal. |
| `defaults {"<key>": "<value>"}` | `ModuleF.Defaults(map[string]string)` | Set default option selections. |
| `filter => { ... }` | `ModuleF.Filter(func() bool)` | Accept or reject the effective matrix selection. |
| `onRequire (proj, deps) => { ... }` | `ModuleF.OnRequire(...)` | Discover direct dependencies. |
| `onBuild (ctx, proj, out) => { ... }` | `ModuleF.OnBuild(...)` | Build and install the module. |
| `onTest (ctx, proj, out) => { ... }` | `ModuleF.OnTest(...)` | Verify the root module after build. |

There is no `matrix` top-level declaration. Do not construct or pass a
`formula.Matrix` from formula code.

## Matrix

Matrix dimensions are declared by accessing selected values through the
`target` auto-property.

| DSL | Type | Meaning |
| --- | --- | --- |
| `target.require["<key>"]` | `[]string` | Read a required build dimension supplied by the client. |
| `target.options["<key>"]` | `[]string` | Read a package build option. |
| `defaults {"<key>": "<value>"}` | `map[string]string` | Provide a default for an option. |
| `filter => { ... }` | `func() bool` | Reject an unsupported effective selection. |

Rules:

- Use `target.require` for required dimensions such as platform properties.
- Use `target.options` for package configuration choices.
- Matrix values are slices. Import and use an appropriate standard-library
  helper when testing membership.
- `defaults` initializes options not overridden by the client.
- `filter` validates values after defaults and client selections are applied.
- LLAR discovers keys from lookups in `onRequire`, `onBuild`, and `onTest`.
- A lookup used only by `filter` does not declare a dimension. Read that key in
  at least one hook as well.
- Prefer literal keys and direct data flow.
- Do not range over a target matrix, store it globally, send it through a
  channel, convert it with reflection or `unsafe`, or pass it to an external or
  dynamic function. LLAR rejects matrix uses it cannot analyze safely.

Client selection syntax:

| Selection | Command form |
| --- | --- |
| Required dimension | `--<key> <value>` or `--require <key>=<value>` |
| Option | `--option <key>=<value>` |

Do not document or depend on the internal matrix string encoding. Formula code
must read selections through `target`.

## Hook APIs

### Project

| DSL | Go API | Responsibility |
| --- | --- | --- |
| `proj.readFile(path)` | `Project.ReadFile(string)` | Read a file. In `onRequire`, the filesystem is the upstream source at the selected tag. In build/test hooks, it is the formula module filesystem. |
| `proj.Deps` | `Project.Deps []module.Version` | Access resolved build dependencies. |

### ModuleDeps

| DSL | Go API | Responsibility |
| --- | --- | --- |
| `deps.require(path, version)` | `ModuleDeps.Require(string, string)` | Declare a direct dependency. |
| `deps.deps()` | `ModuleDeps.Deps()` | Read dependencies declared so far. |

### Context

| DSL | Go API | Responsibility |
| --- | --- | --- |
| `ctx.SourceDir` | `Context.SourceDir` | Locate the upstream source checkout. |
| `ctx.outputDir()` | `Context.OutputDir__0()` | Locate the current module install directory. |
| `ctx.outputDir(dep)` | `Context.OutputDir__1(module.Version)` | Locate a dependency install directory. |
| `ctx.buildResult(dep)` | `Context.BuildResult(module.Version)` | Read a dependency build result and presence boolean. |

There is no `ctx.currentMatrix()` method. Read selections through
`target.require` and `target.options`.

### BuildResult

| DSL | Go API | Responsibility |
| --- | --- | --- |
| `out.addErr(err)` | `BuildResult.AddErr(error)` | Record a build failure. |
| `out.setMetadata(value)` | `BuildResult.SetMetadata(string)` | Set consumer link or pkg-config-style metadata. |
| `out.metadata()` | `BuildResult.Metadata()` | Read stored metadata. |
| `out.errs()` | `BuildResult.Errs()` | Read recorded build errors. |

### TestResult

| DSL | Go API | Responsibility |
| --- | --- | --- |
| `out.addErr(err)` | `TestResult.AddErr(error)` | Record a test failure. |
| `out.errs()` | `TestResult.Errs()` | Read recorded test errors. |

`TestResult` has no metadata. `onTest` verifies the root build and does not
change its build result. Tests must also work when `onBuild` is skipped on a
cache hit, so do not depend on an `onBuild` scratch directory.

## versions.json and Dependencies

Every module needs a `versions.json` with:

| Field | Type | Meaning |
| --- | --- | --- |
| `path` | string | Module path. |
| `deps` | map from module version to dependency array | Pinned dependency fallback for each upstream version. |
| dependency `path` | string | Dependency module path. |
| dependency `version` | string | Upstream dependency tag or version. |

Dependency behavior:

- Use `onRequire` when dependencies are discovered from upstream source or
  depend on matrix selections.
- Dependencies returned by `onRequire` take precedence.
- If `onRequire` declares an empty dependency version, LLAR tries to fill it
  from the selected version's `versions.json` entry.
- If `onRequire` yields no usable dependencies, LLAR falls back to the selected
  version's `versions.json` entry.
- In `onBuild`, obtain dependency roots with `ctx.outputDir(dep)` and pass each
  root to the selected build tool's `use` method when required.

Do not hardcode a dependency merely because another package recipe uses it.
Confirm it from the target package and selected configuration.

## Build Tools

`autotools` and `cmake` are imported automatically in `_llar.gox` files.

### autotools

| DSL | Go API | Responsibility |
| --- | --- | --- |
| `autotools.new(source, build, install)` | `autotools.New(string, string, string)` | Create a builder. |
| `a.source(dir)` | `AutoTools.Source(string)` | Override the source directory. |
| `a.use(root)` | `AutoTools.Use(string)` | Add dependency include, library, and pkg-config paths. |
| `a.configure(args...)` | `AutoTools.Configure(...string)` | Run the source configure script in the build directory. |
| `a.build(args...)` | `AutoTools.Build(...string)` | Run `make`. |
| `a.install(args...)` | `AutoTools.Install(...string)` | Run `make install`. |
| `a.outputDir()` | `AutoTools.OutputDir()` | Read the configured install or build directory. |

### cmake

| DSL | Go API | Responsibility |
| --- | --- | --- |
| `cmake.new(source, build, install)` | `cmake.New(string, string, string)` | Create a builder. |
| `c.source(dir)` | `CMake.Source(string)` | Override the source directory. |
| `c.generator(name)` | `CMake.Generator(string)` | Select the CMake generator. |
| `c.buildType(value)` | `CMake.BuildType(string)` | Set `CMAKE_BUILD_TYPE`. |
| `c.toolchain(path)` | `CMake.Toolchain(string)` | Set `CMAKE_TOOLCHAIN_FILE`. |
| `c.define(key, value)` | `CMake.Define(string, string)` | Set a string cache definition. |
| `c.defineBool(key, value)` | `CMake.DefineBool(string, bool)` | Set a boolean cache definition. |
| `c.use(root)` | `CMake.Use(string)` | Add a dependency prefix. |
| `c.configure(args...)` | `CMake.Configure(...string)` | Configure the build tree. |
| `c.build(args...)` | `CMake.Build(...string)` | Build configured targets. |
| `c.install(args...)` | `CMake.Install(...string)` | Install configured targets. |
| `c.outputDir()` | `CMake.OutputDir()` | Read the configured install or build directory. |

Inspect the wrapper source before relying on additional behavior. Check every
returned error and record failures with the hook result's `addErr` method.

## Shell Surface

`formula.ModuleF` embeds `gsh.App`.

| DSL | Go API | Responsibility |
| --- | --- | --- |
| `exec(commandLine)` | `gsh.App.Exec__1(string)` | Run a shell command line. |
| `exec(name, args...)` | `gsh.App.Exec__2(string, ...string)` | Run a command with explicit arguments. |
| `capout => { ... }` | `gsh.App.Capout(func())` | Capture stdout from a block. |
| `output` | `gsh.App.Output()` | Read the last captured output. |
| `lastErr` | `gsh.App.LastErr()` | Read the last command error. |
| `exitCode()` | `gsh.App.ExitCode()` | Read the last command exit code. |

Use `capout` before reading `output`. Record `lastErr` on the active
`BuildResult` or `TestResult`.

## Version Comparator

Without a `_cmp.gox`, LLAR uses GNU/Debian-style comparison. Add a comparator
only when the upstream tag format requires different ordering.

| DSL | Go API | Responsibility |
| --- | --- | --- |
| `compareVer (a, b) => { ... }` | `CmpApp.CompareVer(func(module.Version, module.Version) int)` | Compare two versions. |

`semver` and `gnu` are imported automatically in `_cmp.gox` files. Read
existing comparator files before adding one.

## XGo Syntax

Read [XGo Classfile Guide](references/xgo-classfile.md) before editing a `.gox`
file. Keep detailed XGo and classfile syntax in that reference rather than
duplicating it here. Do not use undocumented syntax; prefer forms present in
current LLAR source fixtures.

## Validation

Run from the llarhub repository root:

```text
llar make -v ./<owner>/<repo>@<exact-upstream-tag> [matrix flags]
```

Verify all of the following:

1. The formula parses and the exact upstream tag exists.
2. Headers, libraries, tools, and metadata are installed under
   `ctx.outputDir()` as intended by the package.
3. Dependency roots come from `ctx.outputDir(dep)`.
4. Consumer metadata matches the installed output.
5. Every declared matrix selection is exercised, defaults are applied, and
   unsupported selections are rejected by `filter`.
6. `onTest`, when present, succeeds both after a fresh build and a cache hit.
