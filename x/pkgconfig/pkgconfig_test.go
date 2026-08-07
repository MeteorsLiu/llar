package pkgconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/goplus/llar/internal/execbroker"
)

func TestUse(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "lib", "pkgconfig")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PKG_CONFIG_PATH", "/existing")

	Use(root)

	if got, want := os.Getenv("PKG_CONFIG_PATH"), dir+string(os.PathListSeparator)+"/existing"; got != want {
		t.Fatalf("PKG_CONFIG_PATH = %q, want %q", got, want)
	}
}

func TestQueries(t *testing.T) {
	tests := []struct {
		name  string
		query func(string) (string, error)
		args  []string
	}{
		{name: "cflags", query: CFlags, args: []string{"--cflags", "demo"}},
		{name: "libs", query: Libs, args: []string{"--libs", "demo"}},
		{name: "static libs", query: StaticLibs, args: []string{"--static", "--libs", "demo"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var request execbroker.Request
			var output string
			err := execbroker.Do(execbroker.Scope{
				Middleware: func(req execbroker.Request) execbroker.Request {
					request = req
					req.Name = os.Args[0]
					req.Args = []string{"-test.run=TestLookupHelperProcess"}
					req.Env = append(os.Environ(), "GO_WANT_PKGCONFIG_HELPER=1")
					return req
				},
			}, func() error {
				var err error
				output, err = tt.query("demo")
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
			if request.Name != "pkg-config" {
				t.Fatalf("command = %q, want pkg-config", request.Name)
			}
			if !slices.Equal(request.Args, tt.args) {
				t.Fatalf("args = %q, want %q", request.Args, tt.args)
			}
			if output != "-I/include -ldemo" {
				t.Fatalf("output = %q", output)
			}
		})
	}
}

func TestLookupHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_PKGCONFIG_HELPER") != "1" {
		return
	}
	fmt.Fprintln(os.Stdout, "  -I/include -ldemo  ")
	os.Exit(0)
}
