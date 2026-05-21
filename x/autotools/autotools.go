// Package autotools wraps the classic configure/make/make-install workflow.
package autotools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/goplus/llar/internal/execbroker"
)

// AutoTools drives Autotools-style builds.
type AutoTools struct {
	sourceDir  string
	buildDir   string
	installDir string
	sysroot    string
}

// New returns a ready-to-use AutoTools.
func New(sourceDir, buildDir, installDir string) *AutoTools {
	return &AutoTools{
		sourceDir:  sourceDir,
		buildDir:   buildDir,
		installDir: installDir,
	}
}

// Source overrides the source directory.
func (a *AutoTools) Source(dir string) { a.sourceDir = dir }

// Sysroot overrides the build default sysroot metadata for this AutoTools instance.
func (a *AutoTools) Sysroot(metadata string) { a.sysroot = metadata }

// Use configures the process environment so that compilers and build tools
// find headers, libraries and pkg-config files from a non-system dependency
// installed at root.
func (a *AutoTools) Use(root string) {
	includeDir := filepath.Join(root, "include")
	libDir := filepath.Join(root, "lib")
	pkgconfigDir := filepath.Join(libDir, "pkgconfig")

	if _, err := os.Stat(pkgconfigDir); err == nil {
		prependPath("PKG_CONFIG_PATH", pkgconfigDir)
	}
	prependPath("CMAKE_PREFIX_PATH", root)
	if _, err := os.Stat(includeDir); err == nil {
		prependPath("CMAKE_INCLUDE_PATH", includeDir)
	}
	if _, err := os.Stat(libDir); err == nil {
		prependPath("CMAKE_LIBRARY_PATH", libDir)
	}

	if runtime.GOOS == "windows" {
		if _, err := os.Stat(includeDir); err == nil {
			prependPath("INCLUDE", includeDir)
		}
		if _, err := os.Stat(libDir); err == nil {
			prependPath("LIB", libDir)
		}
	} else {
		if _, err := os.Stat(includeDir); err == nil {
			appendFlag("CPPFLAGS", "-I"+includeDir)
		}
		if _, err := os.Stat(libDir); err == nil {
			appendFlag("LDFLAGS", "-L"+libDir)
		}
	}
}

// Configure runs the configure script from sourceDir in the build directory.
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
	env := os.Environ()
	if a.sysroot != "" {
		env = setEnv(env, "LLAR_EXECBROKER_SYSROOT", a.sysroot)
	}
	cmd := execbroker.CommandEnv(env, name, args...)
	if a.sysroot != "" {
		cmd.Env = unsetEnv(cmd.Env, "LLAR_EXECBROKER_SYSROOT")
		for _, key := range []string{"CPPFLAGS", "CFLAGS", "CXXFLAGS", "LDFLAGS"} {
			cmd.Env = setEnv(cmd.Env, key, appendFlagValue(envValue(cmd.Env, key), a.sysroot))
		}
	}
	cmd.Dir = a.workDir()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// prependPath prepends value to a PATH-style env var.
func prependPath(key, value string) {
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	if cur := os.Getenv(key); cur != "" {
		value += sep + cur
	}
	os.Setenv(key, value)
}

// appendFlag appends a space-separated flag to an env var.
func appendFlag(key, flag string) {
	if cur := os.Getenv(key); cur != "" {
		flag = cur + " " + flag
	}
	os.Setenv(key, flag)
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix)
		}
	}
	return ""
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i := range env {
		if strings.HasPrefix(env[i], prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func unsetEnv(env []string, key string) []string {
	prefix := key + "="
	out := env[:0]
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			out = append(out, item)
		}
	}
	return out
}

func appendFlagValue(current, flag string) string {
	if current == "" {
		return flag
	}
	return current + " " + flag
}
