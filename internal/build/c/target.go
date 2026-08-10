package c

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goplus/llar/internal/build"
	"github.com/goplus/llar/mod/module"
)

const (
	linuxSysrootPath     = "bminor/glibc"
	linuxSysrootVersion  = "glibc-2.17"
	darwinSysrootPath    = "joseluisq/macosx-sdks"
	darwinSysrootVersion = "14.5"
)

// Config contains the facts required to prepare a C target.
type Config struct {
	Matrix    string
	Toolchain Toolchain
	Sysroot   string
}

// Target contains prepared C command defaults for one build target.
type Target struct {
	target           string
	systemName       string
	systemProcessor  string
	targetTriple     string
	deploymentTarget string
	sysroot          string
	toolchain        Toolchain
	toolchainFile    string
	tempDir          string
}

// Sysroot returns the fixed compatibility sysroot Formula for a built-in C
// target.
func Sysroot(targetOS, targetArch string) (module.Version, bool) {
	switch targetOS {
	case "linux":
		switch targetArch {
		case "amd64", "arm64":
			return module.Version{Path: linuxSysrootPath, Version: linuxSysrootVersion}, true
		}
	case "darwin":
		switch targetArch {
		case "amd64", "arm64":
			return module.Version{Path: darwinSysrootPath, Version: darwinSysrootVersion}, true
		}
	}
	return module.Version{}, false
}

// NewTarget prepares C command defaults from config.
func NewTarget(config Config) (*Target, error) {
	target := config.Matrix
	if value, _, ok := strings.Cut(config.Matrix, "|"); ok {
		target = value
	}
	var systemName, processor, triple, deploymentTarget string
	switch target {
	case "amd64-linux":
		systemName = "Linux"
		processor = "x86_64"
		triple = "x86_64-linux-gnu"
	case "arm64-linux":
		systemName = "Linux"
		processor = "aarch64"
		triple = "aarch64-linux-gnu"
	case "amd64-darwin":
		systemName = "Darwin"
		processor = "x86_64"
		triple = "x86_64-apple-darwin"
		deploymentTarget = "10.13"
	case "arm64-darwin":
		systemName = "Darwin"
		processor = "arm64"
		triple = "arm64-apple-darwin"
		deploymentTarget = "11.0"
	default:
		return nil, fmt.Errorf("unsupported C target %s", target)
	}
	if err := validateToolchain(config.Toolchain); err != nil {
		return nil, fmt.Errorf("prepare C target %s: %w", target, err)
	}

	return &Target{
		target:           target,
		systemName:       systemName,
		systemProcessor:  processor,
		targetTriple:     triple,
		deploymentTarget: deploymentTarget,
		sysroot:          config.Sysroot,
		toolchain:        config.Toolchain,
	}, nil
}

// Close removes generated build-system configuration.
func (c *Target) Close() error {
	if c.tempDir == "" {
		return nil
	}
	return os.RemoveAll(c.tempDir)
}

// Use returns C target defaults for cmd. Explicit Formula settings are
// preserved.
func (c *Target) Use(cmd build.Command) build.Patch {
	base := filepath.Base(cmd.Name)
	if base == "configure" {
		return c.autotoolsPatch(cmd)
	}
	if filepath.Base(cmd.Name) != cmd.Name {
		return build.Patch{}
	}

	switch base {
	case "cmake":
		if isCMakeConfigure(cmd.Args) && !hasCMakeToolchain(cmd.Args) {
			if c.toolchainFile == "" {
				tempDir, err := os.MkdirTemp("", "llar-c-target-*")
				if err != nil {
					panic(fmt.Errorf("prepare CMake toolchain for %s: %w", c.target, err))
				}
				toolchainFile := filepath.Join(tempDir, "toolchain.cmake")
				if err := os.WriteFile(toolchainFile, []byte(c.cmakeToolchain()), 0o600); err != nil {
					_ = os.RemoveAll(tempDir)
					panic(fmt.Errorf("prepare CMake toolchain for %s: %w", c.target, err))
				}
				c.tempDir = tempDir
				c.toolchainFile = toolchainFile
			}
			return build.Patch{AppendArg: []string{"-DCMAKE_TOOLCHAIN_FILE:FILEPATH=" + c.toolchainFile}}
		}
	case "pkg-config":
		return c.pkgConfigPatch(cmd.Env)
	case "cc", "gcc", "clang":
		return c.compilerPatch(c.toolchain.CC(), cmd.Args)
	case "c++", "g++", "clang++":
		return c.compilerPatch(c.toolchain.CXX(), cmd.Args)
	case "ar", "llvm-ar":
		return build.Patch{Name: c.toolchain.Archiver()}
	case "ranlib", "llvm-ranlib":
		return build.Patch{Name: c.toolchain.Ranlib()}
	case "nm", "llvm-nm":
		return build.Patch{Name: c.toolchain.NM()}
	case "strip", "llvm-strip":
		return build.Patch{Name: c.toolchain.Strip()}
	}
	return build.Patch{}
}

func (c *Target) compilerPatch(name string, args []string) build.Patch {
	linker := true
	for _, arg := range args {
		switch arg {
		case "-c", "-E", "-S":
			linker = false
		}
	}
	flags := missingCompilerFlags(args, c.compilerFlags(linker))
	return build.Patch{Name: name, PrependArg: flags}
}

func (c *Target) compilerFlags(linker bool) []string {
	flags := []string{"--target=" + c.targetTriple}
	if c.sysroot != "" {
		if c.systemName == "Darwin" {
			flags = append(flags, "-isysroot"+c.sysroot)
		} else {
			flags = append(flags, "--sysroot="+c.sysroot)
		}
	}
	if c.deploymentTarget != "" {
		flags = append(flags, "-mmacosx-version-min="+c.deploymentTarget)
	}
	if linker && c.systemName == "Darwin" {
		flags = append(flags, "-fuse-ld=lld")
	}
	return flags
}

func (c *Target) autotoolsPatch(cmd build.Command) build.Patch {
	env := append([]string(nil), cmd.Env...)
	env = setMissingEnv(env, "CC", c.toolchain.CC())
	env = setMissingEnv(env, "CXX", c.toolchain.CXX())
	env = setMissingEnv(env, "AR", c.toolchain.Archiver())
	env = setMissingEnv(env, "RANLIB", c.toolchain.Ranlib())
	env = setMissingEnv(env, "NM", c.toolchain.NM())
	env = setMissingEnv(env, "STRIP", c.toolchain.Strip())

	compilerFlags := c.compilerFlags(false)
	env = setEnvFlags(env, "CFLAGS", compilerFlags)
	env = setEnvFlags(env, "CXXFLAGS", compilerFlags)
	if c.sysroot != "" {
		sysrootFlag := "--sysroot=" + c.sysroot
		if c.systemName == "Darwin" {
			sysrootFlag = "-isysroot" + c.sysroot
		}
		env = setEnvFlags(env, "CPPFLAGS", []string{sysrootFlag})
	}
	env = setEnvFlags(env, "LDFLAGS", c.compilerFlags(true))

	var args []string
	if !hasOption(cmd.Args, "--host") {
		args = append(args, "--host="+c.targetTriple)
	}
	return build.Patch{AppendArg: args, Env: env}
}

func (c *Target) pkgConfigPatch(commandEnv []string) build.Patch {
	if c.sysroot == "" {
		return build.Patch{}
	}
	env := append([]string(nil), commandEnv...)
	env = setMissingEnv(env, "PKG_CONFIG_SYSROOT_DIR", c.sysroot)
	libDirs, _ := envValue(env, "PKG_CONFIG_PATH")
	paths := filepath.SplitList(libDirs)
	if c.systemName == "Linux" {
		paths = append(paths, filepath.Join(c.sysroot, "usr", "lib", c.targetTriple, "pkgconfig"))
	}
	paths = append(paths,
		filepath.Join(c.sysroot, "usr", "lib64", "pkgconfig"),
		filepath.Join(c.sysroot, "usr", "lib", "pkgconfig"),
		filepath.Join(c.sysroot, "usr", "share", "pkgconfig"),
	)
	env = setMissingEnv(env, "PKG_CONFIG_LIBDIR", strings.Join(paths, string(os.PathListSeparator)))
	return build.Patch{Env: env}
}

func (c *Target) cmakeToolchain() string {
	values := [][2]string{
		{"CMAKE_SYSTEM_NAME", c.systemName},
		{"CMAKE_SYSTEM_PROCESSOR", c.systemProcessor},
	}
	if c.systemName == "Darwin" {
		values = append(values,
			[2]string{"CMAKE_OSX_ARCHITECTURES", c.systemProcessor},
			[2]string{"CMAKE_OSX_DEPLOYMENT_TARGET", c.deploymentTarget},
		)
		if c.sysroot != "" {
			values = append(values, [2]string{"CMAKE_OSX_SYSROOT", c.sysroot})
		}
	} else if c.sysroot != "" {
		values = append(values, [2]string{"CMAKE_SYSROOT", c.sysroot})
	}
	values = append(values,
		[2]string{"CMAKE_C_COMPILER", c.toolchain.CC()},
		[2]string{"CMAKE_CXX_COMPILER", c.toolchain.CXX()},
		[2]string{"CMAKE_AR", c.toolchain.Archiver()},
		[2]string{"CMAKE_RANLIB", c.toolchain.Ranlib()},
		[2]string{"CMAKE_NM", c.toolchain.NM()},
		[2]string{"CMAKE_STRIP", c.toolchain.Strip()},
		[2]string{"CMAKE_C_COMPILER_TARGET", c.targetTriple},
		[2]string{"CMAKE_CXX_COMPILER_TARGET", c.targetTriple},
	)
	if c.systemName == "Darwin" {
		values = append(values,
			[2]string{"CMAKE_EXE_LINKER_FLAGS_INIT", "-fuse-ld=lld"},
			[2]string{"CMAKE_SHARED_LINKER_FLAGS_INIT", "-fuse-ld=lld"},
			[2]string{"CMAKE_MODULE_LINKER_FLAGS_INIT", "-fuse-ld=lld"},
		)
	}
	if c.sysroot != "" {
		values = append(values,
			[2]string{"CMAKE_FIND_ROOT_PATH_MODE_PROGRAM", "NEVER"},
			[2]string{"CMAKE_FIND_ROOT_PATH_MODE_LIBRARY", "ONLY"},
			[2]string{"CMAKE_FIND_ROOT_PATH_MODE_INCLUDE", "ONLY"},
			[2]string{"CMAKE_FIND_ROOT_PATH_MODE_PACKAGE", "ONLY"},
		)
	}
	var out strings.Builder
	for _, value := range values {
		fmt.Fprintf(&out, "if(NOT DEFINED %s)\n  set(%s \"%s\")\nendif()\n", value[0], value[0], cmakeEscape(value[1]))
	}
	return out.String()
}

func validateToolchain(toolchain Toolchain) error {
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

func missingCompilerFlags(args, defaults []string) []string {
	var flags []string
	for _, flag := range defaults {
		if strings.HasPrefix(flag, "--target=") && hasTargetFlag(args) {
			continue
		}
		if (strings.HasPrefix(flag, "--sysroot=") || strings.HasPrefix(flag, "-isysroot")) && hasSysrootFlag(args) {
			continue
		}
		if strings.HasPrefix(flag, "-mmacosx-version-min=") && hasFlag(args, "-mmacosx-version-min") {
			continue
		}
		if strings.HasPrefix(flag, "-fuse-ld=") && hasFlag(args, "-fuse-ld") {
			continue
		}
		flags = append(flags, flag)
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
		if (strings.HasPrefix(flag, "--sysroot=") || strings.HasPrefix(flag, "-isysroot")) && hasSysrootFlag(args) {
			continue
		}
		if strings.HasPrefix(flag, "-mmacosx-version-min=") && hasFlag(args, "-mmacosx-version-min") {
			continue
		}
		if strings.HasPrefix(flag, "-fuse-ld=") && hasFlag(args, "-fuse-ld") {
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
