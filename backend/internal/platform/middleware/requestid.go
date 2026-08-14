package middleware

import (
	"net/http"

	"github.com/google/uuid"
)

const HeaderRequestID = "X-Request-ID"

// RequestID propagates an inbound request ID or generates one, and seeds the
// per-request info record used by the logger.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set(HeaderRequestID, id)
		ctx := withInfo(r.Context(), &requestInfo{RequestID: id})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
