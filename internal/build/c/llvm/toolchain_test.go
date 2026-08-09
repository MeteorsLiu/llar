package llvm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewUsesPreparedPath(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"clang", "clang++", "llvm-ar", "llvm-ranlib", "llvm-nm", "llvm-strip"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("tool"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)

	toolchain, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if toolchain.CC() != filepath.Join(dir, "clang") {
		t.Fatalf("CC = %q", toolchain.CC())
	}
}
