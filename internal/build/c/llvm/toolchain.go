package llvm

import (
	"fmt"
	"os/exec"

	"github.com/goplus/llar/internal/build/c"
)

// Toolchain is a prepared LLVM C-family toolchain.
type Toolchain struct {
	c.Toolchain
}

// New prepares an LLVM Toolchain from commands available in PATH.
func New() (*Toolchain, error) {
	find := func(name string) (string, error) {
		path, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("find prepared LLVM command %q: %w", name, err)
		}
		return path, nil
	}
	cc, err := find("clang")
	if err != nil {
		return nil, err
	}
	cxx, err := find("clang++")
	if err != nil {
		return nil, err
	}
	archiver, err := find("llvm-ar")
	if err != nil {
		return nil, err
	}
	ranlib, err := find("llvm-ranlib")
	if err != nil {
		return nil, err
	}
	nm, err := find("llvm-nm")
	if err != nil {
		return nil, err
	}
	strip, err := find("llvm-strip")
	if err != nil {
		return nil, err
	}
	return &Toolchain{Toolchain: c.NewToolchain(cc, cxx, archiver, ranlib, nm, strip)}, nil
}
