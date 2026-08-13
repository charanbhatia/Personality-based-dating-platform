// Command server runs the HTTP API.
package main

import (
	"context"
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
	"github.com/bits-assignment/dating-platform/backend/migrations"
	"github.com/joho/godotenv"
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

	// Signal-aware root context: Ctrl-C or SIGTERM begins a graceful shutdown
	// rather than dropping in-flight requests.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL, db.Options{MaxConns: cfg.DBMaxConns})
	if err != nil {
		return err
	}
	defer pool.Close()
	// Kept for the packages that still read the package-level pool.
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

	application, err := app.New(cfg, pool, app.Options{})
	if err != nil {
		return err
	}
	waitForBackground := application.StartBackground(ctx)

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           application.Handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		// No WriteTimeout: it would also cap long-lived responses, which Person C's
		// WebSocket upgrade will need on this same server.
		IdleTimeout: 2 * time.Minute,
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

	// Stop accepting new work and let in-flight requests finish before the
	// background workers and the pool go away.
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

// autoMigrate reports whether the server should apply migrations at boot.
// Enabled by default so a fresh clone works with one command; set
// AUTO_MIGRATE=false where a deployment applies them as a separate step.
func autoMigrate() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AUTO_MIGRATE"))) {
	case "false", "0", "no":
		return false
	default:
		return true
	}
}
