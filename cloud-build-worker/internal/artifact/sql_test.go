package artifact

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSQLStoreGetMissAndPut(t *testing.T) {
	store, closeDB := newTestSQLStore(t)
	defer closeDB()

	ctx := context.Background()
	key := Key{Module: "example.com/mod", Version: "v1.2.3", MatrixStr: "linux-amd64"}
	artifact := Artifact{
		Source:   Source{Type: "url", URL: "https://example.com/artifact.tar.gz"},
		Type:     "archive",
		Metadata: `{"os":"linux","arch":"amd64"}`,
		Checksum: "sha256:abc",
	}

	got, ok, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get miss returned error: %v", err)
	}
	if ok {
		t.Fatalf("Get miss returned ok with artifact: %+v", got)
	}

	inserted, err := store.Put(ctx, key, artifact)
	if err != nil {
		t.Fatalf("Put returned error: %v", err)
	}
	if inserted != artifact {
		t.Fatalf("Put returned %+v, want %+v", inserted, artifact)
	}

	got, ok, err = store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after Put returned error: %v", err)
	}
	if !ok {
		t.Fatal("Get after Put returned miss")
	}
	if got != artifact {
		t.Fatalf("Get returned %+v, want %+v", got, artifact)
	}
}

func TestSQLStorePutSameChecksumIsIdempotent(t *testing.T) {
	store, closeDB := newTestSQLStore(t)
	defer closeDB()

	ctx := context.Background()
	key := Key{Module: "example.com/mod", Version: "v1.2.3", MatrixStr: "linux-amd64"}
	artifact := Artifact{
		Source:   Source{Type: "url", URL: "https://example.com/artifact.tar.gz"},
		Type:     "archive",
		Metadata: `{"first":true}`,
		Checksum: "sha256:abc",
	}
	repeated := Artifact{
		Source:   Source{Type: "url", URL: "https://example.com/other.tar.gz"},
		Type:     "package",
		Metadata: `{"first":false}`,
		Checksum: artifact.Checksum,
	}

	inserted, err := store.Put(ctx, key, artifact)
	if err != nil {
		t.Fatalf("initial Put returned error: %v", err)
	}

	got, err := store.Put(ctx, key, repeated)
	if err != nil {
		t.Fatalf("idempotent Put returned error: %v", err)
	}
	if got != inserted {
		t.Fatalf("idempotent Put returned %+v, want existing %+v", got, inserted)
	}
}

func TestSQLStorePutDifferentChecksumConflicts(t *testing.T) {
	store, closeDB := newTestSQLStore(t)
	defer closeDB()

	ctx := context.Background()
	key := Key{Module: "example.com/mod", Version: "v1.2.3", MatrixStr: "linux-amd64"}
	artifact := Artifact{
		Source:   Source{Type: "url", URL: "https://example.com/artifact.tar.gz"},
		Type:     "archive",
		Metadata: `{"os":"linux","arch":"amd64"}`,
		Checksum: "sha256:abc",
	}
	conflicting := artifact
	conflicting.Checksum = "sha256:def"

	if _, err := store.Put(ctx, key, artifact); err != nil {
		t.Fatalf("initial Put returned error: %v", err)
	}

	got, err := store.Put(ctx, key, conflicting)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting Put error = %v, want %v", err, ErrConflict)
	}
	if got != (Artifact{}) {
		t.Fatalf("conflicting Put returned artifact %+v, want zero value", got)
	}
}

func TestSQLStoreDelete(t *testing.T) {
	store, closeDB := newTestSQLStore(t)
	defer closeDB()

	ctx := context.Background()
	key := Key{Module: "example.com/mod", Version: "v1.2.3", MatrixStr: "linux-amd64"}
	artifact := Artifact{
		Source:   Source{Type: "url", URL: "https://example.com/artifact.tar.gz"},
		Type:     "archive",
		Metadata: `{"os":"linux","arch":"amd64"}`,
		Checksum: "sha256:abc",
	}

	if _, err := store.Put(ctx, key, artifact); err != nil {
		t.Fatalf("Put returned error: %v", err)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}

	got, ok, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get after Delete returned error: %v", err)
	}
	if ok {
		t.Fatalf("Get after Delete returned ok with artifact: %+v", got)
	}
}

func newTestSQLStore(t *testing.T) (*SQLStore, func()) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}

	store, err := NewSQLStore(db)
	if err != nil {
		if closeErr := db.Close(); closeErr != nil {
			t.Fatalf("NewSQLStore returned error %v, and db.Close returned error %v", err, closeErr)
		}
		t.Fatalf("NewSQLStore returned error: %v", err)
	}

	assertPrimaryKey(t, db)

	return store, func() {
		if err := db.Close(); err != nil {
			t.Fatalf("db.Close returned error: %v", err)
		}
	}
}

func assertPrimaryKey(t *testing.T, db *sql.DB) {
	t.Helper()

	rows, err := db.Query(`PRAGMA table_info(artifacts)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info returned error: %v", err)
	}
	defer rows.Close()

	primaryKeyColumns := make(map[int]string)
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("PRAGMA row scan returned error: %v", err)
		}
		if pk != 0 {
			primaryKeyColumns[pk] = name
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("PRAGMA rows returned error: %v", err)
	}

	want := map[int]string{
		1: "module",
		2: "version",
		3: "matrix_str",
	}
	if len(primaryKeyColumns) != len(want) {
		t.Fatalf("primary key columns = %+v, want %+v", primaryKeyColumns, want)
	}
	for position, column := range want {
		if primaryKeyColumns[position] != column {
			t.Fatalf("primary key column %d = %q, want %q", position, primaryKeyColumns[position], column)
		}
	}
}
