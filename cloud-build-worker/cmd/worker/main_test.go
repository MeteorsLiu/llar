package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/goplus/llar/cloud-build-worker/internal/artifact"
	"github.com/goplus/llar/cloud-build-worker/internal/build"
)

type fakeBuilds struct {
	result build.Result
	err    error
	logs   []string
	req    build.Request
}

func (b *fakeBuilds) Build(_ context.Context, req build.Request, log io.Writer) (build.Result, error) {
	b.req = req
	for _, text := range b.logs {
		if log != nil {
			_, _ = io.WriteString(log, text)
		}
	}
	return b.result, b.err
}

func TestPostJobsMissingTargetReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := routes(&fakeBuilds{})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader(`{"matrix":{"require":{"arch":"amd64","os":"linux"}}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPostJobsInvalidTargetReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := routes(&fakeBuilds{})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader(`{"matrix":{"require":{"arch":"amd64","os":"linux"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(targetHeader, "pnggroup/libpng")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPostJobsInvalidJSONReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := routes(&fakeBuilds{})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader(`{"matrix":`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(targetHeader, "madler/zlib@v1.3.1#amd64-linux")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPostJobsMissingMatrixReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := routes(&fakeBuilds{})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader(`{"matrix":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(targetHeader, "madler/zlib@v1.3.1#amd64-linux")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPostJobsNonVerboseCompleted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	builds := &fakeBuilds{result: build.Result{Artifact: readyArtifact()}}
	r := routes(builds)
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader(`{"matrix":{"require":{"arch":"amd64","os":"linux"},"options":{"debug":"false"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(targetHeader, "madler/zlib@v1.3.1#amd64-linux|debug=false")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if builds.req.Target.Module != "madler/zlib" || builds.req.Target.Version != "v1.3.1" || builds.req.MatrixStr != "amd64-linux|debug=false" {
		t.Fatalf("build request = %+v", builds.req)
	}
	if builds.req.Matrix.Options["debug"] != "false" {
		t.Fatalf("matrix options = %+v", builds.req.Matrix.Options)
	}
	var got statusMessage
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("Decode response: %v", err)
	}
	if got.Type != "status" || got.State != "completed" {
		t.Fatalf("message = %+v, want completed status", got)
	}
	if got.Body.Artifact == nil || got.Body.Artifact.Checksum != readyArtifact().Checksum {
		t.Fatalf("artifact = %+v", got.Body.Artifact)
	}
}

func TestPostJobsVerboseStreamsLogThenStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	builds := &fakeBuilds{
		result: build.Result{Artifact: readyArtifact()},
		logs:   []string{"checking\n", "building\n"},
	}
	r := routes(builds)
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs?verbose=1", strings.NewReader(`{"matrix":{"require":{"arch":"amd64","os":"linux"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(targetHeader, "madler/zlib@v1.3.1#amd64-linux")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	dec := json.NewDecoder(w.Body)
	var first logMessage
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("Decode first log: %v", err)
	}
	if first.Type != "log" || first.Data.Stream != "stderr" || first.Data.Text != "checking\n" {
		t.Fatalf("first log = %+v", first)
	}
	var second logMessage
	if err := dec.Decode(&second); err != nil {
		t.Fatalf("Decode second log: %v", err)
	}
	if second.Type != "log" || second.Data.Stream != "stderr" || second.Data.Text != "building\n" {
		t.Fatalf("second log = %+v", second)
	}
	var terminal statusMessage
	if err := dec.Decode(&terminal); err != nil {
		t.Fatalf("Decode terminal status: %v", err)
	}
	if terminal.State != "completed" || terminal.Body.Artifact == nil {
		t.Fatalf("terminal = %+v", terminal)
	}
}

func TestPostJobsConflictReturnsFailedStatus409(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := routes(&fakeBuilds{err: artifact.ErrConflict})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader(`{"matrix":{"require":{"arch":"amd64","os":"linux"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(targetHeader, "madler/zlib@v1.3.1#amd64-linux")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got statusMessage
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("Decode response: %v", err)
	}
	if got.State != "failed" || got.Body.Status != http.StatusConflict {
		t.Fatalf("message = %+v, want failed 409", got)
	}
}

func TestPostJobsBuildErrorReturnsFailedStatus500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := routes(&fakeBuilds{err: errors.New("llar make failed")})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader(`{"matrix":{"require":{"arch":"amd64","os":"linux"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(targetHeader, "madler/zlib@v1.3.1#amd64-linux")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got statusMessage
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("Decode response: %v", err)
	}
	if got.State != "failed" || got.Body.Status != http.StatusInternalServerError || got.Body.Message != "llar make failed" {
		t.Fatalf("message = %+v, want failed 500", got)
	}
}

func readyArtifact() artifact.Artifact {
	return artifact.Artifact{
		Source:   artifact.Source{Type: "ghcr", URL: "https://ghcr.io/v2/owner/madler/zlib/blobs/sha256:abc"},
		Type:     "zip",
		Metadata: "-lz",
		Checksum: "sha256:abc",
	}
}
