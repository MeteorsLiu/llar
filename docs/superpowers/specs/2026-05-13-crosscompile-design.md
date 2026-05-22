# Cross Compile And Default Sysroot Design

## Goal

LLAR should support hidden cross-platform C/C++ builds without requiring ordinary formula authors to know about compiler toolchains or sysroot wiring. The build matrix selects the target platform. Native builds use the system libc or SDK normally. Cross builds receive the target sysroot and cross compiler automatically when LLAR has a target-OS policy for them, while still allowing a formula to explicitly override the sysroot when a package needs a special one.

This design is scoped to narrow cross compilation:

- managed LLVM compiler tools;
- compiler/binutils/target injection;
- cross-Linux default `glibc` sysroot selection;
- CMake and Autotools command injection.

It does not cover broad build tools such as Conan-style `tool_requires`, framework SDKs such as ESP-IDF, or user-specified custom toolchains. The internal cross compiler selector should leave room for custom toolchains later, but MVP only implements LLAR-managed LLVM.

## Definitions

**Toolchain** means build-platform executables and target-selection parameters used to generate target-platform binaries:

- `clang`, `clang++`;
- `llvm-ar`, `llvm-ranlib`, `llvm-strip`, `ld.lld`;
- target triple and compiler target flags;
- CMake and Autotools projections of those compiler settings.

**Sysroot** means a target-system root used by the compiler and build system:

- libc/system headers;
- startup objects such as `crt1.o`, `crti.o`, `crtn.o`;
- libc and related system libraries;
- target pkg-config roots.

The sysroot is not owned by `crosscompile`. Native builds use the system libc or SDK. For cross-Linux MVP, the default sysroot comes from the official package named `glibc`. Other target operating systems must not reuse the Linux `glibc` rule.

## High-Level Flow

For a cross-Linux target matrix such as:

```text
arm64-linux
```

when the build platform is not Linux arm64, LLAR does the following:

1. Parse the matrix as target `os=linux`, `arch=arm64`.
2. Inject a hidden default dependency on `glibc@<default>`, unless dependency resolution already selects an explicit `glibc` version.
3. Build `glibc` using the same matrix. The `glibc` formula produces a Linux arm64 sysroot and records C/C++ flags in `BuildResult.Metadata`, for example `--sysroot=<glibc-output-dir>`.
4. Parse the selected `glibc` metadata with `metadata/cc` and use the parsed sysroot directory as the default sysroot.
5. Download/cache the LLAR-managed LLVM toolchain for the build platform.
6. Install `execbroker` middleware while formula build hooks run.
7. Patch CMake/Autotools/compiler commands with both compiler settings and the active sysroot.

Ordinary formula code stays unchanged:

```go
c := cmake.new(ctx.SourceDir, ctx.SourceDir+"/_build", installDir)
c.configure()
c.build()
c.install()
```

If a package needs a special sysroot, it can override the default:

```go
c.Sysroot(customSysrootDir)
```

Explicit sysroot directories win over the default sysroot parsed from selected `glibc` metadata.

## Matrix Semantics

The matrix selects target platform only:

```text
arm64-linux -> os=linux, arch=arm64
arm64       -> arch=arm64, no os
```

The matrix does not encode libc version or user toolchain choice.

Native build rule:

- if build platform equals target platform, LLAR does not inject a default sysroot package. The system libc or SDK is used by the host toolchain as usual.

Cross build rules:

- `target os=linux`: inject default `glibc` and use the sysroot directory parsed from its build metadata as the default sysroot.
- `target os=darwin`: do not inject `glibc`. MVP does not auto-download an Apple SDK. A future Apple SDK locator package/provider can resolve an SDK path from user-authorized Xcode/Command Line Tools downloads, a predownloaded cache, or local configuration.
- no target `os`: do not inject `glibc` and do not use a default sysroot. This covers freestanding and embedded targets.

For MVP, target triple is derived from matrix `os/arch` for Linux targets:

```text
arm64-linux -> aarch64-linux-gnu
amd64-linux -> x86_64-linux-gnu
```

## Default `glibc`

`glibc` is the official default cross-Linux sysroot package name. It is intentionally written as `glibc`, not as `goplus/glibc` or another namespaced module path.

This rule applies only to cross-Linux targets. Native Linux builds use the system libc. Darwin/macOS targets, embedded targets, and targets without an `os` field do not receive a hidden `glibc` dependency.

Implementation must account for the current source repository rules if they require namespaced paths. The design requirement is that the official default sysroot package is addressed as `glibc`, while its default source repository resolves to `github.com/goplus/glibc`. In MVP/testing, this repository can be temporarily substituted by the configured source repository provider, but formulas and dependency resolution still see the package as `glibc`.

Dependency rules:

- Cross-Linux targets receive a hidden default dependency on `glibc@<default>`.
- If formulas explicitly require `glibc@x`, the explicit requirement participates in dependency/version selection with the default requirement.
- The selected `glibc` version becomes part of the downstream package build variant and cache key.
- If the build is native, if the target OS is not Linux, or if no `os` exists in the matrix, no hidden `glibc` dependency is injected.

The `glibc` package can be implemented as a normal formula that uses `ctx.currentMatrix()` to produce the target sysroot for that matrix. MVP can use prebuilt sysroot assets rather than building glibc from source. Its `BuildResult.Metadata` remains a raw C/C++ flags string and must contain a parseable sysroot flag, such as `--sysroot=<output-dir>`. Downstream helpers must consume the parsed sysroot directory, not pass raw metadata strings as the sysroot override API.

## C/C++ Metadata

Add:

```text
metadata/cc
```

`metadata/cc` owns LLAR's interpretation of C/C++ raw metadata strings. It does not own toolchain selection, dependency resolution, build helper workflows, or command execution.

Initial public shape:

```go
package cc

type Metadata struct {
	CCFLAGS []string
	CFLAGS  []string
	LDFLAGS []string
	// sysroot storage is unexported
}

func Parse(raw string) (Metadata, error)
func (m Metadata) Sysroot() string
```

`BuildResult.Metadata` remains a string because existing formulas already emit raw C/C++ flags such as `-lz`, `-L<dir>`, and `--sysroot=<dir>`. `metadata/cc.Parse` converts that string into the minimal structured form helpers need.

MVP parsing rules:

- split raw metadata with a mature shell-like splitter rather than ad hoc whitespace splitting;
- recognize `--sysroot=<dir>`, `--sysroot <dir>`, and `-isysroot <dir>`;
- if multiple sysroot flags exist, the last one wins, matching normal compiler flag override behavior;
- omit recognized sysroot flags from `CCFLAGS`, `CFLAGS`, and `LDFLAGS`;
- classify obvious linker flags such as `-L...`, `-l...`, and `-Wl,...` into `LDFLAGS`;
- put other flags into `CCFLAGS`;
- keep `CFLAGS` available for future C-only metadata, but MVP does not invent C-only classification without an explicit metadata convention;
- return an error for malformed sysroot flags that require a following path but do not have one.

`Sysroot()` returns the parsed directory only. It must not return `--sysroot=<dir>` or `-isysroot <dir>`.

## Sysroot Selection

Sysroot priority:

```text
1. helper explicit Sysroot(dir)
2. target-OS default sysroot dir parsed from selected `glibc` BuildResult.Metadata for cross-Linux
3. no sysroot
```

`x/cmake` and `x/autotools` should expose:

```go
func (c *CMake) Sysroot(dir string)
func (a *AutoTools) Sysroot(dir string)
```

These APIs are overrides, not the ordinary path. Most formulas should not call them.

The build layer owns the default sysroot context because it knows the selected target-OS default metadata, such as the selected `glibc` build metadata for cross-Linux, and can parse the sysroot directory through `metadata/cc`. Helpers can own explicit override state because the formula called `Sysroot(dir)` on that helper instance.

Explicit override mechanics:

- `CMake.Sysroot(dir)` applies an explicit sysroot directory to configure/build command environments. Build middleware must not add the default `glibc` sysroot when helper-local sysroot is present.
- `AutoTools.Sysroot(dir)` applies an explicit sysroot directory after `execbroker.Command` returns, so helper-local values override default middleware values on the final `exec.Cmd`.
- Direct `exec.Command` calls have no helper-local override. They receive only the build default sysroot, if one is active.

## Crosscompile Module

Add or update:

```text
internal/build/crosscompile
```

Responsibilities:

- Parse the active matrix as target platform input.
- Compare target platform with current build platform.
- Download/cache the LLAR-managed LLVM toolchain for the build platform when cross compilation is required.
- Derive the target triple from matrix `os/arch`.
- Inspect a command and return a neutral compiler/binutils patch.

Non-responsibilities:

- It does not own or select `glibc`.
- It does not own sysroot paths or sysroot versions.
- It does not resolve package dependency graphs.
- It does not import `internal/execbroker`.
- It does not expose formula APIs.
- It does not implement user custom toolchains in MVP.

Public shape should stay small:

```go
package crosscompile

type Command struct {
	Name string
	Args []string
	Env  []string
	Dir  string
}

type Patch struct {
	Name       string
	PrependArg []string
	AppendArg  []string
	SetEnv     map[string]string
	PrependEnv map[string][]string
}

type CrossCompile struct {
	// unexported
}

func New(matrix string) (*CrossCompile, error)

func (c *CrossCompile) Use(cmd Command) Patch
```

`New(matrix)` may fail while preparing the managed LLVM toolchain or resolving the target triple. `Use(command)` must not fail because `execbroker.Middleware` cannot return errors.

## Command Injection

`internal/build` owns middleware lifecycle:

```go
cc, err := crosscompile.New(matrix)
if err != nil {
	return err
}

restore := execbroker.SetMiddleware(func(req execbroker.Request) execbroker.Request {
	patch := cc.Use(crosscompile.Command{
		Name: req.Name,
		Args: req.Args,
		Env:  req.Env,
	})
	patch = mergePatch(patch, activeSysrootPatch(req))
	return applyPatch(req, patch)
})
defer restore()
```

`crosscompile` returns compiler/binutils/target patch. `internal/build` supplies the default sysroot patch derived from parsed metadata. `x/cmake` and `x/autotools` explicit `Sysroot(dir)` overrides must win over the default sysroot.

### CMake

For CMake configure commands, LLAR injects a generated toolchain file unless the user already supplied one.

The generated file contains compiler settings from `crosscompile`:

```cmake
set(CMAKE_SYSTEM_NAME Linux)
set(CMAKE_SYSTEM_PROCESSOR aarch64)
set(CMAKE_C_COMPILER <managed-clang>)
set(CMAKE_CXX_COMPILER <managed-clang++>)
set(CMAKE_C_COMPILER_TARGET aarch64-linux-gnu)
set(CMAKE_CXX_COMPILER_TARGET aarch64-linux-gnu)
set(CMAKE_AR <managed-llvm-ar>)
set(CMAKE_RANLIB <managed-llvm-ranlib>)
set(CMAKE_STRIP <managed-llvm-strip>)
```

If an active sysroot directory exists, CMake configure/build commands receive that sysroot through compile and link flag environment:

```text
CFLAGS   += --sysroot=<dir>
CXXFLAGS += --sysroot=<dir>
LDFLAGS  += --sysroot=<dir>
```

For example, `glibc` can emit `--sysroot=<glibc-output-dir>` as raw metadata, `metadata/cc.Parse` returns `<glibc-output-dir>` from `Sysroot()`, and CMake injection converts the directory back to the platform-appropriate sysroot flag. MVP does not need to map this into `CMAKE_SYSROOT`; a later refinement can do that if CMake projects need more structured sysroot handling.

If the formula explicitly sets `CMAKE_TOOLCHAIN_FILE`, LLAR must not overwrite it. Active sysroot directories can still flow through flags, but compiler/toolchain correctness is the formula's responsibility because LLAR cannot merge its managed LLVM settings into an arbitrary user toolchain file.

Because current LLAR source directories are temporary per build, MVP does not add extra CMake build-directory isolation. If LLAR later introduces stable source caching, CMake build directories must include a matrix/toolchain/sysroot variant to avoid stale `CMakeCache.txt`.

### Autotools

For configure commands, LLAR injects compiler tools and target flags:

```text
CC=<managed-clang>
CXX=<managed-clang++>
AR=<managed-llvm-ar>
RANLIB=<managed-llvm-ranlib>
STRIP=<managed-llvm-strip>
CFLAGS += --target=<triple>
CXXFLAGS += --target=<triple>
--host=<triple>
--build=<build-triple>
```

If an active sysroot directory exists, Autotools/compiler environment also receives:

```text
CPPFLAGS += --sysroot=<dir>
CFLAGS   += --sysroot=<dir>
CXXFLAGS += --sysroot=<dir>
LDFLAGS  += --sysroot=<dir>
```

### Direct Compiler Commands

For direct compiler/binutils commands:

```text
cc, gcc  -> managed clang + --target=<triple> [+ active sysroot dir converted to flags]
c++, g++ -> managed clang++ + --target=<triple> [+ active sysroot dir converted to flags]
ar       -> managed llvm-ar
ranlib   -> managed llvm-ranlib
strip    -> managed llvm-strip
```

## Cache Variant

Package build cache keys must include more than `version-matrix` when hidden sysroot/toolchain inputs exist.

At minimum, cache variant must include:

- matrix;
- selected `glibc` version when a cross-Linux default or explicit `glibc` sysroot is active;
- managed LLVM toolchain identity/version/digest when cross compilation is active.

This prevents reusing artifacts built with a previous cross-Linux default `glibc` or previous managed LLVM.

## Error Handling

Failures before formula execution:

- unsupported matrix target for cross compilation;
- unsupported target triple;
- managed LLVM manifest missing current build platform;
- download failure;
- checksum failure;
- extraction/cache failure;
- cross-Linux target cannot resolve/build selected `glibc`;
- cross-Linux downstream build has no selected `glibc` metadata when a default sysroot is expected.
- selected default sysroot metadata cannot be parsed by `metadata/cc`.
- Darwin/macOS targets are not covered by the Linux default sysroot rule; if they need a sysroot in MVP, formula code must use explicit `Sysroot(dir)` or the build must fail with a target-OS-specific unsupported message.

Errors should mention the matrix, build platform, target platform, and selected toolchain or `glibc` version when relevant.

## Testing

Required coverage:

- cross-Linux matrix injects hidden `glibc`.
- Darwin/macOS matrix does not inject hidden `glibc`.
- Matrix without `os` does not inject `glibc`.
- Explicit `glibc` requirement participates in version selection with the default.
- `metadata/cc.Parse` classifies raw C/C++ metadata flags and returns sysroot directories from supported sysroot flags.
- Selected `glibc` metadata parses into the default sysroot directory.
- Explicit `CMake.Sysroot`/`AutoTools.Sysroot` directory overrides default sysroot.
- Cache variant changes when selected `glibc` version changes.
- Cache variant changes when managed LLVM identity changes.
- CMake injection includes compiler target and sysroot flags derived from the active sysroot directory.
- Autotools injection includes `--host`, compiler target flags, and sysroot flags derived from the active sysroot directory.
- Direct compiler commands receive managed compiler and target flags.
- `internal/build/crosscompile` does not import `internal/execbroker`.
