package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/goplus/llar/cloud-build-worker/internal/artifact"
	"github.com/goplus/llar/cloud-build-worker/internal/build"
	"github.com/goplus/llar/cloud-build-worker/internal/upload"
	_ "modernc.org/sqlite"
)

const targetHeader = "X-LLAR-Target"

type matrix struct {
	Require map[string]string `json:"require"`
	Options map[string]string `json:"options,omitempty"`
}

type jobRequest struct {
	Matrix matrix `json:"matrix"`
}

type target struct {
	Module    string
	Version   string
	MatrixStr string
}

type statusMessage struct {
	Type  string     `json:"type"`
	State string     `json:"state"`
	Body  statusBody `json:"body"`
}

type statusBody struct {
	Artifact *artifact.Artifact `json:"artifact,omitempty"`
	Status   int                `json:"status,omitempty"`
	Message  string             `json:"message,omitempty"`
}

type logMessage struct {
	Type string  `json:"type"`
	Data logData `json:"data"`
}

type logData struct {
	Stream string `json:"stream,omitempty"`
	Text   string `json:"text"`
}

type builds interface {
	Build(context.Context, build.Request, io.Writer) (build.Result, error)
}

func main() {
	db, err := sql.Open("sqlite", "artifacts.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	artifacts, err := artifact.NewSQLStore(db)
	if err != nil {
		log.Fatal(err)
	}
	builds := build.New(build.Options{
		Artifacts: artifacts,
		Uploader:  upload.NewGHCR(upload.GHCRConfig{}),
		Runner:    build.NewSubprocessRunner(),
	})
	if err := routes(builds).Run(); err != nil {
		log.Fatal(err)
	}
}

func parseTargetHeader(value string) (target, error) {
	beforeMatrix, matrixStr, ok := strings.Cut(value, "#")
	if !ok || matrixStr == "" {
		return target{}, fmt.Errorf("invalid %s", targetHeader)
	}
	module, version, ok := strings.Cut(beforeMatrix, "@")
	if !ok || module == "" || version == "" {
		return target{}, fmt.Errorf("invalid %s", targetHeader)
	}
	return target{Module: module, Version: version, MatrixStr: matrixStr}, nil
}

func routes(builds builds) *gin.Engine {
	r := gin.New()
	r.POST("/v1/jobs", func(c *gin.Context) {
		t, err := parseTargetHeader(c.GetHeader(targetHeader))
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		var body jobRequest
		if err := c.ShouldBindJSON(&body); err != nil || matrixEmpty(body.Matrix) {
			c.Status(http.StatusBadRequest)
			return
		}
		var log io.Writer
		if c.Query("verbose") == "1" {
			c.Header("Content-Type", "application/json")
			log = newVerboseWriter(c.Writer)
		}
		result, err := builds.Build(c.Request.Context(), build.Request{
			Target:    build.Target{Module: t.Module, Version: t.Version},
			MatrixStr: t.MatrixStr,
			Matrix: build.Matrix{
				Require: body.Matrix.Require,
				Options: body.Matrix.Options,
			},
		}, log)
		if err != nil {
			c.JSON(http.StatusOK, failedStatus(err))
			return
		}
		c.JSON(http.StatusOK, statusMessage{Type: "status", State: "completed", Body: statusBody{Artifact: &result.Artifact}})
	})
	return r
}

func matrixEmpty(matrix matrix) bool {
	return len(matrix.Require) == 0 && len(matrix.Options) == 0
}

func failedStatus(err error) statusMessage {
	status := http.StatusInternalServerError
	if errors.Is(err, artifact.ErrConflict) {
		status = http.StatusConflict
	}
	return statusMessage{
		Type:  "status",
		State: "failed",
		Body:  statusBody{Status: status, Message: err.Error()},
	}
}

type verboseWriter struct {
	writer gin.ResponseWriter
	enc    *json.Encoder
	mu     sync.Mutex
}

func newVerboseWriter(writer gin.ResponseWriter) *verboseWriter {
	return &verboseWriter{writer: writer, enc: json.NewEncoder(writer)}
}

func (w *verboseWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.enc.Encode(logMessage{
		Type: "log",
		Data: logData{Stream: "stderr", Text: string(p)},
	}); err != nil {
		return 0, err
	}
	w.writer.Flush()
	return len(p), nil
}
