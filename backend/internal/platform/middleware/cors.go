package middleware

import (
	"net/http"
	"strings"
)

const corsMaxAge = "86400"

// CORS applies an origin allowlist and answers preflight requests before they
// reach the router. Preflight must be handled here because gorilla/mux only
// runs route middleware on a successful method match, so an OPTIONS request
// would otherwise fall through to a 405 without CORS headers.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowAll := false
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
			continue
		}
		allowed[strings.ToLower(strings.TrimRight(o, "/"))] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			w.Header().Add("Vary", "Origin")

			if origin != "" {
				_, ok := allowed[strings.ToLower(strings.TrimRight(origin, "/"))]
				if allowAll || ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, Idempotency-Key")
					w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID")
					w.Header().Set("Access-Control-Max-Age", corsMaxAge)
				}
			}

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
