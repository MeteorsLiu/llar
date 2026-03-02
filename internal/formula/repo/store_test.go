// Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package repo

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/goplus/llar/internal/vcs"
)

// mockRepo is a mock implementation of vcs.Repo for testing
type mockRepo struct {
	syncFn func(ctx context.Context, ref, path, localDir string) error
}

func (m *mockRepo) Tags(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (m *mockRepo) Latest(ctx context.Context) (string, error) {
	return "", nil
}

func (m *mockRepo) At(ref, localDir string) fs.FS {
	return nil
}

func (m *mockRepo) Sync(ctx context.Context, ref, path, localDir string) error {
	if m.syncFn != nil {
		return m.syncFn(ctx, ref, path, localDir)
	}
	return nil
}

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()
	repo := &mockRepo{}

	store := New(tmpDir, repo).(*remoteStore)
	if store == nil {
		t.Fatal("New returned nil")
	}
	if store.dir != tmpDir {
		t.Errorf("dir = %q, want %q", store.dir, tmpDir)
	}
}

func TestDefaultDir(t *testing.T) {
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir() failed: %v", err)
	}
	if dir == "" {
		t.Error("DefaultDir() returned empty string")
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Errorf("DefaultDir() returned non-existent path: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("DefaultDir() returned non-directory path: %s", dir)
	}
}

func TestStore_ModuleFS(t *testing.T) {
	tmpDir := t.TempDir()

	// Create module directory with a test file
	modDir := filepath.Join(tmpDir, "DaveGamble", "cJSON")
	if err := os.MkdirAll(modDir, 0755); err != nil {
		t.Fatalf("failed to create module dir: %v", err)
	}
	testFile := filepath.Join(modDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	syncCalled := false
	repo := &mockRepo{
		syncFn: func(ctx context.Context, ref, path, localDir string) error {
			syncCalled = true
			if ref != "" {
				t.Errorf("syncFn ref = %q, want empty string", ref)
			}
			if path != "DaveGamble/cJSON" {
				t.Errorf("syncFn path = %q, want %q", path, "DaveGamble/cJSON")
			}
			if localDir != tmpDir {
				t.Errorf("syncFn localDir = %q, want %q", localDir, tmpDir)
			}
			return nil
		},
	}

	store := New(tmpDir, repo)
	fsys, err := store.ModuleFS(context.Background(), "DaveGamble/cJSON")
	if err != nil {
		t.Fatalf("ModuleFS() failed: %v", err)
	}

	if !syncCalled {
		t.Error("syncFn was not called")
	}

	// Verify fs.FS works
	f, err := fsys.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open file from fs.FS: %v", err)
	}
	f.Close()
}

func TestStore_ModuleFS_SyncError(t *testing.T) {
	tmpDir := t.TempDir()
	expectedErr := errors.New("sync failed")

	repo := &mockRepo{
		syncFn: func(ctx context.Context, ref, path, localDir string) error {
			return expectedErr
		},
	}

	store := New(tmpDir, repo)
	_, err := store.ModuleFS(context.Background(), "test/module")
	if err != expectedErr {
		t.Errorf("ModuleFS() error = %v, want %v", err, expectedErr)
	}
}

func TestStore_ModuleFS_InvalidModulePath(t *testing.T) {
	tests := []string{"", "../../../etc", "owner//repo"}

	for _, modPath := range tests {
		t.Run(modPath, func(t *testing.T) {
			tmpDir := t.TempDir()
			syncCalled := false
			repo := &mockRepo{
				syncFn: func(ctx context.Context, ref, path, localDir string) error {
					syncCalled = true
					return nil
				},
			}

			store := New(tmpDir, repo)
			_, err := store.ModuleFS(context.Background(), modPath)
			if err == nil {
				t.Fatalf("ModuleFS() expected error for invalid module path %q", modPath)
			}
			if syncCalled {
				t.Errorf("syncFn should not be called for invalid module path %q", modPath)
			}
		})
	}
}

func TestStore_moduleDirOf(t *testing.T) {
	tmpDir := t.TempDir()
	store := New(tmpDir, &mockRepo{}).(*remoteStore)

	tests := []struct {
		modPath string
		wantDir string
	}{
		{"DaveGamble/cJSON", filepath.Join(tmpDir, "DaveGamble", "cJSON")},
		{"madler/zlib", filepath.Join(tmpDir, "madler", "zlib")},
	}

	for _, tt := range tests {
		t.Run(tt.modPath, func(t *testing.T) {
			got, err := store.moduleDirOf(tt.modPath)
			if err != nil {
				t.Fatalf("moduleDirOf() failed: %v", err)
			}
			if got != tt.wantDir {
				t.Errorf("moduleDirOf() = %q, want %q", got, tt.wantDir)
			}

			// Verify directory was created
			info, err := os.Stat(got)
			if err != nil {
				t.Errorf("moduleDirOf() directory not created: %v", err)
			}
			if !info.IsDir() {
				t.Errorf("moduleDirOf() path is not a directory")
			}
		})
	}
}

func TestStore_moduleDirOf_MkdirAllError(t *testing.T) {
	tmpDir := t.TempDir()
	store := New(tmpDir, &mockRepo{}).(*remoteStore)

	parent := filepath.Join(tmpDir, "owner")
	if err := os.WriteFile(parent, []byte("not-a-dir"), 0600); err != nil {
		t.Fatalf("failed to create parent file: %v", err)
	}

	_, err := store.moduleDirOf("owner/repo")
	if err == nil {
		t.Fatal("moduleDirOf() expected error when parent path is a file")
	}
}

func TestDefaultDir_UserCacheDirError(t *testing.T) {
	switch runtime.GOOS {
	case "darwin":
		t.Setenv("HOME", "")
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris", "aix":
		t.Setenv("XDG_CACHE_HOME", "relative/path")
	case "windows":
		t.Setenv("LocalAppData", "")
	default:
		t.Skipf("unsupported GOOS: %s", runtime.GOOS)
	}

	_, err := DefaultDir()
	if err == nil {
		t.Fatal("DefaultDir() expected error from os.UserCacheDir")
	}
}

func TestDefaultDir_MkdirAllError(t *testing.T) {
	switch runtime.GOOS {
	case "darwin":
		home := t.TempDir()
		if err := os.WriteFile(filepath.Join(home, "Library"), []byte("not-a-dir"), 0600); err != nil {
			t.Fatalf("failed to create blocking file: %v", err)
		}
		t.Setenv("HOME", home)
	case "linux", "freebsd", "openbsd", "netbsd", "dragonfly", "solaris", "aix":
		cacheFile := filepath.Join(t.TempDir(), "cache-file")
		if err := os.WriteFile(cacheFile, []byte("not-a-dir"), 0600); err != nil {
			t.Fatalf("failed to create cache file: %v", err)
		}
		t.Setenv("XDG_CACHE_HOME", cacheFile)
	case "windows":
		cacheFile := filepath.Join(t.TempDir(), "cache-file")
		if err := os.WriteFile(cacheFile, []byte("not-a-dir"), 0600); err != nil {
			t.Fatalf("failed to create cache file: %v", err)
		}
		t.Setenv("LocalAppData", cacheFile)
	default:
		t.Skipf("unsupported GOOS: %s", runtime.GOOS)
	}

	_, err := DefaultDir()
	if err == nil {
		t.Fatal("DefaultDir() expected MkdirAll error")
	}
}

func TestStore_LockModule(t *testing.T) {
	tmpDir := t.TempDir()
	store := New(tmpDir, &mockRepo{})

	unlock, err := store.LockModule("DaveGamble/cJSON")
	if err != nil {
		t.Fatalf("LockModule() failed: %v", err)
	}
	defer unlock()

	// Verify lock file was created
	lockFile := filepath.Join(tmpDir, "DaveGamble", "cJSON", ".lock")
	if _, err := os.Stat(lockFile); err != nil {
		t.Errorf("lock file not created: %v", err)
	}
}

func TestStore_LockModule_Exclusive(t *testing.T) {
	tmpDir := t.TempDir()
	store := New(tmpDir, &mockRepo{})

	unlock, err := store.LockModule("madler/zlib")
	if err != nil {
		t.Fatalf("LockModule() failed: %v", err)
	}

	// Try to acquire the same lock from another goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		unlock2, err := store.LockModule("madler/zlib")
		if err != nil {
			t.Errorf("second LockModule() failed: %v", err)
			return
		}
		unlock2()
	}()

	// The goroutine should be blocked; give it a moment then release
	select {
	case <-done:
		t.Error("second lock acquired before first was released")
	case <-time.After(50 * time.Millisecond):
		// expected: still blocked
	}

	unlock()
	// Now the goroutine should complete
	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Error("second lock not acquired after first was released")
	}
}

func TestStore_LockModule_InvalidPath(t *testing.T) {
	tmpDir := t.TempDir()
	store := New(tmpDir, &mockRepo{})

	_, err := store.LockModule("")
	if err == nil {
		t.Error("LockModule() expected error for empty path")
	}
}

// ---------------------------------------------------------------------------
// localStore tests
// ---------------------------------------------------------------------------

func TestNewLocalStore(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore() failed: %v", err)
	}
	if store == nil {
		t.Fatal("NewLocalStore() returned nil")
	}
	s := store.(*localStore)
	if s.root != root {
		t.Errorf("root = %q, want %q", s.root, root)
	}
	if s.lockDir == "" {
		t.Error("lockDir should not be empty")
	}
}

func TestLocalStore_ModuleFS(t *testing.T) {
	root := t.TempDir()

	// Create a formula directory under root
	modDir := filepath.Join(root, "owner", "mylib")
	if err := os.MkdirAll(modDir, 0755); err != nil {
		t.Fatalf("failed to create module dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "versions.json"), []byte(`{"path":"owner/mylib","deps":{}}`), 0644); err != nil {
		t.Fatalf("failed to write versions.json: %v", err)
	}

	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore() failed: %v", err)
	}

	fsys, err := store.ModuleFS(context.Background(), "owner/mylib")
	if err != nil {
		t.Fatalf("ModuleFS() failed: %v", err)
	}

	// Verify the file is accessible via the returned FS
	f, err := fsys.Open("versions.json")
	if err != nil {
		t.Fatalf("failed to open versions.json from FS: %v", err)
	}
	f.Close()
}

func TestLocalStore_ModuleFS_NonexistentDir(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore() failed: %v", err)
	}

	// ModuleFS on a nonexistent path returns an FS (os.DirFS doesn't validate existence)
	fsys, err := store.ModuleFS(context.Background(), "owner/nonexistent")
	if err != nil {
		t.Fatalf("ModuleFS() unexpected error: %v", err)
	}

	// But opening a file from it should fail
	_, err = fsys.Open("versions.json")
	if err == nil {
		t.Error("expected error opening file in nonexistent dir")
	}
}

func TestLocalStore_LockModule(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore() failed: %v", err)
	}

	unlock, err := store.LockModule("owner/mylib")
	if err != nil {
		t.Fatalf("LockModule() failed: %v", err)
	}
	defer unlock()

	// Verify lock file exists in lockDir (not in root)
	s := store.(*localStore)
	lockFile := filepath.Join(s.lockDir, "owner", "mylib", ".lock")
	if _, err := os.Stat(lockFile); err != nil {
		t.Errorf("lock file not created at %s: %v", lockFile, err)
	}

	// Verify lock file is NOT in the formula source root
	sourceLockFile := filepath.Join(root, "owner", "mylib", ".lock")
	if _, err := os.Stat(sourceLockFile); err == nil {
		t.Error("lock file should not be created in formula source root")
	}
}

func TestLocalStore_LockModule_Exclusive(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore() failed: %v", err)
	}

	unlock, err := store.LockModule("owner/mylib")
	if err != nil {
		t.Fatalf("first LockModule() failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		unlock2, err := store.LockModule("owner/mylib")
		if err != nil {
			t.Errorf("second LockModule() failed: %v", err)
			return
		}
		unlock2()
	}()

	select {
	case <-done:
		t.Error("second lock acquired before first was released")
	case <-time.After(50 * time.Millisecond):
		// expected: still blocked
	}

	unlock()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Error("second lock not acquired after first was released")
	}
}

func TestLocalStore_LockModule_InvalidPath(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore() failed: %v", err)
	}

	_, err = store.LockModule("")
	if err == nil {
		t.Error("LockModule() expected error for empty path")
	}
}

func TestStore_ModuleFS_RealRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real repo test in short mode")
	}

	tmpDir := t.TempDir()

	// Use real vcs.Repo with llarmvp-formula repository
	repo, err := vcs.NewRepo("github.com/MeteorsLiu/llarmvp-formula")
	if err != nil {
		t.Fatalf("failed to create vcs.Repo: %v", err)
	}

	store := New(tmpDir, repo)

	// Test syncing madler/zlib module (exists in llarmvp-formula)
	ctx := context.Background()
	fsys, err := store.ModuleFS(ctx, "madler/zlib")
	if err != nil {
		t.Fatalf("ModuleFS() failed: %v", err)
	}

	// Verify formula file exists
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		t.Fatalf("failed to read module directory: %v", err)
	}

	if len(entries) == 0 {
		t.Error("module directory is empty after sync")
	}

	// Look for formula files
	hasFormulaFile := false
	for _, entry := range entries {
		t.Logf("found entry: %s", entry.Name())
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".gox" {
			hasFormulaFile = true
		}
	}

	if !hasFormulaFile {
		// Check subdirectories for formula files
		for _, entry := range entries {
			if entry.IsDir() {
				subEntries, err := fs.ReadDir(fsys, entry.Name())
				if err != nil {
					continue
				}
				for _, subEntry := range subEntries {
					t.Logf("found subentry: %s/%s", entry.Name(), subEntry.Name())
					if filepath.Ext(subEntry.Name()) == ".gox" {
						hasFormulaFile = true
						break
					}
				}
			}
		}
	}

	if !hasFormulaFile {
		t.Error("no formula files (.gox) found in synced module")
	}
}
