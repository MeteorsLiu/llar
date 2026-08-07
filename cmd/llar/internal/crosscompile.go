package internal

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/goplus/llar/formula"
	"github.com/goplus/llar/internal/build"
	"github.com/goplus/llar/internal/build/crosscompile"
	"github.com/goplus/llar/internal/execbroker"
	"github.com/goplus/llar/internal/formula/repo"
	"github.com/goplus/llar/internal/modules"
	"github.com/goplus/llar/mod/module"
)

type llvmPathToolchain struct {
	cc       string
	cxx      string
	archiver string
	ranlib   string
	nm       string
	strip    string
}

func (t llvmPathToolchain) CC() string       { return t.cc }
func (t llvmPathToolchain) CXX() string      { return t.cxx }
func (t llvmPathToolchain) Archiver() string { return t.archiver }
func (t llvmPathToolchain) Ranlib() string   { return t.ranlib }
func (t llvmPathToolchain) NM() string       { return t.nm }
func (t llvmPathToolchain) Strip() string    { return t.strip }

var newCrossCompileToolchain = func() (crosscompile.Toolchain, error) {
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
	return llvmPathToolchain{
		cc:       cc,
		cxx:      cxx,
		archiver: archiver,
		ranlib:   ranlib,
		nm:       nm,
		strip:    strip,
	}, nil
}

func prepareCrossCompile(
	ctx context.Context,
	store repo.Store,
	root module.Version,
	matrix formula.Matrix,
	buildOpts build.Options,
) (*crosscompile.CrossCompile, *modules.Module, error) {
	targetOS, targetArch := matrixTarget(matrix)
	sysroot, ok := crosscompile.Sysroot(targetOS, targetArch)
	if !ok || targetOS == runtime.GOOS && targetArch == runtime.GOARCH || root.Path == sysroot.Path {
		return nil, nil, nil
	}

	sysrootMods, err := modules.Load(ctx, sysroot, modules.Options{
		FormulaStore: store,
		Matrix:       matrix,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("load cross compile sysroot %s@%s: %w", sysroot.Path, sysroot.Version, err)
	}
	buildOpts.RunTest = false
	builder, err := build.NewBuilder(buildOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("create sysroot builder: %w", err)
	}
	results, err := builder.Build(ctx, sysrootMods)
	if err != nil {
		return nil, nil, fmt.Errorf("build cross compile sysroot %s@%s: %w", sysroot.Path, sysroot.Version, err)
	}

	toolchain, err := newCrossCompileToolchain()
	if err != nil {
		return nil, nil, err
	}
	rewriter, err := crosscompile.New(targetOS, targetArch, toolchain, results[len(results)-1].Metadata)
	if err != nil {
		return nil, nil, fmt.Errorf("prepare cross compile target %s/%s: %w", targetOS, targetArch, err)
	}
	return rewriter, sysrootMods[0], nil
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

func injectSysroot(mods []*modules.Module, sysroot *modules.Module) []*modules.Module {
	for _, mod := range mods {
		mod.Deps = append(mod.Deps, sysroot)
	}
	return append(mods, sysroot)
}

func wrapCrossCompileHooks(mods []*modules.Module, rewriter *crosscompile.CrossCompile, stdout, stderr io.Writer) {
	middleware := func(req execbroker.Request) execbroker.Request {
		patch := rewriter.Use(crosscompile.Command{
			Name: req.Name,
			Args: req.Args,
			Env:  effectiveCommandEnv(req.Env),
			Dir:  req.Dir,
		})
		return applyCrossCompilePatch(req, patch)
	}
	for _, mod := range mods {
		if hook := mod.OnBuild; hook != nil {
			mod.OnBuild = func(ctx *formula.Context) {
				_ = execbroker.Do(execbroker.Scope{
					Dir:        ctx.SourceDir,
					Stdin:      os.Stdin,
					Stdout:     stdout,
					Stderr:     stderr,
					Middleware: middleware,
				}, func() error {
					hook(ctx)
					return nil
				})
			}
		}
		if hook := mod.OnTest; hook != nil {
			mod.OnTest = func(ctx *formula.Context) {
				_ = execbroker.Do(execbroker.Scope{
					Dir:        ctx.SourceDir,
					Stdin:      os.Stdin,
					Stdout:     stdout,
					Stderr:     stderr,
					Middleware: middleware,
				}, func() error {
					hook(ctx)
					return nil
				})
			}
		}
	}
}

func applyCrossCompilePatch(req execbroker.Request, patch crosscompile.Patch) execbroker.Request {
	if patch.Name != "" {
		req.Name = patch.Name
	}
	if len(patch.PrependArg) > 0 {
		req.Args = append(append([]string(nil), patch.PrependArg...), req.Args...)
	}
	if len(patch.AppendArg) > 0 {
		req.Args = append(req.Args, patch.AppendArg...)
	}
	if patch.Env != nil {
		req.Env = append([]string(nil), patch.Env...)
	}
	return req
}

func effectiveCommandEnv(env []string) []string {
	if env != nil {
		return append([]string(nil), env...)
	}
	return os.Environ()
}
