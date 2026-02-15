package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/google/uuid"
)

type contextKey string

const UserIDKey contextKey = "user_id"

func Auth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				writeJSON(w, http.StatusUnauthorized, `{"error":"missing authorization"}`)
				return
			}
			parts := strings.SplitN(header, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				writeJSON(w, http.StatusUnauthorized, `{"error":"invalid authorization"}`)
				return
			}
			userID, err := auth.ParseJWT(secret, parts[1])
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, `{"error":"invalid token"}`)
				return
			}
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(UserIDKey).(uuid.UUID)
	return id, ok
}

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
