// Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/goplus/llar/internal/artifact"
	"github.com/goplus/llar/internal/build/cache"
	"github.com/goplus/llar/mod/module"
)

func main() {
	// llar make owns sysroot preparation. Publish its result through the normal
	// Kodo cache API so both workers can use the run's isolated artifact prefix.
	var result struct {
		Path     string `json:"path"`
		Version  string `json:"version"`
		Dir      string `json:"dir"`
		Metadata string `json:"metadata"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&result); err != nil {
		log.Fatal(err)
	}
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		log.Fatal(err)
	}
	accessKey := os.Getenv("QINIU_ACCESS_KEY")
	secretKey := os.Getenv("QINIU_SECRET_KEY")
	bucket := os.Getenv("QINIU_BUCKET")
	prefix := os.Args[1]
	artifacts := artifact.NewKodoArtifact(artifact.KodoArtifactConfig{
		AccessKey: accessKey,
		SecretKey: secretKey,
		Bucket:    bucket,
		Prefix:    prefix,
	})
	buildCache := cache.NewKodo(cache.KodoConfig{
		AccessKey:    accessKey,
		SecretKey:    secretKey,
		Bucket:       bucket,
		PublicDomain: os.Getenv("QINIU_PUBLIC_DOMAIN"),
		Prefix:       prefix,
		WorkspaceDir: filepath.Join(userCacheDir, ".llar", "workspaces"),
		Artifacts:    artifacts,
	})
	key := cache.Key{
		Module: module.Version{Path: result.Path, Version: result.Version},
		Matrix: "arm64-linux",
	}
	if _, err := buildCache.Put(context.Background(), key, os.DirFS(result.Dir), cache.Entry{Metadata: result.Metadata}); err != nil {
		log.Fatal(err)
	}
	log.Printf("prepared cluster cache for %s@%s (%s)", key.Module.Path, key.Module.Version, key.Matrix)
}
