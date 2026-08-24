package internal

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/goplus/llar/internal/artifact/archiver"
	"github.com/goplus/llar/internal/metadata"
	"github.com/goplus/llar/mod/module"
)

func TestWriteModuleResultJSONOmitsEmptyDeps(t *testing.T) {
	result := moduleOutputResult{
		Module:    module.Version{Path: "test/root", Version: "v1.0.0"},
		OutputDir: "/workspace/test/root@v1.0.0-linux-amd64",
		Metadata:  "-lroot",
	}

	var encoded bytes.Buffer
	if err := writeModuleResult(&encoded, result, true); err != nil {
		t.Fatal(err)
	}
	var output map[string]json.RawMessage
	if err := json.Unmarshal(encoded.Bytes(), &output); err != nil {
		t.Fatalf("JSON output is invalid: %v", err)
	}
	if _, ok := output["deps"]; ok {
		t.Fatalf("JSON output contains deps for an empty dependency list: %s", encoded.Bytes())
	}
	if got := string(output["dir"]); got != `"/workspace/test/root@v1.0.0-linux-amd64"` {
		t.Fatalf("JSON dir = %s", got)
	}
}

func TestWriteModuleOutputArchiveIncludesDependencies(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "root.a"), []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := moduleOutputResult{
		Module:    module.Version{Path: "test/root", Version: "v1.0.0"},
		Deps:      []moduleOutputDep{{Module: module.Version{Path: "test/dep", Version: "v1.2.3"}}},
		OutputDir: source,
		Metadata:  "-lroot",
	}
	dest := filepath.Join(t.TempDir(), "root.tar.gz")
	if err := writeModuleOutput(result, dest); err != nil {
		t.Fatalf("writeModuleOutput() error = %v", err)
	}

	metadataBytes, err := archiver.Unpack(dest, t.TempDir())
	if err != nil {
		t.Fatalf("archiver.Unpack() error = %v", err)
	}
	info, err := metadata.Decode(metadataBytes, source)
	if err != nil {
		t.Fatalf("metadata.Decode() error = %v", err)
	}
	if len(info.Deps) != 1 || info.Deps[0] != (module.Version{Path: "test/dep", Version: "v1.2.3"}) {
		t.Fatalf("metadata deps = %+v, want test/dep@v1.2.3", info.Deps)
	}
}

func TestWriteModuleOutputReturnsStatError(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(parent, "root.tar.gz")
	if err := writeModuleOutput(moduleOutputResult{}, dest); err == nil {
		t.Fatal("writeModuleOutput() succeeded below a regular file")
	}
}
