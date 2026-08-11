package build

import (
	"context"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"testing/fstest"

	classfile "github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/build/c"
	"github.com/goplus/llar/internal/execbroker"
	internalformula "github.com/goplus/llar/internal/formula"
	"github.com/goplus/llar/internal/modules"
	"github.com/goplus/llar/internal/vcs"
)

func newCTarget(t *testing.T, targetOS, targetArch string) *c.Target {
	t.Helper()
	triple := "x86_64-linux-gnu"
	if targetArch == "arm64" {
		triple = "aarch64-linux-gnu"
	}
	target, err := c.NewTarget(c.Config{
		Matrix: targetArch + "-" + targetOS,
		Toolchain: c.NewToolchain(
			[]string{"/toolchain/cc", "--target=" + triple, "--sysroot=/sdk"},
			[]string{"/toolchain/c++", "--target=" + triple, "--sysroot=/sdk"},
			[]string{"/toolchain/ld.lld"},
			"/toolchain/ar",
			"/toolchain/ranlib",
			"/toolchain/nm",
			"/toolchain/strip",
		),
		Sysroot: "/sdk",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = target.Close() })
	return target
}

func TestTargetMiddleware(t *testing.T) {
	target := newCTarget(t, "linux", "arm64")
	got := targetMiddleware(target)(execbroker.Request{
		Name: "cc",
		Args: []string{"-c", "a.c"},
		Env:  []string{"CFLAGS=-O2"},
		Dir:  "/src",
	})
	if got.Name != "/toolchain/cc" {
		t.Fatalf("Name = %q", got.Name)
	}
	if want := []string{"--target=aarch64-linux-gnu", "--sysroot=/sdk", "-c", "a.c"}; !reflect.DeepEqual(got.Args, want) {
		t.Fatalf("Args = %q, want %q", got.Args, want)
	}
	if want := []string{"CFLAGS=-O2"}; !reflect.DeepEqual(got.Env, want) {
		t.Fatalf("Env = %q, want %q", got.Env, want)
	}
}

func TestBuildAppliesTargetToFormulaCommands(t *testing.T) {
	store := setupTestStore(t)
	targetOS, targetArch := "linux", "amd64"
	if runtime.GOOS == targetOS && runtime.GOARCH == targetArch {
		targetArch = "arm64"
	}
	target := newCTarget(t, targetOS, targetArch)
	builder, err := NewBuilder(Options{
		Store: store,
		Matrix: classfile.Matrix{Require: map[string][]string{
			"os":   {targetOS},
			"arch": {targetArch},
		}},
		WorkspaceDir: t.TempDir(),
		Target:       target,
	})
	if err != nil {
		t.Fatal(err)
	}
	builder.newRepo = func(string) (vcs.Repo, error) {
		return newMockRepo(filepath.Join(testSourceDir, "test", "liba")), nil
	}
	t.Setenv("PATH", "")

	var commandName string
	root := &modules.Module{
		Formula: &internalformula.Formula{OnBuild: func(*classfile.Context) {
			commandName = execbroker.Command("cc", "-c", "a.c").Args[0]
		}},
		FS:      fstest.MapFS{"README": {Data: []byte("test")}},
		Path:    "test/liba",
		Version: "1.0.0",
	}
	if _, err := builder.Build(context.Background(), []*modules.Module{root}); err != nil {
		t.Fatal(err)
	}
	if commandName != "/toolchain/cc" {
		t.Fatalf("command name = %q, want /toolchain/cc", commandName)
	}
}
