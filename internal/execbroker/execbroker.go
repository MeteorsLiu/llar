package execbroker

import (
	"context"
	"os/exec"
	"sync"
)

// Request describes a command before it is turned into an exec.Cmd.
type Request struct {
	Name string
	Args []string
	Env  []string
	Dir  string
}

// Middleware may rewrite command name, arguments, or environment.
type Middleware func(Request) Request

var (
	mu         sync.RWMutex
	middleware Middleware = passthrough
)

func passthrough(req Request) Request {
	return req
}

// SetMiddleware installs a process-local command middleware and returns a restore
// function. It is intentionally internal so toolchain policy stays out of formula
// APIs.
func SetMiddleware(next Middleware) func() {
	if next == nil {
		next = passthrough
	}

	mu.Lock()
	prev := middleware
	middleware = next
	mu.Unlock()

	return func() {
		mu.Lock()
		middleware = prev
		mu.Unlock()
	}
}

// Command is the brokered equivalent of exec.Command.
func Command(name string, args ...string) *exec.Cmd {
	return CommandEnv(nil, name, args...)
}

// CommandContext is the brokered equivalent of exec.CommandContext.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	req := rewrite(Request{Name: name, Args: clone(args)})
	cmd := exec.CommandContext(ctx, req.Name, req.Args...)
	applyEnv(cmd, req.Env)
	return cmd
}

// CommandEnv is Command with an explicit environment visible to middleware.
func CommandEnv(env []string, name string, args ...string) *exec.Cmd {
	req := rewrite(Request{Name: name, Args: clone(args), Env: clone(env)})
	cmd := exec.Command(req.Name, req.Args...)
	applyEnv(cmd, req.Env)
	return cmd
}

func rewrite(req Request) Request {
	req.Args = clone(req.Args)
	req.Env = clone(req.Env)

	mu.RLock()
	next := middleware
	mu.RUnlock()

	req = next(req)
	req.Args = clone(req.Args)
	req.Env = clone(req.Env)
	return req
}

func applyEnv(cmd *exec.Cmd, env []string) {
	if env != nil {
		cmd.Env = clone(env)
	}
}

func clone(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}
