package middleware

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/httpx"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/ratelimit"
)

const maxPeekBody = 1 << 20

type RateLimitConfig struct {
	Limiter ratelimit.Limiter
	Limit   int
	Window  time.Duration
	Scope   string
	// FailClosed rejects requests when the limiter backend is unavailable.
	// Auth routes use this; general traffic degrades open instead.
	FailClosed bool
	// KeyByEmail adds a second counter keyed on the request body's email so
	// credential stuffing cannot be spread across many IPs.
	KeyByEmail bool
	Log        *slog.Logger
}

func RateLimit(cfg RateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if cfg.Limiter == nil || cfg.Limit <= 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			keys := []string{fmt.Sprintf("rl:%s:ip:%s:%s", cfg.Scope, ClientIP(r), r.URL.Path)}

			if cfg.KeyByEmail {
				if email := peekEmail(r); email != "" {
					sum := sha256.Sum256([]byte(strings.ToLower(email)))
					keys = append(keys, fmt.Sprintf("rl:%s:email:%s:%s", cfg.Scope, hex.EncodeToString(sum[:])[:16], r.URL.Path))
				}
			}

			for _, key := range keys {
				res, err := cfg.Limiter.Allow(r.Context(), key, cfg.Limit, cfg.Window)
				if err != nil {
					if cfg.Log != nil {
						cfg.Log.Warn("rate limiter unavailable", "error", err, "scope", cfg.Scope, "fail_closed", cfg.FailClosed)
					}
					if cfg.FailClosed {
						httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeInternalError, "service temporarily unavailable")
						return
					}
					break
				}

				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(res.Limit))
				w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))

				if !res.Allowed {
					w.Header().Set("Retry-After", strconv.Itoa(int(res.RetryAfter.Seconds())))
					httpx.WriteError(w, http.StatusTooManyRequests, httpx.CodeRateLimited, "too many requests, please retry later")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// peekEmail reads the email field from a JSON body and restores the body so the
// downstream handler can decode it again.
func peekEmail(r *http.Request) string {
	if r.Body == nil || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPeekBody))
	if err != nil {
		return ""
	}
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))

	var payload struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return payload.Email
}
