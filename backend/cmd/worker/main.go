package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/media"
	"github.com/bits-assignment/dating-platform/backend/internal/notifications"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/config"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/events"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/logging"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/metrics"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/queue"
	platformredis "github.com/bits-assignment/dating-platform/backend/internal/platform/redis"
	"github.com/joho/godotenv"
	goredis "github.com/redis/go-redis/v9"
)

// consumer binds a handler to a stream. Domain packages register theirs in
// registerConsumers as they land.
type consumer struct {
	stream  string
	group   string
	handler queue.Handler
}

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	log := logging.New(cfg.LogLevel)

	if !cfg.RedisEnabled() {
		log.Error("REDIS_URL is required to run the worker")
		os.Exit(1)
	}

	if err := db.Init(cfg.DatabaseURL); err != nil {
		log.Error("database init failed", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	redisClient, err := platformredis.New(ctx, cfg.RedisURL)
	if err != nil {
		log.Error("redis init failed", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	q := queue.New(redisClient, log, cfg.QueueMaxLen)
	consumers, err := registerConsumers(cfg, q, redisClient, log)
	if err != nil {
		log.Error("consumer registration failed", "error", err)
		os.Exit(1)
	}
	if len(consumers) == 0 {
		log.Warn("worker started with no consumers registered")
	}

	var wg sync.WaitGroup
	for _, c := range consumers {
		wg.Add(1)
		go func(c consumer) {
			defer wg.Done()
			log.Info("consuming", "stream", c.stream, "group", c.group, "consumer", cfg.WorkerName)

			err := q.Consume(ctx, queue.ConsumerConfig{
				Stream:       c.stream,
				Group:        c.group,
				Consumer:     cfg.WorkerName,
				Concurrency:  cfg.WorkerConcurrency,
				MaxAttempts:  cfg.QueueMaxAttempts,
				RetryDelay:   cfg.QueueRetryDelay,
				BlockTimeout: cfg.QueueBlockTimeout,
			}, c.handler)
			if err != nil && ctx.Err() == nil {
				log.Error("consumer stopped", "stream", c.stream, "error", err)
			}
		}(c)
	}

	// The worker has no API surface, but it still needs to be scrapeable and
	// probeable.
	metricsSrv := &http.Server{
		Addr:              ":" + cfg.WorkerMetricsPort,
		Handler:           workerAdminHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("metrics listener failed", "error", err)
		}
	}()

	go metrics.SampleQueueDepth(ctx, redisClient, streamsOf(consumers), cfg.MetricsSampleInterval)

	log.Info("worker started",
		"name", cfg.WorkerName,
		"concurrency", cfg.WorkerConcurrency,
		"metrics_addr", metricsSrv.Addr)
	<-ctx.Done()
	log.Info("worker shutting down")

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	_ = metricsSrv.Shutdown(shutdownCtx)

	wg.Wait()
	log.Info("worker stopped")
}

func workerAdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

func streamsOf(consumers []consumer) []string {
	out := make([]string, 0, len(consumers))
	for _, c := range consumers {
		out = append(out, c.stream)
	}
	return out
}

func registerConsumers(cfg *config.Config, q *queue.Queue, redisClient *goredis.Client, log *slog.Logger) ([]consumer, error) {
	var consumers []consumer

	if cfg.MediaEnabled() {
		mediaSvc, err := media.NewServiceFromConfig(db.Pool, cfg, q, log)
		if err != nil {
			return nil, err
		}
		consumers = append(consumers, consumer{
			stream:  events.StreamMedia,
			group:   "media-workers",
			handler: media.ProcessHandler(mediaSvc),
		})
	} else {
		log.Warn("object storage not configured; media processing disabled")
	}

	notificationSvc := notifications.NewServiceFromConfig(db.Pool, redisClient, cfg, log)
	consumers = append(consumers,
		consumer{
			stream:  events.StreamNotifications,
			group:   "notification-workers",
			handler: notifications.NotificationsHandler(notificationSvc, log),
		},
		consumer{
			stream:  events.StreamEmail,
			group:   "email-workers",
			handler: notifications.EmailHandler(notificationSvc, log),
		},
	)

	return consumers, nil
}
