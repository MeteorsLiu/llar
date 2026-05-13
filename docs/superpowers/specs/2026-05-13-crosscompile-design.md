# Cross Compile Command Injection Design

## Goal

LLAR should support hidden cross-platform C/C++ builds without requiring formula authors to know that a cross compiler exists. The active matrix is the only target-platform input. When the matrix target differs from the build machine, LLAR prepares a cross compile context and uses command middleware to patch relevant build commands.

This design is intentionally scoped to narrow cross compilation: target compiler, binutils, sysroot, pkg-config, CMake toolchain injection, and Autotools host/build injection. Conan-style `tool_requires` and other build-context tool packages are a separate future feature and are not part of this module.

## Module Boundary

Add an internal build module:

```text
internal/build/crosscompile
```

Responsibilities:

- Parse the active matrix as the target platform input.
- Compare the target platform with the current build platform.
- Prepare cross compiler/binutils/sysroot/pkg-config information when cross compilation is needed.
- Inspect a command and return a neutral patch that should be applied to that command.

Non-responsibilities:

- It does not execute commands.
- It does not import or expose `internal/execbroker`.
- It does not expose any formula API.
- It does not resolve package dependency graphs.
- It does not manage future broad build tools such as Conan-style `tool_requires`.

Dependency direction:

```text
internal/build -> internal/build/crosscompile
internal/build -> internal/execbroker
internal/build/crosscompile does not depend on internal/build
internal/build/crosscompile does not depend on internal/execbroker
```

## Public Shape

The module API should stay small:

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
	// unexported fields
}

func New(matrix string) (*CrossCompile, error)

func (c *CrossCompile) Use(cmd Command) Patch
```

`New(matrix)` is the only fallible step. It parses the matrix, determines build and target platforms, and performs any required tool discovery. For native builds it returns a valid object whose `Use` method returns a zero patch.

`Use(command)` is non-fallible. It classifies the command internally and returns the patch needed for cross compilation. A zero `Patch` means passthrough.

Command roles such as CMake configure, Autotools configure, compiler, archiver, and pkg-config are internal implementation details. They are not exposed in the API.

## Build Integration

`internal/build` owns lifecycle and middleware installation:

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
	return applyCrossCompilePatch(req, patch)
})
defer restore()
```

`applyCrossCompilePatch` lives in `internal/build` because it converts the neutral `crosscompile.Patch` into an `execbroker.Request`. This keeps `crosscompile` independent from the command broker.

## Matrix Semantics

The matrix is the only target-platform input for cross compilation. MVP platform parsing supports:

```text
os
arch
```

For example, the existing combination string `arm64-linux` represents target platform:

```text
os=linux
arch=arm64
```

The build platform comes from the current process platform. Cross compilation is enabled only when target platform differs from build platform.

## First Command Rules

The first implementation should cover only narrow cross compile commands:

- `cmake -S ... -B ...`: append `-DCMAKE_TOOLCHAIN_FILE=...` when the user did not already pass a toolchain file.
- `./configure` or `*/configure`: append `--host=<target-triple>` and `--build=<build-triple>` when absent, and set compiler/binutils environment variables.
- `pkg-config`: set target sysroot and library search environment.
- `cc`, `c++`, `gcc`, `g++`, `ar`, `ranlib`, `strip`: rewrite to the target tool paths when applicable.
- `make` and `ninja`: do not rewrite the executable. They are build-platform tools and should receive cross settings through configure/CMake output or inherited environment.

The exact command matching logic stays inside `crosscompile`.

## Error Handling

Middleware cannot currently return errors, so runtime command patching must not fail. All fallible work belongs in `crosscompile.New`:

- Invalid or unsupported matrix target.
- Unsupported platform-to-triple mapping.
- Missing required target compiler or binutils.
- Failure to prepare a CMake toolchain file or equivalent cross compile metadata.

If `New` fails, the build fails before formula execution. If `Use` cannot match a command, it returns a zero patch.

## Future Boundary

Broad tool packages such as Conan-style `tool_requires` should not be folded into `crosscompile`. They are build-context tools that run on the build platform, while this module handles target-platform compilation. A future module can manage build tools separately and may also plug into `execbroker` middleware, but it should not change the narrow crosscompile API unless command patch composition requires it.

