package internal

import (
	"io"
	"os"
	"reflect"
	"testing"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/build/crosscompile"
	"github.com/goplus/llar/internal/execbroker"
	"github.com/goplus/llar/internal/modules"
)

type testLLVMToolchain struct{}

func (testLLVMToolchain) CC() string       { return "/llvm/bin/clang" }
func (testLLVMToolchain) CXX() string      { return "/llvm/bin/clang++" }
func (testLLVMToolchain) Archiver() string { return "/llvm/bin/llvm-ar" }
func (testLLVMToolchain) Ranlib() string   { return "/llvm/bin/llvm-ranlib" }
func (testLLVMToolchain) NM() string       { return "/llvm/bin/llvm-nm" }
func (testLLVMToolchain) Strip() string    { return "/llvm/bin/llvm-strip" }

func TestMatrixTarget(t *testing.T) {
	matrix := formula.Matrix{Require: map[string][]string{
		"os":   {"linux"},
		"arch": {"arm64"},
	}}
	if targetOS, targetArch := matrixTarget(matrix); targetOS != "linux" || targetArch != "arm64" {
		t.Fatalf("matrixTarget = %s/%s, want linux/arm64", targetOS, targetArch)
	}
}

func TestInjectSysroot(t *testing.T) {
	root := &modules.Module{Path: "owner/root", Version: "v1"}
	dep := &modules.Module{Path: "owner/dep", Version: "v2"}
	sysroot := &modules.Module{Path: "bminor/glibc", Version: "glibc-2.17"}
	root.Deps = []*modules.Module{dep}

	got := injectSysroot([]*modules.Module{root, dep}, sysroot)
	if want := []*modules.Module{root, dep, sysroot}; !reflect.DeepEqual(got, want) {
		t.Fatalf("modules = %+v, want %+v", got, want)
	}
	if want := []*modules.Module{dep, sysroot}; !reflect.DeepEqual(root.Deps, want) {
		t.Fatalf("root deps = %+v, want %+v", root.Deps, want)
	}
	if want := []*modules.Module{sysroot}; !reflect.DeepEqual(dep.Deps, want) {
		t.Fatalf("dep deps = %+v, want %+v", dep.Deps, want)
	}
}

func TestApplyCrossCompilePatch(t *testing.T) {
	req := execbroker.Request{
		Name: "cc",
		Args: []string{"-c", "a.c"},
		Env:  []string{"CFLAGS=-O2"},
	}
	got := applyCrossCompilePatch(req, crosscompile.Patch{
		Name:       "/llvm/bin/clang",
		PrependArg: []string{"--target=aarch64-linux-gnu"},
		AppendArg:  []string{"--sysroot=/sdk"},
		Env:        []string{"CFLAGS=-O2 --sysroot=/sdk"},
	})
	if got.Name != "/llvm/bin/clang" {
		t.Fatalf("Name = %q", got.Name)
	}
	if want := []string{"--target=aarch64-linux-gnu", "-c", "a.c", "--sysroot=/sdk"}; !reflect.DeepEqual(got.Args, want) {
		t.Fatalf("Args = %q, want %q", got.Args, want)
	}
	if want := []string{"CFLAGS=-O2 --sysroot=/sdk"}; !reflect.DeepEqual(got.Env, want) {
		t.Fatalf("Env = %q, want %q", got.Env, want)
	}
}

func TestWrapCrossCompileHooks(t *testing.T) {
	rewriter, err := crosscompile.New("linux", "arm64", testLLVMToolchain{}, "--sysroot=/sdk")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rewriter.Close() })

	var commandName string
	var commandArgs []string
	mod := &modules.Module{Path: "owner/root", Version: "v1"}
	mod.OnBuild = func(*formula.Context) {
		cmd := execbroker.Command("cc", "-c", "a.c")
		commandName = cmd.Path
		commandArgs = append([]string(nil), cmd.Args...)
	}
	wrapCrossCompileHooks([]*modules.Module{mod}, rewriter, io.Discard, io.Discard)
	mod.OnBuild(formula.NewContext(nil, t.TempDir(), "", "", nil))

	if commandName != "/llvm/bin/clang" {
		t.Fatalf("command path = %q", commandName)
	}
	want := []string{"/llvm/bin/clang", "--target=aarch64-linux-gnu", "--sysroot=/sdk", "-c", "a.c"}
	if !reflect.DeepEqual(commandArgs, want) {
		t.Fatalf("command args = %q, want %q", commandArgs, want)
	}
	if cmd := execbroker.Command("cc"); cmd.Path == "/llvm/bin/clang" {
		t.Fatal("cross compile middleware leaked after Formula hook")
	}
}

func TestNewCrossCompileToolchainUsesPreparedPath(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"clang", "clang++", "llvm-ar", "llvm-ranlib", "llvm-nm", "llvm-strip"} {
		path := dir + string(os.PathSeparator) + name
		if err := os.WriteFile(path, []byte("tool"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	toolchain, err := newCrossCompileToolchain()
	if err != nil {
		t.Fatal(err)
	}
	if toolchain.CC() != dir+string(os.PathSeparator)+"clang" {
		t.Fatalf("CC = %q", toolchain.CC())
	}
}
