package build

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

type SubprocessRunner struct {
	command string
}

func NewSubprocessRunner() *SubprocessRunner {
	return &SubprocessRunner{command: "llar"}
}

func (r *SubprocessRunner) Run(ctx context.Context, req Request, log io.Writer) (RunResult, error) {
	tmpDir, err := os.MkdirTemp("", "llar-worker-*")
	if err != nil {
		return RunResult{}, err
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, "artifact.zip")
	args := []string{"make", "-v", "-o", archivePath, targetArg(req.Target)}
	args = append(args, matrixArgs(req.Matrix)...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.command, args...)
	cmd.Stdout = &stdout
	if log == nil {
		cmd.Stderr = &stderr
	} else {
		cmd.Stderr = io.MultiWriter(log, &stderr)
	}

	if err := cmd.Run(); err != nil {
		return RunResult{}, fmt.Errorf("llar make: %w: %s", err, stderr.String())
	}
	metadata, stdoutLog := splitMetadata(stdout.Bytes())
	if log != nil && len(stdoutLog) > 0 {
		if _, err := log.Write(stdoutLog); err != nil {
			return RunResult{}, err
		}
	}

	archive, err := os.Open(archivePath)
	if err != nil {
		return RunResult{}, err
	}
	return RunResult{
		Archive:  archive,
		Type:     "zip",
		Metadata: metadata,
	}, nil
}

func targetArg(target Target) string {
	if target.Version == "" {
		return target.Module
	}
	return target.Module + "@" + target.Version
}

func matrixArgs(matrix Matrix) []string {
	args := make([]string, 0, len(matrix.Require)*2+len(matrix.Options)*2)

	requireKeys := sortedKeys(matrix.Require)
	for _, key := range requireKeys {
		args = append(args, "--"+key, matrix.Require[key])
	}

	optionKeys := sortedKeys(matrix.Options)
	for _, key := range optionKeys {
		args = append(args, "--matrix-"+key, matrix.Options[key])
	}
	return args
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func splitMetadata(stdout []byte) (metadata string, log []byte) {
	end := len(stdout)
	for end > 0 && (stdout[end-1] == '\n' || stdout[end-1] == '\r') {
		end--
	}
	if end == 0 {
		return "", stdout
	}
	start := end
	for start > 0 && stdout[start-1] != '\n' && stdout[start-1] != '\r' {
		start--
	}
	return string(stdout[start:end]), stdout[:start]
}
