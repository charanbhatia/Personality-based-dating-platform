// Package metrics exposes Prometheus instrumentation for the HTTP API, the
// websocket gateway and the queue workers.
package metrics

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	goredis "github.com/redis/go-redis/v9"
)

const namespace = "kindred"

var (
	registry = prometheus.NewRegistry()

	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "http_requests_total",
		Help:      "HTTP requests by route, method and status.",
	}, []string{"route", "method", "status"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "http_request_duration_seconds",
		Help:      "HTTP request latency by route and method.",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
	}, []string{"route", "method"})

	rateLimitRejections = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "rate_limit_rejections_total",
		Help:      "Requests rejected by a rate limiter, by scope.",
	}, []string{"scope"})

	queueEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "queue_events_total",
		Help:      "Queue events by stream and outcome.",
	}, []string{"stream", "outcome"})

	queueHandlerDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "queue_handler_duration_seconds",
		Help:      "Queue handler latency by stream.",
		Buckets:   []float64{0.005, 0.025, 0.1, 0.5, 1, 5, 15},
	}, []string{"stream"})

	queueDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "queue_depth",
		Help:      "Entries currently held in a stream.",
	}, []string{"stream"})
)

func init() {
	registry.MustRegister(
		httpRequests,
		httpDuration,
		rateLimitRejections,
		queueEvents,
		queueHandlerDuration,
		queueDepth,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// Handler serves the Prometheus exposition format. It carries no
// authentication, so it should not be reachable from the public internet.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// ObserveHTTP records one request. route should be a path template rather than
// a concrete path, otherwise per-id labels explode cardinality.
func ObserveHTTP(route, method string, status int, d time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	httpRequests.WithLabelValues(route, method, strconv.Itoa(status)).Inc()
	httpDuration.WithLabelValues(route, method).Observe(d.Seconds())
}

func ObserveRateLimitRejection(scope string) {
	rateLimitRejections.WithLabelValues(scope).Inc()
}

// Queue outcomes.
const (
	OutcomeProcessed  = "processed"
	OutcomeFailed     = "failed"
	OutcomeDeadLetter = "dead_letter"
	OutcomeSkipped    = "skipped"
)

func ObserveQueueEvent(stream, outcome string) {
	queueEvents.WithLabelValues(stream, outcome).Inc()
}

func ObserveQueueHandler(stream string, d time.Duration) {
	queueHandlerDuration.WithLabelValues(stream).Observe(d.Seconds())
}

// RegisterWebSocketGauge reports live connection counts for this process.
func RegisterWebSocketGauge(count func() int64) error {
	return registry.Register(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "websocket_connections",
		Help:      "Websocket connections currently open on this instance.",
	}, func() float64 { return float64(count()) }))
}

// SampleQueueDepth polls stream lengths in the background so scraping never
// blocks on Redis.
func SampleQueueDepth(ctx context.Context, client *goredis.Client, streams []string, interval time.Duration) {
	if client == nil || len(streams) == 0 {
		return
	}
	if interval <= 0 {
		interval = 15 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		for _, stream := range streams {
			if n, err := client.XLen(ctx, stream).Result(); err == nil {
				queueDepth.WithLabelValues(stream).Set(float64(n))
			}
			if n, err := client.XLen(ctx, stream+":dead").Result(); err == nil {
				queueDepth.WithLabelValues(stream + ":dead").Set(float64(n))
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
