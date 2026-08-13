package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

const (
	HeaderTraceParent = "traceparent"
	traceVersion      = "00"
	traceFlagsSampled = "01"
)

// Trace maintains W3C trace context across services: an inbound traceparent is
// continued, otherwise a new trace is started. The id is attached to every log
// line and echoed back, so requests can be correlated today and handed to an
// OpenTelemetry exporter later without changing call sites.
func Trace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID, ok := parseTraceParent(r.Header.Get(HeaderTraceParent))
		if !ok {
			traceID = randomHex(16)
		}
		spanID := randomHex(8)

		if info := infoFrom(r.Context()); info != nil {
			info.TraceID = traceID
		}
		w.Header().Set(HeaderTraceParent, strings.Join([]string{traceVersion, traceID, spanID, traceFlagsSampled}, "-"))

		next.ServeHTTP(w, r)
	})
}

// TraceIDFrom returns the current trace id, if the middleware is installed.
func TraceIDFrom(ctx context.Context) string {
	if info := infoFrom(ctx); info != nil {
		return info.TraceID
	}
	return ""
}

// parseTraceParent accepts version-traceid-spanid-flags and rejects the
// all-zero trace id the spec treats as invalid.
func parseTraceParent(header string) (string, bool) {
	parts := strings.Split(header, "-")
	if len(parts) != 4 || len(parts[1]) != 32 {
		return "", false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", false
	}
	if strings.Trim(parts[1], "0") == "" {
		return "", false
	}
	return parts[1], true
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", n*2-1) + "1"
	}
	return hex.EncodeToString(b)
}
