package build

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSubprocessRunnerRunsRealLlarMake(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping llar make integration test in short mode")
	}
	if _, err := exec.LookPath("cmake"); err != nil {
		t.Skip("cmake not found, skipping llar make integration test")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found, skipping llar make integration test")
	}

	binDir := t.TempDir()
	buildLlarBinary(t, binDir)
	withPath(t, binDir)

	req := Request{
		Target:    Target{Module: "madler/zlib", Version: "v1.3.1"},
		MatrixStr: runtime.GOARCH + "-" + runtime.GOOS,
		Matrix: Matrix{
			Require: map[string]string{"arch": runtime.GOARCH, "os": runtime.GOOS},
		},
	}
	var logs bytes.Buffer
	got, err := NewSubprocessRunner().Run(context.Background(), req, &logs)
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	if got.Type != "zip" {
		t.Fatalf("Type = %q, want zip", got.Type)
	}
	if strings.TrimSpace(got.Metadata) != "-lz" {
		t.Fatalf("Metadata = %q, want -lz", got.Metadata)
	}
	if logs.Len() == 0 {
		t.Fatal("logs are empty, want verbose llar make output")
	}
	if _, err := got.Archive.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("Archive is not seekable: %v", err)
	}
	archiveBytes, err := io.ReadAll(got.Archive)
	if err != nil {
		t.Fatalf("ReadAll archive: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	if !zipHasPrefix(zr, "include/") {
		t.Fatal("archive missing include/ entries")
	}
	if !zipHasPrefix(zr, "lib/") {
		t.Fatal("archive missing lib/ entries")
	}
}

func TestSplitMetadataUsesLastStdoutLine(t *testing.T) {
	metadata, log := splitMetadata([]byte("checking\nbuilding\n-lz\n"))
	if metadata != "-lz" {
		t.Fatalf("metadata = %q, want -lz", metadata)
	}
	if string(log) != "checking\nbuilding\n" {
		t.Fatalf("log = %q, want stdout before metadata", log)
	}
}

func buildLlarBinary(t *testing.T, binDir string) {
	t.Helper()
	repoRoot := repoRoot(t)
	bin := filepath.Join(binDir, "llar")
	cmd := exec.Command("go", "build", "-ldflags=-checklinkname=0", "-o", bin, "./cmd/llar")
	cmd.Dir = repoRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/llar: %v\n%s", err, output)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "cmd", "llar", "main.go")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}

func withPath(t *testing.T, dir string) {
	t.Helper()
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("PATH", oldPath)
	})
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath); err != nil {
		t.Fatalf("Setenv PATH: %v", err)
	}
}

func zipHasPrefix(r *zip.Reader, prefix string) bool {
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, prefix) {
			return true
		}
	}
	return false
}
