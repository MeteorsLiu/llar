package llvm

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestNewUsesPreparedPath(t *testing.T) {
	dir := fakeToolDir(t)
	t.Setenv("PATH", dir)

	toolchain, err := New(Config{OS: "linux", Arch: "arm64", Sysroot: "/sdk"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := toolchain.CC(), []string{filepath.Join(dir, "clang"), "--target=aarch64-linux-gnu", "-fuse-ld=lld", "--sysroot=/sdk"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CC = %q, want %q", got, want)
	}
	if got, want := toolchain.Linker(), []string{filepath.Join(dir, "ld.lld")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Linker = %q, want %q", got, want)
	}

	toolchain, err = New(Config{OS: "darwin", Arch: "amd64", Sysroot: "/sdk"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := toolchain.CC(), []string{filepath.Join(dir, "clang"), "--target=x86_64-apple-macos10.13", "-fuse-ld=lld", "-isysroot/sdk"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Darwin CC = %q, want %q", got, want)
	}
	if got, want := toolchain.Linker(), []string{filepath.Join(dir, "ld64.lld")}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Darwin Linker = %q, want %q", got, want)
	}

	toolchain, err = New(Config{OS: "linux", Arch: "amd64"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := toolchain.CC(), []string{filepath.Join(dir, "clang"), "--target=x86_64-linux-gnu", "-fuse-ld=lld"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Linux amd64 CC = %q, want %q", got, want)
	}

	toolchain, err = New(Config{OS: "darwin", Arch: "arm64"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := toolchain.CC(), []string{filepath.Join(dir, "clang"), "--target=arm64-apple-macos11.0", "-fuse-ld=lld"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Darwin arm64 CC = %q, want %q", got, want)
	}
}

func TestNewErrors(t *testing.T) {
	if _, err := New(Config{OS: "plan9", Arch: "amd64"}); err == nil || !strings.Contains(err.Error(), "unsupported LLVM target") {
		t.Fatalf("New unsupported target error = %v", err)
	}

	for _, missing := range []string{"clang", "clang++", "ld.lld", "llvm-ar", "llvm-ranlib", "llvm-nm", "llvm-strip"} {
		t.Run(missing, func(t *testing.T) {
			dir := fakeToolDir(t)
			if err := os.Remove(filepath.Join(dir, missing)); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir)
			_, err := New(Config{OS: "linux", Arch: "arm64"})
			if err == nil || !strings.Contains(err.Error(), `find prepared LLVM command "`+missing+`"`) {
				t.Fatalf("New missing %s error = %v", missing, err)
			}
		})
	}
}

func fakeToolDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"clang", "clang++", "ld.lld", "ld64.lld", "llvm-ar", "llvm-ranlib", "llvm-nm", "llvm-strip"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("tool"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
