// Copyright 2026 The XGo Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package git provides Git-backed version comparison to comparator formulas.
package git

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/goplus/llar/internal/execbroker"
	"github.com/goplus/llar/mod/module"
)

type revision struct {
	commitID      string
	commitTime    time.Time
	exactTag      string
	reachableTags []string
	tagsAtCommit  []string
}

type comparableRevision struct {
	commitID   string
	baseTag    string
	afterBase  bool
	commitTime time.Time
}

// CompareFunc compares two versions of the same module using Git history and
// compareTag to order tags.
func CompareFunc(a, b module.Version, compareTag func(a, b string) int) int {
	if a.Version == b.Version {
		return 0
	}

	dir, err := os.MkdirTemp("", "llar-vcs-compare-*")
	if err != nil {
		panic(fmt.Errorf("create VCS history directory for %s: %w", a.Path, err))
	}
	defer os.RemoveAll(dir)

	if err := fetchHistory(dir, a.Path); err != nil {
		panic(fmt.Errorf("prepare VCS history for %s: %w", a.Path, err))
	}
	left, err := resolveRevision(dir, a.Version)
	if err != nil {
		panic(fmt.Errorf("resolve VCS version %s@%s: %w", a.Path, a.Version, err))
	}
	right, err := resolveRevision(dir, b.Version)
	if err != nil {
		panic(fmt.Errorf("resolve VCS version %s@%s: %w", b.Path, b.Version, err))
	}
	return compareRevisions(left, right, compareTag)
}

func fetchHistory(dir, modPath string) error {
	repoURL := fmt.Sprintf("https://github.com/%s.git", modPath)
	if _, err := runGit(dir, "init", "--bare", "."); err != nil {
		return err
	}
	if _, err := runGit(dir, "remote", "add", "origin", repoURL); err != nil {
		return err
	}
	_, err := runGit(
		dir,
		"fetch",
		"--force",
		"--filter=blob:none",
		"origin",
		"+refs/heads/*:refs/remotes/origin/*",
		"+refs/tags/*:refs/tags/*",
	)
	return err
}

func resolveRevision(dir, ref string) (revision, error) {
	commitID, exactTag, err := resolveCommit(dir, ref)
	if err != nil {
		return revision{}, err
	}
	timestamp, err := runGit(dir, "show", "-s", "--format=%ct", commitID)
	if err != nil {
		return revision{}, err
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(string(timestamp)), 10, 64)
	if err != nil {
		return revision{}, fmt.Errorf("parse commit time: %w", err)
	}
	reachableTags, err := gitLines(dir, "tag", "--merged", commitID)
	if err != nil {
		return revision{}, err
	}
	tagsAtCommit, err := gitLines(dir, "tag", "--points-at", commitID)
	if err != nil {
		return revision{}, err
	}
	return revision{
		commitID:      commitID,
		commitTime:    time.Unix(seconds, 0).UTC(),
		exactTag:      exactTag,
		reachableTags: reachableTags,
		tagsAtCommit:  tagsAtCommit,
	}, nil
}

func resolveCommit(dir, ref string) (commitID, exactTag string, err error) {
	tagRef := "refs/tags/" + ref
	if _, checkErr := runGit(dir, "check-ref-format", tagRef); checkErr == nil {
		if output, tagErr := runGit(dir, "rev-parse", "--verify", "--end-of-options", tagRef+"^{commit}"); tagErr == nil {
			return strings.TrimSpace(string(output)), ref, nil
		}
	}
	output, err := runGit(dir, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(string(output)), "", nil
}

func compareRevisions(left, right revision, compareTag func(a, b string) int) int {
	a := comparableRevisionOf(left, compareTag)
	b := comparableRevisionOf(right, compareTag)
	if a.baseTag == "" && b.baseTag != "" {
		return -1
	}
	if a.baseTag != "" && b.baseTag == "" {
		return 1
	}
	if a.baseTag != "" {
		if order := compareTag(a.baseTag, b.baseTag); order != 0 {
			return order
		}
	}
	if a.afterBase != b.afterBase {
		if a.afterBase {
			return 1
		}
		return -1
	}
	if !a.afterBase {
		return 0
	}
	if order := a.commitTime.Compare(b.commitTime); order != 0 {
		return order
	}
	return strings.Compare(a.commitID, b.commitID)
}

func comparableRevisionOf(rev revision, compareTag func(a, b string) int) comparableRevision {
	if rev.exactTag != "" {
		return comparableRevision{
			commitID:   rev.commitID,
			baseTag:    rev.exactTag,
			commitTime: rev.commitTime,
		}
	}

	var baseTag string
	for _, tag := range rev.reachableTags {
		if baseTag == "" || compareTag(tag, baseTag) > 0 {
			baseTag = tag
		}
	}
	return comparableRevision{
		commitID:   rev.commitID,
		baseTag:    baseTag,
		afterBase:  baseTag == "" || !slices.Contains(rev.tagsAtCommit, baseTag),
		commitTime: rev.commitTime,
	}
}

func gitLines(dir string, args ...string) ([]string, error) {
	output, err := runGit(dir, args...)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(output))
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}

func runGit(dir string, args ...string) ([]byte, error) {
	cmd := execbroker.Command("git", args...)
	cmd.Dir = dir
	if cmd.Env == nil {
		cmd.Env = execbroker.Environ()
	}
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(output)))
	}
	return output, nil
}
