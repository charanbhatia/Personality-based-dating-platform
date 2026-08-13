package router

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bits-assignment/dating-platform/backend/internal/platform/config"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Load()
	cfg.CORSOrigins = []string{"http://localhost:5173"}
	cfg.RateLimitEnabled = false
	h, err := New(Deps{
		Config: cfg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	return h
}

func TestPreflightIsAnsweredWithoutRouteMatch(t *testing.T) {
	h := testHandler(t)

	// PUT /api/profile is a route that only accepts PUT; the OPTIONS preflight
	// must still succeed with CORS headers rather than falling through to 405.
	req := httptest.NewRequest(http.MethodOptions, "/api/profile", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "PUT")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("Access-Control-Allow-Origin = %q, want the request origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Error("Access-Control-Allow-Headers must be present on preflight")
	}
}

func TestPreflightFromDisallowedOriginGetsNoCORSHeaders(t *testing.T) {
	h := testHandler(t)

	req := httptest.NewRequest(http.MethodOptions, "/api/profile", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for a disallowed origin", got)
	}
}

func TestHealthAndReadiness(t *testing.T) {
	h := testHandler(t)

	for _, path := range []string{"/healthz", "/health"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}

	// No database pool is configured in this test, so readiness must fail.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("GET /readyz = %d, want 503 without a database", rec.Code)
	}
}

func TestRequestIDIsAlwaysReturned(t *testing.T) {
	h := testHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Header().Get("X-Request-ID") == "" {
		t.Error("X-Request-ID must be set on every response")
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "caller-supplied-id")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Request-ID"); got != "caller-supplied-id" {
		t.Errorf("X-Request-ID = %q, want the inbound value to be propagated", got)
	}
}

func TestLegacyRoutesStillRequireAuth(t *testing.T) {
	h := testHandler(t)

	paths := []string{"/api/auth/me", "/api/profile", "/api/matches", "/api/conversations"}
	for _, p := range paths {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s = %d, want 401", p, rec.Code)
		}
	}
}

func TestUnknownRouteReturnsErrorEnvelope(t *testing.T) {
	h := testHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/does-not-exist", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}
