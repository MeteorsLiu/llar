package cache

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	artifact "github.com/goplus/llar/internal/artfact"
	"github.com/qiniu/go-sdk/v7/storagev2/credentials"
	httpclient "github.com/qiniu/go-sdk/v7/storagev2/http_client"
	"github.com/qiniu/go-sdk/v7/storagev2/objects"
)

type KodoArtifactConfig struct {
	AccessKey string
	SecretKey string
	Bucket    string
	Prefix    string
}

type kodoArtifact struct {
	bucket  string
	prefix  string
	objects *objects.ObjectsManager
}

func NewKodoArtifact(cfg KodoArtifactConfig) artifact.Store {
	cred := credentials.NewCredentials(cfg.AccessKey, cfg.SecretKey)
	return &kodoArtifact{
		bucket: cfg.Bucket,
		prefix: strings.Trim(cfg.Prefix, "/"),
		objects: objects.NewObjectsManager(&objects.ObjectsManagerOptions{
			Options: httpclient.Options{Credentials: cred},
		}),
	}
}

func (s *kodoArtifact) Get(ctx context.Context, key artifact.Key) (artifact.Artifact, bool, error) {
	objectName := s.objectName(key)
	object, err := s.objects.Bucket(s.bucket).Object(objectName).Stat().Call(ctx)
	if err != nil {
		if isKodoObjectNotFound(err) {
			return artifact.Artifact{}, false, nil
		}
		return artifact.Artifact{}, false, err
	}
	got, err := s.artifact(objectName, object.Metadata)
	if err != nil {
		return artifact.Artifact{}, false, err
	}
	return got, true, nil
}

func (s *kodoArtifact) Put(ctx context.Context, key artifact.Key, art artifact.Artifact) (artifact.Artifact, error) {
	got, ok, err := s.Get(ctx, key)
	if err != nil {
		return art, err
	}
	if !ok {
		return art, fmt.Errorf("kodo artifact object %s missing", s.objectName(key))
	}
	return got, nil
}

func (s *kodoArtifact) Delete(ctx context.Context, key artifact.Key) error {
	err := s.objects.Bucket(s.bucket).Object(s.objectName(key)).Delete().Call(ctx)
	if err != nil && !isKodoObjectNotFound(err) {
		return err
	}
	return nil
}

func (s *kodoArtifact) objectName(key artifact.Key) string {
	parts := make([]string, 0, 4)
	if s.prefix != "" {
		parts = append(parts, s.prefix)
	}
	parts = append(parts, strings.Trim(key.Module, "/"), strings.Trim(key.Version, "/"), key.MatrixStr+".tar.gz")
	return strings.Join(parts, "/")
}

func (s *kodoArtifact) artifact(objectName string, metadata map[string]string) (artifact.Artifact, error) {
	got, ok := kodoArtifactFromMetadata(metadata)
	if !ok {
		return artifact.Artifact{}, fmt.Errorf("read kodo artifact metadata for %s", objectName)
	}
	return got, nil
}

func encodeKodoArtifact(art artifact.Artifact) (string, error) {
	data, err := json.Marshal(art)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func kodoArtifactFromMetadata(metadata map[string]string) (artifact.Artifact, bool) {
	raw := kodoMetadataValue(metadata, kodoArtifactMetadataKey)
	if raw == "" {
		return artifact.Artifact{}, false
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return artifact.Artifact{}, false
	}
	var art artifact.Artifact
	if err := json.Unmarshal(data, &art); err != nil {
		return artifact.Artifact{}, false
	}
	return art, true
}
