// Command migrate applies pending SQL migrations and exits.
//
//	go run ./cmd/migrate            # apply pending migrations
//	go run ./cmd/migrate -status    # report state without applying
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/config"
	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/logging"
	"github.com/bits-assignment/dating-platform/backend/internal/migrate"
	"github.com/bits-assignment/dating-platform/backend/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	statusOnly := flag.Bool("status", false, "print migration status without applying anything")
	timeout := flag.Duration("timeout", 5*time.Minute, "overall timeout")
	flag.Parse()

	_ = godotenv.Load()
	logging.Setup(os.Getenv("LOG_LEVEL"), os.Getenv("LOG_FORMAT"))

	if err := run(*statusOnly, *timeout); err != nil {
		slog.Error("migrate failed", "error", err)
		os.Exit(1)
	}
}

func run(statusOnly bool, timeout time.Duration) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pool, err := db.Connect(ctx, cfg.DatabaseURL, db.Options{MaxConns: 2})
	if err != nil {
		return err
	}
	defer pool.Close()

	runner := migrate.Runner{Pool: pool}

	if statusOnly {
		return printStatus(ctx, pool)
	}

	applied, err := runner.Apply(ctx, migrations.FS)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		slog.Info("database already up to date")
		return nil
	}
	slog.Info("migrations applied", "count", len(applied), "versions", applied)
	return nil
}

func printStatus(ctx context.Context, pool *pgxpool.Pool) error {
	all, err := migrate.Load(migrations.FS)
	if err != nil {
		return err
	}
	applied := map[string]time.Time{}
	rows, err := pool.Query(ctx, `SELECT version, applied_at FROM schema_migrations`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var version string
			var at time.Time
			if err := rows.Scan(&version, &at); err != nil {
				return err
			}
			applied[version] = at
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	for _, m := range all {
		if at, ok := applied[m.Version]; ok {
			fmt.Printf("applied  %s  (%s)\n", m.Version, at.UTC().Format(time.RFC3339))
		} else {
			fmt.Printf("pending  %s\n", m.Version)
		}
	}
	return nil
}
