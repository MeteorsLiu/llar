# Cross Compile Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the dependency graph and build-context foundation for hidden cross-Linux sysroot selection, so later command injection can consume a selected `glibc` sysroot without formula changes.

**Architecture:** Matrix parsing lives in a small internal package shared by module loading and build. `modules.Load` injects hidden cross-Linux `glibc` before MVS so normal version selection remains the source of truth. `internal/build` treats `glibc` as an official source-less package and exposes its `BuildResult.Metadata` as default sysroot metadata for downstream builds.

**Tech Stack:** Go, existing `internal/modules`, existing `internal/build`, existing `formula.BuildResult.Metadata`, standard library `runtime`, `strings`, `testing`, `os`.

---

## Ground Rules

- Before creating or editing any LLAR formula `.gox` file, read the local `write-formula` skill and follow it. Do not guess formula syntax, helper names, generated method names, or DSL conventions.
- Repository-specific claims must be verified from source files or tests before being written into code.
- Keep formula changes limited to fixtures needed by this plan.

## Scope

This plan implements the foundation only:

- matrix target parsing;
- hidden cross-Linux `glibc` injection before MVS;
- official source-less `glibc` build path;
- selected `glibc` metadata propagation inside build context;
- build cache variant including selected `glibc` version.

This plan does not implement LLVM download, command rewriting, CMake toolchain files, Autotools flags, or `CMake.Sysroot` / `AutoTools.Sysroot`. Those belong in the next plan after this foundation is merged.

## File Structure

- Create `internal/build/buildtarget/target.go`
  - Parses LLAR matrix strings into `arch` plus optional `os`.
  - Decides native vs cross against a supplied host.
  - Decides whether a target needs the default cross-Linux `glibc`.

- Create `internal/build/buildtarget/target_test.go`
  - Covers native, cross Linux, Darwin, no-os, and malformed matrix cases.

- Modify `internal/modules/load.go`
  - Adds matrix-aware options.
  - Injects hidden `glibc@2.39` into main requirements before MVS for cross-Linux only.

- Modify `internal/modules/load_test.go`
  - Adds tests for hidden default `glibc` injection, non-Linux/no-os skips, and explicit `glibc` version selection.

- Add `internal/modules/testdata/load/glibc/...`
  - Minimal single-segment official package fixture.

- Modify `internal/build/build.go`
  - Adds official source-less source checkout policy for `glibc`.
  - Captures selected `glibc` metadata and applies it to downstream build contexts.
  - Uses build variant for cache/install paths.

- Modify `internal/build/cache.go`
  - Changes internal cache key/install dir to accept a variant string.

- Modify `internal/build/build_test.go`
  - Adds tests for source-less `glibc`, sysroot metadata propagation, and cache variant.

- Add `internal/build/testdata/formulas/glibc/...`
  - Minimal build fixture that emits fake sysroot metadata.

## Task 1: Add Matrix Target Parser

**Files:**
- Create: `internal/build/buildtarget/target_test.go`
- Create: `internal/build/buildtarget/target.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/build/buildtarget/target_test.go`:

```go
package buildtarget

import "testing"

func TestParseLinuxTarget(t *testing.T) {
	target, err := Parse("arm64-linux")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if target.Arch != "arm64" || target.OS != "linux" {
		t.Fatalf("target = %+v, want arch=arm64 os=linux", target)
	}
}

func TestParseNoOSTarget(t *testing.T) {
	target, err := Parse("arm64")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if target.Arch != "arm64" || target.OS != "" {
		t.Fatalf("target = %+v, want arch=arm64 no os", target)
	}
}

func TestParseRejectsMalformedMatrix(t *testing.T) {
	for _, matrix := range []string{"", "-linux", "arm64-", "arm64-linux-debug"} {
		if _, err := Parse(matrix); err == nil {
			t.Fatalf("Parse(%q) error = nil, want error", matrix)
		}
	}
}

func TestTargetClassification(t *testing.T) {
	host := Platform{Arch: "arm64", OS: "darwin"}

	tests := []struct {
		name        string
		matrix      string
		wantNative  bool
		wantGlibc   bool
	}{
		{name: "native", matrix: "arm64-darwin", wantNative: true, wantGlibc: false},
		{name: "cross linux", matrix: "amd64-linux", wantNative: false, wantGlibc: true},
		{name: "cross darwin", matrix: "amd64-darwin", wantNative: false, wantGlibc: false},
		{name: "no os", matrix: "arm64", wantNative: false, wantGlibc: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target, err := Parse(tt.matrix)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := target.IsNative(host); got != tt.wantNative {
				t.Fatalf("IsNative = %v, want %v", got, tt.wantNative)
			}
			if got := target.NeedsDefaultGlibc(host); got != tt.wantGlibc {
				t.Fatalf("NeedsDefaultGlibc = %v, want %v", got, tt.wantGlibc)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:

```bash
go test ./internal/build/buildtarget
```

Expected: FAIL because `internal/build/buildtarget` does not exist.

- [ ] **Step 3: Implement the parser**

Create `internal/build/buildtarget/target.go`:

```go
package buildtarget

import (
	"fmt"
	"strings"
)

type Platform struct {
	Arch string
	OS   string
}

func Parse(matrix string) (Platform, error) {
	if matrix == "" {
		return Platform{}, fmt.Errorf("invalid matrix %q: empty", matrix)
	}
	parts := strings.Split(matrix, "-")
	switch len(parts) {
	case 1:
		if parts[0] == "" {
			return Platform{}, fmt.Errorf("invalid matrix %q: empty arch", matrix)
		}
		return Platform{Arch: parts[0]}, nil
	case 2:
		if parts[0] == "" || parts[1] == "" {
			return Platform{}, fmt.Errorf("invalid matrix %q: want <arch>-<os>", matrix)
		}
		return Platform{Arch: parts[0], OS: parts[1]}, nil
	default:
		return Platform{}, fmt.Errorf("invalid matrix %q: want <arch> or <arch>-<os>", matrix)
	}
}

func (p Platform) IsNative(host Platform) bool {
	return p.Arch == host.Arch && p.OS != "" && p.OS == host.OS
}

func (p Platform) NeedsDefaultGlibc(host Platform) bool {
	return p.OS == "linux" && !p.IsNative(host)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:

```bash
go test ./internal/build/buildtarget
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/build/buildtarget
git commit -m "feat(build): add matrix target parser"
```

## Task 2: Inject Hidden `glibc` Before MVS

**Files:**
- Modify: `internal/modules/load.go`
- Modify: `internal/modules/load_test.go`
- Create: `internal/modules/testdata/load/glibc/versions.json`
- Create: `internal/modules/testdata/load/glibc/2.39/Glibc_llar.gox`
- Create: `internal/modules/testdata/load/glibc/2.40/Glibc_llar.gox`

- [ ] **Step 1: Add the `glibc` formula fixtures**

Before this step, read `/Users/haolan/.codex/skills/write-formula/SKILL.md` and verify the `.gox` syntax used below against existing formula fixtures in this repository.

Create `internal/modules/testdata/load/glibc/versions.json`:

```json
{
  "path": "glibc",
  "deps": {}
}
```

Create `internal/modules/testdata/load/glibc/2.39/Glibc_llar.gox`:

```go
id "glibc"

fromVer "2.39"

onBuild (ctx, proj, out) => {
	out.setMetadata "--sysroot=" + ctx.outputDir()
}
```

Create `internal/modules/testdata/load/glibc/2.40/Glibc_llar.gox`:

```go
id "glibc"

fromVer "2.40"

onBuild (ctx, proj, out) => {
	out.setMetadata "--sysroot=" + ctx.outputDir()
}
```

- [ ] **Step 2: Write the failing module loading tests**

Append to `internal/modules/load_test.go`:

```go
func TestLoad_CrossLinuxInjectsDefaultGlibc(t *testing.T) {
	store := setupTestStore(t, "testdata/load")
	ctx := context.Background()
	main := module.Version{Path: "towner/standalone", Version: "1.0.0"}

	mods, err := Load(ctx, main, Options{
		FormulaStore: store,
		MatrixStr:    "amd64-linux",
		HostOS:       "darwin",
		HostArch:     "arm64",
	})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	glibc := findModule(mods, "glibc")
	if glibc == nil {
		t.Fatalf("glibc not found in build list: %#v", mods)
	}
	if glibc.Version != "2.39" {
		t.Fatalf("glibc version = %q, want 2.39", glibc.Version)
	}
	if mods[0].Path != "towner/standalone" {
		t.Fatalf("mods[0].Path = %q, want root module", mods[0].Path)
	}
}

func TestLoad_DefaultGlibcSkippedForNativeDarwinAndNoOS(t *testing.T) {
	store := setupTestStore(t, "testdata/load")
	ctx := context.Background()
	main := module.Version{Path: "towner/standalone", Version: "1.0.0"}

	tests := []struct {
		name   string
		matrix string
	}{
		{name: "native", matrix: "arm64-darwin"},
		{name: "cross darwin", matrix: "amd64-darwin"},
		{name: "no os", matrix: "arm64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mods, err := Load(ctx, main, Options{
				FormulaStore: store,
				MatrixStr:    tt.matrix,
				HostOS:       "darwin",
				HostArch:     "arm64",
			})
			if err != nil {
				t.Fatalf("Load failed: %v", err)
			}
			if glibc := findModule(mods, "glibc"); glibc != nil {
				t.Fatalf("glibc unexpectedly found for %s: %+v", tt.matrix, glibc)
			}
		})
	}
}

func TestLoad_ExplicitGlibcRequirementParticipatesInMVS(t *testing.T) {
	store := setupTestStore(t, "testdata/load")
	ctx := context.Background()
	main := module.Version{Path: "towner/withglibc", Version: "1.0.0"}

	mods, err := Load(ctx, main, Options{
		FormulaStore: store,
		MatrixStr:    "amd64-linux",
		HostOS:       "darwin",
		HostArch:     "arm64",
	})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	glibc := findModule(mods, "glibc")
	if glibc == nil {
		t.Fatal("glibc not found")
	}
	if glibc.Version != "2.40" {
		t.Fatalf("glibc version = %q, want explicit higher version 2.40", glibc.Version)
	}
}
```

Create `internal/modules/testdata/load/towner/withglibc/versions.json`:

```json
{
  "path": "towner/withglibc",
  "deps": {
    "1.0.0": [
      {
        "path": "glibc",
        "version": "2.40"
      }
    ]
  }
}
```

Create `internal/modules/testdata/load/towner/withglibc/1.0.0/Withglibc_llar.gox`:

```go
id "towner/withglibc"

fromVer "1.0.0"

onBuild (ctx, proj, out) => {
	out.setMetadata "-lwithglibc"
}
```

- [ ] **Step 3: Run the focused tests to verify they fail**

Run:

```bash
go test ./internal/modules -run 'TestLoad_CrossLinuxInjectsDefaultGlibc|TestLoad_DefaultGlibcSkippedForNativeDarwinAndNoOS|TestLoad_ExplicitGlibcRequirementParticipatesInMVS'
```

Expected: FAIL because `Options` has no `MatrixStr`, `HostOS`, or `HostArch`, and hidden `glibc` is not injected.

- [ ] **Step 4: Implement matrix-aware default dependency injection**

Modify the imports in `internal/modules/load.go` to add:

```go
	"runtime"

	"github.com/goplus/llar/internal/build/buildtarget"
```

Modify `type Options struct` in `internal/modules/load.go`:

```go
type Options struct {
	// FormulaStore is the store for downloading and caching formulas.
	FormulaStore repo.Store

	// MatrixStr is the active target matrix. Empty means no platform default
	// dependencies are injected.
	MatrixStr string

	// HostOS and HostArch are test seams. Production callers leave them empty
	// and the runtime host is used.
	HostOS   string
	HostArch string
}
```

Add this helper to `internal/modules/load.go`:

```go
const defaultGlibcVersion = "2.39"

func defaultDepsForMatrix(matrix, hostOS, hostArch string) ([]module.Version, error) {
	if matrix == "" {
		return nil, nil
	}
	if hostOS == "" {
		hostOS = runtime.GOOS
	}
	if hostArch == "" {
		hostArch = runtime.GOARCH
	}
	target, err := buildtarget.Parse(matrix)
	if err != nil {
		return nil, err
	}
	host := buildtarget.Platform{OS: hostOS, Arch: hostArch}
	if !target.NeedsDefaultGlibc(host) {
		return nil, nil
	}
	return []module.Version{{Path: "glibc", Version: defaultGlibcVersion}}, nil
}

func appendDefaultDeps(deps []module.Version, defaults []module.Version) []module.Version {
	if len(defaults) == 0 {
		return deps
	}
	out := append([]module.Version(nil), deps...)
	out = append(out, defaults...)
	return out
}
```

In `Load`, immediately after `mainDeps` is resolved, replace:

```go
	mainDeps, err := resolveDeps(main, mainMod.fsys.(fs.ReadFileFS), mainFormula)
	if err != nil {
		return nil, err
	}
```

with:

```go
	mainDeps, err := resolveDeps(main, mainMod.fsys.(fs.ReadFileFS), mainFormula)
	if err != nil {
		return nil, err
	}
	defaultDeps, err := defaultDepsForMatrix(opts.MatrixStr, opts.HostOS, opts.HostArch)
	if err != nil {
		return nil, err
	}
	mainDeps = appendDefaultDeps(mainDeps, defaultDeps)
```

- [ ] **Step 5: Run the focused tests to verify they pass or expose ixgo linker blocker**

Run:

```bash
go test ./internal/modules -run 'TestLoad_CrossLinuxInjectsDefaultGlibc|TestLoad_DefaultGlibcSkippedForNativeDarwinAndNoOS|TestLoad_ExplicitGlibcRequirementParticipatesInMVS'
```

Expected: PASS. If the local ixgo linker fails with `invalid reference to math/rand/v2.globalRand`, record that exact blocker and run:

```bash
go test ./internal/build/buildtarget ./mod/module ./mod/versions
```

Expected fallback: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/build/buildtarget internal/modules/load.go internal/modules/load_test.go internal/modules/testdata/load/glibc internal/modules/testdata/load/towner/withglibc
git commit -m "feat(modules): inject default glibc for cross linux"
```

## Task 3: Add Matrix Options To CLI Build Loading

**Files:**
- Modify: `cmd/llar/internal/make.go`
- Modify: `cmd/llar/internal/test.go`

- [ ] **Step 1: Update `buildModule` to pass matrix into `modules.Load`**

In `cmd/llar/internal/make.go`, replace:

```go
	mods, err := modules.Load(ctx, module.Version{Path: modPath, Version: version}, modules.Options{
		FormulaStore: store,
	})
```

with:

```go
	mods, err := modules.Load(ctx, module.Version{Path: modPath, Version: version}, modules.Options{
		FormulaStore: store,
		MatrixStr:    matrixStr,
	})
```

No separate edit is needed in `cmd/llar/internal/test.go` if it calls the same `buildModule` helper.

- [ ] **Step 2: Run the package build**

Run:

```bash
go build ./cmd/llar/internal
```

Expected: PASS. If the local ixgo linker fails with `invalid reference to math/rand/v2.globalRand`, record the blocker.

- [ ] **Step 3: Commit**

```bash
git add cmd/llar/internal/make.go
git commit -m "fix(cmd): pass matrix to module loading"
```

## Task 4: Add Source-Less Build Path For Official `glibc`

**Files:**
- Modify: `internal/build/build.go`
- Modify: `internal/build/build_test.go`
- Create: `internal/build/testdata/formulas/glibc/versions.json`
- Create: `internal/build/testdata/formulas/glibc/2.39/Glibc_llar.gox`

- [ ] **Step 1: Add the build fixture**

Before this step, read `/Users/haolan/.codex/skills/write-formula/SKILL.md` and verify the `.gox` syntax used below against existing formula fixtures in this repository.

Create `internal/build/testdata/formulas/glibc/versions.json`:

```json
{
  "path": "glibc",
  "deps": {}
}
```

Create `internal/build/testdata/formulas/glibc/2.39/Glibc_llar.gox`:

```go
id "glibc"

fromVer "2.39"

onBuild (ctx, proj, out) => {
	dir, err := ctx.outputDir()
	if err != nil {
		out.addErr err
		return
	}
	out.setMetadata "--sysroot=" + dir
}
```

- [ ] **Step 2: Write the failing build test**

Append to `internal/build/build_test.go`:

```go
func TestBuild_SourceLessGlibcDoesNotCloneGithubGlibc(t *testing.T) {
	store := setupTestStore(t)
	b := setupBuilder(t, store, "amd64-linux")
	b.newRepo = func(repoPath string) (vcs.Repo, error) {
		if repoPath == "github.com/glibc" {
			return nil, fmt.Errorf("unexpected source clone for %s", repoPath)
		}
		modPath := strings.TrimPrefix(repoPath, "github.com/")
		return newMockRepo(filepath.Join(testSourceDir, modPath)), nil
	}

	ctx := context.Background()
	mods, err := modules.Load(ctx, module.Version{Path: "glibc", Version: "2.39"}, modules.Options{
		FormulaStore: store,
	})
	if err != nil {
		t.Fatalf("modules.Load: %v", err)
	}

	results, err := b.Build(ctx, mods)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if !strings.HasPrefix(results[0].Metadata, "--sysroot=") {
		t.Fatalf("metadata = %q, want sysroot metadata", results[0].Metadata)
	}
}
```

- [ ] **Step 3: Run the focused test to verify it fails**

Run:

```bash
go test ./internal/build -run TestBuild_SourceLessGlibcDoesNotCloneGithubGlibc
```

Expected: FAIL with `unexpected source clone for github.com/glibc`.

- [ ] **Step 4: Implement official source-less source setup**

Add this helper near other private helpers in `internal/build/build.go`:

```go
func isSourceLessOfficialPackage(modPath string) bool {
	return modPath == "glibc"
}
```

In the `build` closure in `internal/build/build.go`, replace the source clone block:

```go
		// Before we start to build, clone source to tmpSourceDir
		// And switch current dir to it.
		repo, err := b.newRepo(fmt.Sprintf("github.com/%s", mod.Path))
		if err != nil {
			return Result{}, err
		}
		if err := repo.Sync(ctx, mod.Version, "", tmpSourceDir); err != nil {
			return Result{}, err
		}
```

with:

```go
		if !isSourceLessOfficialPackage(mod.Path) {
			// Before we start to build, clone source to tmpSourceDir
			// And switch current dir to it.
			repo, err := b.newRepo(fmt.Sprintf("github.com/%s", mod.Path))
			if err != nil {
				return Result{}, err
			}
			if err := repo.Sync(ctx, mod.Version, "", tmpSourceDir); err != nil {
				return Result{}, err
			}
		}
```

- [ ] **Step 5: Run the focused test**

Run:

```bash
go test ./internal/build -run TestBuild_SourceLessGlibcDoesNotCloneGithubGlibc
```

Expected: PASS. If the local ixgo linker fails with `invalid reference to math/rand/v2.globalRand`, record the blocker.

- [ ] **Step 6: Commit**

```bash
git add internal/build/build.go internal/build/build_test.go internal/build/testdata/formulas/glibc
git commit -m "feat(build): support source-less glibc package"
```

## Task 5: Add Build Helper For Selected `glibc` Sysroot Metadata

**Files:**
- Modify: `internal/build/build.go`
- Modify: `internal/build/build_test.go`

- [ ] **Step 1: Write the failing helper tests**

Append to `internal/build/build_test.go`:

```go
func TestDefaultSysrootMetadataReturnsSelectedGlibcMetadata(t *testing.T) {
	var glibcResult classfile.BuildResult
	glibcResult.SetMetadata("--sysroot=/fake/glibc")

	targets := []*modules.Module{
		{Path: "test/app", Version: "1.0.0"},
		{Path: "glibc", Version: "2.39"},
	}
	results := map[module.Version]classfile.BuildResult{
		{Path: "glibc", Version: "2.39"}: glibcResult,
	}

	got, ok := defaultSysrootMetadata(targets, results)
	if !ok {
		t.Fatal("defaultSysrootMetadata ok = false, want true")
	}
	if got != "--sysroot=/fake/glibc" {
		t.Fatalf("metadata = %q, want --sysroot=/fake/glibc", got)
	}
}

func TestDefaultSysrootMetadataSkipsMissingOrEmptyGlibc(t *testing.T) {
	targets := []*modules.Module{
		{Path: "test/app", Version: "1.0.0"},
		{Path: "glibc", Version: "2.39"},
	}
	if got, ok := defaultSysrootMetadata(targets, nil); ok || got != "" {
		t.Fatalf("missing result: metadata=%q ok=%v, want empty false", got, ok)
	}

	results := map[module.Version]classfile.BuildResult{
		{Path: "glibc", Version: "2.39"}: {},
	}
	if got, ok := defaultSysrootMetadata(targets, results); ok || got != "" {
		t.Fatalf("empty result: metadata=%q ok=%v, want empty false", got, ok)
	}
}
```

- [ ] **Step 2: Run the focused test to verify it fails**

Run:

```bash
go test ./internal/build -run 'TestDefaultSysrootMetadataReturnsSelectedGlibcMetadata|TestDefaultSysrootMetadataSkipsMissingOrEmptyGlibc'
```

Expected: FAIL because `defaultSysrootMetadata` does not exist, or because the ixgo linker blocker appears.

- [ ] **Step 3: Implement selected `glibc` metadata lookup**

Add these helpers near other private helpers in `internal/build/build.go`:

```go
func selectedGlibc(targets []*modules.Module) (module.Version, bool) {
	for _, target := range targets {
		if target.Path == "glibc" {
			return module.Version{Path: target.Path, Version: target.Version}, true
		}
	}
	return module.Version{}, false
}

func defaultSysrootMetadata(targets []*modules.Module, results map[module.Version]classfile.BuildResult) (string, bool) {
	glibc, ok := selectedGlibc(targets)
	if !ok {
		return "", false
	}
	result, ok := results[glibc]
	if !ok {
		return "", false
	}
	metadata := result.Metadata()
	if metadata == "" {
		return "", false
	}
	return metadata, true
}
```

Do not call the helper from the build loop in this task. The tests are in package `build`, so they can verify the unexported helper directly. The next plan will call this helper from middleware/sysroot environment patching.

- [ ] **Step 4: Run the focused test**

Run:

```bash
go test ./internal/build -run 'TestDefaultSysrootMetadataReturnsSelectedGlibcMetadata|TestDefaultSysrootMetadataSkipsMissingOrEmptyGlibc'
```

Expected: PASS, unless the ixgo linker blocker appears.

- [ ] **Step 5: Commit**

```bash
git add internal/build/build.go internal/build/build_test.go
git commit -m "feat(build): identify selected glibc sysroot metadata"
```

## Task 6: Add Build Cache Variant For Selected `glibc`

**Files:**
- Modify: `internal/build/cache.go`
- Modify: `internal/build/build.go`
- Modify: `internal/build/cache_test.go`
- Modify: `internal/build/build_test.go`

- [ ] **Step 1: Write failing cache variant tests**

Append to `internal/build/cache_test.go`:

```go
func TestBuildVariantIncludesGlibcVersion(t *testing.T) {
	got := buildVariant("amd64-linux", "2.39")
	want := "amd64-linux+glibc-2.39"
	if got != want {
		t.Fatalf("buildVariant = %q, want %q", got, want)
	}
}

func TestBuildVariantWithoutGlibcUsesMatrix(t *testing.T) {
	got := buildVariant("arm64-darwin", "")
	want := "arm64-darwin"
	if got != want {
		t.Fatalf("buildVariant = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the focused tests to verify they fail**

Run:

```bash
go test ./internal/build -run 'TestBuildVariantIncludesGlibcVersion|TestBuildVariantWithoutGlibcUsesMatrix'
```

Expected: FAIL because `buildVariant` does not exist.

- [ ] **Step 3: Implement variant helper and use it in cache keys**

Add to `internal/build/cache.go`:

```go
func buildVariant(matrix, glibcVersion string) string {
	if glibcVersion == "" {
		return matrix
	}
	return matrix + "+glibc-" + glibcVersion
}
```

Add this helper to `internal/build/build.go`:

```go
func selectedGlibcVersion(targets []*modules.Module) string {
	for _, target := range targets {
		if target.Path == "glibc" {
			return target.Version
		}
	}
	return ""
}
```

At the start of `Build`, after `rootID` is computed, add:

```go
	variant := buildVariant(b.matrix, selectedGlibcVersion(targets))
```

Replace cache lookups in `internal/build/build.go`:

```go
if entry, ok := cache.get(mod.Version, b.matrix); ok {
```

with:

```go
if entry, ok := cache.get(mod.Version, variant); ok {
```

Replace cache writes:

```go
cache.set(mod.Version, b.matrix, &buildEntry{
```

with:

```go
cache.set(mod.Version, variant, &buildEntry{
```

Replace install dir lookup inside `build`:

```go
dir, _ := b.installDir(mod.Path, mod.Version)
```

with:

```go
dir, _ := b.installDirForVariant(mod.Path, mod.Version, variant)
```

Add this helper to `internal/build/cache.go`:

```go
func (b *Builder) installDirForVariant(modPath, version, variant string) (string, error) {
	escaped, err := module.EscapePath(modPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(b.workspaceDir, fmt.Sprintf("%s@%s-%s", escaped, version, variant)), nil
}
```

Change existing `installDir` in `internal/build/cache.go` to delegate:

```go
func (b *Builder) installDir(modPath, version string) (string, error) {
	return b.installDirForVariant(modPath, version, b.matrix)
}
```

In `internal/build/build.go`, replace calls that need the active build output path with `installDirForVariant(..., variant)`:

```go
installDir, err := b.installDirForVariant(mod.Path, mod.Version, variant)
```

and:

```go
return b.installDirForVariant(m.Path, m.Version, variant)
```

- [ ] **Step 4: Run focused cache tests**

Run:

```bash
go test ./internal/build -run 'TestBuildVariantIncludesGlibcVersion|TestBuildVariantWithoutGlibcUsesMatrix|TestCacheKey|TestInstallDir'
```

Expected: PASS, unless the ixgo linker blocker appears.

- [ ] **Step 5: Commit**

```bash
git add internal/build/cache.go internal/build/build.go internal/build/cache_test.go internal/build/build_test.go
git commit -m "feat(build): include glibc in build variant"
```

## Task 7: Final Verification

**Files:**
- Review all files changed by Tasks 1-6.

- [ ] **Step 1: Format changed Go files**

Run:

```bash
gofmt -w internal/build/buildtarget/target.go internal/build/buildtarget/target_test.go internal/modules/load.go internal/modules/load_test.go internal/build/build.go internal/build/build_test.go internal/build/cache.go internal/build/cache_test.go
```

Expected: command exits 0.

- [ ] **Step 2: Run non-ixgo package checks**

Run:

```bash
go test ./internal/build/buildtarget ./mod/module ./mod/versions ./internal/formula/repo
```

Expected: PASS.

- [ ] **Step 3: Run focused affected tests**

Run:

```bash
go test ./internal/modules ./internal/build
```

Expected: PASS. If the environment fails with `invalid reference to math/rand/v2.globalRand`, record the blocker in the implementation notes and include the exact command output.

- [ ] **Step 4: Run build checks that currently compile in this environment**

Run:

```bash
go build ./x/cmake ./x/autotools ./internal/execbroker ./internal/formula/repo ./mod/module ./mod/versions
```

Expected: PASS.

- [ ] **Step 5: Commit final formatting if needed**

Run:

```bash
git status --short
```

If only formatting or test fixture changes from this plan remain, commit them:

```bash
git add internal/build/buildtarget internal/modules/load.go internal/modules/load_test.go internal/modules/testdata/load/glibc internal/modules/testdata/load/towner/withglibc internal/build/build.go internal/build/build_test.go internal/build/cache.go internal/build/cache_test.go internal/build/testdata/formulas/glibc internal/build/testdata/formulas/test/ctxcheck/1.0.0/Ctxcheck_llar.gox cmd/llar/internal/make.go
git commit -m "chore(build): verify cross compile foundation"
```

Expected: either a commit is created or there are no remaining plan changes to commit.

## Self-Review

Spec coverage:

- Matrix selects target platform: Task 1.
- Cross-Linux hidden `glibc`: Task 2.
- Native/Darwin/no-os skip rules: Task 2.
- `glibc` as single-segment official package: Task 2 fixtures and Task 4 source-less build path.
- `glibc` metadata as sysroot source: Tasks 4 and 5.
- Cache variant includes selected `glibc`: Task 6.

Placeholder scan:

- No task contains banned placeholder instructions.
- Each code-changing step includes exact file paths and code snippets.
- Each test step includes an exact command and expected result.

Type consistency:

- `buildtarget.Platform`, `Parse`, `IsNative`, and `NeedsDefaultGlibc` are defined in Task 1 before Task 2 imports them.
- `Options.MatrixStr`, `Options.HostOS`, and `Options.HostArch` are defined in Task 2 before Task 3 passes `MatrixStr`.
- `buildVariant` and `installDirForVariant` are defined in Task 6 before use in `build.go`.
