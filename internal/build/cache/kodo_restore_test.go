package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goplus/llar/internal/artifact"
	"github.com/goplus/llar/internal/artifact/archiver"
	"github.com/goplus/llar/internal/metadata"
	"github.com/goplus/llar/mod/module"
	qiniuclient "github.com/qiniu/go-sdk/v7/client"
)

func TestKodoObjectName(t *testing.T) {
	c := newAuthenticatedKodo(KodoConfig{Prefix: "/cache/"})
	key := Key{
		Module: module.Version{Path: "madler/zlib", Version: "v1.3.2"},
		Matrix: "amd64-linux",
	}
	if got, want := c.objectName(key), "cache/madler/zlib/v1.3.2/amd64-linux.tar.gz"; got != want {
		t.Fatalf("object name = %q, want %q", got, want)
	}
	got, err := kodoSourceURL("llar.liuxi.ng", c.objectName(key))
	if err != nil {
		t.Fatal(err)
	}
	if want := "http://llar.liuxi.ng/cache/madler/zlib/v1.3.2/amd64-linux.tar.gz"; got != want {
		t.Fatalf("source url = %q, want %q", got, want)
	}
}

func newAuthenticatedKodo(cfg KodoConfig) *kodoCache {
	cfg.AccessKey = "test-access-key"
	cfg.SecretKey = "test-secret-key"
	return NewKodo(cfg).(*kodoCache)
}

func TestKodoPublicGet(t *testing.T) {
	workspaceDir := t.TempDir()
	key := Key{
		Module: module.Version{Path: "test/liba", Version: "1.0.0"},
		Matrix: "amd64-linux",
	}
	installDir := filepath.Join(workspaceDir, "test", "liba@1.0.0-amd64-linux")
	meta, err := metadata.Encode(metadata.Info{Metadata: "-L" + installDir + "/lib -lpublic"}, installDir)
	if err != nil {
		t.Fatal(err)
	}
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "include"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "include", "liba.h"), []byte("liba"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := archiver.Pack(source, archive, meta); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test/liba/1.0.0/amd64-linux.tar.gz" {
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	c := NewKodo(KodoConfig{PublicDomain: server.URL, WorkspaceDir: workspaceDir}).(*kodoCache)
	entry, ok, err := c.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("Get miss, want hit")
	}
	if want := "-L" + installDir + "/lib -lpublic"; entry.Metadata != want {
		t.Fatalf("metadata = %q, want %q", entry.Metadata, want)
	}
	if data, err := os.ReadFile(filepath.Join(installDir, "include", "liba.h")); err != nil || string(data) != "liba" {
		t.Fatalf("restored header = %q, %v", data, err)
	}

	missing := key
	missing.Module.Version = "2.0.0"
	if _, ok, err := c.Get(context.Background(), missing); err != nil || ok {
		t.Fatalf("missing Get = ok:%v err:%v, want miss", ok, err)
	}
}

func TestKodoPublicGetFailures(t *testing.T) {
	key := Key{
		Module: module.Version{Path: "test/liba", Version: "1.0.0"},
		Matrix: "amd64-linux",
	}

	t.Run("workspace required", func(t *testing.T) {
		c := NewKodo(KodoConfig{PublicDomain: "https://example.com"}).(*kodoCache)
		if _, _, err := c.Get(context.Background(), key); err == nil {
			t.Fatal("Get should require a workspace")
		}
	})
	t.Run("invalid domain", func(t *testing.T) {
		c := NewKodo(KodoConfig{PublicDomain: "file:///tmp", WorkspaceDir: t.TempDir()}).(*kodoCache)
		if _, _, err := c.Get(context.Background(), key); err == nil {
			t.Fatal("Get should reject an invalid public domain")
		}
	})
	t.Run("unreachable domain", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := listener.Addr().String()
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		c := NewKodo(KodoConfig{PublicDomain: "http://" + addr, WorkspaceDir: t.TempDir()}).(*kodoCache)
		if _, ok, err := c.Get(context.Background(), key); err != nil || ok {
			t.Fatalf("Get = ok:%v err:%v, want miss", ok, err)
		}
	})
	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}))
		defer server.Close()
		c := NewKodo(KodoConfig{PublicDomain: server.URL, WorkspaceDir: t.TempDir()}).(*kodoCache)
		if _, ok, err := c.Get(context.Background(), key); err != nil || ok {
			t.Fatalf("Get = ok:%v err:%v, want miss", ok, err)
		}
	})
	t.Run("corrupt archive", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("not a gzip archive"))
		}))
		defer server.Close()
		c := NewKodo(KodoConfig{PublicDomain: server.URL, WorkspaceDir: t.TempDir()}).(*kodoCache)
		if _, _, err := c.Get(context.Background(), key); err == nil {
			t.Fatal("Get should fail for a corrupt archive")
		}
	})
	t.Run("temp dir unavailable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(nil)
		}))
		defer server.Close()
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		c := NewKodo(KodoConfig{PublicDomain: server.URL, WorkspaceDir: t.TempDir()}).(*kodoCache)
		if _, _, err := c.Get(context.Background(), key); err == nil {
			t.Fatal("Get should fail without a temporary directory")
		}
	})
	t.Run("install dir blocked", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(nil)
		}))
		defer server.Close()
		workspaceDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workspaceDir, "test"), []byte("not a directory"), 0o644); err != nil {
			t.Fatal(err)
		}
		c := NewKodo(KodoConfig{PublicDomain: server.URL, WorkspaceDir: workspaceDir}).(*kodoCache)
		if _, _, err := c.Get(context.Background(), key); err == nil {
			t.Fatal("Get should fail when the install dir cannot be replaced")
		}
	})
}

func TestKodoPublicPutRequiresCredentials(t *testing.T) {
	c := NewKodo(KodoConfig{PublicDomain: "https://example.com", WorkspaceDir: t.TempDir()}).(*kodoCache)
	key := Key{
		Module: module.Version{Path: "test/liba", Version: "1.0.0"},
		Matrix: "amd64-linux",
	}
	if _, err := c.Put(context.Background(), key, os.DirFS(t.TempDir()), Entry{}); err == nil {
		t.Fatal("Put should require credentials")
	}
}

func TestKodoPublicGetInvalidMetadata(t *testing.T) {
	source := t.TempDir()
	archive := filepath.Join(t.TempDir(), "artifact.tar.gz")
	if err := archiver.Pack(source, archive, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	c := NewKodo(KodoConfig{PublicDomain: server.URL, WorkspaceDir: t.TempDir()}).(*kodoCache)
	key := Key{
		Module: module.Version{Path: "test/liba", Version: "1.0.0"},
		Matrix: "amd64-linux",
	}
	if _, _, err := c.Get(context.Background(), key); err == nil || !strings.Contains(err.Error(), "metadata is required") {
		t.Fatalf("Get error = %v, want metadata error", err)
	}
}

func TestKodoPublicGetInvalidModulePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(nil)
	}))
	defer server.Close()

	c := NewKodo(KodoConfig{PublicDomain: server.URL, WorkspaceDir: t.TempDir()}).(*kodoCache)
	key := Key{
		Module: module.Version{Path: "../evil", Version: "1.0.0"},
		Matrix: "amd64-linux",
	}
	if _, _, err := c.Get(context.Background(), key); err == nil {
		t.Fatal("Get should reject an invalid module path")
	}
}

func TestKodoPublicGetReadOnlyWorkspace(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(nil)
	}))
	defer server.Close()

	workspaceDir := t.TempDir()
	if err := os.Chmod(workspaceDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(workspaceDir, 0o755) })

	c := NewKodo(KodoConfig{PublicDomain: server.URL, WorkspaceDir: workspaceDir}).(*kodoCache)
	key := Key{
		Module: module.Version{Path: "test/liba", Version: "1.0.0"},
		Matrix: "amd64-linux",
	}
	if _, _, err := c.Get(context.Background(), key); err == nil {
		t.Fatal("Get should fail with a read-only workspace")
	}
}

func TestKodoPutInvalidModulePath(t *testing.T) {
	c := newAuthenticatedKodo(KodoConfig{Bucket: "test-bucket", WorkspaceDir: t.TempDir()})
	key := Key{
		Module: module.Version{Path: "../evil", Version: "1.0.0"},
		Matrix: "amd64-linux",
	}
	if _, err := c.Put(context.Background(), key, os.DirFS(t.TempDir()), Entry{}); err == nil {
		t.Fatal("Put should reject an invalid module path")
	}
}

func TestKodoGetArtifactMissAndError(t *testing.T) {
	key := Key{
		Module: module.Version{Path: "madler/zlib", Version: "v1.3.2"},
		Matrix: "linux-amd64",
	}
	t.Run("miss", func(t *testing.T) {
		c := newAuthenticatedKodo(KodoConfig{
			Artifacts: &recordingArtifactStore{err: artifact.ErrNotFound},
		})
		if _, ok, err := c.Get(context.Background(), key); err != nil {
			t.Fatal(err)
		} else if ok {
			t.Fatal("Get hit, want miss")
		}
	})
	t.Run("store error", func(t *testing.T) {
		wantErr := errors.New("artifact store failed")
		c := newAuthenticatedKodo(KodoConfig{
			Artifacts: &recordingArtifactStore{err: wantErr},
		})
		if _, _, err := c.Get(context.Background(), key); !errors.Is(err, wantErr) {
			t.Fatalf("Get error = %v, want %v", err, wantErr)
		}
	})
	t.Run("workspace required", func(t *testing.T) {
		c := newAuthenticatedKodo(KodoConfig{
			Artifacts: &recordingArtifactStore{art: artifact.Artifact{Type: "tar.gz"}},
		})
		if _, _, err := c.Get(context.Background(), key); err == nil {
			t.Fatal("Get should require a workspace for an artifact hit")
		}
	})
	t.Run("invalid install path", func(t *testing.T) {
		c := newAuthenticatedKodo(KodoConfig{
			WorkspaceDir: t.TempDir(),
			Artifacts: &recordingArtifactStore{art: artifact.Artifact{
				Type: "tar.gz",
			}},
		})
		if _, _, err := c.Get(context.Background(), Key{Matrix: "linux-amd64"}); err == nil {
			t.Fatal("Get should fail for empty module path")
		}
	})
}

func TestKodoPutLocalErrors(t *testing.T) {
	key := Key{
		Module: module.Version{Path: "madler/zlib", Version: "v1.3.2"},
		Matrix: "linux-amd64",
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "libz.a"), []byte("archive\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("empty bucket", func(t *testing.T) {
		c := newAuthenticatedKodo(KodoConfig{
			PublicDomain: "https://cdn.example.com",
			Artifacts:    &recordingArtifactStore{},
		})
		if _, err := c.Put(context.Background(), key, os.DirFS(src), Entry{}); err == nil {
			t.Fatal("Put should reject empty bucket")
		}
	})

	t.Run("temp file", func(t *testing.T) {
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		c := newAuthenticatedKodo(KodoConfig{
			Bucket:       "bucket",
			PublicDomain: "https://cdn.example.com",
			Artifacts:    &recordingArtifactStore{},
		})
		if _, err := c.Put(context.Background(), key, os.DirFS(src), Entry{}); err == nil {
			t.Fatal("Put should fail when temp dir is missing")
		}
	})

	t.Run("archive", func(t *testing.T) {
		src := t.TempDir()
		if err := os.Symlink("target", filepath.Join(src, "link")); err != nil {
			t.Fatal(err)
		}
		c := newAuthenticatedKodo(KodoConfig{
			Bucket:       "bucket",
			PublicDomain: "https://cdn.example.com",
			Artifacts:    &recordingArtifactStore{},
		})
		if _, err := c.Put(context.Background(), key, os.DirFS(src), Entry{}); err == nil {
			t.Fatal("Put should reject unsupported archive entry")
		}
	})

	t.Run("source url", func(t *testing.T) {
		c := newAuthenticatedKodo(KodoConfig{
			Bucket:       "bucket",
			PublicDomain: "ftp://cdn.example.com",
			Artifacts:    &recordingArtifactStore{},
		})
		if _, err := c.Put(context.Background(), key, os.DirFS(src), Entry{}); err == nil {
			t.Fatal("Put should reject non-http public domain")
		}
	})

	t.Run("upload", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		c := newAuthenticatedKodo(KodoConfig{
			AccessKey:    "ak",
			SecretKey:    "sk",
			Bucket:       "bucket",
			PublicDomain: "https://cdn.example.com",
			Artifacts:    &recordingArtifactStore{},
		})
		if _, err := c.Put(ctx, key, os.DirFS(src), Entry{}); err == nil {
			t.Fatal("Put should fail with canceled context")
		}
	})
}

func TestKodoRestoreLocalErrors(t *testing.T) {
	key := Key{
		Module: module.Version{Path: "madler/zlib", Version: "v1.3.2"},
		Matrix: "linux-amd64",
	}

	t.Run("temp file", func(t *testing.T) {
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		c := newAuthenticatedKodo(KodoConfig{
			Bucket:       "bucket",
			WorkspaceDir: t.TempDir(),
		})
		if _, err := c.restore(context.Background(), key, c.objectName(key), "tar.gz", ""); err == nil {
			t.Fatal("restore should fail when temp dir is missing")
		}
	})

	t.Run("download", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		c := newAuthenticatedKodo(KodoConfig{
			AccessKey:    "ak",
			SecretKey:    "sk",
			Bucket:       "bucket",
			WorkspaceDir: t.TempDir(),
		})
		if _, err := c.restore(ctx, key, c.objectName(key), "tar.gz", ""); err == nil {
			t.Fatal("restore should fail with canceled context")
		}
	})
}

func TestKodoHelpersRejectInvalidInputs(t *testing.T) {
	if _, err := kodoSourceURL("ftp://cdn.example.com", "object.tar.gz"); err == nil {
		t.Fatal("kodoSourceURL should reject ftp domain")
	}
	if _, err := kodoSourceURL("http://[::1", "object.tar.gz"); err == nil {
		t.Fatal("kodoSourceURL should reject invalid domain")
	}

	if !isKodoObjectNotFound(&qiniuclient.ErrorInfo{Code: 612}) {
		t.Fatal("612 should be object not found")
	}
	if isKodoObjectNotFound(&qiniuclient.ErrorInfo{Code: 614}) {
		t.Fatal("614 should not be object not found")
	}
	if !isKodoObjectExists(&qiniuclient.ErrorInfo{Code: 614}) {
		t.Fatal("614 should be object exists")
	}
	if isKodoObjectExists(errors.New("plain error")) {
		t.Fatal("plain error should not be object exists")
	}
}

func TestKodoFileSHA256(t *testing.T) {
	name := filepath.Join(t.TempDir(), "artifact.tar.gz")
	body := []byte("artifact bytes\n")
	if err := os.WriteFile(name, body, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	got, err := fileSHA256(name)
	if err != nil {
		t.Fatal(err)
	}
	if got != hex.EncodeToString(sum[:]) {
		t.Fatalf("fileSHA256 = %s, want %s", got, hex.EncodeToString(sum[:]))
	}

	if _, err := fileSHA256(filepath.Join(t.TempDir(), "missing.tar.gz")); err == nil {
		t.Fatal("fileSHA256 should fail for missing file")
	}
}

type recordingArtifactStore struct {
	art    artifact.Artifact
	err    error
	putErr error
}

func (s *recordingArtifactStore) Get(context.Context, artifact.Key) (artifact.Artifact, error) {
	if s.err != nil {
		return artifact.Artifact{}, s.err
	}
	return s.art, nil
}

func (s *recordingArtifactStore) Put(_ context.Context, _ artifact.Key, art artifact.Artifact) (artifact.Artifact, error) {
	if s.putErr != nil {
		return artifact.Artifact{}, s.putErr
	}
	s.art = art
	return art, nil
}

func (s *recordingArtifactStore) Delete(context.Context, artifact.Key) error {
	return nil
}
