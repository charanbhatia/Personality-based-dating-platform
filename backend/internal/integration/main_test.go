// Package integration exercises the HTTP API against a real Postgres instance.
//
// These tests run the same object graph as cmd/server (via app.New) behind an
// httptest server, so they cover routing, middleware, SQL and transaction
// behaviour that unit tests cannot reach — the discovery query, the mutual-match
// advisory lock and refresh-token rotation are only meaningful against a real
// database.
//
// They are skipped unless TEST_DATABASE_URL is set, and they TRUNCATE the tables
// they use, so that variable must point at a throwaway database:
//
//	$env:TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/dating_test?sslmode=disable"
//	go test ./internal/integration/...
package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/app"
	"github.com/bits-assignment/dating-platform/backend/internal/config"
	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/migrate"
	"github.com/bits-assignment/dating-platform/backend/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testRetakeInterval is short enough that a retake test does not have to wait,
// but long enough that a fresh assessment still reports can_retake=false.
const testRetakeInterval = time.Hour

var (
	testApp    *app.App
	testServer *httptest.Server
	testPool   *pgxpool.Pool
)

func TestMain(m *testing.M) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "integration: TEST_DATABASE_URL is not set; skipping")
		os.Exit(0)
	}

	// Handler errors are expected throughout these tests; keep the output readable.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn, db.Options{MaxConns: 10})
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: connect: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if _, err := (migrate.Runner{Pool: pool}).Apply(ctx, migrations.FS); err != nil {
		fmt.Fprintf(os.Stderr, "integration: migrate: %v\n", err)
		os.Exit(1)
	}

	application, err := app.New(testConfig(dsn), pool, app.Options{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "integration: build app: %v\n", err)
		os.Exit(1)
	}

	testPool = pool
	testApp = application
	testServer = httptest.NewServer(application.Handler)
	defer testServer.Close()

	os.Exit(m.Run())
}

func testConfig(dsn string) *config.Config {
	return &config.Config{
		Env:                      "test",
		Port:                     "0",
		DatabaseURL:              dsn,
		JWTSecret:                "integration-test-secret-material-0123456789",
		JWTIssuer:                "dating-platform",
		AccessTokenTTL:           15 * time.Minute,
		RefreshTokenTTL:          30 * 24 * time.Hour,
		PasswordResetTTL:         time.Hour,
		EmailVerificationTTL:     24 * time.Hour,
		AssessmentRetakeInterval: testRetakeInterval,
		DiscoverDefaultLimit:     20,
		DiscoverMaxLimit:         50,
		TraitCacheTTL:            time.Minute,
		OutboxBatchSize:          100,
		OutboxPollInterval:       time.Second,
		OutboxMaxAttempts:        5,
		ReadHeaderTimeout:        10 * time.Second,
		ShutdownTimeout:          5 * time.Second,
	}
}

// resetDB clears all user-derived data. The question bank is seeded by migration
// and deliberately preserved.
//
// Tests that assert on the contents of the discovery feed need the candidate set
// to be exactly what they created, so they start from an empty database. Go runs
// tests within a package sequentially unless they opt into t.Parallel, which none
// of these do.
func resetDB(t *testing.T) {
	t.Helper()
	// Cached trait vectors are keyed by user id, and every run creates fresh
	// ids, so entries for truncated users can never be read back.
	_, err := testPool.Exec(context.Background(),
		`TRUNCATE users, outbox_events RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("reset database: %v", err)
	}
}
