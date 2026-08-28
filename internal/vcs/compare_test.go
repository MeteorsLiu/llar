// Copyright 2026 The XGo Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package vcs

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/mod/semver"
)

func TestRepoCompare(t *testing.T) {
	oldTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	r := &repo{revisions: map[string]revision{
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
	}}

	tests := []struct {
		name string
		a    string
		b    string
		want int
	}{
		{name: "same version", a: "after-v1-old", b: "after-v1-old", want: 0},
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
			got := r.CompareFunc(tt.a, tt.b, semver.Compare)
			if got < 0 && tt.want >= 0 || got == 0 && tt.want != 0 || got > 0 && tt.want <= 0 {
				t.Fatalf("CompareFunc(%q, %q) = %d, want sign %d", tt.a, tt.b, got, tt.want)
			}
			if reverse := r.CompareFunc(tt.b, tt.a, semver.Compare); got != -reverse {
				t.Fatalf("reverse comparison = %d, want %d", reverse, -got)
			}
		})
	}
}

func TestRepoCompareFuncUsesTagComparator(t *testing.T) {
	order := map[string]int{
		"release-one": 1,
		"release-two": 2,
	}
	compareTag := func(a, b string) int {
		return order[a] - order[b]
	}
	r := &repo{revisions: map[string]revision{
		"after-one": {
			commitID:      "1000",
			reachableTags: []string{"release-one"},
		},
		"after-two": {
			commitID:      "2000",
			reachableTags: []string{"release-one", "release-two"},
		},
	}}

	if got := r.CompareFunc("after-one", "after-two", compareTag); got >= 0 {
		t.Fatalf("CompareFunc with custom tag order = %d, want negative", got)
	}
}

func TestRepoCompareGitHistory(t *testing.T) {
	dir := t.TempDir()
	runCompareGit(t, dir, nil, "init")
	runCompareGit(t, dir, nil, "config", "user.name", "LLAR Test")
	runCompareGit(t, dir, nil, "config", "user.email", "llar@example.com")

	firstID := commitCompareFile(t, dir, "first\n", "2026-01-02T03:04:05Z")
	runCompareGit(t, dir, nil, "tag", "v1.0.0")
	secondID := commitCompareFile(t, dir, "second\n", "2026-02-03T04:05:06Z")
	thirdID := commitCompareFile(t, dir, "third\n", "2026-03-04T05:06:07Z")
	runCompareGit(t, dir, nil, "tag", "v2.0.0")

	r := &repo{
		historyDir:         dir,
		historyInitialized: true,
	}
	if got := r.CompareFunc("v1.0.0", secondID[:12], semver.Compare); got >= 0 {
		t.Fatalf("tag and descendant comparison = %d, want negative", got)
	}
	if got := r.CompareFunc(secondID, thirdID, semver.Compare); got >= 0 {
		t.Fatalf("base tag comparison = %d, want negative", got)
	}
	if got := r.CompareFunc(thirdID, "v2.0.0", semver.Compare); got != 0 {
		t.Fatalf("commit at tag comparison = %d, want 0", got)
	}
	if got := r.revisions["v1.0.0"].commitID; got != firstID {
		t.Fatalf("resolved tag commit = %q, want %q", got, firstID)
	}
}

func commitCompareFile(t *testing.T, dir, content, date string) string {
	t.Helper()
	if err := os.WriteFile(dir+"/source.txt", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	runCompareGit(t, dir, nil, "add", "source.txt")
	env := []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}
	runCompareGit(t, dir, env, "commit", "-m", strings.TrimSpace(content))
	return runCompareGit(t, dir, nil, "rev-parse", "HEAD")
}

func runCompareGit(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
