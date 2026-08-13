// Package testdb provides a Postgres pool for integration tests. Tests skip
// when TEST_DATABASE_URL is unset so the default `go test ./...` run needs no
// external services.
package testdb

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New returns a pool with every migration applied and all data cleared.
//
// Each test package gets its own database. `go test ./...` runs packages in
// parallel, so a single shared database would let one package truncate another
// package's fixtures mid-test.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()

	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping database integration test")
	}

	ctx := context.Background()
	url, err := ensureDatabase(ctx, base)
	if err != nil {
		t.Fatalf("prepare test database: %v", err)
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	t.Cleanup(pool.Close)

	for _, file := range migrationFiles(t) {
		sql, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", filepath.Base(file), err)
		}
	}

	// Every domain table is reachable from users by cascade.
	if _, err := pool.Exec(ctx, "TRUNCATE users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return pool
}

// CreateUser inserts a minimal user row for tests that only need a valid ID.
func CreateUser(t *testing.T, pool *pgxpool.Pool, email, name string) uuid.UUID {
	t.Helper()

	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, password_hash, name) VALUES ($1, $2, 'x', $3)`, id, email, name)
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return id
}

// ensureDatabase creates this package's database if needed and returns a URL
// pointing at it.
func ensureDatabase(ctx context.Context, base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}

	name := packageDatabase(strings.TrimPrefix(u.Path, "/"))
	if name == strings.TrimPrefix(u.Path, "/") {
		return base, nil
	}

	admin := *u
	admin.Path = "/" + strings.TrimPrefix(u.Path, "/")
	conn, err := pgx.Connect(ctx, admin.String())
	if err != nil {
		return "", err
	}
	defer conn.Close(ctx)

	// Two packages may reach this at the same time; losing the race is fine.
	_, err = conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", pgx.Identifier{name}.Sanitize()))
	if err != nil && !isDuplicateDatabase(err) {
		return "", err
	}

	target := *u
	target.Path = "/" + name
	return target.String(), nil
}

// packageDatabase derives a stable per-package name from the test binary, for
// example dating_platform_test_realtime.
func packageDatabase(base string) string {
	pkg := strings.TrimSuffix(filepath.Base(os.Args[0]), ".test")
	if pkg == "" || base == "" {
		return base
	}

	name := base + "_" + strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return '_'
	}, pkg)

	const maxIdentifier = 63
	if len(name) > maxIdentifier {
		name = name[:maxIdentifier]
	}
	return name
}

func isDuplicateDatabase(err error) bool {
	var pgErr *pgconn.PgError
	// 42P04 duplicate_database, 23505 unique_violation on a concurrent create.
	return errors.As(err, &pgErr) && (pgErr.Code == "42P04" || pgErr.Code == "23505")
}

func migrationFiles(t *testing.T) []string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve migrations directory")
	}

	dir := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "migrations")
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found in %s: %v", dir, err)
	}
	sort.Strings(files)
	return files
}
