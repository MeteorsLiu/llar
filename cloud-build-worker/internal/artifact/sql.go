package artifact

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type SQLStore struct {
	db *sql.DB
}

func NewSQLStore(db *sql.DB) (*SQLStore, error) {
	if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS artifacts (
  module      TEXT NOT NULL,
  version     TEXT NOT NULL,
  matrix_str  TEXT NOT NULL,
  source_type TEXT NOT NULL,
  source_url  TEXT NOT NULL,
  type        TEXT NOT NULL,
  metadata    TEXT NOT NULL,
  checksum    TEXT NOT NULL,
  created_at  TIMESTAMP NOT NULL,
  expires_at  TIMESTAMP NULL,
  PRIMARY KEY (module, version, matrix_str)
);`); err != nil {
		return nil, fmt.Errorf("create artifacts table: %w", err)
	}

	return &SQLStore{db: db}, nil
}

func (s *SQLStore) Get(ctx context.Context, key Key) (Artifact, bool, error) {
	artifact, ok, err := s.get(ctx, key)
	if err != nil {
		return Artifact{}, false, err
	}
	return artifact, ok, nil
}

func (s *SQLStore) Put(ctx context.Context, key Key, artifact Artifact) (Artifact, error) {
	existing, ok, err := s.get(ctx, key)
	if err != nil {
		return Artifact{}, err
	}
	if ok {
		if existing.Checksum != artifact.Checksum {
			return Artifact{}, ErrConflict
		}
		return existing, nil
	}

	if _, err := s.db.ExecContext(ctx, `
INSERT INTO artifacts (
  module,
  version,
  matrix_str,
  source_type,
  source_url,
  type,
  metadata,
  checksum,
  created_at,
  expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		key.Module,
		key.Version,
		key.MatrixStr,
		artifact.Source.Type,
		artifact.Source.URL,
		artifact.Type,
		artifact.Metadata,
		artifact.Checksum,
		time.Now().UTC(),
	); err != nil {
		return Artifact{}, fmt.Errorf("insert artifact: %w", err)
	}

	return artifact, nil
}

func (s *SQLStore) Delete(ctx context.Context, key Key) error {
	if _, err := s.db.ExecContext(ctx, `
DELETE FROM artifacts
WHERE module = ? AND version = ? AND matrix_str = ?`,
		key.Module,
		key.Version,
		key.MatrixStr,
	); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	return nil
}

func (s *SQLStore) get(ctx context.Context, key Key) (Artifact, bool, error) {
	var artifact Artifact
	err := s.db.QueryRowContext(ctx, `
SELECT source_type, source_url, type, metadata, checksum
FROM artifacts
WHERE module = ? AND version = ? AND matrix_str = ?`,
		key.Module,
		key.Version,
		key.MatrixStr,
	).Scan(
		&artifact.Source.Type,
		&artifact.Source.URL,
		&artifact.Type,
		&artifact.Metadata,
		&artifact.Checksum,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Artifact{}, false, nil
	}
	if err != nil {
		return Artifact{}, false, fmt.Errorf("get artifact: %w", err)
	}
	return artifact, true, nil
}
