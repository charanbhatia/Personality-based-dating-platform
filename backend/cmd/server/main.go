// Command server runs the HTTP API.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/app"
	"github.com/bits-assignment/dating-platform/backend/internal/config"
	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/logging"
	"github.com/bits-assignment/dating-platform/backend/internal/migrate"
	"github.com/bits-assignment/dating-platform/backend/internal/outbox"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/events"
	platformlog "github.com/bits-assignment/dating-platform/backend/internal/platform/logging"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/queue"
	platformredis "github.com/bits-assignment/dating-platform/backend/internal/platform/redis"
	"github.com/bits-assignment/dating-platform/backend/migrations"
	"github.com/joho/godotenv"
	goredis "github.com/redis/go-redis/v9"
)

func main() {
	_ = godotenv.Load()
	logging.Setup(os.Getenv("LOG_LEVEL"), os.Getenv("LOG_FORMAT"))

	if err := run(); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log := platformlog.New(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL, db.Options{MaxConns: cfg.DBMaxConns, MinConns: cfg.DBMinConns})
	if err != nil {
		return err
	}
	defer pool.Close()
	db.Pool = pool
	slog.Info("database connected", "max_conns", pool.Config().MaxConns)

	if autoMigrate() {
		applied, err := migrate.Runner{Pool: pool}.Apply(ctx, migrations.FS)
		if err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		if len(applied) > 0 {
			slog.Info("migrations applied", "versions", applied)
		}
	}

	var redisClient *goredis.Client
	if cfg.RedisEnabled() {
		client, err := platformredis.New(ctx, cfg.RedisURL)
		if err != nil {
			return fmt.Errorf("redis: %w", err)
		}
		redisClient = client
		defer redisClient.Close()
		slog.Info("redis connected")
	} else {
		slog.Warn("redis not configured; rate limiting, queues and cross-replica websocket fanout are disabled")
	}

	opts := app.Options{
		Redis:  redisClient,
		Logger: log,
	}
	if redisClient != nil {
		opts.Publisher = redisOutboxPublisher{queue.New(redisClient, log, cfg.QueueMaxLen)}
	}

	application, err := app.New(cfg, pool, opts)
	if err != nil {
		return err
	}
	waitForBackground := application.StartBackground(ctx)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           application.Handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		IdleTimeout:       2 * time.Minute,
	}

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", server.Addr, "env", cfg.Env)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
			return
		}
		serverErrors <- nil
	}()

	select {
	case err := <-serverErrors:
		waitForBackground()
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
		slog.Info("shutdown signal received", "timeout", cfg.ShutdownTimeout)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed; closing connections", "error", err)
		_ = server.Close()
	}
	waitForBackground()
	slog.Info("server stopped")
	return nil
}

func autoMigrate() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AUTO_MIGRATE"))) {
	case "false", "0", "no":
		return false
	default:
		return true
	}
}

// redisOutboxPublisher bridges B's outbox rows onto C's Redis Streams so
// notifications and email workers see match.created and password-reset events.
type redisOutboxPublisher struct {
	q *queue.Queue
}

func (p redisOutboxPublisher) Publish(ctx context.Context, event outbox.Event) error {
	var payload any
	if len(event.Payload) > 0 {
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
	}
	stream := events.StreamNotifications
	switch event.Type {
	case outbox.EventPasswordResetRequired, outbox.EventEmailVerificationRequested:
		stream = events.StreamEmail
	}
	_, err := p.q.Publish(ctx, stream, event.Type, payload)
	return err
}
