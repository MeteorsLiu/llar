# Cross Compile And Default Sysroot Design

## Goal

LLAR should support hidden cross-platform C/C++ builds without requiring ordinary formula authors to know about compiler toolchains or sysroot wiring. The build matrix selects the target platform. LLAR supplies the default Linux sysroot and the cross compiler automatically, while still allowing a formula to explicitly override the sysroot when a package needs a special one.

This design is scoped to narrow cross compilation:

- managed LLVM compiler tools;
- compiler/binutils/target injection;
- default Linux `glibc` sysroot selection;
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

The sysroot is not owned by `crosscompile`. For Linux MVP, the default sysroot comes from the official package named `glibc`. Other target operating systems must not reuse the Linux `glibc` rule.

## High-Level Flow

For a Linux target matrix such as:

```text
arm64-linux
```

LLAR does the following:

1. Parse the matrix as target `os=linux`, `arch=arm64`.
2. Inject a hidden default dependency on `glibc@<default>`, unless dependency resolution already selects an explicit `glibc` version.
3. Build `glibc` using the same matrix. The `glibc` formula produces a Linux arm64 sysroot in its output directory.
4. Build downstream packages with the selected `glibc` output directory as the default sysroot.
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
c.Sysroot(customRoot)
```

Explicit sysroot wins over the default `glibc` sysroot.

## Matrix Semantics

The matrix selects target platform only:

```text
arm64-linux -> os=linux, arch=arm64
arm64       -> arch=arm64, no os
```

Rules:

- `os=linux`: inject default `glibc` and use its output directory as default sysroot.
- `os=darwin`: do not inject `glibc`. Darwin/macOS targets need an Apple SDK-style sysroot, which is a separate policy and not part of the Linux `glibc` default.
- no `os`: do not inject `glibc` and do not use a default sysroot. This covers freestanding and embedded targets.
- Matrix does not encode libc version.
- Matrix does not encode user toolchain choice.

For MVP, target triple is derived from matrix `os/arch` for Linux targets:

```text
arm64-linux -> aarch64-linux-gnu
amd64-linux -> x86_64-linux-gnu
```

## Default `glibc`

`glibc` is the official default Linux sysroot package name. It is intentionally written as `glibc`, not as `goplus/glibc` or another namespaced module path.

This rule applies only to Linux targets. Darwin/macOS targets, embedded targets, and targets without an `os` field do not receive a hidden `glibc` dependency.

Implementation must account for the current module path rules if they require namespaced paths. The design requirement is that the official default sysroot package is addressed as `glibc`.

Dependency rules:

- Linux targets receive a hidden default dependency on `glibc@<default>`.
- If formulas explicitly require `glibc@x`, the explicit requirement participates in dependency/version selection with the default requirement.
- The selected `glibc` version becomes part of the downstream package build variant and cache key.
- If the target OS is not Linux, or if no `os` exists in the matrix, no hidden `glibc` dependency is injected.

The `glibc` package can be implemented as a normal formula that uses `ctx.currentMatrix()` to produce the target sysroot for that matrix. MVP can use prebuilt sysroot assets rather than building glibc from source.

## Sysroot Selection

Sysroot priority:

```text
1. helper explicit Sysroot(root)
2. target-OS default sysroot, such as selected `glibc` output directory for Linux
3. no sysroot
```

`x/cmake` and `x/autotools` should expose:

```go
func (c *CMake) Sysroot(root string)
func (a *AutoTools) Sysroot(root string)
```

These APIs are overrides, not the ordinary path. Most formulas should not call them.

The build layer owns the default sysroot context because it knows the selected target-OS default sysroot, such as the selected `glibc` output directory for Linux. Helpers can own explicit override state because the formula called `Sysroot(root)` on that helper instance.

Explicit override mechanics:

- `CMake.Sysroot(root)` appends an explicit `CMAKE_SYSROOT` setting during configure. Build middleware must not add the default `glibc` sysroot when the CMake configure command already carries an explicit `CMAKE_SYSROOT`.
- `AutoTools.Sysroot(root)` applies explicit sysroot environment after `execbroker.Command` returns, so helper-local values override default middleware values on the final `exec.Cmd`.
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

`crosscompile` returns compiler/binutils/target patch. `internal/build` supplies the default sysroot patch. `x/cmake` and `x/autotools` explicit `Sysroot(root)` overrides must win over the default sysroot.

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

If an active sysroot exists, CMake also receives:

```cmake
set(CMAKE_SYSROOT <sysroot>)
```

If the formula explicitly sets `CMAKE_TOOLCHAIN_FILE`, LLAR must not overwrite it. In that case MVP may still append `CMAKE_SYSROOT` when an active sysroot exists, but compiler/toolchain correctness is the formula's responsibility because LLAR cannot merge its managed LLVM settings into an arbitrary user toolchain file.

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

If an active sysroot exists, Autotools/compiler environment also receives:

```text
CPPFLAGS += --sysroot=<sysroot>
CFLAGS   += --sysroot=<sysroot>
CXXFLAGS += --sysroot=<sysroot>
LDFLAGS  += --sysroot=<sysroot>
PKG_CONFIG_SYSROOT_DIR=<sysroot>
PKG_CONFIG_LIBDIR=<sysroot>/usr/lib/pkgconfig:<sysroot>/usr/share/pkgconfig:<sysroot>/lib/pkgconfig
```

### Direct Compiler Commands

For direct compiler/binutils commands:

```text
cc, gcc  -> managed clang + --target=<triple> [+ --sysroot=<sysroot>]
c++, g++ -> managed clang++ + --target=<triple> [+ --sysroot=<sysroot>]
ar       -> managed llvm-ar
ranlib   -> managed llvm-ranlib
strip    -> managed llvm-strip
```

## Cache Variant

Package build cache keys must include more than `version-matrix` when hidden sysroot/toolchain inputs exist.

At minimum, cache variant must include:

- matrix;
- selected `glibc` version when a default or explicit `glibc` sysroot is active;
- managed LLVM toolchain identity/version/digest when cross compilation is active.

This prevents reusing artifacts built with a previous default `glibc` or previous managed LLVM.

## Error Handling

Failures before formula execution:

- unsupported matrix target for cross compilation;
- unsupported target triple;
- managed LLVM manifest missing current build platform;
- download failure;
- checksum failure;
- extraction/cache failure;
- Linux target cannot resolve/build selected `glibc`;
- Linux downstream build has no selected `glibc` output directory when default sysroot is expected.
- Darwin/macOS targets are not covered by the Linux default sysroot rule; if they need a sysroot in MVP, formula code must use explicit `Sysroot(root)` or the build must fail with a target-OS-specific unsupported message.

Errors should mention the matrix, build platform, target platform, and selected toolchain or `glibc` version when relevant.

## Testing

Required coverage:

- Linux matrix injects hidden `glibc`.
- Darwin/macOS matrix does not inject hidden `glibc`.
- Matrix without `os` does not inject `glibc`.
- Explicit `glibc` requirement participates in version selection with the default.
- Selected `glibc` output directory becomes the default sysroot.
- Explicit `CMake.Sysroot`/`AutoTools.Sysroot` overrides default sysroot.
- Cache variant changes when selected `glibc` version changes.
- Cache variant changes when managed LLVM identity changes.
- CMake injection includes compiler target and `CMAKE_SYSROOT`.
- Autotools injection includes `--host`, compiler target flags, and sysroot flags.
- Direct compiler commands receive managed compiler and target flags.
- `internal/build/crosscompile` does not import `internal/execbroker`.
