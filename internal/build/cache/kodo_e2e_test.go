package cache

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/goplus/llar/mod/module"
)

func TestKodoObjectName(t *testing.T) {
	c := NewKodo(KodoConfig{Prefix: "/cache/"}).(*kodoCache)
	key := Key{
		Module: module.Version{Path: "madler/zlib", Version: "v1.3.2"},
		Matrix: "amd64-linux",
	}
	if got, want := c.objectName(key), "cache/madler/zlib/v1.3.2/amd64-linux.tar.gz"; got != want {
		t.Fatalf("object name = %q, want %q", got, want)
	}
}

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
	const zlibVersion = "v1.3.1"
	matrix := hostMatrix()
	key := Key{
		Module: module.Version{Path: "madler/zlib", Version: zlibVersion},
		Matrix: matrix,
	}
	objectName := c.objectName(key)
	if want := prefix + "/madler/zlib/" + zlibVersion + "/" + matrix + ".tar.gz"; objectName != want {
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

	installDir, metadata := buildZlibWithLLAR(t, zlibVersion, matrix)
	want := Entry{
		Metadata: metadata,
	}
	got, err := c.Put(ctx, key, os.DirFS(installDir), want)
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

func buildZlibWithLLAR(t *testing.T, version, matrix string) (string, string) {
	t.Helper()

	llar, err := exec.LookPath("llar")
	if err != nil {
		t.Skip("llar not found in PATH")
	}
	for _, tool := range []string{"cmake", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found in PATH", tool)
		}
	}

	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	cacheHome := filepath.Join(dir, "cache")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("create home: %v", err)
	}
	if err := os.MkdirAll(cacheHome, 0o755); err != nil {
		t.Fatalf("create cache home: %v", err)
	}

	cmd := exec.Command(llar, "make", "madler/zlib@"+version)
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CACHE_HOME="+cacheHome)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("llar make madler/zlib@%s failed: %v\n%s", version, err, out)
	}

	metadata := strings.TrimSpace(string(out))
	if metadata != "-lz" {
		t.Fatalf("llar make metadata = %q, want -lz", metadata)
	}

	installDir := filepath.Join(cacheHome, ".llar", "workspaces", fmt.Sprintf("madler/zlib@%s-%s", version, matrix))
	if _, err := os.Stat(filepath.Join(installDir, "include", "zlib.h")); err != nil {
		t.Fatalf("zlib include not found in %s: %v", installDir, err)
	}
	if _, err := os.Stat(filepath.Join(installDir, "lib")); err != nil {
		t.Fatalf("zlib lib dir not found in %s: %v", installDir, err)
	}
	return installDir, metadata
}

func hostMatrix() string {
	return runtime.GOARCH + "-" + runtime.GOOS
}
