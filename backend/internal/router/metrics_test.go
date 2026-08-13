package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func scrape(t *testing.T, h http.Handler) string {
	t.Helper()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestMetricsEndpointExposesRequestSeries(t *testing.T) {
	h := testHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	body := scrape(t, h)
	for _, want := range []string{
		"kindred_http_requests_total",
		"kindred_http_request_duration_seconds",
		"kindred_websocket_connections",
		"go_goroutines",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape is missing %q", want)
		}
	}
}

// Metrics must label on the route template; labelling on the raw path would
// create one time series per conversation id.
func TestMetricsLabelOnRouteTemplateNotConcretePath(t *testing.T) {
	h := testHandler(t)

	id := "11111111-2222-3333-4444-555555555555"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/conversations/"+id+"/messages", nil)
	h.ServeHTTP(httptest.NewRecorder(), req)

	body := scrape(t, h)
	if strings.Contains(body, id) {
		t.Error("a conversation id leaked into a metric label")
	}
	if !strings.Contains(body, `route="/api/v1/conversations/{id}/messages"`) {
		t.Error("expected the matched route template as the label")
	}
}

func TestUnmatchedRoutesShareOneLabel(t *testing.T) {
	h := testHandler(t)

	for _, path := range []string{"/nope/one", "/nope/two"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	body := scrape(t, h)
	if !strings.Contains(body, `route="unmatched"`) {
		t.Error("unmatched requests should collapse into a single series")
	}
	if strings.Contains(body, "/nope/one") {
		t.Error("unmatched paths must not become labels")
	}
}

func TestTraceParentIsPropagatedAndGenerated(t *testing.T) {
	h := testHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	generated := rec.Header().Get("traceparent")
	if generated == "" {
		t.Fatal("expected a traceparent on the response")
	}
	if parts := strings.Split(generated, "-"); len(parts) != 4 || len(parts[1]) != 32 {
		t.Errorf("traceparent %q is not W3C shaped", generated)
	}

	inbound := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("traceparent", inbound)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	got := rec.Header().Get("traceparent")
	if !strings.Contains(got, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Errorf("traceparent = %q, want the inbound trace id to be continued", got)
	}
	if got == inbound {
		t.Error("expected a fresh span id within the inbound trace")
	}
}
