package internal

import (
	"runtime"
	"testing"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/mod/module"
)

func TestMatrixTarget(t *testing.T) {
	matrix := formula.Matrix{Require: map[string][]string{
		"os":   {"linux"},
		"arch": {"arm64"},
	}}
	if targetOS, targetArch := matrixTarget(matrix); targetOS != "linux" || targetArch != "arm64" {
		t.Fatalf("matrixTarget = %s/%s, want linux/arm64", targetOS, targetArch)
	}
}

func TestCrossCompileTarget(t *testing.T) {
	targetArch := "amd64"
	if runtime.GOOS == "linux" && runtime.GOARCH == targetArch {
		targetArch = "arm64"
	}
	matrix := formula.Matrix{Require: map[string][]string{
		"os":   {"linux"},
		"arch": {targetArch},
	}}
	if got, ok := crossCompileTarget(matrix); !ok || got != targetArch+"-linux" {
		t.Fatalf("crossCompileTarget = %q, %v; want %q, true", got, ok, targetArch+"-linux")
	}
	matrix.Require["libc"] = []string{"glibc-2.13"}
	if got, ok := crossCompileTarget(matrix); !ok || got != targetArch+"-linux" {
		t.Fatalf("crossCompileTarget with libc = %q, %v; want %q, true", got, ok, targetArch+"-linux")
	}
}

func TestCrossCompileSysrootSkipsDefaultForLibcRequirement(t *testing.T) {
	targetArch := "amd64"
	if runtime.GOOS == "linux" && runtime.GOARCH == targetArch {
		targetArch = "arm64"
	}
	matrix := formula.Matrix{Require: map[string][]string{
		"os":   {"linux"},
		"arch": {targetArch},
	}}
	root := module.Version{Path: "owner/root", Version: "v1"}

	if _, ok := crossCompileSysroot(root, matrix); !ok {
		t.Fatal("crossCompileSysroot did not select the default sysroot")
	}
	matrix.Require["libc"] = nil
	if got, ok := crossCompileSysroot(root, matrix); ok || got != (module.Version{}) {
		t.Fatalf("crossCompileSysroot = %+v, %v; want no default sysroot", got, ok)
	}
}
