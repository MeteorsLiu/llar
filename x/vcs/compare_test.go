// Copyright 2026 The XGo Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vcs

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/goplus/llar/internal/execbroker"
	"github.com/goplus/llar/mod/module"
	"golang.org/x/mod/semver"
)

func TestCompareRevisions(t *testing.T) {
	oldTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	revisions := map[string]revision{
		"v1.0.0": {
			commitID: "1000",
			exactTag: "v1.0.0",
		},
		"v2.0.0": {
			commitID: "2000",
			exactTag: "v2.0.0",
		},
		"after-v1-old": {
			commitID:      "1100",
			commitTime:    oldTime,
			reachableTags: []string{"v1.0.0"},
		},
		"after-v1-new": {
			commitID:      "1200",
			commitTime:    newTime,
			reachableTags: []string{"v1.0.0"},
		},
		"after-v2-old": {
			commitID:      "2100",
			commitTime:    oldTime,
			reachableTags: []string{"v1.0.0", "v2.0.0"},
		},
		"at-v2": {
			commitID:      "2000",
			commitTime:    oldTime,
			reachableTags: []string{"v1.0.0", "v2.0.0"},
			tagsAtCommit:  []string{"v2.0.0"},
		},
		"untagged-old": {
			commitID:   "0100",
			commitTime: oldTime,
		},
		"untagged-new": {
			commitID:   "0200",
			commitTime: newTime,
		},
		"same-time-low": {
			commitID:      "1300",
			commitTime:    newTime,
			reachableTags: []string{"v1.0.0"},
		},
		"same-time-high": {
			commitID:      "1400",
			commitTime:    newTime,
			reachableTags: []string{"v1.0.0"},
		},
	}

	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "tag before descendant", a: "v1.0.0", b: "after-v1-old", want: -1},
		{name: "same base by time", a: "after-v1-old", b: "after-v1-new", want: -1},
		{name: "base tag before time", a: "after-v1-new", b: "after-v2-old", want: -1},
		{name: "commit at tag equals tag", a: "at-v2", b: "v2.0.0", want: 0},
		{name: "untagged before tagged", a: "untagged-new", b: "v1.0.0", want: -1},
		{name: "untagged by time", a: "untagged-old", b: "untagged-new", want: -1},
		{name: "same time by commit", a: "same-time-low", b: "same-time-high", want: -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compareRevisions(revisions[tt.a], revisions[tt.b], semver.Compare)
			if got < 0 && tt.want >= 0 || got == 0 && tt.want != 0 || got > 0 && tt.want <= 0 {
				t.Fatalf("compareRevisions(%q, %q) = %d, want sign %d", tt.a, tt.b, got, tt.want)
			}
			if reverse := compareRevisions(revisions[tt.b], revisions[tt.a], semver.Compare); got != -reverse {
				t.Fatalf("reverse comparison = %d, want %d", reverse, -got)
			}
		})
	}
}

func TestCompareRevisionsUsesTagComparator(t *testing.T) {
	order := map[string]int{
		"release-one": 1,
		"release-two": 2,
	}
	compareTag := func(a, b string) int {
		return order[a] - order[b]
	}
	left := revision{
		commitID:      "1000",
		reachableTags: []string{"release-one"},
	}
	right := revision{
		commitID:      "2000",
		reachableTags: []string{"release-one", "release-two"},
	}

	if got := compareRevisions(left, right, compareTag); got >= 0 {
		t.Fatalf("comparison with custom tag order = %d, want negative", got)
	}
}

func TestCompareFuncGitHistory(t *testing.T) {
	dir := t.TempDir()
	mustRunGit(t, dir, nil, "init")
	mustRunGit(t, dir, nil, "config", "user.name", "LLAR Test")
	mustRunGit(t, dir, nil, "config", "user.email", "llar@example.com")

	commitFile(t, dir, "first\n", "2026-01-02T03:04:05Z")
	mustRunGit(t, dir, nil, "tag", "v1.0.0")
	secondID := commitFile(t, dir, "second\n", "2026-02-03T04:05:06Z")
	thirdID := commitFile(t, dir, "third\n", "2026-03-04T05:06:07Z")
	mustRunGit(t, dir, nil, "tag", "v2.0.0")

	err := execbroker.Do(execbroker.Scope{
		Middleware: func(req execbroker.Request) (execbroker.Request, error) {
			if req.Name == "git" && len(req.Args) == 4 && req.Args[0] == "remote" && req.Args[1] == "add" {
				req.Args[3] = dir
			}
			return req, nil
		},
	}, func() error {
		path := "owner/repo"
		if got := CompareFunc(module.Version{Path: path, Version: "v1.0.0"}, module.Version{Path: path, Version: secondID[:12]}, semver.Compare); got >= 0 {
			t.Fatalf("tag and descendant comparison = %d, want negative", got)
		}
		if got := CompareFunc(module.Version{Path: path, Version: secondID}, module.Version{Path: path, Version: thirdID}, semver.Compare); got >= 0 {
			t.Fatalf("commit time comparison = %d, want negative", got)
		}
		if got := CompareFunc(module.Version{Path: path, Version: thirdID}, module.Version{Path: path, Version: "v2.0.0"}, semver.Compare); got != 0 {
			t.Fatalf("commit at tag comparison = %d, want 0", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func commitFile(t *testing.T, dir, content, date string) string {
	t.Helper()
	if err := os.WriteFile(dir+"/source.txt", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, dir, nil, "add", "source.txt")
	env := []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}
	mustRunGit(t, dir, env, "commit", "-m", strings.TrimSpace(content))
	return mustRunGit(t, dir, nil, "rev-parse", "HEAD")
}

func mustRunGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := execbroker.Command("git", args...)
	cmd.Dir = dir
	if cmd.Env == nil {
		cmd.Env = execbroker.Environ()
	}
	cmd.Env = append(cmd.Env, env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
