package internal

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testdataFormulasDir returns the absolute path to testdata/formulas.
func testdataFormulasDir(t *testing.T) string {
	t.Helper()
	// The test binary runs from the package directory.
	abs, err := filepath.Abs("testdata/formulas")
	if err != nil {
		t.Fatalf("failed to resolve testdata path: %v", err)
	}
	return abs
}

// runMakeCmdInDir changes the working directory to dir, runs runMakeCmd,
// then restores the original directory.
func runMakeCmdInDir(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
	return runMakeCmd(t, args...)
}

func TestParseModuleArg(t *testing.T) {
	tests := []struct {
		arg         string
		wantModPath string
		wantVersion string
		wantLocal   bool
	}{
		{"owner/repo@v1.0.0", "owner/repo", "v1.0.0", false},
		{"owner/repo@1.0.0", "owner/repo", "1.0.0", false},
		{"owner/repo", "owner/repo", "", false},
		{"org/owner/repo@v2.0.0", "org/owner/repo", "v2.0.0", false},
		{"simple@latest", "simple", "latest", false},
		{"no-version", "no-version", "", false},
		{"multiple@at@signs", "multiple@at", "signs", false},
		{"./owner/repo@v1.0.0", "owner/repo", "v1.0.0", true},
		{"./owner/repo", "owner/repo", "", true},
		{"/abs/owner/repo@v1.0.0", "/abs/owner/repo", "v1.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			modPath, version, isLocal := parseModuleArg(tt.arg)
			if modPath != tt.wantModPath {
				t.Errorf("parseModuleArg(%q) modPath = %q, want %q", tt.arg, modPath, tt.wantModPath)
			}
			if version != tt.wantVersion {
				t.Errorf("parseModuleArg(%q) version = %q, want %q", tt.arg, version, tt.wantVersion)
			}
			if isLocal != tt.wantLocal {
				t.Errorf("parseModuleArg(%q) isLocal = %v, want %v", tt.arg, isLocal, tt.wantLocal)
			}
		})
	}
}

func setupTestSrcDir(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "lib"), 0755)
	os.WriteFile(filepath.Join(src, "lib", "libfoo.a"), []byte("archive"), 0644)
	os.MkdirAll(filepath.Join(src, "include"), 0755)
	os.WriteFile(filepath.Join(src, "include", "foo.h"), []byte("#pragma once"), 0644)
	return src
}

func TestOutputResult_CopyDir(t *testing.T) {
	src := setupTestSrcDir(t)
	dest := filepath.Join(t.TempDir(), "out")

	if err := outputResult(src, dest); err != nil {
		t.Fatalf("outputResult copy: %v", err)
	}

	// Verify files exist
	for _, rel := range []string{"lib/libfoo.a", "include/foo.h"} {
		path := filepath.Join(dest, rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}

	// Verify content
	data, err := os.ReadFile(filepath.Join(dest, "lib", "libfoo.a"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "archive" {
		t.Errorf("content = %q, want %q", data, "archive")
	}
}

func TestOutputResult_Zip(t *testing.T) {
	src := setupTestSrcDir(t)
	dest := filepath.Join(t.TempDir(), "out.zip")

	if err := outputResult(src, dest); err != nil {
		t.Fatalf("outputResult zip: %v", err)
	}

	// Open and verify zip contents
	r, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()

	want := map[string]bool{
		"lib/libfoo.a":  false,
		"include/foo.h": false,
	}
	for _, f := range r.File {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("zip missing %s", name)
		}
	}
}

func TestOutputResult_ZipContent(t *testing.T) {
	src := setupTestSrcDir(t)
	dest := filepath.Join(t.TempDir(), "out.zip")

	if err := outputResult(src, dest); err != nil {
		t.Fatalf("outputResult zip: %v", err)
	}

	r, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()

	// Verify file content inside zip
	for _, f := range r.File {
		if f.Name == "lib/libfoo.a" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open zip entry: %v", err)
			}
			data, _ := io.ReadAll(rc)
			rc.Close()
			if string(data) != "archive" {
				t.Errorf("zip content of lib/libfoo.a = %q, want %q", data, "archive")
			}
		}
	}
}

func TestOutputResult_EmptyDir(t *testing.T) {
	src := t.TempDir() // empty directory

	// Copy empty dir
	destDir := filepath.Join(t.TempDir(), "empty-out")
	if err := outputResult(src, destDir); err != nil {
		t.Fatalf("outputResult copy empty dir: %v", err)
	}
	info, err := os.Stat(destDir)
	if err != nil {
		t.Fatalf("dest dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("dest should be a directory")
	}

	// Zip empty dir
	destZip := filepath.Join(t.TempDir(), "empty.zip")
	if err := outputResult(src, destZip); err != nil {
		t.Fatalf("outputResult zip empty dir: %v", err)
	}
	r, err := zip.OpenReader(destZip)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()
	if len(r.File) != 0 {
		t.Errorf("zip of empty dir has %d entries, want 0", len(r.File))
	}
}

func TestOutputResult_InvalidSrc(t *testing.T) {
	nonexistent := filepath.Join(t.TempDir(), "does-not-exist")

	// Zip with invalid src
	dest := filepath.Join(t.TempDir(), "bad.zip")
	if err := outputResult(nonexistent, dest); err == nil {
		t.Error("expected error for nonexistent src dir")
	}

	// Copy with invalid src
	destDir := filepath.Join(t.TempDir(), "bad-out")
	if err := outputResult(nonexistent, destDir); err == nil {
		t.Error("expected error for nonexistent src dir")
	}
}

func TestOutputResult_NestedDirs(t *testing.T) {
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "a", "b", "c"), 0755)
	os.WriteFile(filepath.Join(src, "a", "b", "c", "deep.txt"), []byte("deep"), 0644)
	os.WriteFile(filepath.Join(src, "a", "top.txt"), []byte("top"), 0644)

	// Test copy
	destDir := filepath.Join(t.TempDir(), "nested-out")
	if err := outputResult(src, destDir); err != nil {
		t.Fatalf("outputResult copy nested: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destDir, "a", "b", "c", "deep.txt"))
	if err != nil {
		t.Fatalf("missing deep file: %v", err)
	}
	if string(data) != "deep" {
		t.Errorf("deep.txt = %q, want %q", data, "deep")
	}

	// Test zip
	destZip := filepath.Join(t.TempDir(), "nested.zip")
	if err := outputResult(src, destZip); err != nil {
		t.Fatalf("outputResult zip nested: %v", err)
	}
	r, err := zip.OpenReader(destZip)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()

	found := false
	for _, f := range r.File {
		if f.Name == filepath.Join("a", "b", "c", "deep.txt") {
			found = true
			rc, _ := f.Open()
			data, _ := io.ReadAll(rc)
			rc.Close()
			if string(data) != "deep" {
				t.Errorf("zip deep.txt = %q, want %q", data, "deep")
			}
		}
	}
	if !found {
		t.Error("zip missing a/b/c/deep.txt")
	}
}

// TestRunMake_RemotePath_FailsAtLoad tests the non-local (remote) store setup path.
// vcs.NewRepo for llarhub succeeds without network; modules.Load will fail when
// trying to sync from remote, giving us "failed to load modules".
func TestRunMake_RemotePath_FailsAtLoad(t *testing.T) {
	_, err := runMakeCmd(t, "test/fakepkg@v1.0.0")
	if err == nil {
		t.Fatal("expected error for remote module, got nil")
	}
	if !strings.Contains(err.Error(), "failed to load modules") &&
		!strings.Contains(err.Error(), "failed to build") &&
		!strings.Contains(err.Error(), "failed to get formula dir") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunMake_LocalPath_ModulesLoad tests that runMake with a local path (./...)
// correctly sets up the localStore and reaches modules.Load.
// The formula is loaded from testdata/formulas; the build fails at repo.Sync
// (network) but we verify we get past store setup and modules.Load.
func TestRunMake_LocalPath_ModulesLoad(t *testing.T) {
	formulasDir := testdataFormulasDir(t)
	_, err := runMakeCmdInDir(t, formulasDir, "./test/fakepkg@v1.0.0")
	// Expect failure at builder.Build (network/source sync), not at store setup.
	// Must NOT be a "failed to create local formula store" or "failed to load modules" error.
	if err == nil {
		t.Fatal("expected build error (network), got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "failed to create local formula store") {
		t.Errorf("failed at local store creation: %v", err)
	}
	if strings.Contains(msg, "failed to load modules") {
		t.Errorf("failed at modules.Load: %v", err)
	}
}

// TestRunMake_LocalPath_NoFormula tests that a missing formula dir produces
// a "failed to load modules" error (not a panic or store error).
func TestRunMake_LocalPath_NoFormula(t *testing.T) {
	formulasDir := testdataFormulasDir(t)
	_, err := runMakeCmdInDir(t, formulasDir, "./owner/nonexistent@v1.0.0")
	if err == nil {
		t.Fatal("expected error for nonexistent formula, got nil")
	}
	if !strings.Contains(err.Error(), "failed to load modules") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRunMake_LocalPath_Silent tests that the silent (non-verbose) code path
// is exercised with a local formula.
func TestRunMake_LocalPath_Silent(t *testing.T) {
	formulasDir := testdataFormulasDir(t)

	// Force non-verbose mode via the flag
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(formulasDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) })

	// Reset to non-verbose (default)
	makeVerbose = false
	makeOutput = ""

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs([]string{"make", "./test/fakepkg@v1.0.0"})
	runErr := rootCmd.Execute()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)

	// We expect a build error (network), not a devnull setup error
	if runErr != nil && strings.Contains(runErr.Error(), "failed to open devnull") {
		t.Errorf("devnull open failed: %v", runErr)
	}
}

// TestRunMake_LocalPath_WithOutput tests that -o flag resolves to absolute path
// before build (the abs resolution code path).
func TestRunMake_LocalPath_WithOutput(t *testing.T) {
	formulasDir := testdataFormulasDir(t)
	dest := filepath.Join(t.TempDir(), "out.zip")
	_, err := runMakeCmdInDir(t, formulasDir, "-o", dest, "./test/fakepkg@v1.0.0")
	// Expect failure at build (network), not at output path resolution
	if err != nil && strings.Contains(err.Error(), "failed to resolve output path") {
		t.Errorf("output path resolution failed: %v", err)
	}
}

// Integration tests that run the real `llar make` command.
// Requires network, git, and cmake.

func runMakeCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()

	// Reset flags to defaults before each run
	makeVerbose = true
	makeOutput = ""

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := rootCmd
	cmd.SetArgs(append([]string{"make"}, args...))
	err := cmd.Execute()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String(), err
}

func TestMakeReal_Verbose(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	out, err := runMakeCmd(t, "-v", "madler/zlib@v1.3.1")
	if err != nil {
		t.Fatalf("llar make -v failed: %v", err)
	}
	if !strings.Contains(out, "-lz") {
		t.Errorf("expected metadata '-lz' in output, got: %s", out)
	}
}

func TestMakeReal_Silent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	out, err := runMakeCmd(t, "madler/zlib@v1.3.1")
	if err != nil {
		t.Fatalf("llar make failed: %v", err)
	}
	// Should only have metadata, no cmake output
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 || strings.TrimSpace(lines[0]) != "-lz" {
		t.Errorf("expected only '-lz', got %d lines: %q", len(lines), out)
	}
}

func TestMakeReal_OutputZip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dest := filepath.Join(t.TempDir(), "zlib.zip")
	_, err := runMakeCmd(t, "-o", dest, "madler/zlib@v1.3.1")
	if err != nil {
		t.Fatalf("llar make -o zip failed: %v", err)
	}

	r, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()

	hasLib := false
	hasInclude := false
	for _, f := range r.File {
		if strings.HasPrefix(f.Name, "lib/") {
			hasLib = true
		}
		if strings.HasPrefix(f.Name, "include/") {
			hasInclude = true
		}
	}
	if !hasLib {
		t.Error("zip missing lib/ entries")
	}
	if !hasInclude {
		t.Error("zip missing include/ entries")
	}
}

func TestMakeReal_InvalidModule(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, err := runMakeCmd(t, "nonexistent/fakepkg@v0.0.1")
	if err == nil {
		t.Fatal("expected error for nonexistent module")
	}
	if !strings.Contains(err.Error(), "failed to load modules") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMakeReal_NoVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// No version specified — modules.Load should still work (resolves latest)
	// or fail gracefully
	_, err := runMakeCmd(t, "nonexistent/fakepkg")
	if err == nil {
		t.Fatal("expected error for nonexistent module without version")
	}
}

// TODO: resolve dynamic library symlink issue
// func TestMakeReal_OutputDir(t *testing.T) {
// 	if testing.Short() {
// 		t.Skip("skipping integration test in short mode")
// 	}
//
// 	dest := filepath.Join(t.TempDir(), "zlib-out")
// 	_, err := runMakeCmd(t, "-o", dest, "madler/zlib@v1.3.1")
// 	if err != nil {
// 		t.Fatalf("llar make -o dir failed: %v", err)
// 	}
//
// 	// Verify lib and include directories exist
// 	if _, err := os.Stat(filepath.Join(dest, "lib")); err != nil {
// 		t.Errorf("missing lib/: %v", err)
// 	}
// 	if _, err := os.Stat(filepath.Join(dest, "include")); err != nil {
// 		t.Errorf("missing include/: %v", err)
// 	}
// }
