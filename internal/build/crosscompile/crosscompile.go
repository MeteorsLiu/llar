package crosscompile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goplus/llar/mod/module"
	ccmetadata "github.com/goplus/llar/x/metadata/cc"
)

const (
	linuxSysrootPath    = "bminor/glibc"
	linuxSysrootVersion = "glibc-2.17"
)

// Toolchain supplies prepared C-family tools for the build host.
type Toolchain interface {
	CC() string
	CXX() string
	Archiver() string
	Ranlib() string
	NM() string
	Strip() string
}

// Command describes a command before cross-compilation defaults are applied.
type Command struct {
	Name string
	Args []string
	Env  []string
	Dir  string
}

// Patch contains the changes required for one command.
type Patch struct {
	Name       string
	PrependArg []string
	AppendArg  []string
	Env        []string
}

// CrossCompile contains prepared command-rewrite facts for one target.
type CrossCompile struct {
	systemProcessor string
	targetTriple    string
	sysroot         string
	toolchain       Toolchain
	toolchainFile   string
	tempDir         string
}

// Sysroot returns the fixed compatibility sysroot Formula for a built-in
// Linux target.
func Sysroot(targetOS, targetArch string) (module.Version, bool) {
	if targetOS != "linux" {
		return module.Version{}, false
	}
	switch targetArch {
	case "amd64", "arm64":
		return module.Version{Path: linuxSysrootPath, Version: linuxSysrootVersion}, true
	default:
		return module.Version{}, false
	}
}

// New prepares command rewriting for a built-in Linux target.
func New(targetOS, targetArch string, toolchain Toolchain, sysrootMetadata string) (*CrossCompile, error) {
	triple, processor, err := linuxTarget(targetOS, targetArch)
	if err != nil {
		return nil, err
	}
	if err := validateToolchain(toolchain); err != nil {
		return nil, fmt.Errorf("prepare cross compiler for %s/%s: %w", targetOS, targetArch, err)
	}
	metadata, err := ccmetadata.Parse(sysrootMetadata)
	if err != nil {
		return nil, fmt.Errorf("parse sysroot metadata for %s/%s: %w", targetOS, targetArch, err)
	}
	if metadata.Sysroot() == "" {
		return nil, fmt.Errorf("sysroot metadata for %s/%s has no sysroot", targetOS, targetArch)
	}

	tempDir, err := os.MkdirTemp("", "llar-crosscompile-*")
	if err != nil {
		return nil, fmt.Errorf("prepare CMake toolchain for %s/%s: %w", targetOS, targetArch, err)
	}
	c := &CrossCompile{
		systemProcessor: processor,
		targetTriple:    triple,
		sysroot:         metadata.Sysroot(),
		toolchain:       toolchain,
		tempDir:         tempDir,
	}
	c.toolchainFile = filepath.Join(tempDir, "toolchain.cmake")
	if err := os.WriteFile(c.toolchainFile, []byte(c.cmakeToolchain()), 0o600); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("prepare CMake toolchain for %s/%s: %w", targetOS, targetArch, err)
	}
	return c, nil
}

// Close removes generated build-system configuration.
func (c *CrossCompile) Close() error {
	return os.RemoveAll(c.tempDir)
}

// Use returns cross-compilation defaults for cmd. Explicit Formula settings
// are preserved.
func (c *CrossCompile) Use(cmd Command) Patch {
	base := filepath.Base(cmd.Name)
	if base == "configure" {
		return c.autotoolsPatch(cmd)
	}
	if filepath.Base(cmd.Name) != cmd.Name {
		return Patch{}
	}

	switch base {
	case "cmake":
		if isCMakeConfigure(cmd.Args) && !hasCMakeToolchain(cmd.Args) {
			return Patch{AppendArg: []string{"-DCMAKE_TOOLCHAIN_FILE:FILEPATH=" + c.toolchainFile}}
		}
	case "pkg-config":
		return c.pkgConfigPatch(cmd.Env)
	case "cc", "gcc", "clang":
		return c.compilerPatch(c.toolchain.CC(), cmd.Args)
	case "c++", "g++", "clang++":
		return c.compilerPatch(c.toolchain.CXX(), cmd.Args)
	case "ar", "llvm-ar":
		return Patch{Name: c.toolchain.Archiver()}
	case "ranlib", "llvm-ranlib":
		return Patch{Name: c.toolchain.Ranlib()}
	case "nm", "llvm-nm":
		return Patch{Name: c.toolchain.NM()}
	case "strip", "llvm-strip":
		return Patch{Name: c.toolchain.Strip()}
	}
	return Patch{}
}

func (c *CrossCompile) compilerPatch(name string, args []string) Patch {
	flags := missingCompilerFlags(args, c.targetTriple, c.sysroot)
	return Patch{Name: name, PrependArg: flags}
}

func (c *CrossCompile) autotoolsPatch(cmd Command) Patch {
	env := append([]string(nil), cmd.Env...)
	env = setMissingEnv(env, "CC", c.toolchain.CC())
	env = setMissingEnv(env, "CXX", c.toolchain.CXX())
	env = setMissingEnv(env, "AR", c.toolchain.Archiver())
	env = setMissingEnv(env, "RANLIB", c.toolchain.Ranlib())
	env = setMissingEnv(env, "NM", c.toolchain.NM())
	env = setMissingEnv(env, "STRIP", c.toolchain.Strip())

	compilerFlags := []string{"--target=" + c.targetTriple, "--sysroot=" + c.sysroot}
	env = setEnvFlags(env, "CFLAGS", compilerFlags)
	env = setEnvFlags(env, "CXXFLAGS", compilerFlags)
	env = setEnvFlags(env, "CPPFLAGS", []string{"--sysroot=" + c.sysroot})
	env = setEnvFlags(env, "LDFLAGS", compilerFlags)

	var args []string
	if !hasOption(cmd.Args, "--host") {
		args = append(args, "--host="+c.targetTriple)
	}
	return Patch{AppendArg: args, Env: env}
}

func (c *CrossCompile) pkgConfigPatch(commandEnv []string) Patch {
	env := append([]string(nil), commandEnv...)
	env = setMissingEnv(env, "PKG_CONFIG_SYSROOT_DIR", c.sysroot)
	libDirs, _ := envValue(env, "PKG_CONFIG_PATH")
	paths := filepath.SplitList(libDirs)
	paths = append(paths,
		filepath.Join(c.sysroot, "usr", "lib", c.targetTriple, "pkgconfig"),
		filepath.Join(c.sysroot, "usr", "lib64", "pkgconfig"),
		filepath.Join(c.sysroot, "usr", "lib", "pkgconfig"),
		filepath.Join(c.sysroot, "usr", "share", "pkgconfig"),
	)
	env = setMissingEnv(env, "PKG_CONFIG_LIBDIR", strings.Join(paths, string(os.PathListSeparator)))
	return Patch{Env: env}
}

func (c *CrossCompile) cmakeToolchain() string {
	values := [][2]string{
		{"CMAKE_SYSTEM_NAME", "Linux"},
		{"CMAKE_SYSTEM_PROCESSOR", c.systemProcessor},
		{"CMAKE_SYSROOT", c.sysroot},
		{"CMAKE_C_COMPILER", c.toolchain.CC()},
		{"CMAKE_CXX_COMPILER", c.toolchain.CXX()},
		{"CMAKE_AR", c.toolchain.Archiver()},
		{"CMAKE_RANLIB", c.toolchain.Ranlib()},
		{"CMAKE_NM", c.toolchain.NM()},
		{"CMAKE_STRIP", c.toolchain.Strip()},
		{"CMAKE_C_COMPILER_TARGET", c.targetTriple},
		{"CMAKE_CXX_COMPILER_TARGET", c.targetTriple},
		{"CMAKE_FIND_ROOT_PATH_MODE_PROGRAM", "NEVER"},
		{"CMAKE_FIND_ROOT_PATH_MODE_LIBRARY", "ONLY"},
		{"CMAKE_FIND_ROOT_PATH_MODE_INCLUDE", "ONLY"},
		{"CMAKE_FIND_ROOT_PATH_MODE_PACKAGE", "ONLY"},
	}
	var out strings.Builder
	for _, value := range values {
		fmt.Fprintf(&out, "if(NOT DEFINED %s)\n  set(%s \"%s\")\nendif()\n", value[0], value[0], cmakeEscape(value[1]))
	}
	return out.String()
}

func linuxTarget(targetOS, targetArch string) (triple, processor string, err error) {
	if targetOS != "linux" {
		return "", "", fmt.Errorf("unsupported cross compile target %s/%s", targetOS, targetArch)
	}
	switch targetArch {
	case "amd64":
		return "x86_64-linux-gnu", "x86_64", nil
	case "arm64":
		return "aarch64-linux-gnu", "aarch64", nil
	default:
		return "", "", fmt.Errorf("unsupported cross compile target %s/%s", targetOS, targetArch)
	}
}

func validateToolchain(toolchain Toolchain) error {
	if toolchain == nil {
		return fmt.Errorf("toolchain is required")
	}
	tools := []struct {
		name string
		path string
	}{
		{"CC", toolchain.CC()},
		{"CXX", toolchain.CXX()},
		{"archiver", toolchain.Archiver()},
		{"ranlib", toolchain.Ranlib()},
		{"nm", toolchain.NM()},
		{"strip", toolchain.Strip()},
	}
	for _, tool := range tools {
		if tool.path == "" {
			return fmt.Errorf("%s is required", tool.name)
		}
	}
	return nil
}

func isCMakeConfigure(args []string) bool {
	if len(args) == 0 {
		return true
	}
	switch args[0] {
	case "--build", "--install", "--open", "--workflow":
		return false
	default:
		return true
	}
}

func hasCMakeToolchain(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-DCMAKE_TOOLCHAIN_FILE") || arg == "--toolchain" || strings.HasPrefix(arg, "--toolchain=") {
			return true
		}
	}
	return false
}

func missingCompilerFlags(args []string, triple, sysroot string) []string {
	var flags []string
	if !hasTargetFlag(args) {
		flags = append(flags, "--target="+triple)
	}
	if !hasSysrootFlag(args) {
		flags = append(flags, "--sysroot="+sysroot)
	}
	return flags
}

func hasTargetFlag(args []string) bool {
	return hasFlag(args, "--target", "-target")
}

func hasSysrootFlag(args []string) bool {
	return hasFlag(args, "--sysroot", "-sysroot", "-isysroot")
}

func hasFlag(args []string, names ...string) bool {
	for _, arg := range args {
		for _, name := range names {
			if arg == name || strings.HasPrefix(arg, name+"=") || name == "-isysroot" && strings.HasPrefix(arg, name) {
				return true
			}
		}
	}
	return false
}

func hasOption(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

func envValue(env []string, key string) (string, bool) {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix), true
		}
	}
	return "", false
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func setMissingEnv(env []string, key, value string) []string {
	if _, ok := envValue(env, key); ok {
		return env
	}
	return append(env, key+"="+value)
}

func setEnvFlags(env []string, key string, defaults []string) []string {
	value, _ := envValue(env, key)
	original := value
	args := strings.Fields(value)
	for _, flag := range defaults {
		if strings.HasPrefix(flag, "--target=") && hasTargetFlag(args) {
			continue
		}
		if strings.HasPrefix(flag, "--sysroot=") && hasSysrootFlag(args) {
			continue
		}
		if value != "" {
			value += " "
		}
		value += flag
		args = append(args, flag)
	}
	if value != original {
		return setEnv(env, key, value)
	}
	return env
}

func cmakeEscape(value string) string {
	value = strings.ReplaceAll(value, "\\", "/")
	return strings.ReplaceAll(value, "\"", "\\\"")
}
