package httpx

import (
	"encoding/json"
	"net/http"
)

// Error codes used across v1 responses.
const (
	CodeBadRequest    = "bad_request"
	CodeUnauthorized  = "unauthorized"
	CodeForbidden     = "forbidden"
	CodeNotFound      = "not_found"
	CodeConflict      = "conflict"
	CodeRateLimited   = "rate_limited"
	CodeInternalError = "internal_error"
)

// ErrorBody matches the shared API convention:
// { "error": { "code": "...", "message": "...", "details": {} } }
type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type errorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// Page is the shared cursor-pagination envelope.
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, errorEnvelope{Error: ErrorBody{Code: code, Message: message}})
}

func WriteErrorDetails(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	WriteJSON(w, status, errorEnvelope{Error: ErrorBody{Code: code, Message: message, Details: details}})
}

func ReadJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
