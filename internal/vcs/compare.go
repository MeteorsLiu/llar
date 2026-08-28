// Copyright 2026 The XGo Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vcs

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
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

// CompareFunc compares two tags or commits using repository history and
// compareTag to order tags.
func (r *repo) CompareFunc(a, b string, compareTag func(a, b string) int) int {
	if a == b {
		return 0
	}

	r.historyMu.Lock()
	defer r.historyMu.Unlock()

	left := r.comparableRevision(r.resolveRevision(a), compareTag)
	right := r.comparableRevision(r.resolveRevision(b), compareTag)
	if left.baseTag == "" && right.baseTag != "" {
		return -1
	}
	if left.baseTag != "" && right.baseTag == "" {
		return 1
	}
	if left.baseTag != "" {
		if order := compareTag(left.baseTag, right.baseTag); order != 0 {
			return order
		}
	}
	if left.afterBase != right.afterBase {
		if left.afterBase {
			return 1
		}
		return -1
	}
	if !left.afterBase {
		return 0
	}
	if order := left.commitTime.Compare(right.commitTime); order != 0 {
		return order
	}
	return strings.Compare(left.commitID, right.commitID)
}

func (r *repo) comparableRevision(rev revision, compareTag func(a, b string) int) comparableRevision {
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

func (r *repo) resolveRevision(ref string) revision {
	if rev, ok := r.revisions[ref]; ok {
		return rev
	}
	if err := r.prepareHistory(); err != nil {
		panic(fmt.Errorf("prepare VCS history for %s/%s/%s: %w", r.host, r.owner, r.name, err))
	}

	commitID, exactTag, err := r.resolveCommit(ref)
	if err != nil {
		panic(fmt.Errorf("resolve VCS version %s/%s/%s@%s: %w", r.host, r.owner, r.name, ref, err))
	}
	timestamp, err := historyGit(context.Background(), r.historyDir, "show", "-s", "--format=%ct", commitID)
	if err != nil {
		panic(fmt.Errorf("resolve VCS version %s/%s/%s@%s: %w", r.host, r.owner, r.name, ref, err))
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(string(timestamp)), 10, 64)
	if err != nil {
		panic(fmt.Errorf("parse commit time for %s/%s/%s@%s: %w", r.host, r.owner, r.name, ref, err))
	}
	reachableTags, err := r.gitLines("tag", "--merged", commitID)
	if err != nil {
		panic(fmt.Errorf("resolve VCS version %s/%s/%s@%s: %w", r.host, r.owner, r.name, ref, err))
	}
	tagsAtCommit, err := r.gitLines("tag", "--points-at", commitID)
	if err != nil {
		panic(fmt.Errorf("resolve VCS version %s/%s/%s@%s: %w", r.host, r.owner, r.name, ref, err))
	}
	rev := revision{
		commitID:      commitID,
		commitTime:    time.Unix(seconds, 0).UTC(),
		exactTag:      exactTag,
		reachableTags: reachableTags,
		tagsAtCommit:  tagsAtCommit,
	}
	if r.revisions == nil {
		r.revisions = make(map[string]revision)
	}
	r.revisions[ref] = rev
	return rev
}

func (r *repo) prepareHistory() (err error) {
	if r.historyInitialized {
		return nil
	}
	dir, err := os.MkdirTemp("", "llar-vcs-compare-*")
	if err != nil {
		return err
	}
	r.historyDir = dir
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
			r.historyDir = ""
		}
	}()
	if err = r.client.syncHistory(context.Background(), r.owner, r.name, dir); err != nil {
		return err
	}
	r.historyInitialized = true
	runtime.AddCleanup(r, func(path string) {
		_ = os.RemoveAll(path)
	}, dir)
	return nil
}

func (r *repo) resolveCommit(ref string) (commitID, exactTag string, err error) {
	tagRef := "refs/tags/" + ref
	if _, checkErr := historyGit(context.Background(), r.historyDir, "check-ref-format", tagRef); checkErr == nil {
		if output, tagErr := historyGit(context.Background(), r.historyDir, "rev-parse", "--verify", "--end-of-options", tagRef+"^{commit}"); tagErr == nil {
			return strings.TrimSpace(string(output)), ref, nil
		}
	}
	output, err := historyGit(context.Background(), r.historyDir, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(string(output)), "", nil
}

func (r *repo) gitLines(args ...string) ([]string, error) {
	output, err := historyGit(context.Background(), r.historyDir, args...)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(output))
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}
