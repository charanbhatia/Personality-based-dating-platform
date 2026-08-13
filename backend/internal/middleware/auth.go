// Package middleware is the pre-v1 HTTP middleware kept for the legacy /api
// handlers. Authentication now lives in internal/auth (Person B defines the JWT
// claims); this package only forwards to it so both route trees resolve the
// caller from a single implementation and context key.
//
// Person C's platform/middleware supersedes this package.
package middleware

import (
	"context"
	"net/http"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/google/uuid"
)

// Auth authenticates a request using the shared token manager.
func Auth(tokens *auth.TokenManager) func(http.Handler) http.Handler {
	return auth.Middleware(tokens)
}

// UserIDFromContext returns the authenticated user id.
func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	return auth.UserIDFromContext(ctx)
}
