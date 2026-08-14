// Package migrate applies the numbered SQL migrations in order, exactly once,
// recording a checksum per file so accidental edits to already-applied
// migrations are reported instead of silently ignored.
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

// advisoryLockKey serializes concurrent migrators (two API replicas booting at
// once). The constant is arbitrary but must be stable across builds.
const advisoryLockKey int64 = 8123472913470001

const createLedger = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT PRIMARY KEY,
    checksum   TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// Migration is a single embedded SQL file.
type Migration struct {
	Version  string
	SQL      string
	Checksum string
}

// Load reads and sorts every *.sql file in fsys.
func Load(fsys fs.FS) ([]Migration, error) {
	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("scan migrations: %w", err)
	}
	sort.Strings(names)

	out := make([]Migration, 0, len(names))
	for _, name := range names {
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		sum := sha256.Sum256(normalize(body))
		out = append(out, Migration{
			Version:  strings.TrimSuffix(name, ".sql"),
			SQL:      string(body),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	return out, nil
}

// normalize strips carriage returns so a file checked out with CRLF line endings
// on Windows hashes the same as on Linux CI.
func normalize(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if c != '\r' {
			out = append(out, c)
		}
	}
	return out
}

// Runner applies migrations against a database.
type Runner struct {
	Pool interface {
		db.Querier
		db.Beginner
	}
}

// Apply runs every pending migration in order and returns the versions applied.
// Each file runs in its own transaction, so a failure leaves earlier migrations
// committed and the failing one fully rolled back.
func (r Runner) Apply(ctx context.Context, fsys fs.FS) ([]string, error) {
	all, err := Load(fsys)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}

	// Session-level lock held for the whole run; released explicitly below.
	if _, err := r.Pool.Exec(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		return nil, fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		if _, err := r.Pool.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, advisoryLockKey); err != nil {
			slog.Warn("release migration lock failed", "error", err)
		}
	}()

	if _, err := r.Pool.Exec(ctx, createLedger); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := r.loadLedger(ctx)
	if err != nil {
		return nil, err
	}

	var ran []string
	for _, m := range all {
		if existing, ok := applied[m.Version]; ok {
			if existing != m.Checksum {
				return ran, fmt.Errorf(
					"migration %s was modified after being applied (recorded %s, found %s); "+
						"add a new migration instead of editing an applied one",
					m.Version, short(existing), short(m.Checksum))
			}
			continue
		}
		if err := r.applyOne(ctx, m); err != nil {
			return ran, err
		}
		slog.Info("migration applied", "version", m.Version)
		ran = append(ran, m.Version)
	}
	return ran, nil
}

func (r Runner) loadLedger(ctx context.Context) (map[string]string, error) {
	rows, err := r.Pool.Query(ctx, `SELECT version, checksum FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]string)
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[version] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	return applied, nil
}

func (r Runner) applyOne(ctx context.Context, m Migration) error {
	err := db.InTx(ctx, r.Pool, func(tx pgx.Tx) error {
		// Called without arguments so pgx uses the simple protocol, which allows
		// the multi-statement scripts these files contain.
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			return fmt.Errorf("execute: %w", err)
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)`,
			m.Version, m.Checksum)
		return err
	})
	if err != nil {
		return fmt.Errorf("migration %s: %w", m.Version, err)
	}
	return nil
}

func short(checksum string) string {
	if len(checksum) > 12 {
		return checksum[:12]
	}
	return checksum
}
