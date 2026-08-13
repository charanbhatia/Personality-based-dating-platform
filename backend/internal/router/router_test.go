package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/config"
)

// probeRouter builds the real routing table. Handlers are never reached by these
// tests, so nil services are fine: mux resolves the wrong-verb and unknown-path
// cases before dispatch.
func probeRouter(t *testing.T) http.Handler {
	t.Helper()
	tokens, err := auth.NewTokenManager("router-test-secret-material-0123456789", "router-test", time.Minute)
	if err != nil {
		t.Fatalf("token manager: %v", err)
	}
	return New(Deps{Config: &config.Config{}, Tokens: tokens})
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
	h := New(Deps{
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
