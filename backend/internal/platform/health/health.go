package health

import (
	"context"
	"net/http"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/httpx"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

const checkTimeout = 2 * time.Second

type Handler struct {
	Pool  *pgxpool.Pool
	Redis *goredis.Client
}

// Live reports process liveness only; it must not touch dependencies.
func (h *Handler) Live(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Ready reports whether dependencies are usable. Redis is only required when it
// has been configured.
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), checkTimeout)
	defer cancel()

	checks := map[string]string{}
	ready := true

	switch {
	case h.Pool == nil:
		checks["postgres"] = "not configured"
		ready = false
	case h.Pool.Ping(ctx) != nil:
		checks["postgres"] = "unreachable"
		ready = false
	default:
		checks["postgres"] = "ok"
	}

	switch {
	case h.Redis == nil:
		checks["redis"] = "disabled"
	case h.Redis.Ping(ctx).Err() != nil:
		checks["redis"] = "unreachable"
		ready = false
	default:
		checks["redis"] = "ok"
	}

	status := http.StatusOK
	body := "ok"
	if !ready {
		status = http.StatusServiceUnavailable
		body = "degraded"
	}
	httpx.WriteJSON(w, status, map[string]any{"status": body, "checks": checks})
}
