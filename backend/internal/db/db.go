package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is the process-wide connection pool. Person C's platform layer will own
// pool construction; until then Init keeps the existing entrypoints working.
var Pool *pgxpool.Pool

// Options tunes the pool. Zero values fall back to pgx defaults.
type Options struct {
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout    time.Duration
}

// DefaultOptions are sized for a single API replica in front of a small
// Postgres: enough concurrency for request bursts, with recycling so a
// long-lived process does not pin stale backends.
func DefaultOptions() Options {
	return Options{
		MaxConns:          10,
		MinConns:          2,
		MaxConnLifetime:   time.Hour,
		MaxConnIdleTime:   30 * time.Minute,
		HealthCheckPeriod: time.Minute,
		ConnectTimeout:    10 * time.Second,
	}
}

// Connect builds and verifies a pool. Callers own closing it.
func Connect(ctx context.Context, databaseURL string, opts Options) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	defaults := DefaultOptions()
	if opts.MaxConns <= 0 {
		opts.MaxConns = defaults.MaxConns
	}
	if opts.MinConns <= 0 {
		opts.MinConns = defaults.MinConns
	}
	if opts.MinConns > opts.MaxConns {
		opts.MinConns = opts.MaxConns
	}
	if opts.MaxConnLifetime <= 0 {
		opts.MaxConnLifetime = defaults.MaxConnLifetime
	}
	if opts.MaxConnIdleTime <= 0 {
		opts.MaxConnIdleTime = defaults.MaxConnIdleTime
	}
	if opts.HealthCheckPeriod <= 0 {
		opts.HealthCheckPeriod = defaults.HealthCheckPeriod
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = defaults.ConnectTimeout
	}

	cfg.MaxConns = opts.MaxConns
	cfg.MinConns = opts.MinConns
	cfg.MaxConnLifetime = opts.MaxConnLifetime
	cfg.MaxConnIdleTime = opts.MaxConnIdleTime
	cfg.HealthCheckPeriod = opts.HealthCheckPeriod
	cfg.ConnConfig.ConnectTimeout = opts.ConnectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, opts.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Init connects and assigns the package-level Pool.
func Init(databaseURL string) error {
	return InitWithOptions(context.Background(), databaseURL, DefaultOptions())
}

func InitWithOptions(ctx context.Context, databaseURL string, opts Options) error {
	pool, err := Connect(ctx, databaseURL, opts)
	if err != nil {
		return err
	}
	Pool = pool
	slog.Info("database connected", "max_conns", pool.Config().MaxConns)
	return nil
}

func Close() {
	if Pool != nil {
		Pool.Close()
		Pool = nil
	}
}
