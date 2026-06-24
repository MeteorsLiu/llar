LLAR Formula
====

A formula is a `_llar.gox` file. It declares how one module discovers direct
dependencies, builds source, emits metadata, selects matrix variants, and tests
installed output.

## Formula Classfile

`_llar.gox` is an XGo classfile for the LLAR formula class framework. The
framework abstracts LLAR formula knowledge into a formula surface used directly
inside the file.

| Surface | Purpose |
|---|---|
| `id` | declare the module path served by this formula |
| `fromVer` | declare the first module version served by this formula |
| `target` | declare and read the build matrix |
| `default` | set default option selections |
| `filter` | reject invalid matrix selections |
| `onRequire` | register the callback LLAR runs to find direct deps |
| `onBuild` | register the callback LLAR runs to build and install |
| `onTest` | register the callback LLAR runs to test installed output |

The formula surface is LLAR's domain knowledge exposed to formula authors.
The concept is the build matrix; the formula name is `target`. Callbacks and
filters read the active build matrix through `target`.

The `on*` entries register callbacks. LLAR calls them at different phases:
`onRequire` while resolving direct dependencies, `onBuild` when a module must
be built, and `onTest` when the installed output should be checked.

## Shape

```coffee
import "slices"

id "pnggroup/libpng"  # module id

fromVer "1.0.0"  # run formula from this version

default {
    "zlib": "OFF",
}

onRequire (proj, deps) => {  # register dependency discovery
    if slices.contains(target.options["zlib"], "ON") {
        deps.require "madler/zlib", "v1.3.1"
    }
}

onBuild (ctx, proj, out) => {  # register build steps
    ...
}

onTest (ctx, proj, out) => {  # register installed-output test
    ...
}
```

`id` is the module path served by this formula.

`fromVer` is the first module version served by this formula file. If a later
formula for the same module has a higher `fromVer`, that later formula takes
over from its version.

## Matrix Selection

Formulae use `target` for the build matrix. They read the active matrix
selection where it affects deps or build behavior.

```coffee
import "slices"

default {
    "debug": "OFF",
    "zlib": "OFF",
}

onRequire (proj, deps) => {
    if slices.contains(target.options["zlib"], "ON") {
        deps.require "madler/zlib", "v1.3.1"
    }
}

onBuild (ctx, proj, out) => {
    if slices.contains(target.require["os"], "linux") {
        ...
    }
    if slices.contains(target.options["debug"], "ON") {
        ...
    }
}
```

The build matrix space and the active target selection are different.

```go
type Matrix struct {
    Require        map[string][]string
    Options        map[string][]string
    DefaultOptions map[string][]string
}
```

| Name | Meaning |
|---|---|
| `Matrix` | Describes possible build matrix values. |
| `target` | Exposes the selected values for one formula invocation. |
| `target.require` | `map[string][]string`. Required dimensions such as `os`, `arch`, libc, ABI, or toolchain. Required values propagate through dependency resolution. Dependencies may ignore keys they do not read, but must not select conflicting values. |
| `target.options` | `map[string][]string`. Option selections for the current module, such as `debug`, `shared`, `zlib`, or `tests`. Options are not global requirements. A dependency may read the same option key if it supports that option; otherwise the option has no effect on that dependency. |

Use `slices.contains` to test whether a value is selected.

`default {}` sets default selected values for `target.options` only. It is a
prebuild hint, not a schema. It does not declare legal values and does not set
`target.require`.

Use `filter` to remove invalid selections:

```coffee
filter => {
    if slices.contains(target.require["os"], "darwin") &&
        slices.contains(target.require["arch"], "mips") {
        return false
    }
    return true
}
```

LLAR tracks the matrix keys actually read by the formula. Unread keys are not
part of that formula's matrix semantics.

## Discover Dependencies

`onRequire` keeps dependency maintenance small: read upstream build files,
translate dependency names to LLAR module ids, and declare direct deps. LLAR
resolves transitives outside the formula.

```coffee
func pkgID(name string) string {
    switch name {
    case "zlib":
        return "madler/zlib"
    case "libpng":
        return "pnggroup/libpng"
    }
    return ""
}

onRequire (proj, deps) => {
    cmake, err := proj.readFile("CMakeLists.txt")
    if err != nil {
        return
    }

    # find_package(zlib REQUIRED) -> {name: "zlib", version: ""}
    matches := findDeps(cmake)

    for m in matches {
        id := pkgID(m.Name)
        if id == "" {
            continue
        }
        deps.require id, m.Version
    }
}
```

Types used here:

| Name | Type |
|---|---|
| `proj` | `*Project` |
| `proj.readFile(path)` | `func(string) ([]byte, error)` |
| `deps` | `*ModuleDeps` |
| `deps.require(path, version)` | `func(string, string)` |

For Meson, read `meson.build`, then read `.wrap` when the version is pinned
there:

```coffee
onRequire (proj, deps) => {
    meson, err := proj.readFile("meson.build")
    if err != nil {
        return
    }

    matches := findDeps(meson)
    for m in matches {
        if m.Version == "" {
            wrap, err := proj.readFile("subprojects/" + m.Name + ".wrap")
            if err != nil {
                continue
            }
            m.Version = findWrapVersion(wrap)
        }

        id := pkgID(m.Name)
        if id == "" {
            continue
        }
        deps.require id, m.Version
    }
}
```

`findDeps`, `findWrapVersion`, and `pkgID` are formula-local helpers. LLAR does
not guess how an upstream name maps to a module id.

## Build With CMake

`onBuild` compiles and installs the project. `cmake.CMake` wraps CMake
configure, build, and install commands.

```coffee
id "madler/zlib"

fromVer "1.0.0"

onBuild (ctx, proj, out) => {
    installDir, err := ctx.outputDir()
    if err != nil {
        out.addErr err
        return
    }

    c := cmake.new(ctx.SourceDir, ctx.SourceDir+"/_build", installDir)
    c.buildType "Release"
    c.define "CMAKE_POLICY_VERSION_MINIMUM", "3.5"

    err = c.configure()
    if err != nil {
        out.addErr err
        return
    }
    err = c.build()
    if err != nil {
        out.addErr err
        return
    }
    err = c.install()
    if err != nil {
        out.addErr err
        return
    }

    out.setMetadata "-lz"
}
```

Types used here:

| Name | Type |
|---|---|
| `ctx` | `*Context` |
| `ctx.SourceDir` | `string` |
| `ctx.outputDir()` | `func() (string, error)` |
| `out` | `*BuildResult` |
| `out.addErr(err)` | `func(error)` |
| `out.setMetadata(metadata)` | `func(string)` |
| `cmake.new(sourceDir, buildDir, installDir)` | `func(string, string, string) *cmake.CMake` |

Common CMake methods:

| Method | Type |
|---|---|
| `c.use(root)` | `func(string)` |
| `c.buildType(name)` | `func(string)` |
| `c.define(key, value)` | `func(string, string)` |
| `c.defineBool(key, value)` | `func(string, bool)` |
| `c.configure(args...)` | `func(...string) error` |
| `c.build(args...)` | `func(...string) error` |
| `c.install(args...)` | `func(...string) error` |

`c.use(root)` adds include, library, pkg-config, and CMake search paths for a
dependency installed under `root`.

## Build With Autotools

`autotools.AutoTools` wraps `configure`, `make`, and `make install`.

```coffee
id "pnggroup/libpng"

fromVer "1.0.0"

onRequire (proj, deps) => {
    deps.require "madler/zlib", "v1.2.11"
}

onBuild (ctx, proj, out) => {
    installDir, err := ctx.outputDir()
    if err != nil {
        out.addErr err
        return
    }

    a := autotools.new(ctx.SourceDir, ctx.SourceDir+"/_build", installDir)

    for _, dep := range proj.Deps {
        depDir, err := ctx.outputDir(dep)
        if err != nil {
            out.addErr err
            return
        }
        a.use depDir
    }

    err = a.configure()
    if err != nil {
        out.addErr err
        return
    }
    err = a.build()
    if err != nil {
        out.addErr err
        return
    }
    err = a.install()
    if err != nil {
        out.addErr err
        return
    }

    out.setMetadata "-lpng"
}
```

Types used here:

| Name | Type |
|---|---|
| `proj.Deps` | `[]module.Version` |
| `ctx.outputDir(dep)` | `func(module.Version) (string, error)` |
| `autotools.new(sourceDir, buildDir, installDir)` | `func(string, string, string) *autotools.AutoTools` |
| `module.Version` | `struct { Path string; Version string }` |

Common Autotools methods:

| Method | Type |
|---|---|
| `a.use(root)` | `func(string)` |
| `a.configure(args...)` | `func(...string) error` |
| `a.build(args...)` | `func(...string) error` |
| `a.install(args...)` | `func(...string) error` |

`a.configure()` passes `--prefix=<installDir>` when `installDir` is set.

## Metadata

Metadata is package usage information. It is not the dependency list.

```coffee
out.setMetadata "-lz"
out.setMetadata "-lpng"
```

or:

```coffee
c.use installDir
capout => {
    exec "pkg-config", "--libs", "freetype2"
}
out.setMetadata output
```

Shell values used here:

| Name | Type |
|---|---|
| `exec name, args...` | `func(string, ...string) error` |
| `capout => { ... }` | `func(func()) (string, error)` |
| `output` | `string` |
| `lastErr` | `error` |

## Test

`onTest` runs after `onBuild` succeeds. It verifies the installed output and
does not emit metadata.

```coffee
onTest (ctx, proj, out) => {
    installDir, err := ctx.outputDir()
    if err != nil {
        out.addErr err
        return
    }

    testSrc := ctx.SourceDir + "/tests"
    testBuild := ctx.SourceDir + "/_testbuild"

    tc := cmake.new(testSrc, testBuild, testBuild+"/_out")
    tc.buildType "Release"
    tc.define "CMAKE_POLICY_VERSION_MINIMUM", "3.5"
    tc.use installDir

    err = tc.configure()
    if err != nil {
        out.addErr err
        return
    }
    err = tc.build()
    if err != nil {
        out.addErr err
        return
    }

    exec testBuild + "/cmtadd_check"
    if lastErr != nil {
        out.addErr lastErr
    }
}
```

Types used here:

| Name | Type |
|---|---|
| `onTest` | `func(*Context, *Project, *TestResult)` |
| `out` | `*TestResult` |
| `out.addErr(err)` | `func(error)` |
