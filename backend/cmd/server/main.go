package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/config"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/logging"
	platformredis "github.com/bits-assignment/dating-platform/backend/internal/platform/redis"
	"github.com/bits-assignment/dating-platform/backend/internal/router"
	"github.com/joho/godotenv"
	goredis "github.com/redis/go-redis/v9"
)

const shutdownTimeout = 15 * time.Second

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	log := logging.New(cfg.LogLevel)

	if err := db.Init(cfg.DatabaseURL); err != nil {
		log.Error("database init failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	var redisClient *goredis.Client
	if cfg.RedisEnabled() {
		client, err := platformredis.New(context.Background(), cfg.RedisURL)
		if err != nil {
			log.Error("redis init failed", "error", err)
			os.Exit(1)
		}
		redisClient = client
		defer redisClient.Close()
		log.Info("redis connected")
	} else {
		log.Warn("redis not configured; rate limiting disabled")
	}

	rootCtx, stopBackground := context.WithCancel(context.Background())
	defer stopBackground()

	handler, err := router.New(router.Deps{Config: cfg, Redis: redisClient, Logger: log, Context: rootCtx})
	if err != nil {
		log.Error("router init failed", "error", err)
		os.Exit(1)
	}
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info("server listening", "addr", srv.Addr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown failed", "error", err)
	}
	log.Info("server stopped")
}
