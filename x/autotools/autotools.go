// Package autotools wraps the classic configure/make/make-install workflow.
package autotools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/mod/module"
)

// AutoTools drives Autotools-style builds.
type AutoTools struct {
	matrix       formula.Matrix
	workspaceDir string
	sourceDir    string
	buildDir     string
	installDir   string
	env          map[string]string
}

// New returns a ready-to-use AutoTools.
func New(matrix formula.Matrix, workspaceDir, sourceDir, buildDir, installDir string) *AutoTools {
	return &AutoTools{
		matrix:       matrix,
		workspaceDir: workspaceDir,
		sourceDir:    sourceDir,
		buildDir:     buildDir,
		installDir:   installDir,
		env:          make(map[string]string),
	}
}

// Source overrides the source directory.
func (a *AutoTools) Source(dir string) { a.sourceDir = dir }

// Env sets key=value in the build environment.
// The value is recorded in the instance and passed to every spawned command.
func (a *AutoTools) Env(key, value string) {
	a.env[key] = value
}

// Use adds include/lib/pkgconfig paths of a built dependency to the build environment.
func (a *AutoTools) Use(mod module.Version) error {
	escaped, err := module.EscapePath(mod.Path)
	if err != nil {
		return err
	}
	combos := a.matrix.Combinations()
	if len(combos) == 0 {
		return fmt.Errorf("use %s@%s: matrix has no combinations", mod.Path, mod.Version)
	}
	root := filepath.Join(a.workspaceDir, escaped+"-"+combos[0])
	if _, err := os.Stat(root); err != nil {
		return fmt.Errorf("use %s@%s: %w", mod.Path, mod.Version, err)
	}

	includeDir := filepath.Join(root, "include")
	libDir := filepath.Join(root, "lib")
	pkgconfigDir := filepath.Join(libDir, "pkgconfig")

	includeExists := dirExists(includeDir)
	libExists := dirExists(libDir)

	if dirExists(pkgconfigDir) {
		a.prependPath("PKG_CONFIG_PATH", pkgconfigDir)
	}
	a.prependPath("CMAKE_PREFIX_PATH", root)
	if includeExists {
		a.prependPath("CMAKE_INCLUDE_PATH", includeDir)
	}
	if libExists {
		a.prependPath("CMAKE_LIBRARY_PATH", libDir)
	}

	if runtime.GOOS == "windows" {
		if includeExists {
			a.prependPath("INCLUDE", includeDir)
		}
		if libExists {
			a.prependPath("LIB", libDir)
		}
	} else {
		if includeExists {
			a.appendFlag("CPPFLAGS", "-I"+includeDir)
		}
		if libExists {
			a.appendFlag("LDFLAGS", "-L"+libDir)
		}
	}
	return nil
}

// Configure runs <sourceDir>/configure inside buildDir.
// --prefix is prepended automatically when installDir is set.
// Extra flags are appended after --prefix.
func (a *AutoTools) Configure(args ...string) error {
	dir := a.workDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	exe := filepath.Join(a.sourceDir, "configure")
	if dir == "." {
		exe = "./configure"
	}
	flags := make([]string, 0, 1+len(args))
	if a.installDir != "" {
		flags = append(flags, "--prefix="+a.installDir)
	}
	return a.run(exe, append(flags, args...))
}

// Build runs "make" with optional extra arguments.
func (a *AutoTools) Build(args ...string) error {
	return a.run("make", args)
}

// Install runs "make install" with optional extra arguments appended.
func (a *AutoTools) Install(args ...string) error {
	return a.run("make", append([]string{"install"}, args...))
}

// OutputDir returns installDir if set, otherwise buildDir.
func (a *AutoTools) OutputDir() string {
	if a.installDir != "" {
		return a.installDir
	}
	return a.buildDir
}

func (a *AutoTools) workDir() string {
	if a.buildDir == "" {
		return "."
	}
	return a.buildDir
}

func (a *AutoTools) run(name string, args []string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = a.workDir()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = mergeEnv(os.Environ(), a.env)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s in %s: %w", name, cmd.Dir, err)
	}
	return nil
}

// mergeEnv returns base with every key in overrides replaced or appended.
func mergeEnv(base []string, overrides map[string]string) []string {
	idx := make(map[string]int, len(base))
	for i, kv := range base {
		if k, _, ok := strings.Cut(kv, "="); ok {
			idx[k] = i
		}
	}
	for k, v := range overrides {
		if i, ok := idx[k]; ok {
			base[i] = k + "=" + v
		} else {
			base = append(base, k+"="+v)
		}
	}
	return base
}

// getEnv returns the value of key from a.env, falling back to os.Getenv.
func (a *AutoTools) getEnv(key string) string {
	if v, ok := a.env[key]; ok {
		return v
	}
	return os.Getenv(key)
}

// prependPath prepends value to a PATH-style env var in a.env.
func (a *AutoTools) prependPath(key, value string) {
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	if cur := a.getEnv(key); cur != "" {
		value += sep + cur
	}
	a.env[key] = value
}

// appendFlag appends a space-separated flag to an env var in a.env.
func (a *AutoTools) appendFlag(key, flag string) {
	if cur := a.getEnv(key); cur != "" {
		flag = cur + " " + flag
	}
	a.env[key] = flag
}

// dirExists reports whether path exists and is a directory.
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}
