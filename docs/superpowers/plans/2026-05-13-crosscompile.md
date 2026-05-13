# Cross Compile Command Injection Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add hidden cross compilation command patching through `internal/build/crosscompile`, wired into the existing `execbroker` middleware during builds.

**Architecture:** `internal/build/crosscompile` owns matrix parsing, cross-platform detection, target GNU tool lookup, and neutral command patches. `internal/build` owns middleware lifecycle and converts neutral patches into `execbroker.Request`. Formula APIs remain unchanged.

**Tech Stack:** Go, existing `internal/build`, existing `internal/execbroker`, standard library `os/exec`, `runtime`, `path/filepath`, `strings`, `testing`.

---

## File Structure

- Create `internal/build/crosscompile/crosscompile.go`
  - Public API: `Command`, `Patch`, `CrossCompile`, `New`, `Use`.
  - Private options/test seam: `newWithOptions`.
  - Platform parsing and native/cross enablement.

- Create `internal/build/crosscompile/tools.go`
  - Platform-to-GNU-triple mapping.
  - Target tool lookup.
  - CMake toolchain file generation in LLAR cache.

- Create `internal/build/crosscompile/rules.go`
  - Command classification and patch generation for CMake configure, Autotools configure, pkg-config, and compiler/binutils commands.

- Create `internal/build/crosscompile/crosscompile_test.go`
  - Unit tests for native passthrough, matrix parsing, unsupported targets, tool lookup, CMake patches, configure patches, pkg-config patches, and direct compiler/binutils rewrites.

- Modify `internal/build/build.go`
  - Import `internal/build/crosscompile` and `internal/execbroker`.
  - Create the crosscompile context once at the start of `Build`.
  - Install/restore middleware around formula execution.
  - Add a private `applyCrossCompilePatch` helper.

- Modify `internal/build/build_test.go`
  - Add tests for `applyCrossCompilePatch`.
  - Add a build-level test proving the middleware is installed during formula execution and restored after build.

## Task 1: Add CrossCompile API and Native No-Op

**Files:**
- Create: `internal/build/crosscompile/crosscompile.go`
- Create: `internal/build/crosscompile/crosscompile_test.go`

- [ ] **Step 1: Write failing tests for native passthrough and invalid matrix**

Add `internal/build/crosscompile/crosscompile_test.go`:

```go
package crosscompile

import (
	"runtime"
	"testing"
)

func TestNewNativeUseReturnsZeroPatch(t *testing.T) {
	matrix := runtime.GOARCH + "-" + runtime.GOOS

	cc, err := New(matrix)
	if err != nil {
		t.Fatalf("New(%q): %v", matrix, err)
	}

	patch := cc.Use(Command{Name: "cc", Args: []string{"-c", "hello.c"}})
	if patch.Name != "" {
		t.Fatalf("patch.Name = %q, want empty", patch.Name)
	}
	if len(patch.PrependArg) != 0 || len(patch.AppendArg) != 0 {
		t.Fatalf("patch args = %#v/%#v, want empty", patch.PrependArg, patch.AppendArg)
	}
	if len(patch.SetEnv) != 0 || len(patch.PrependEnv) != 0 {
		t.Fatalf("patch env = %#v/%#v, want empty", patch.SetEnv, patch.PrependEnv)
	}
}

func TestNewRejectsInvalidMatrix(t *testing.T) {
	_, err := New("linux")
	if err == nil {
		t.Fatal("New(invalid matrix) error = nil, want error")
	}
}

func TestParseMatrix(t *testing.T) {
	got, err := parseMatrix("arm64-linux")
	if err != nil {
		t.Fatalf("parseMatrix: %v", err)
	}
	if got.Arch != "arm64" || got.OS != "linux" {
		t.Fatalf("platform = %+v, want arch=arm64 os=linux", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/build/crosscompile
```

Expected: FAIL because package `internal/build/crosscompile` does not exist yet.

- [ ] **Step 3: Add minimal API and native behavior**

Create `internal/build/crosscompile/crosscompile.go`:

```go
package crosscompile

import (
	"fmt"
	"runtime"
	"strings"
)

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
	enabled bool
	build   platform
	target  platform
	tools   targetTools
}

type platform struct {
	OS   string
	Arch string
}

type options struct {
	build   platform
	lookPath func(string) (string, error)
	cacheDir string
}

func New(matrix string) (*CrossCompile, error) {
	return newWithOptions(matrix, options{
		build: platform{OS: runtime.GOOS, Arch: runtime.GOARCH},
	})
}

func newWithOptions(matrix string, opts options) (*CrossCompile, error) {
	target, err := parseMatrix(matrix)
	if err != nil {
		return nil, err
	}
	cc := &CrossCompile{
		enabled: target != opts.build,
		build:   opts.build,
		target:  target,
	}
	return cc, nil
}

func (c *CrossCompile) Use(cmd Command) Patch {
	return Patch{}
}

func parseMatrix(matrix string) (platform, error) {
	arch, osName, ok := strings.Cut(matrix, "-")
	if !ok || arch == "" || osName == "" || strings.Contains(osName, "-") {
		return platform{}, fmt.Errorf("invalid cross compile matrix %q, want <arch>-<os>", matrix)
	}
	return platform{OS: osName, Arch: arch}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/build/crosscompile
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/build/crosscompile/crosscompile.go internal/build/crosscompile/crosscompile_test.go
git commit -m "feat(build): add crosscompile api skeleton"
```

## Task 2: Add Target Tool Discovery and CMake Toolchain File

**Files:**
- Modify: `internal/build/crosscompile/crosscompile.go`
- Create: `internal/build/crosscompile/tools.go`
- Modify: `internal/build/crosscompile/crosscompile_test.go`

- [ ] **Step 1: Add failing tests for GNU target lookup and unsupported targets**

Append to `internal/build/crosscompile/crosscompile_test.go`:

```go
func TestNewCrossFindsGNUTargetTools(t *testing.T) {
	cacheDir := t.TempDir()
	looked := map[string]bool{}
	fakeLookPath := func(name string) (string, error) {
		looked[name] = true
		return "/fake/bin/" + name, nil
	}

	cc, err := newWithOptions("arm64-linux", options{
		build:    platform{OS: "darwin", Arch: "arm64"},
		lookPath: fakeLookPath,
		cacheDir: cacheDir,
	})
	if err != nil {
		t.Fatalf("newWithOptions: %v", err)
	}
	if !cc.enabled {
		t.Fatal("enabled = false, want true")
	}
	if cc.tools.hostTriple != "aarch64-linux-gnu" {
		t.Fatalf("hostTriple = %q, want aarch64-linux-gnu", cc.tools.hostTriple)
	}
	if cc.tools.cc != "/fake/bin/aarch64-linux-gnu-gcc" {
		t.Fatalf("cc = %q", cc.tools.cc)
	}
	for _, name := range []string{
		"aarch64-linux-gnu-gcc",
		"aarch64-linux-gnu-g++",
		"aarch64-linux-gnu-ar",
		"aarch64-linux-gnu-ranlib",
		"aarch64-linux-gnu-strip",
	} {
		if !looked[name] {
			t.Fatalf("lookPath did not check %s", name)
		}
	}
	if cc.tools.cmakeToolchainFile == "" {
		t.Fatal("cmakeToolchainFile is empty")
	}
}

func TestNewCrossRejectsUnsupportedTarget(t *testing.T) {
	_, err := newWithOptions("arm64-windows", options{
		build: platform{OS: "darwin", Arch: "arm64"},
		lookPath: func(name string) (string, error) {
			return "/fake/bin/" + name, nil
		},
		cacheDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("newWithOptions unsupported target error = nil, want error")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/build/crosscompile
```

Expected: FAIL because `targetTools`, triple mapping, tool lookup, and CMake file generation are not implemented.

- [ ] **Step 3: Implement target tool discovery**

Create `internal/build/crosscompile/tools.go`:

```go
package crosscompile

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type targetTools struct {
	buildTriple string
	hostTriple  string

	cc     string
	cxx    string
	ar     string
	ranlib string
	strip  string

	pkgConfig string
	sysroot   string

	cmakeToolchainFile string
}

func prepareTargetTools(target platform, opts options) (targetTools, error) {
	hostTriple, err := gnuTriple(target)
	if err != nil {
		return targetTools{}, err
	}
	buildTriple, err := gnuTriple(opts.build)
	if err != nil {
		buildTriple = opts.build.Arch + "-" + opts.build.OS
	}

	lookPath := opts.lookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	lookupRequired := func(suffix string) (string, error) {
		name := hostTriple + "-" + suffix
		path, err := lookPath(name)
		if err != nil {
			return "", fmt.Errorf("cross compile tool %s not found: %w", name, err)
		}
		return path, nil
	}

	tools := targetTools{
		buildTriple: buildTriple,
		hostTriple:  hostTriple,
	}
	if tools.cc, err = lookupRequired("gcc"); err != nil {
		return targetTools{}, err
	}
	if tools.cxx, err = lookupRequired("g++"); err != nil {
		return targetTools{}, err
	}
	if tools.ar, err = lookupRequired("ar"); err != nil {
		return targetTools{}, err
	}
	if tools.ranlib, err = lookupRequired("ranlib"); err != nil {
		return targetTools{}, err
	}
	if tools.strip, err = lookupRequired("strip"); err != nil {
		return targetTools{}, err
	}
	if pkgConfig, err := lookPath(hostTriple + "-pkg-config"); err == nil {
		tools.pkgConfig = pkgConfig
	}

	cacheDir := opts.cacheDir
	if cacheDir == "" {
		userCacheDir, err := os.UserCacheDir()
		if err != nil {
			return targetTools{}, err
		}
		cacheDir = filepath.Join(userCacheDir, ".llar", "crosscompile")
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return targetTools{}, err
	}
	tools.cmakeToolchainFile = filepath.Join(cacheDir, hostTriple+".cmake")
	if err := writeCMakeToolchainFile(tools.cmakeToolchainFile, target, tools); err != nil {
		return targetTools{}, err
	}
	return tools, nil
}

func gnuTriple(p platform) (string, error) {
	switch p.OS + "/" + p.Arch {
	case "linux/arm64":
		return "aarch64-linux-gnu", nil
	case "linux/amd64":
		return "x86_64-linux-gnu", nil
	default:
		return "", fmt.Errorf("unsupported cross compile target %s/%s", p.OS, p.Arch)
	}
}

func cmakeSystemName(osName string) string {
	switch osName {
	case "linux":
		return "Linux"
	default:
		return osName
	}
}

func cmakeProcessor(arch string) string {
	switch arch {
	case "arm64":
		return "aarch64"
	case "amd64":
		return "x86_64"
	default:
		return arch
	}
}

func writeCMakeToolchainFile(path string, target platform, tools targetTools) error {
	content := strings.Join([]string{
		"set(CMAKE_SYSTEM_NAME " + cmakeSystemName(target.OS) + ")",
		"set(CMAKE_SYSTEM_PROCESSOR " + cmakeProcessor(target.Arch) + ")",
		"set(CMAKE_C_COMPILER " + tools.cc + ")",
		"set(CMAKE_CXX_COMPILER " + tools.cxx + ")",
		"set(CMAKE_AR " + tools.ar + ")",
		"set(CMAKE_RANLIB " + tools.ranlib + ")",
		"set(CMAKE_STRIP " + tools.strip + ")",
		"",
	}, "\n")
	return os.WriteFile(path, []byte(content), 0o644)
}
```

Update `newWithOptions` in `internal/build/crosscompile/crosscompile.go` so cross builds prepare tools:

```go
func newWithOptions(matrix string, opts options) (*CrossCompile, error) {
	target, err := parseMatrix(matrix)
	if err != nil {
		return nil, err
	}
	cc := &CrossCompile{
		enabled: target != opts.build,
		build:   opts.build,
		target:  target,
	}
	if cc.enabled {
		tools, err := prepareTargetTools(target, opts)
		if err != nil {
			return nil, err
		}
		cc.tools = tools
	}
	return cc, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/build/crosscompile
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/build/crosscompile/crosscompile.go internal/build/crosscompile/tools.go internal/build/crosscompile/crosscompile_test.go
git commit -m "feat(build): probe cross compile target tools"
```

## Task 3: Implement Command Patches

**Files:**
- Create: `internal/build/crosscompile/rules.go`
- Modify: `internal/build/crosscompile/crosscompile.go`
- Modify: `internal/build/crosscompile/crosscompile_test.go`

- [ ] **Step 1: Add failing command patch tests**

Append to `internal/build/crosscompile/crosscompile_test.go`:

```go
func newTestCrossCompile(t *testing.T) *CrossCompile {
	t.Helper()
	cc, err := newWithOptions("arm64-linux", options{
		build: platform{OS: "darwin", Arch: "arm64"},
		lookPath: func(name string) (string, error) {
			return "/fake/bin/" + name, nil
		},
		cacheDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("newWithOptions: %v", err)
	}
	return cc
}

func TestUsePatchesCMakeConfigure(t *testing.T) {
	cc := newTestCrossCompile(t)

	patch := cc.Use(Command{Name: "cmake", Args: []string{"-S", ".", "-B", "build"}})

	want := "-DCMAKE_TOOLCHAIN_FILE=" + cc.tools.cmakeToolchainFile
	if !contains(patch.AppendArg, want) {
		t.Fatalf("AppendArg = %#v, want %q", patch.AppendArg, want)
	}
}

func TestUseDoesNotOverrideExplicitCMakeToolchain(t *testing.T) {
	cc := newTestCrossCompile(t)

	patch := cc.Use(Command{Name: "cmake", Args: []string{"-S", ".", "-B", "build", "-DCMAKE_TOOLCHAIN_FILE=/user/toolchain.cmake"}})

	if len(patch.AppendArg) != 0 {
		t.Fatalf("AppendArg = %#v, want empty", patch.AppendArg)
	}
}

func TestUsePatchesConfigure(t *testing.T) {
	cc := newTestCrossCompile(t)

	patch := cc.Use(Command{Name: "./configure", Args: []string{"--prefix=/out"}})

	if !contains(patch.AppendArg, "--host=aarch64-linux-gnu") {
		t.Fatalf("AppendArg = %#v, want --host", patch.AppendArg)
	}
	if !contains(patch.AppendArg, "--build=arm64-darwin") {
		t.Fatalf("AppendArg = %#v, want --build", patch.AppendArg)
	}
	if patch.SetEnv["CC"] != "/fake/bin/aarch64-linux-gnu-gcc" {
		t.Fatalf("CC env = %q", patch.SetEnv["CC"])
	}
	if patch.SetEnv["AR"] != "/fake/bin/aarch64-linux-gnu-ar" {
		t.Fatalf("AR env = %q", patch.SetEnv["AR"])
	}
}

func TestUsePatchesDirectCompilerAndBinutils(t *testing.T) {
	cc := newTestCrossCompile(t)
	tests := []struct {
		name string
		want string
	}{
		{"cc", "/fake/bin/aarch64-linux-gnu-gcc"},
		{"gcc", "/fake/bin/aarch64-linux-gnu-gcc"},
		{"c++", "/fake/bin/aarch64-linux-gnu-g++"},
		{"g++", "/fake/bin/aarch64-linux-gnu-g++"},
		{"ar", "/fake/bin/aarch64-linux-gnu-ar"},
		{"ranlib", "/fake/bin/aarch64-linux-gnu-ranlib"},
		{"strip", "/fake/bin/aarch64-linux-gnu-strip"},
	}
	for _, tt := range tests {
		patch := cc.Use(Command{Name: tt.name})
		if patch.Name != tt.want {
			t.Fatalf("%s patch.Name = %q, want %q", tt.name, patch.Name, tt.want)
		}
	}
}

func TestUsePatchesPkgConfig(t *testing.T) {
	cc := newTestCrossCompile(t)

	patch := cc.Use(Command{Name: "pkg-config", Args: []string{"--libs", "zlib"}})

	if patch.Name != "/fake/bin/aarch64-linux-gnu-pkg-config" {
		t.Fatalf("patch.Name = %q", patch.Name)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run:

```bash
go test ./internal/build/crosscompile
```

Expected: FAIL because `Use` still returns a zero patch.

- [ ] **Step 3: Implement command patch rules**

Create `internal/build/crosscompile/rules.go`:

```go
package crosscompile

import (
	"path/filepath"
	"strings"
)

func (c *CrossCompile) useCMake(cmd Command) Patch {
	if !isCMakeConfigure(cmd.Args) || hasCMakeToolchain(cmd.Args) {
		return Patch{}
	}
	return Patch{AppendArg: []string{"-DCMAKE_TOOLCHAIN_FILE=" + c.tools.cmakeToolchainFile}}
}

func (c *CrossCompile) useConfigure(cmd Command) Patch {
	args := make([]string, 0, 2)
	if !hasPrefixArg(cmd.Args, "--host=") {
		args = append(args, "--host="+c.tools.hostTriple)
	}
	if !hasPrefixArg(cmd.Args, "--build=") {
		args = append(args, "--build="+c.tools.buildTriple)
	}
	return Patch{
		AppendArg: args,
		SetEnv: map[string]string{
			"CC":     c.tools.cc,
			"CXX":    c.tools.cxx,
			"AR":     c.tools.ar,
			"RANLIB": c.tools.ranlib,
			"STRIP":  c.tools.strip,
		},
	}
}

func (c *CrossCompile) usePkgConfig() Patch {
	patch := Patch{SetEnv: map[string]string{}}
	if c.tools.pkgConfig != "" {
		patch.Name = c.tools.pkgConfig
	}
	if c.tools.sysroot != "" {
		patch.SetEnv["PKG_CONFIG_SYSROOT_DIR"] = c.tools.sysroot
		patch.SetEnv["PKG_CONFIG_LIBDIR"] = filepath.Join(c.tools.sysroot, "usr", "lib", "pkgconfig")
	}
	if len(patch.SetEnv) == 0 {
		patch.SetEnv = nil
	}
	return patch
}

func isCMakeConfigure(args []string) bool {
	return hasExactArg(args, "-S") && hasExactArg(args, "-B")
}

func hasCMakeToolchain(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-DCMAKE_TOOLCHAIN_FILE=") ||
			strings.HasPrefix(arg, "-DCMAKE_TOOLCHAIN_FILE:") {
			return true
		}
	}
	return false
}

func hasExactArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func hasPrefixArg(args []string, prefix string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

func isConfigureName(name string) bool {
	base := filepath.Base(name)
	return base == "configure" || name == "./configure"
}
```

Update `Use` in `internal/build/crosscompile/crosscompile.go`:

```go
func (c *CrossCompile) Use(cmd Command) Patch {
	if c == nil || !c.enabled {
		return Patch{}
	}
	switch filepath.Base(cmd.Name) {
	case "cmake":
		return c.useCMake(cmd)
	case "pkg-config":
		return c.usePkgConfig()
	case "cc", "gcc":
		return Patch{Name: c.tools.cc}
	case "c++", "g++":
		return Patch{Name: c.tools.cxx}
	case "ar":
		return Patch{Name: c.tools.ar}
	case "ranlib":
		return Patch{Name: c.tools.ranlib}
	case "strip":
		return Patch{Name: c.tools.strip}
	}
	if isConfigureName(cmd.Name) {
		return c.useConfigure(cmd)
	}
	return Patch{}
}
```

Add `path/filepath` to the import list in `internal/build/crosscompile/crosscompile.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run:

```bash
go test ./internal/build/crosscompile
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/build/crosscompile
git commit -m "feat(build): patch cross compile commands"
```

## Task 4: Wire CrossCompile Into Builder Middleware

**Files:**
- Modify: `internal/build/build.go`
- Modify: `internal/build/build_test.go`

- [ ] **Step 1: Add failing tests for patch application**

Append to `internal/build/build_test.go`:

```go
func TestApplyCrossCompilePatch(t *testing.T) {
	req := execbroker.Request{
		Name: "cmake",
		Args: []string{"-S", ".", "-B", "build"},
		Env:  []string{"PATH=/usr/bin", "CC=cc"},
	}
	patch := crosscompile.Patch{
		Name:      "/opt/cmake",
		AppendArg: []string{"-DCMAKE_TOOLCHAIN_FILE=/tmp/tc.cmake"},
		SetEnv: map[string]string{
			"CC": "aarch64-linux-gnu-gcc",
		},
		PrependEnv: map[string][]string{
			"PATH": []string{"/opt/cross/bin"},
		},
	}

	got := applyCrossCompilePatch(req, patch)

	if got.Name != "/opt/cmake" {
		t.Fatalf("Name = %q", got.Name)
	}
	if strings.Join(got.Args, " ") != "-S . -B build -DCMAKE_TOOLCHAIN_FILE=/tmp/tc.cmake" {
		t.Fatalf("Args = %#v", got.Args)
	}
	env := strings.Join(got.Env, "\n")
	if !strings.Contains(env, "CC=aarch64-linux-gnu-gcc") {
		t.Fatalf("Env missing CC override: %#v", got.Env)
	}
	if !strings.Contains(env, "PATH=/opt/cross/bin:/usr/bin") {
		t.Fatalf("Env missing PATH prepend: %#v", got.Env)
	}
}
```

Add imports to `internal/build/build_test.go`:

```go
	"github.com/goplus/llar/internal/build/crosscompile"
	"github.com/goplus/llar/internal/execbroker"
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
go test ./internal/build -run TestApplyCrossCompilePatch
```

Expected: FAIL because `applyCrossCompilePatch` does not exist.

- [ ] **Step 3: Implement patch application and middleware install**

Modify imports in `internal/build/build.go`:

```go
	"runtime"

	"github.com/goplus/llar/internal/build/crosscompile"
	"github.com/goplus/llar/internal/execbroker"
```

Before wiring `Build`, add a private interface and default constructor near the type definitions:

```go
type crossCompiler interface {
	Use(crosscompile.Command) crosscompile.Patch
}

var newCrossCompile = func(matrix string) (crossCompiler, error) {
	return crosscompile.New(matrix)
}
```

Modify `Builder` in `internal/build/build.go`:

```go
type Builder struct {
	store        repo.Store
	matrix       string
	runTest      bool
	workspaceDir string
	newRepo      func(repoPath string) (vcs.Repo, error)
	newCrossCompile func(matrix string) (crossCompiler, error)
}
```

In `NewBuilder`, set:

```go
		newCrossCompile: newCrossCompile,
```

In `setupBuilder` in `internal/build/build_test.go`, set the new seam:

```go
		newCrossCompile: func(matrix string) (crossCompiler, error) {
			return noopCrossCompiler{}, nil
		},
```

Add the no-op test implementation near other test helpers in `internal/build/build_test.go`:

```go
type noopCrossCompiler struct{}

func (noopCrossCompiler) Use(crosscompile.Command) crosscompile.Patch {
	return crosscompile.Patch{}
}
```

This keeps existing build tests independent from the host machine's installed cross compilers. Individual tests that need middleware behavior override `b.newCrossCompile` explicitly.

At the start of `Build`, before `builtResults := ...`, create and install the middleware:

```go
func (b *Builder) Build(ctx context.Context, targets []*modules.Module) ([]Result, error) {
	cc, err := b.newCrossCompile(b.matrix)
	if err != nil {
		return nil, err
	}
	restoreMiddleware := execbroker.SetMiddleware(func(req execbroker.Request) execbroker.Request {
		patch := cc.Use(crosscompile.Command{
			Name: req.Name,
			Args: req.Args,
			Env:  req.Env,
		})
		return applyCrossCompilePatch(req, patch)
	})
	defer restoreMiddleware()

	builtResults := make(map[module.Version]classfile.BuildResult)
```

Add helpers near the bottom of `internal/build/build.go`:

```go
func applyCrossCompilePatch(req execbroker.Request, patch crosscompile.Patch) execbroker.Request {
	if patch.Name != "" {
		req.Name = patch.Name
	}
	if len(patch.PrependArg) > 0 {
		req.Args = append(append([]string(nil), patch.PrependArg...), req.Args...)
	}
	if len(patch.AppendArg) > 0 {
		req.Args = append(append([]string(nil), req.Args...), patch.AppendArg...)
	}
	if len(patch.SetEnv) > 0 || len(patch.PrependEnv) > 0 {
		req.Env = applyEnvPatch(req.Env, patch)
	}
	return req
}

func applyEnvPatch(env []string, patch crosscompile.Patch) []string {
	values := make(map[string]string)
	order := make([]string, 0, len(env)+len(patch.SetEnv)+len(patch.PrependEnv))
	for _, item := range env {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	for key, value := range patch.SetEnv {
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	for key, valuesToPrepend := range patch.PrependEnv {
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		prefix := strings.Join(valuesToPrepend, sep)
		if cur := values[key]; cur != "" {
			values[key] = prefix + sep + cur
		} else {
			values[key] = prefix
		}
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, key+"="+values[key])
	}
	return out
}
```

Remove the `runtime` import if the file already has a platform-specific path separator helper by the time this task is executed.

- [ ] **Step 4: Run focused tests**

Run:

```bash
go test ./internal/build -run TestApplyCrossCompilePatch
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/build/build.go internal/build/build_test.go
git commit -m "feat(build): install crosscompile middleware"
```

## Task 5: Add Build-Level Middleware Behavior Test

**Files:**
- Modify: `internal/build/build.go`
- Modify: `internal/build/build_test.go`

- [ ] **Step 1: Add failing test for middleware use and restore**

Append to `internal/build/build_test.go`:

```go
type fakeCrossCompiler struct {
	seen []crosscompile.Command
}

func (f *fakeCrossCompiler) Use(cmd crosscompile.Command) crosscompile.Patch {
	f.seen = append(f.seen, cmd)
	if cmd.Name == "llar-crosscompile-probe" {
		return crosscompile.Patch{
			Name: os.Args[0],
			AppendArg: []string{"-test.run=TestBuildCrossCompileHelperProcess"},
			SetEnv: map[string]string{
				"LLAR_BUILDER_CROSSCOMPILE_HELPER": "1",
			},
		}
	}
	return crosscompile.Patch{}
}

func TestBuildInstallsCrossCompileMiddleware(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "arm64-linux")
	fake := &fakeCrossCompiler{}
	b.newCrossCompile = func(matrix string) (crossCompiler, error) {
		if matrix != "arm64-linux" {
			t.Fatalf("matrix = %q, want arm64-linux", matrix)
		}
		return fake, nil
	}

	loadedFormula := &loadedformula.Formula{
		ModPath: "test/probe",
		FromVer: "1.0.0",
		OnBuild: func(ctx *classfile.Context, proj *classfile.Project, out *classfile.BuildResult) {
			data, err := execbroker.Command("llar-crosscompile-probe").Output()
			if err != nil {
				out.AddErr(err)
				return
			}
			out.SetMetadata(string(data))
		},
	}
	target := &modules.Module{
		Formula: loadedFormula,
		FS:      os.DirFS(t.TempDir()),
		Path:    "test/probe",
		Version: "1.0.0",
	}

	results, err := b.Build(context.Background(), []*modules.Module{target})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(results) != 1 || results[0].Metadata != "crosscompile-ok" {
		t.Fatalf("results = %#v, want metadata crosscompile-ok", results)
	}
	if len(fake.seen) == 0 {
		t.Fatal("cross compiler did not see any commands")
	}

	req := execbroker.Command("llar-crosscompile-probe")
	if req.Path == os.Args[0] {
		t.Fatal("execbroker middleware was not restored after Build")
	}
}

func TestBuildCrossCompileHelperProcess(t *testing.T) {
	if os.Getenv("LLAR_BUILDER_CROSSCOMPILE_HELPER") != "1" {
		return
	}
	fmt.Print("crosscompile-ok")
	os.Exit(0)
}
```

Ensure these imports exist in `internal/build/build_test.go`:

```go
	loadedformula "github.com/goplus/llar/internal/formula"
	"github.com/goplus/llar/internal/execbroker"
```

The file already imports `context`, `fmt`, `os`, and the public formula package alias used above:

```go
	classfile "github.com/goplus/llar/formula"
```

- [ ] **Step 2: Run the focused test**

Run:

```bash
go test ./internal/build -run 'TestBuildInstallsCrossCompileMiddleware|TestBuildCrossCompileHelperProcess'
```

Expected: PASS after the test seam and middleware are wired correctly.

- [ ] **Step 3: Commit**

```bash
git add internal/build/build.go internal/build/build_test.go
git commit -m "test(build): verify crosscompile middleware lifecycle"
```

## Task 6: Final Verification and Cleanup

**Files:**
- Review: `internal/build/crosscompile/*.go`
- Review: `internal/build/build.go`
- Review: `internal/build/build_test.go`

- [ ] **Step 1: Format touched Go files**

Run:

```bash
gofmt -w internal/build/crosscompile/*.go internal/build/build.go internal/build/build_test.go
```

Expected: command exits 0.

- [ ] **Step 2: Run focused tests**

Run:

```bash
go test ./internal/build/crosscompile ./internal/build
```

Expected: PASS.

- [ ] **Step 3: Run broader build verification**

Run:

```bash
go build -ldflags='-checklinkname=0' ./...
```

Expected: PASS.

- [ ] **Step 4: Inspect diff for module-boundary violations**

Run:

```bash
rg -n 'internal/execbroker' internal/build/crosscompile || true
rg -n 'crosscompile' formula internal/formula x || true
git diff --stat
```

Expected:

- First `rg` prints nothing.
- Second `rg` prints nothing outside intentional build integration.
- Diff contains only `internal/build/crosscompile`, `internal/build/build.go`, and `internal/build/build_test.go` unless previous tasks intentionally changed another listed file.

- [ ] **Step 5: Commit any final cleanup**

If formatting or cleanup changed files after the last task:

```bash
git add internal/build/crosscompile internal/build/build.go internal/build/build_test.go
git commit -m "chore(build): verify crosscompile integration"
```

If there are no changes, do not create an empty commit.
