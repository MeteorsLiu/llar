package crosscompile

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goplus/llar/internal/build/buildtarget"
)

func TestNewWithHostNativeDisabled(t *testing.T) {
	cc, err := newWithHost("arm64-darwin", buildtarget.Platform{Arch: "arm64", OS: "darwin"})
	if err != nil {
		t.Fatalf("newWithHost: %v", err)
	}
	if cc.Identity() != "" {
		t.Fatalf("Identity = %q, want empty for native build", cc.Identity())
	}
	if patch := cc.Use(Command{Name: "cc"}); patch.Name != "" {
		t.Fatalf("Use native patch = %+v, want empty", patch)
	}
}

func TestNewWithHostRejectsUnsupportedCrossDarwin(t *testing.T) {
	_, err := newWithHost("amd64-darwin", buildtarget.Platform{Arch: "arm64", OS: "darwin"})
	if err == nil {
		t.Fatal("newWithHost error = nil, want unsupported target")
	}
	if !strings.Contains(err.Error(), "unsupported cross compile target matrix") {
		t.Fatalf("error = %v, want unsupported cross compile target matrix", err)
	}
}

func TestUseDirectCompilerPatch(t *testing.T) {
	cc := &CrossCompile{enabled: true, triple: "aarch64-linux-gnu", toolchainDir: "/tc"}
	patch := cc.Use(Command{Name: "gcc"})
	if patch.Name != "/tc/bin/clang" {
		t.Fatalf("Name = %q, want managed clang", patch.Name)
	}
	if got := strings.Join(patch.PrependArg, " "); got != "--target=aarch64-linux-gnu" {
		t.Fatalf("PrependArg = %q, want target", got)
	}
}

func TestUseCMakeConfigureAddsToolchainFile(t *testing.T) {
	cc := &CrossCompile{enabled: true, toolchainFile: "/tc/llar-toolchain.cmake"}
	patch := cc.Use(Command{Name: "cmake", Args: []string{"-S", ".", "-B", "build"}})
	if got := strings.Join(patch.AppendArg, " "); got != "-DCMAKE_TOOLCHAIN_FILE:STRING=/tc/llar-toolchain.cmake" {
		t.Fatalf("AppendArg = %q, want toolchain file", got)
	}
}

func TestUseCMakeConfigureKeepsExplicitToolchainFile(t *testing.T) {
	cc := &CrossCompile{enabled: true, toolchainFile: "/tc/llar-toolchain.cmake"}
	patch := cc.Use(Command{Name: "cmake", Args: []string{"-S", ".", "-B", "build", "-DCMAKE_TOOLCHAIN_FILE:STRING=/custom.cmake"}})
	if len(patch.AppendArg) != 0 {
		t.Fatalf("AppendArg = %+v, want empty when toolchain is explicit", patch.AppendArg)
	}
}

func TestUseAutotoolsConfigurePatch(t *testing.T) {
	cc := &CrossCompile{
		enabled:      true,
		triple:       "x86_64-linux-gnu",
		buildTriple:  "arm64-darwin",
		toolchainDir: "/tc",
	}
	patch := cc.Use(Command{Name: "./configure", Env: []string{"CFLAGS=-O2"}})
	if patch.SetEnv["CC"] != "/tc/bin/clang" {
		t.Fatalf("CC = %q, want managed clang", patch.SetEnv["CC"])
	}
	if patch.SetEnv["CFLAGS"] != "-O2 --target=x86_64-linux-gnu" {
		t.Fatalf("CFLAGS = %q, want target appended", patch.SetEnv["CFLAGS"])
	}
	if got := strings.Join(patch.AppendArg, " "); got != "--host=x86_64-linux-gnu --build=arm64-darwin" {
		t.Fatalf("AppendArg = %q, want host/build", got)
	}
}

func TestNewWithHostDownloadsManagedToolchain(t *testing.T) {
	archive := makeToolchainArchive(t)
	sum := sha256.Sum256(archive)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			fmt.Fprintf(w, `{
  "version": "test",
  "toolchains": {
    "darwin/arm64": {
      "id": "llvm-test",
      "version": "0.1.0",
      "url": "%s/archive.tar.gz",
      "sha256": "%s",
      "strip_prefix": "llvm-test"
    }
  }
}`, server.URL, hex.EncodeToString(sum[:]))
		case "/archive.tar.gz":
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("LLAR_TOOLCHAIN_MANIFEST_URL", server.URL+"/manifest.json")
	t.Setenv("LLAR_TOOLCHAIN_CACHE_DIR", t.TempDir())

	cc, err := newWithHost("amd64-linux", buildtarget.Platform{Arch: "arm64", OS: "darwin"})
	if err != nil {
		t.Fatalf("newWithHost: %v", err)
	}
	if cc.Identity() == "" {
		t.Fatal("Identity is empty, want managed toolchain identity")
	}
	if _, err := os.Stat(filepath.Join(cc.toolchainDir, "bin", "clang")); err != nil {
		t.Fatalf("managed clang not cached: %v", err)
	}
}

func makeToolchainArchive(t *testing.T) []byte {
	t.Helper()
	tmp := t.TempDir()
	path := filepath.Join(tmp, "archive.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range []string{"clang", "clang++", "llvm-ar", "llvm-ranlib", "llvm-strip"} {
		body := []byte("#!/bin/sh\n")
		hdr := &tar.Header{Name: "llvm-test/bin/" + name, Mode: 0o755, Size: int64(len(body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
