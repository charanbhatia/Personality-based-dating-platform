package router

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/config"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func mustNew(t *testing.T, deps Deps) http.Handler {
	t.Helper()
	h, err := New(deps)
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	return h
}

func probeRouter(t *testing.T) http.Handler {
	t.Helper()
	tokens, err := auth.NewTokenManager("router-test-secret-material-0123456789", "router-test", time.Minute)
	if err != nil {
		t.Fatalf("token manager: %v", err)
	}
	return mustNew(t, Deps{
		Config: &config.Config{RateLimitEnabled: false},
		Tokens: tokens,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.CORSOrigins = []string{"http://localhost:5173"}
	cfg.CORSAllowedOrigins = cfg.CORSOrigins
	cfg.RateLimitEnabled = false
	return mustNew(t, Deps{
		Config: cfg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func TestWrongMethodIs405WithAllow(t *testing.T) {
	h := probeRouter(t)

	// mux reports a method mismatch inconsistently underneath a subrouter, so
	// every mount is covered here rather than one representative case.
	cases := []struct {
		method, path string
		wantAllow    string
	}{
		{http.MethodDelete, "/api/v1/profile", "GET, PUT"},
		{http.MethodPost, "/api/v1/profile/photos", "PUT"},
		{http.MethodGet, "/api/v1/auth/login", "POST"},
		{http.MethodGet, "/api/v1/likes", "POST"},
		{http.MethodDelete, "/api/v1/preferences", "GET, PUT"},
		{http.MethodPut, "/api/v1/blocks/00000000-0000-0000-0000-000000000000", "DELETE"},
		{http.MethodGet, "/api/v1/personality/assessment/submit", "POST"},
		{http.MethodDelete, "/api/profile", "GET, PUT"},
		{http.MethodPost, "/api/matches", "GET"},
		{http.MethodPost, "/health", "GET"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))

			if rec.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d, want 405 (body: %s)", rec.Code, rec.Body)
			}
			if got := rec.Header().Get("Allow"); got != tc.wantAllow {
				t.Errorf("Allow = %q, want %q", got, tc.wantAllow)
			}
			if !strings.Contains(rec.Body.String(), "method_not_allowed") {
				t.Errorf("body = %s, want the method_not_allowed code", rec.Body)
			}
		})
	}
}

func TestUnknownPathIs404(t *testing.T) {
	h := probeRouter(t)

	// A path no verb serves must stay a 404, including one that shares a prefix
	// with a real route.
	paths := []string{
		"/",
		"/nope",
		"/api",
		"/api/nope",
		"/api/v1",
		"/api/v1/nope",
		"/api/v1/profile/nope",
		"/api/v2/profile",
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (body: %s)", rec.Code, rec.Body)
			}
			if got := rec.Header().Get("Allow"); got != "" {
				t.Errorf("Allow = %q, want it unset on a 404", got)
			}
			if !strings.Contains(rec.Body.String(), "not_found") {
				t.Errorf("body = %s, want the not_found code", rec.Body)
			}
		})
	}
}

func TestKnownRoutesReachTheirHandlers(t *testing.T) {
	h := probeRouter(t)

	// Guards against a fallback that swallows real routes: an authenticated
	// endpoint must reach the auth middleware and answer 401, not 404 or 405.
	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/profile"},
		{http.MethodPut, "/api/v1/profile"},
		{http.MethodPut, "/api/v1/profile/photos"},
		{http.MethodGet, "/api/v1/profile/options"},
		{http.MethodGet, "/api/v1/users/00000000-0000-0000-0000-000000000000/public"},
		{http.MethodGet, "/api/v1/personality/assessment"},
		{http.MethodPost, "/api/v1/personality/assessment/submit"},
		{http.MethodGet, "/api/v1/personality/me"},
		{http.MethodGet, "/api/v1/preferences"},
		{http.MethodPut, "/api/v1/preferences"},
		{http.MethodGet, "/api/v1/discover"},
		{http.MethodPost, "/api/v1/likes"},
		{http.MethodGet, "/api/v1/matches"},
		{http.MethodGet, "/api/v1/blocks"},
		{http.MethodPost, "/api/v1/blocks"},
		{http.MethodDelete, "/api/v1/blocks/00000000-0000-0000-0000-000000000000"},
		{http.MethodPost, "/api/v1/reports"},
		{http.MethodGet, "/api/v1/auth/me"},
		{http.MethodPost, "/api/v1/auth/logout"},
		{http.MethodGet, "/api/v1/auth/sessions"},
		{http.MethodDelete, "/api/v1/auth/sessions/00000000-0000-0000-0000-000000000000"},
		{http.MethodGet, "/api/auth/me"},
		{http.MethodGet, "/api/profile"},
		{http.MethodPut, "/api/profile"},
		{http.MethodGet, "/api/matches"},
		{http.MethodGet, "/api/matches/00000000-0000-0000-0000-000000000000"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401 (body: %s)", rec.Code, rec.Body)
			}
		})
	}
}

func TestPreflightIsAnsweredWithoutRouting(t *testing.T) {
	h := probeRouter(t)

	// The CORS wrapper answers OPTIONS ahead of the mux, so a preflight for a
	// real endpoint must not be turned into a 405 by the fallback.
	for _, path := range []string{"/api/v1/profile", "/api/v1/likes", "/api/v1/nope"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodOptions, path, nil)
			req.Header.Set("Origin", "http://localhost:5173")
			req.Header.Set("Access-Control-Request-Method", "PUT")

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want 204 (body: %s)", rec.Code, rec.Body)
			}
			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
				t.Errorf("Access-Control-Allow-Origin = %q, want the request origin", got)
			}
		})
	}
}

func TestCORSAllowlistIsEnforced(t *testing.T) {
	tokens, err := auth.NewTokenManager("router-test-secret-material-0123456789", "router-test", time.Minute)
	if err != nil {
		t.Fatalf("token manager: %v", err)
	}
	h := mustNew(t, Deps{
		Config: &config.Config{CORSAllowedOrigins: []string{"https://app.example.test"}},
		Tokens: tokens,
	})

	cases := map[string]string{
		"https://app.example.test": "https://app.example.test",
		"https://APP.example.test": "https://APP.example.test",
		// An origin outside the allowlist gets no header, so the browser blocks
		// the response even though the request was routed.
		"https://evil.example.test": "",
	}
	for origin, want := range cases {
		t.Run(origin, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			req.Header.Set("Origin", origin)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != want {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, want)
			}
			if want != "" && !strings.Contains(rec.Header().Get("Vary"), "Origin") {
				t.Error("Vary does not include Origin, so a shared cache could serve the wrong header")
			}
		})
	}
}

func TestPreflightIsAnsweredWithoutRouteMatch(t *testing.T) {
	h := testHandler(t)

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

func TestWebSocketUpgradeSurvivesTheMiddlewareChain(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.RateLimitEnabled = false
	cfg.WSAllowedOrigins = []string{"*"}

	handler := mustNew(t, Deps{Config: cfg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	userID := uuid.New()
	token, err := auth.NewJWT(cfg.JWTSecret, userID)
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?access_token=" + token
	conn, resp, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("upgrade failed through the middleware chain: %v (status %d)", err, status)
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var frame struct {
		Type string `json:"type"`
	}
	if err := conn.ReadJSON(&frame); err != nil {
		t.Fatalf("read greeting: %v", err)
	}
	if frame.Type != "connected" {
		t.Errorf("first frame = %q, want connected", frame.Type)
	}
}

func TestWebSocketRejectsMissingToken(t *testing.T) {
	h := testHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("GET /ws without a token = %d, want 401", rec.Code)
	}
}

