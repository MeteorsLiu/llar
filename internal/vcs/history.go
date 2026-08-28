// Copyright 2026 The XGo Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vcs

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (g *githubClient) syncHistory(ctx context.Context, owner, repo, destDir string) error {
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	repoURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
	if _, err := historyGit(ctx, destDir, "init", "--bare", "."); err != nil {
		return err
	}
	if _, err := historyGit(ctx, destDir, "remote", "add", "origin", repoURL); err != nil {
		return err
	}
	_, err := historyGit(
		ctx,
		destDir,
		"fetch",
		"--force",
		"--filter=blob:none",
		"origin",
		"+refs/heads/*:refs/remotes/origin/*",
		"+refs/tags/*:refs/tags/*",
	)
	return err
}

func historyGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
