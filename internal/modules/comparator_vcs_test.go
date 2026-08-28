package modules

import (
	"io/fs"
	"os"
	"testing"

	"github.com/goplus/llar/mod/module"
)

func TestLoadComparatorVCS(t *testing.T) {
	cmp, err := loadComparatorFS(
		os.DirFS("testdata").(fs.ReadFileFS),
		"vcscomp/vcscomp_cmp.gox",
	)
	if err != nil {
		t.Fatal(err)
	}

	version := module.Version{Path: "owner/repo", Version: "aaaaaaaa"}
	if got := cmp(version, version); got != 0 {
		t.Fatalf("comparison = %d, want 0", got)
	}
}
