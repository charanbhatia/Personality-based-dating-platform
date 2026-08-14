package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type payload struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func decodeRequest(t *testing.T, body string, contentType string, dst any) error {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return DecodeJSON(httptest.NewRecorder(), req, dst)
}

func assertStatus(t *testing.T, err error, want int) {
	t.Helper()
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T (%v), want *Error", err, err)
	}
	if apiErr.Status != want {
		t.Fatalf("status = %d, want %d (%v)", apiErr.Status, want, apiErr)
	}
}

func TestDecodeJSONAcceptsValidBody(t *testing.T) {
	var got payload
	if err := decodeRequest(t, `{"name":"Ada","age":36}`, "application/json", &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Name != "Ada" || got.Age != 36 {
		t.Fatalf("decoded %+v", got)
	}
}

func TestDecodeJSONAcceptsContentTypeWithCharset(t *testing.T) {
	var got payload
	if err := decodeRequest(t, `{"name":"Ada"}`, "application/json; charset=utf-8", &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeJSONAllowsAMissingContentType(t *testing.T) {
	// Some clients omit the header on a body-bearing POST; the body is still JSON.
	var got payload
	if err := decodeRequest(t, `{"name":"Ada"}`, "", &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeJSONRejectsWrongContentType(t *testing.T) {
	var got payload
	err := decodeRequest(t, `{"name":"Ada"}`, "text/plain", &got)
	assertStatus(t, err, http.StatusUnsupportedMediaType)
}

func TestDecodeJSONRejectsEmptyBody(t *testing.T) {
	var got payload
	err := decodeRequest(t, "", "application/json", &got)
	assertStatus(t, err, http.StatusBadRequest)
	if !strings.Contains(err.Error(), "required") {
		t.Fatalf("error %q should say the body is required", err)
	}
}

func TestDecodeJSONRejectsMalformedJSON(t *testing.T) {
	// Truncated and syntactically invalid bodies fail through different paths in
	// encoding/json, but both must read as malformed JSON to the client.
	for _, body := range []string{`{"name":`, `{"name" "Ada"}`, `{,}`, `["a"`} {
		err := decodeRequest(t, body, "application/json", &payload{})
		assertStatus(t, err, http.StatusBadRequest)
		if !strings.Contains(err.Error(), "malformed JSON") {
			t.Errorf("body %q: error %q should identify malformed JSON", body, err)
		}
	}
}

func TestDecodeJSONRejectsWrongFieldType(t *testing.T) {
	var got payload
	err := decodeRequest(t, `{"name":"Ada","age":"thirty-six"}`, "application/json", &got)
	assertStatus(t, err, http.StatusBadRequest)
	if !strings.Contains(err.Error(), "age") {
		t.Fatalf("error %q should name the offending field", err)
	}
}

func TestDecodeJSONRejectsTrailingContent(t *testing.T) {
	// Two concatenated objects: accepting this would silently ignore the second.
	var got payload
	err := decodeRequest(t, `{"name":"Ada"}{"name":"Grace"}`, "application/json", &got)
	assertStatus(t, err, http.StatusBadRequest)
}

func TestDecodeJSONEnforcesTheSizeCap(t *testing.T) {
	oversized := `{"name":"` + strings.Repeat("a", MaxBodyBytes+1) + `"}`
	var got payload
	err := decodeRequest(t, oversized, "application/json", &got)
	assertStatus(t, err, http.StatusRequestEntityTooLarge)
}

func TestDecodeJSONAcceptsABodyAtTheCap(t *testing.T) {
	filler := MaxBodyBytes - len(`{"name":""}`)
	body := `{"name":"` + strings.Repeat("a", filler) + `"}`
	if len(body) != MaxBodyBytes {
		t.Fatalf("test built a %d byte body, want %d", len(body), MaxBodyBytes)
	}
	var got payload
	if err := decodeRequest(t, body, "application/json", &got); err != nil {
		t.Fatalf("a body exactly at the cap was rejected: %v", err)
	}
}

func TestPathUUID(t *testing.T) {
	valid := uuid.New()
	cases := []struct {
		name    string
		vars    map[string]string
		wantErr bool
	}{
		{"valid", map[string]string{"id": valid.String()}, false},
		{"padded", map[string]string{"id": "  " + valid.String() + "  "}, false},
		{"uppercase", map[string]string{"id": strings.ToUpper(valid.String())}, false},
		{"missing", map[string]string{}, true},
		{"empty", map[string]string{"id": ""}, true},
		{"not a uuid", map[string]string{"id": "12345"}, true},
		{"nil uuid", map[string]string{"id": uuid.Nil.String()}, true},
		{"sql fragment", map[string]string{"id": "' OR 1=1 --"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := mux.SetURLVars(httptest.NewRequest(http.MethodGet, "/", nil), tc.vars)
			got, err := PathUUID(req, "id")
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				assertStatus(t, err, http.StatusBadRequest)
				return
			}
			if got != valid {
				t.Fatalf("got %v, want %v", got, valid)
			}
		})
	}
}

func TestQueryLimit(t *testing.T) {
	cases := []struct {
		query   string
		want    int
		wantErr bool
	}{
		{"", 20, false},
		{"?limit=", 20, false},
		{"?limit=1", 1, false},
		{"?limit=50", 50, false},
		{"?limit=%20%205%20", 5, false},
		{"?limit=0", 0, true},
		{"?limit=-1", 0, true},
		{"?limit=51", 0, true},
		{"?limit=abc", 0, true},
		{"?limit=1.5", 0, true},
		{"?limit=99999999999999999999", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/"+tc.query, nil)
			got, err := QueryLimit(req, 20, 50)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				assertStatus(t, err, http.StatusBadRequest)
				return
			}
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestWrapWritesReturnedErrors(t *testing.T) {
	handler := Wrap(func(http.ResponseWriter, *http.Request) error {
		return NotFound("user not found")
	})
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestWrapLeavesSuccessfulResponsesAlone(t *testing.T) {
	handler := Wrap(func(w http.ResponseWriter, _ *http.Request) error {
		WriteJSON(w, http.StatusCreated, map[string]bool{"ok": true})
		return nil
	})
	rec := httptest.NewRecorder()
	handler(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if rec.Body.String() != `{"ok":true}` {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestRequestIDPrefersTheContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := RequestID(req); got != "" {
		t.Fatalf("RequestID = %q, want empty when nothing set it", got)
	}

	req.Header.Set("X-Request-Id", "from-header")
	if got := RequestID(req); got != "from-header" {
		t.Fatalf("RequestID = %q, want the header value", got)
	}

	withCtx := req.WithContext(WithRequestID(req.Context(), "from-context"))
	if got := RequestID(withCtx); got != "from-context" {
		t.Fatalf("RequestID = %q, want the context value to win", got)
	}
}

func TestWithRequestIDIgnoresEmptyValues(t *testing.T) {
	ctx := WithRequestID(context.Background(), "")
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	req.Header.Set("X-Request-ID", "from-header")

	if got := RequestID(req); got != "from-header" {
		t.Fatalf("RequestID = %q, want the header to remain the fallback", got)
	}
}
