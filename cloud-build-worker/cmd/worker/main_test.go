package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeBuilds struct{}

func (fakeBuilds) Build(context.Context, buildRequest, io.Writer) (buildResult, error) {
	return buildResult{}, nil
}

func TestPostJobsMissingTargetReturns400(t *testing.T) {
	r := routes(fakeBuilds{})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader(`{"matrix":{"require":{"arch":"amd64","os":"linux"}}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestPostJobsInvalidTargetReturns400(t *testing.T) {
	r := routes(fakeBuilds{})
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader(`{"matrix":{"require":{"arch":"amd64","os":"linux"}}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(targetHeader, "pnggroup/libpng")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
