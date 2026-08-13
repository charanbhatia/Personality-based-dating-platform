package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestErrorConstructorsUseMatchingStatusAndCode(t *testing.T) {
	cases := []struct {
		err        *Error
		wantStatus int
		wantCode   string
	}{
		{BadRequest("x"), http.StatusBadRequest, CodeBadRequest},
		{Unauthorized("x"), http.StatusUnauthorized, CodeUnauthorized},
		{Forbidden("x"), http.StatusForbidden, CodeForbidden},
		{NotFound("x"), http.StatusNotFound, CodeNotFound},
		{Conflict("", "x"), http.StatusConflict, CodeConflict},
		{Conflict("already_matched", "x"), http.StatusConflict, "already_matched"},
		{NotImplemented("", "x"), http.StatusNotImplemented, CodeNotImplemented},
		{NotImplemented("media_unavailable", "x"), http.StatusNotImplemented, "media_unavailable"},
		{Internal(errors.New("boom")), http.StatusInternalServerError, CodeInternal},
		{CodedError(http.StatusTeapot, "brewing", "x"), http.StatusTeapot, "brewing"},
	}
	for _, tc := range cases {
		if tc.err.Status != tc.wantStatus {
			t.Errorf("%s: status = %d, want %d", tc.wantCode, tc.err.Status, tc.wantStatus)
		}
		if tc.err.Code != tc.wantCode {
			t.Errorf("code = %q, want %q", tc.err.Code, tc.wantCode)
		}
	}
}

func TestInternalHidesTheCauseFromClients(t *testing.T) {
	secret := errors.New("connection to db-primary.internal:5432 refused")
	err := Internal(secret)

	if !strings.Contains(err.Error(), secret.Error()) {
		t.Fatal("Error() must include the cause so it reaches the logs")
	}
	if !errors.Is(err, secret) {
		t.Fatal("errors.Is cannot reach the cause; Unwrap is not wired up")
	}

	body, marshalErr := json.Marshal(errorEnvelope{Error: err})
	if marshalErr != nil {
		t.Fatalf("marshal: %v", marshalErr)
	}
	if strings.Contains(string(body), "db-primary") {
		t.Fatalf("the serialized error leaks internal detail: %s", body)
	}
	if strings.Contains(string(body), `"Status"`) || strings.Contains(string(body), `"status"`) {
		t.Fatalf("status must not be duplicated in the body: %s", body)
	}
}

func TestErrorSerializesToTheAgreedEnvelope(t *testing.T) {
	err := BadRequest("limit must be an integer").WithDetails(map[string]any{"limit": "not an integer"})

	raw, marshalErr := json.Marshal(errorEnvelope{Error: err})
	if marshalErr != nil {
		t.Fatalf("marshal: %v", marshalErr)
	}
	var decoded struct {
		Error struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Details map[string]string `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Error.Code != CodeBadRequest {
		t.Errorf("code = %q, want %q", decoded.Error.Code, CodeBadRequest)
	}
	if decoded.Error.Message != "limit must be an integer" {
		t.Errorf("message = %q", decoded.Error.Message)
	}
	if decoded.Error.Details["limit"] != "not an integer" {
		t.Errorf("details = %v", decoded.Error.Details)
	}
}

func TestErrorOmitsEmptyDetails(t *testing.T) {
	raw, err := json.Marshal(errorEnvelope{Error: NotFound("user not found")})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "details") {
		t.Fatalf("empty details should be omitted, got %s", raw)
	}
}

func TestWriteErrorUsesTheErrorStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discover", nil)

	WriteError(rec, req, Conflict("assessment_required", "complete the assessment first"))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	var decoded struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Error.Code != "assessment_required" {
		t.Fatalf("code = %q", decoded.Error.Code)
	}
}

func TestWriteErrorMapsUnknownErrorsToAGeneric500(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/discover", nil)

	WriteError(rec, req, errors.New("pq: relation \"secret_table\" does not exist"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rec.Body.String(), "secret_table") {
		t.Fatalf("response leaks the underlying error: %s", rec.Body.String())
	}
}

func TestWriteErrorFindsAWrappedAPIError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	wrapped := errors.Join(errors.New("context"), NotFound("user not found"))
	WriteError(rec, req, wrapped)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; a wrapped *Error must keep its status", rec.Code, http.StatusNotFound)
	}
}

func TestWriteJSONSetsNoSniffHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusOK, map[string]string{"ok": "yes"})

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if rec.Body.String() != `{"ok":"yes"}` {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestWriteJSONWithNilWritesNoBody(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusNoContent, nil)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
}

type unmarshalable struct{}

func (unmarshalable) MarshalJSON() ([]byte, error) { return nil, errors.New("nope") }

func TestWriteJSONFallsBackWhenMarshalFails(t *testing.T) {
	rec := httptest.NewRecorder()
	// Buffering before writing the status is what makes this recoverable; a
	// streaming encoder would have already committed 200.
	WriteJSON(rec, http.StatusOK, unmarshalable{})

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	var decoded struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("fallback body is not valid JSON: %v", err)
	}
	if decoded.Error.Code != CodeInternal {
		t.Fatalf("code = %q, want %q", decoded.Error.Code, CodeInternal)
	}
}

func TestNoContent(t *testing.T) {
	rec := httptest.NewRecorder()
	NoContent(rec)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", rec.Body.String())
	}
}
