package execbroker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestCommandUsesMiddleware(t *testing.T) {
	restore := SetMiddleware(func(req Request) Request {
		if req.Name != "llar-missing-command" {
			t.Fatalf("Name = %q, want llar-missing-command", req.Name)
		}
		req.Name = "go"
		req.Args = []string{"version"}
		return req
	})
	defer restore()

	out, err := Command("llar-missing-command").Output()
	if err != nil {
		t.Fatalf("Command output: %v", err)
	}
	if got := string(out); !strings.HasPrefix(got, "go version ") {
		t.Fatalf("output = %q, want go version prefix", got)
	}
}

func TestCommandContextUsesMiddleware(t *testing.T) {
	restore := SetMiddleware(func(req Request) Request {
		req.Name = "go"
		req.Args = []string{"version"}
		return req
	})
	defer restore()

	out, err := CommandContext(context.Background(), "llar-missing-command").Output()
	if err != nil {
		t.Fatalf("CommandContext output: %v", err)
	}
	if got := string(out); !strings.HasPrefix(got, "go version ") {
		t.Fatalf("output = %q, want go version prefix", got)
	}
}

func TestCommandAppliesMiddlewareEnv(t *testing.T) {
	restore := SetMiddleware(func(req Request) Request {
		req.Name = os.Args[0]
		req.Args = []string{"-test.run=TestExecBrokerHelperProcess"}
		req.Env = append(os.Environ(),
			"LLAR_EXECBROKER_HELPER=1",
			"LLAR_EXECBROKER_VALUE=broker",
		)
		return req
	})
	defer restore()

	out, err := Command("llar-missing-command").Output()
	if err != nil {
		t.Fatalf("Command output: %v", err)
	}
	if got := string(out); got != "broker" {
		t.Fatalf("output = %q, want broker", got)
	}
}

func TestCommandEnvPassesCallerEnvToMiddleware(t *testing.T) {
	restore := SetMiddleware(func(req Request) Request {
		if got := envValue(req.Env, "LLAR_EXECBROKER_TEST_INPUT"); got != "caller" {
			t.Fatalf("middleware env = %q, want caller", got)
		}
		req.Name = os.Args[0]
		req.Args = []string{"-test.run=TestExecBrokerHelperProcess"}
		req.Env = append(req.Env,
			"LLAR_EXECBROKER_HELPER=1",
			"LLAR_EXECBROKER_VALUE=broker",
		)
		return req
	})
	defer restore()

	out, err := CommandEnv([]string{"LLAR_EXECBROKER_TEST_INPUT=caller"}, "llar-missing-command").Output()
	if err != nil {
		t.Fatalf("CommandEnv output: %v", err)
	}
	if got := string(out); got != "broker" {
		t.Fatalf("output = %q, want broker", got)
	}
}

func TestRequestIsCloned(t *testing.T) {
	args := []string{"original"}
	restore := SetMiddleware(func(req Request) Request {
		req.Args[0] = "version"
		return Request{Name: "go", Args: req.Args}
	})
	defer restore()

	cmd := Command("llar-missing-command", args...)
	if args[0] != "original" {
		t.Fatalf("caller args mutated to %q", args[0])
	}
	if got := strings.Join(cmd.Args, " "); got != "go version" {
		t.Fatalf("cmd.Args = %q, want go version", got)
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func TestExecBrokerHelperProcess(t *testing.T) {
	if os.Getenv("LLAR_EXECBROKER_HELPER") != "1" {
		return
	}
	fmt.Print(os.Getenv("LLAR_EXECBROKER_VALUE"))
	os.Exit(0)
}
