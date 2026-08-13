package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/google/uuid"
)

// Error codes returned by the authentication middleware. `token_expired` is
// distinct from `unauthorized` so the client refreshes instead of logging the
// user out.
const (
	CodeTokenExpired = "token_expired"
	CodeTokenInvalid = "token_invalid"
)

type contextKey int

const principalKey contextKey = iota

// Middleware authenticates the bearer token and attaches the principal to the
// request context. Person C's shared middleware can replace this wiring; the
// context helpers below are the contract handlers depend on.
func Middleware(tokens *TokenManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, err := principalFromRequest(tokens, r)
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

func principalFromRequest(tokens *TokenManager, r *http.Request) (Principal, error) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return Principal{}, httpx.Unauthorized("authorization header is required")
	}
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "bearer") || strings.TrimSpace(token) == "" {
		return Principal{}, httpx.CodedError(http.StatusUnauthorized, CodeTokenInvalid,
			"authorization header must be in the form 'Bearer <token>'")
	}

	principal, err := tokens.Verify(strings.TrimSpace(token))
	if err != nil {
		if errors.Is(err, ErrTokenExpired) {
			return Principal{}, httpx.CodedError(http.StatusUnauthorized, CodeTokenExpired,
				"access token has expired")
		}
		return Principal{}, httpx.CodedError(http.StatusUnauthorized, CodeTokenInvalid,
			"access token is not valid").WithCause(err)
	}
	return principal, nil
}

// WithPrincipal stores an authenticated principal on the context.
func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey, principal)
}

// PrincipalFromContext returns the authenticated principal, if any.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey).(Principal)
	return principal, ok
}

// UserIDFromContext returns the authenticated user id.
func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	principal, ok := PrincipalFromContext(ctx)
	if !ok || principal.UserID == uuid.Nil {
		return uuid.Nil, false
	}
	return principal.UserID, true
}

// RequireUser returns the caller's id or a 401. Handlers behind Middleware use
// this instead of re-implementing the check, so a routing mistake that skips
// authentication fails closed.
func RequireUser(ctx context.Context) (uuid.UUID, error) {
	userID, ok := UserIDFromContext(ctx)
	if !ok {
		return uuid.Nil, httpx.Unauthorized("authentication is required")
	}
	return userID, nil
}
