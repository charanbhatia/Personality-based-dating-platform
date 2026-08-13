package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/httpx"
)

func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				log.Error("panic recovered",
					"error", rec,
					"method", r.Method,
					"path", r.URL.Path,
					"request_id", RequestIDFrom(r.Context()),
					"stack", string(debug.Stack()),
				)
				httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "internal server error")
			}()
			next.ServeHTTP(w, r)
		})
	}
}
