// Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package execbroker

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/petermattis/goid"
)

// Request describes a command before it is turned into an exec.Cmd.
type Request struct {
	Name   string
	Args   []string
	Env    []string
	Dir    string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Middleware may rewrite a command before execution. Returning an error
// prevents the command from starting.
type Middleware func(Request) (Request, error)

// Scope supplies process-independent defaults for commands created while fn
// runs. Custom fields already set on a command take precedence.
type Scope struct {
	Env        []string
	Dir        string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Middleware Middleware
}

var (
	scopeMu sync.RWMutex
	scopes  = make(map[int64]Scope)
)

// Do runs fn with the supplied command scope.
func Do(scope Scope, fn func() error) error {
	id := goid.Get()
	scope.Env = clone(scope.Env)
	scopeMu.Lock()
	previous, existed := scopes[id]
	scopes[id] = scope
	scopeMu.Unlock()
	defer func() {
		scopeMu.Lock()
		if existed {
			scopes[id] = previous
		} else {
			delete(scopes, id)
		}
		scopeMu.Unlock()
	}()
	return fn()
}

// Getenv returns the value of key from the active command scope. Without a
// scope, it has the same behavior as os.Getenv.
func Getenv(key string) string {
	id := goid.Get()
	scopeMu.RLock()
	scope, ok := scopes[id]
	if ok && scope.Env != nil {
		value := envValue(scope.Env, key)
		scopeMu.RUnlock()
		return value
	}
	scopeMu.RUnlock()
	return os.Getenv(key)
}

// Setenv sets key in the active command scope. The first scoped write copies
// the process environment so later commands inherit the update without
// changing the process-wide environment. Without a scope, it has the same
// behavior as os.Setenv.
func Setenv(key, value string) error {
	if err := validateEnv(key, value); err != nil {
		return err
	}

	id := goid.Get()
	scopeMu.Lock()
	defer scopeMu.Unlock()

	scope, ok := scopes[id]
	if !ok {
		return os.Setenv(key, value)
	}
	if scope.Env == nil {
		scope.Env = os.Environ()
	}
	scope.Env = setEnv(scope.Env, key, value)
	scopes[id] = scope
	return nil
}

// Println writes to the stdout configured for the active scope.
func Println(a ...any) (int, error) {
	w := io.Writer(os.Stdout)
	scopeMu.RLock()
	if scope, ok := scopes[goid.Get()]; ok && scope.Stdout != nil {
		w = scope.Stdout
	}
	scopeMu.RUnlock()
	return fmt.Fprintln(w, a...)
}

// Command is the brokered equivalent of exec.Command.
func Command(name string, args ...string) *exec.Cmd {
	req, err := rewrite(Request{Name: name, Args: clone(args)})
	cmd := exec.Command(req.Name, req.Args...)
	if err != nil {
		cmd.Err = err
	}
	apply(cmd, req)
	return cmd
}

// CommandContext is the brokered equivalent of exec.CommandContext.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	req, err := rewrite(Request{Name: name, Args: clone(args)})
	cmd := exec.CommandContext(ctx, req.Name, req.Args...)
	if err != nil {
		cmd.Err = err
	}
	apply(cmd, req)
	return cmd
}

// Run applies the active scope to an existing command and runs it. This is
// used by runtimes such as gsh that construct exec.Cmd internally.
func Run(cmd *exec.Cmd) error {
	name := cmd.Path
	args := []string(nil)
	if len(cmd.Args) > 0 {
		name = cmd.Args[0]
		args = cmd.Args[1:]
	}
	req, err := rewrite(Request{
		Name:   name,
		Args:   clone(args),
		Env:    clone(cmd.Env),
		Dir:    cmd.Dir,
		Stdin:  cmd.Stdin,
		Stdout: cmd.Stdout,
		Stderr: cmd.Stderr,
	})
	if err != nil {
		return err
	}
	resolved := exec.Command(req.Name, req.Args...)
	cmd.Path = resolved.Path
	cmd.Args = resolved.Args
	cmd.Err = resolved.Err
	apply(cmd, req)
	return cmd.Run()
}

func rewrite(req Request) (Request, error) {
	req.Args = clone(req.Args)
	req.Env = clone(req.Env)

	scopeMu.RLock()
	scope, ok := scopes[goid.Get()]
	if ok {
		if req.Env == nil {
			req.Env = clone(scope.Env)
		}
		if req.Dir == "" {
			req.Dir = scope.Dir
		}
		// gsh initializes commands with the process streams. Treat those as
		// defaults while preserving custom writers used by features like Capout.
		if scope.Stdin != nil && (req.Stdin == nil || req.Stdin == os.Stdin) {
			req.Stdin = scope.Stdin
		}
		if scope.Stdout != nil && (req.Stdout == nil || req.Stdout == os.Stdout) {
			req.Stdout = scope.Stdout
		}
		if scope.Stderr != nil && (req.Stderr == nil || req.Stderr == os.Stderr) {
			req.Stderr = scope.Stderr
		}
		if scope.Middleware != nil {
			var err error
			req, err = scope.Middleware(req)
			if err != nil {
				scopeMu.RUnlock()
				return req, err
			}
		}
	}
	scopeMu.RUnlock()

	req.Args = clone(req.Args)
	req.Env = clone(req.Env)
	return req, nil
}

func apply(cmd *exec.Cmd, req Request) {
	cmd.Env = clone(req.Env)
	cmd.Dir = req.Dir
	cmd.Stdin = req.Stdin
	cmd.Stdout = req.Stdout
	cmd.Stderr = req.Stderr
}

func clone(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if len(env[i]) >= len(prefix) && env[i][:len(prefix)] == prefix {
			return env[i][len(prefix):]
		}
	}
	return ""
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i := range env {
		if len(env[i]) >= len(prefix) && env[i][:len(prefix)] == prefix {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func validateEnv(key, value string) error {
	if key == "" {
		return fmt.Errorf("invalid environment variable name")
	}
	for i := 0; i < len(key); i++ {
		if key[i] == '=' || key[i] == 0 {
			return fmt.Errorf("invalid environment variable name %q", key)
		}
	}
	for i := 0; i < len(value); i++ {
		if value[i] == 0 {
			return fmt.Errorf("invalid environment variable value for %q", key)
		}
	}
	return nil
}
