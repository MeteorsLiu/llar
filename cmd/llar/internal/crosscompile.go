package internal

import (
	"runtime"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/build/c"
	"github.com/goplus/llar/mod/module"
)

func crossCompileTarget(matrix formula.Matrix) (string, bool) {
	targetOS, targetArch := matrixTarget(matrix)
	if targetOS == runtime.GOOS && targetArch == runtime.GOARCH {
		return "", false
	}
	// TODO: Select support across language implementations when another
	// build.Target is added; c.Sysroot currently defines the supported set.
	if _, ok := c.Sysroot(targetOS, targetArch); !ok {
		return "", false
	}
	return targetArch + "-" + targetOS, true
}

func crossCompileSysroot(
	root module.Version,
	matrix formula.Matrix,
) (module.Version, bool) {
	if _, ok := matrix.Require["libc"]; ok {
		return module.Version{}, false
	}
	targetOS, targetArch := matrixTarget(matrix)
	// TODO: Add language-specific bootstrap inputs alongside this C sysroot
	// policy when another build.Target requires its own preparation.
	sysroot, ok := c.Sysroot(targetOS, targetArch)
	if !ok || targetOS == runtime.GOOS && targetArch == runtime.GOARCH || root.Path == sysroot.Path {
		return module.Version{}, false
	}
	return sysroot, true
}

func matrixTarget(matrix formula.Matrix) (targetOS, targetArch string) {
	if values := matrix.Require["os"]; len(values) > 0 {
		targetOS = values[0]
	}
	if values := matrix.Require["arch"]; len(values) > 0 {
		targetArch = values[0]
	}
	return
}
