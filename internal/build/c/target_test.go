package c

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/llar/internal/build"
	"github.com/goplus/llar/mod/module"
)

func fakeToolchain() Toolchain {
	return NewToolchain(
		"/llvm/bin/clang",
		"/llvm/bin/clang++",
		"/llvm/bin/llvm-ar",
		"/llvm/bin/llvm-ranlib",
		"/llvm/bin/llvm-nm",
		"/llvm/bin/llvm-strip",
	)
}

func TestSysroot(t *testing.T) {
	want := module.Version{Path: "bminor/glibc", Version: "glibc-2.17"}
	for _, arch := range []string{"amd64", "arm64"} {
		got, ok := Sysroot("linux", arch)
		if !ok || got != want {
			t.Fatalf("Sysroot(linux, %s) = %+v, %v; want %+v, true", arch, got, ok, want)
		}
	}
	for _, target := range [][2]string{{"darwin", "arm64"}, {"linux", "riscv64"}, {"", "esp32"}} {
		if got, ok := Sysroot(target[0], target[1]); ok {
			t.Fatalf("Sysroot(%q, %q) = %+v, true; want unsupported", target[0], target[1], got)
		}
	}
}

func TestBootstrapTargetOmitsSysroot(t *testing.T) {
	c, err := NewTarget(Config{Matrix: "arm64-linux", Toolchain: fakeToolchain()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	patch := c.Use(build.Command{Name: "cc", Args: []string{"-c", "a.c"}})
	if got, want := patch.PrependArg, []string{"--target=aarch64-linux-gnu"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PrependArg = %q, want %q", got, want)
	}
	if patch := c.Use(build.Command{Name: "pkg-config"}); patch.Env != nil {
		t.Fatalf("pkg-config Patch = %+v, want no sysroot environment", patch)
	}

	patch = c.Use(build.Command{Name: "cmake", Args: []string{"-S", ".", "-B", "build"}})
	data, err := os.ReadFile(strings.TrimPrefix(patch.AppendArg[0], "-DCMAKE_TOOLCHAIN_FILE:FILEPATH="))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "CMAKE_SYSROOT") {
		t.Fatalf("bootstrap CMake toolchain contains CMAKE_SYSROOT:\n%s", data)
	}
}

func TestUseCMakeWritesToolchainLazily(t *testing.T) {
	c := newTestTarget(t)
	if c.toolchainFile != "" || c.tempDir != "" {
		t.Fatalf("New created CMake files: toolchainFile=%q tempDir=%q", c.toolchainFile, c.tempDir)
	}

	c.Use(build.Command{Name: "cmake", Args: []string{"-S", ".", "-B", "build"}})
	path := c.toolchainFile
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{"CMAKE_SYSTEM_NAME", "aarch64", "/llvm/bin/clang", "aarch64-linux-gnu", "/sdk"} {
		if !strings.Contains(content, want) {
			t.Fatalf("toolchain file does not contain %q:\n%s", want, content)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("toolchain file still exists after Close: %v", err)
	}
}

func TestUseCMake(t *testing.T) {
	c := newTestTarget(t)
	patch := c.Use(build.Command{Name: "cmake", Args: []string{"-S", ".", "-B", "build"}})
	if got, want := patch.AppendArg, []string{"-DCMAKE_TOOLCHAIN_FILE:FILEPATH=" + c.toolchainFile}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AppendArg = %q, want %q", got, want)
	}
	patch = c.Use(build.Command{Name: "cmake", Args: []string{"--build", "build"}})
	if len(patch.AppendArg) != 0 {
		t.Fatalf("build Patch = %+v, want no toolchain argument", patch)
	}
	patch = c.Use(build.Command{Name: "cmake", Args: []string{"-S", ".", "--toolchain", "/custom.cmake"}})
	if len(patch.AppendArg) != 0 {
		t.Fatalf("explicit toolchain Patch = %+v", patch)
	}
}

func TestUseDirectCommands(t *testing.T) {
	c := newTestTarget(t)
	patch := c.Use(build.Command{Name: "cc", Args: []string{"-c", "a.c"}})
	if patch.Name != "/llvm/bin/clang" {
		t.Fatalf("Name = %q", patch.Name)
	}
	want := []string{"--target=aarch64-linux-gnu", "--sysroot=/sdk"}
	if !reflect.DeepEqual(patch.PrependArg, want) {
		t.Fatalf("PrependArg = %q, want %q", patch.PrependArg, want)
	}
	patch = c.Use(build.Command{Name: "cc", Args: []string{"--target=custom", "--sysroot=/custom"}})
	if len(patch.PrependArg) != 0 {
		t.Fatalf("explicit compiler flags were duplicated: %q", patch.PrependArg)
	}
	if patch := c.Use(build.Command{Name: filepath.Join("custom", "cc")}); patch.Name != "" {
		t.Fatalf("explicit compiler path was rewritten: %+v", patch)
	}
}

func TestUseAutotools(t *testing.T) {
	c := newTestTarget(t)
	patch := c.Use(build.Command{
		Name: "/src/configure",
		Args: []string{"--build=x86_64-apple-darwin"},
		Env:  []string{"CC=/custom/cc", "CFLAGS=-O2 --target=custom"},
	})
	if got, _ := envValue(patch.Env, "CC"); got != "/custom/cc" {
		t.Fatalf("CC override = %q, want /custom/cc", got)
	}
	if got, _ := envValue(patch.Env, "CFLAGS"); got != "-O2 --target=custom --sysroot=/sdk" {
		t.Fatalf("CFLAGS = %q", got)
	}
	if got, want := patch.AppendArg, []string{"--host=aarch64-linux-gnu"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("AppendArg = %q, want %q", got, want)
	}
}

func TestUsePkgConfig(t *testing.T) {
	c := newTestTarget(t)
	depPaths := strings.Join([]string{"/deps/a/lib/pkgconfig", "/deps/b/lib/pkgconfig"}, string(os.PathListSeparator))
	patch := c.Use(build.Command{Name: "pkg-config", Env: []string{"PKG_CONFIG_PATH=" + depPaths}})
	if got, _ := envValue(patch.Env, "PKG_CONFIG_SYSROOT_DIR"); got != "/sdk" {
		t.Fatalf("PKG_CONFIG_SYSROOT_DIR = %q", got)
	}
	got, _ := envValue(patch.Env, "PKG_CONFIG_LIBDIR")
	for _, want := range []string{"/deps/a/lib/pkgconfig", "/deps/b/lib/pkgconfig", filepath.Join("/sdk", "usr", "lib", "aarch64-linux-gnu", "pkgconfig")} {
		if !slices.Contains(filepath.SplitList(got), want) {
			t.Fatalf("PKG_CONFIG_LIBDIR = %q, want %q", got, want)
		}
	}
	patch = c.Use(build.Command{Name: "pkg-config", Env: []string{"PKG_CONFIG_LIBDIR=/custom"}})
	if got, _ := envValue(patch.Env, "PKG_CONFIG_LIBDIR"); got != "/custom" {
		t.Fatalf("PKG_CONFIG_LIBDIR override = %q, want /custom", got)
	}
}

func newTestTarget(t *testing.T) *Target {
	t.Helper()
	c, err := NewTarget(Config{Matrix: "arm64-linux|shared", Toolchain: fakeToolchain(), Sysroot: "/sdk"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
