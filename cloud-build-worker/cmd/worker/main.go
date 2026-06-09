package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/goplus/llar/cloud-build-worker/internal/artifact"
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

type buildRequest struct {
	Target    target
	MatrixStr string
	Matrix    matrix
}

type buildResult struct {
	Artifact artifact.Artifact
}

type builds interface {
	Build(context.Context, buildRequest, io.Writer) (buildResult, error)
}

type stubBuilds struct{}

func (stubBuilds) Build(context.Context, buildRequest, io.Writer) (buildResult, error) {
	return buildResult{}, errors.New("build backend not configured")
}

func main() {
	if err := routes(stubBuilds{}).Run(); err != nil {
		panic(err)
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
		if err := c.ShouldBindJSON(&body); err != nil || len(body.Matrix.Require) == 0 {
			c.Status(http.StatusBadRequest)
			return
		}
		result, err := builds.Build(c.Request.Context(), buildRequest{Target: t, MatrixStr: t.MatrixStr, Matrix: body.Matrix}, nil)
		if err != nil {
			c.JSON(http.StatusOK, statusMessage{Type: "status", State: "failed", Body: statusBody{Status: 500, Message: err.Error()}})
			return
		}
		c.JSON(http.StatusOK, statusMessage{Type: "status", State: "completed", Body: statusBody{Artifact: &result.Artifact}})
	})
	return r
}
