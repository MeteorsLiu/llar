package modules

import (
	"context"
	"io/fs"
	"os"
	"testing"

	"github.com/goplus/llar/mod/module"
)

type comparatorRepo struct {
	compareFunc func(a, b string, compareTag func(a, b string) int) int
}

func (r *comparatorRepo) Tags(context.Context) ([]string, error) { return nil, nil }
func (r *comparatorRepo) Latest(context.Context) (string, error) { return "", nil }
func (r *comparatorRepo) CompareFunc(a, b string, compareTag func(a, b string) int) int {
	return r.compareFunc(a, b, compareTag)
}
func (r *comparatorRepo) At(string, string) fs.FS { return nil }
func (r *comparatorRepo) Sync(context.Context, string, string, string) error {
	return nil
}

func TestLoadComparator_VCSCompareBinding(t *testing.T) {
	var gotA, gotB string
	var calls int
	var tagOrder int
	repo := &comparatorRepo{compareFunc: func(a, b string, compareTag func(a, b string) int) int {
		calls++
		gotA, gotB = a, b
		tagOrder = compareTag("v1.0.0", "v2.0.0")
		return -1
	}}
	cmp, err := loadComparatorFS(
		os.DirFS("testdata").(fs.ReadFileFS),
		"vcscomp/vcscomp_cmp.gox",
		repo,
	)
	if err != nil {
		t.Fatal(err)
	}

	path := "owner/repo"
	a := module.Version{Path: path, Version: "aaaaaaaa"}
	b := module.Version{Path: path, Version: "bbbbbbbb"}
	if got := cmp(a, b); got != -1 {
		t.Fatalf("comparison = %d, want -1", got)
	}
	if gotA != a.Version || gotB != b.Version {
		t.Fatalf("Repo.CompareFunc arguments = (%q, %q), want (%q, %q)", gotA, gotB, a.Version, b.Version)
	}
	if tagOrder >= 0 {
		t.Fatalf("tag comparison = %d, want negative", tagOrder)
	}
	if calls != 1 {
		t.Fatalf("Repo.CompareFunc calls = %d, want 1", calls)
	}
}

func TestLoadComparator_VCSCompareContextIsolation(t *testing.T) {
	fsys := os.DirFS("testdata").(fs.ReadFileFS)
	path := "vcscomp/vcscomp_cmp.gox"
	first, err := loadComparatorFS(fsys, path, &comparatorRepo{compareFunc: func(a, b string, compareTag func(a, b string) int) int {
		return 1
	}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadComparatorFS(fsys, path, &comparatorRepo{compareFunc: func(a, b string, compareTag func(a, b string) int) int {
		return -1
	}})
	if err != nil {
		t.Fatal(err)
	}

	a := module.Version{Path: "owner/repo", Version: "aaaaaaaa"}
	b := module.Version{Path: "owner/repo", Version: "bbbbbbbb"}
	if got := first(a, b); got != 1 {
		t.Fatalf("first comparison after loading second context = %d, want 1", got)
	}
	if got := second(a, b); got != -1 {
		t.Fatalf("second comparison = %d, want -1", got)
	}
}
