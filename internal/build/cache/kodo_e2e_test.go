package cache

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/goplus/llar/mod/module"
)

func TestKodoE2E_PutGet(t *testing.T) {
	accessKey := os.Getenv("QINIU_ACCESS_KEY")
	secretKey := os.Getenv("QINIU_SECRET_KEY")
	bucket := os.Getenv("QINIU_BUCKET")
	if accessKey == "" || secretKey == "" || bucket == "" {
		t.Skip("QINIU_ACCESS_KEY, QINIU_SECRET_KEY, and QINIU_BUCKET are required")
	}

	prefix := strings.Trim(os.Getenv("QINIU_PREFIX"), "/")
	if prefix != "" {
		prefix += "/"
	}
	prefix += fmt.Sprintf("llar-kodo-e2e/%d", time.Now().UnixNano())

	c := NewKodo(KodoConfig{
		AccessKey: accessKey,
		SecretKey: secretKey,
		Bucket:    bucket,
		Prefix:    prefix,
	}).(*kodoCache)

	ctx := context.Background()
	key := Key{
		Module: module.Version{Path: "madler/zlib", Version: "v1.2.11"},
		Matrix: "e2e-test",
	}
	objectName := c.objectName(key)
	if want := prefix + "/madler/zlib/e2e-test.tar.gz"; objectName != want {
		t.Fatalf("object name = %q, want %q", objectName, want)
	}
	defer func() {
		if err := c.objects.Bucket(c.bucket).Object(objectName).Delete().Call(ctx); err != nil && !isKodoObjectNotFound(err) {
			t.Errorf("delete %s: %v", objectName, err)
		}
	}()

	if _, ok, err := c.Get(ctx, key); err != nil {
		t.Fatalf("Get before Put failed: %v", err)
	} else if ok {
		t.Fatalf("Get before Put hit %s", objectName)
	}

	want := Entry{
		Metadata: "-lz",
		Deps: []module.Version{
			{Path: "test/dep", Version: "v1.0.0"},
		},
	}
	got, err := c.Put(ctx, key, fstest.MapFS{
		"include/zlib.h": {Data: []byte("#define ZLIB_VERSION \"1.2.11\"\n")},
		"lib/libz.a":     {Data: []byte("archive")},
	}, want)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if got.Metadata != want.Metadata || !slices.Equal(got.Deps, want.Deps) {
		t.Fatalf("Put entry = %+v, want %+v", got, want)
	}

	got, ok, err := c.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after Put failed: %v", err)
	}
	if !ok {
		t.Fatal("Get after Put missed")
	}
	if got.Metadata != want.Metadata || !slices.Equal(got.Deps, want.Deps) {
		t.Fatalf("Get entry = %+v, want %+v", got, want)
	}
}
