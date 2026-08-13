package middleware

import (
	"context"
	"net"
	"net/http"
	"strings"
)

type ctxKey int

const infoKey ctxKey = iota

// requestInfo is a mutable per-request record. The auth middleware fills in the
// user once the token is parsed so the access log can report it.
type requestInfo struct {
	RequestID string
	UserID    string
}

func withInfo(ctx context.Context, info *requestInfo) context.Context {
	return context.WithValue(ctx, infoKey, info)
}

func infoFrom(ctx context.Context) *requestInfo {
	info, _ := ctx.Value(infoKey).(*requestInfo)
	return info
}

// RequestIDFrom returns the request ID assigned by the RequestID middleware.
func RequestIDFrom(ctx context.Context) string {
	if info := infoFrom(ctx); info != nil {
		return info.RequestID
	}
	return ""
}

// SetUserID records the authenticated user on the current request so it appears
// in access logs. Safe to call when the middleware chain is not installed.
func SetUserID(ctx context.Context, userID string) {
	if info := infoFrom(ctx); info != nil {
		info.UserID = userID
	}
}

// ClientIP resolves the caller address, honouring X-Forwarded-For when present.
func ClientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		if first, _, ok := strings.Cut(fwd, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(fwd)
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
