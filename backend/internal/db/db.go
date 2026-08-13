package db

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func Init(databaseURL string) error {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return err
	}
	// Sized so that (API replicas x MaxConns) stays under the Postgres
	// max_connections budget.
	cfg.MaxConns = int32(envInt("DB_MAX_CONNS", 10))
	cfg.MinConns = int32(envInt("DB_MIN_CONNS", 0))
	cfg.MaxConnLifetime = time.Duration(envInt("DB_MAX_CONN_LIFETIME_MINUTES", 60)) * time.Minute
	cfg.MaxConnIdleTime = time.Duration(envInt("DB_MAX_CONN_IDLE_MINUTES", 5)) * time.Minute

	Pool, err = pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		return err
	}
	return Pool.Ping(context.Background())
}

func Close() {
	if Pool != nil {
		Pool.Close()
	}
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
