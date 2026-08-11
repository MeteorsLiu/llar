package llvm

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNewUsesPreparedPath(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"clang", "clang++", "ld.lld", "ld64.lld", "llvm-ar", "llvm-ranlib", "llvm-nm", "llvm-strip"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("tool"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
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
}
