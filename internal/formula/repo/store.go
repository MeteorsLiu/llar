// Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package repo

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/goplus/llar/internal/lockedfile"
	"github.com/goplus/llar/internal/vcs"
	"github.com/goplus/llar/mod/module"
)

// Store is the interface for accessing formula repositories.
type Store interface {
	// ModuleFS returns a filesystem rooted at the given module's formula directory.
	ModuleFS(ctx context.Context, modPath string) (fs.FS, error)
	// LockModule acquires an exclusive lock for the given module path.
	// Returns an unlock function that must be called to release the lock.
	LockModule(modPath string) (unlock func(), err error)
}

// remoteStore manages a remote formula repository, handling storage layout and synchronization.
type remoteStore struct {
	dir     string
	vcsRepo vcs.Repo
}

// New creates a new remote Store backed by a vcs.Repo.
// The dir specifies where this formula repository is cached locally.
func New(dir string, vcsRepo vcs.Repo) Store {
	return &remoteStore{
		dir:     dir,
		vcsRepo: vcsRepo,
	}
}

// ModuleFS returns a filesystem interface for the specified module.
// It synchronizes the module from remote and returns an fs.FS rooted at the module's directory.
func (s *remoteStore) ModuleFS(ctx context.Context, modPath string) (fs.FS, error) {
	modDir, err := s.moduleDirOf(modPath)
	if err != nil {
		return nil, err
	}

	// Sync to the repository root directory, not the module directory.
	// The vcs.Repo.Sync will create the module path structure within the destination.
	if err := s.vcsRepo.Sync(ctx, "", modPath, s.dir); err != nil {
		return nil, err
	}

	return os.DirFS(modDir), nil
}

// moduleDirOf returns the directory path for a module within the repository.
// It creates the directory with 0700 permissions if it doesn't exist.
func (s *remoteStore) moduleDirOf(modPath string) (string, error) {
	escapedModPath, err := module.EscapePath(modPath)
	if err != nil {
		return "", err
	}
	moduleDir := filepath.Join(s.dir, escapedModPath)

	if err := os.MkdirAll(moduleDir, 0700); err != nil {
		return "", err
	}
	return moduleDir, nil
}

// LockModule acquires an exclusive lock for the given module path.
// Returns an unlock function that must be called to release the lock.
func (s *remoteStore) LockModule(modPath string) (unlock func(), err error) {
	modDir, err := s.moduleDirOf(modPath)
	if err != nil {
		return nil, err
	}
	lockFile := filepath.Join(modDir, ".lock")
	return lockedfile.MutexAt(lockFile).Lock()
}

// localStore reads formulas directly from a local directory without remote sync.
type localStore struct {
	root    string // formula repo root (for reading formulas)
	lockDir string // llar cache dir (for lock files)
}

// NewLocalStore creates a Store backed by a local directory.
// root is the formula repo root (e.g. CWD when running inside llarhub).
// Lock files are stored in the llar cache directory to avoid polluting the source tree.
func NewLocalStore(root string) (Store, error) {
	lockDir, err := DefaultDir()
	if err != nil {
		return nil, err
	}
	return &localStore{root: root, lockDir: lockDir}, nil
}

// ModuleFS returns an fs.FS rooted at the module's local formula directory.
func (s *localStore) ModuleFS(_ context.Context, modPath string) (fs.FS, error) {
	escapedModPath, err := module.EscapePath(modPath)
	if err != nil {
		return nil, err
	}
	return os.DirFS(filepath.Join(s.root, escapedModPath)), nil
}

// LockModule acquires an exclusive lock for the given module path.
// Lock files are stored in the llar cache directory.
func (s *localStore) LockModule(modPath string) (unlock func(), err error) {
	escapedModPath, err := module.EscapePath(modPath)
	if err != nil {
		return nil, err
	}
	lockDir := filepath.Join(s.lockDir, escapedModPath)
	if err := os.MkdirAll(lockDir, 0700); err != nil {
		return nil, err
	}
	lockFile := filepath.Join(lockDir, ".lock")
	return lockedfile.MutexAt(lockFile).Lock()
}

// DefaultDir returns the default root directory where all formula repositories are stored.
// It creates the directory with 0700 permissions if it doesn't exist.
// The directory is located at <UserCacheDir>/.llar/formulas.
func DefaultDir() (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	formulaDir := filepath.Join(userCacheDir, ".llar", "formulas")

	if err := os.MkdirAll(formulaDir, 0700); err != nil {
		return "", err
	}
	return formulaDir, nil
}
