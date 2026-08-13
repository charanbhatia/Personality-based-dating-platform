package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// MaxBodyBytes caps request bodies for JSON endpoints. Photo galleries and
// assessment submissions are the largest payloads and stay far below this.
const MaxBodyBytes = 1 << 20 // 1 MiB

// WriteJSON serializes v with the given status. It buffers first so a
// marshalling failure cannot emit a half-written body after the status line.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	if v == nil {
		w.WriteHeader(status)
		return
	}
	body, err := json.Marshal(v)
	if err != nil {
		slog.Error("response marshal failed", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"an unexpected error occurred"}}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// NoContent ends the request with 204.
func NoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// WriteError renders err using the standard envelope. Unrecognised errors
// become a generic 500 so internal details never reach the client.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		apiErr = Internal(err)
	}
	if apiErr.Status >= http.StatusInternalServerError {
		slog.ErrorContext(r.Context(), "request failed",
			"method", r.Method,
			"path", r.URL.Path,
			"request_id", RequestID(r),
			"code", apiErr.Code,
			"error", err,
		)
	} else if cause := apiErr.Unwrap(); cause != nil {
		slog.WarnContext(r.Context(), "request rejected",
			"method", r.Method,
			"path", r.URL.Path,
			"request_id", RequestID(r),
			"status", apiErr.Status,
			"code", apiErr.Code,
			"error", cause,
		)
	}
	WriteJSON(w, apiErr.Status, errorEnvelope{Error: apiErr})
}

// requestIDKey carries the correlation id through the request context.
type requestIDKey struct{}

// WithRequestID stores a correlation id on the context. The request-id
// middleware is owned by the platform layer (F24); this is the seam it writes
// through so handler logs pick the value up.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID reads the correlation id for a request, preferring the context
// value set by middleware and falling back to the inbound header.
func RequestID(r *http.Request) string {
	if id, ok := r.Context().Value(requestIDKey{}).(string); ok && id != "" {
		return id
	}
	if id := r.Header.Get("X-Request-Id"); id != "" {
		return id
	}
	return r.Header.Get("X-Request-ID")
}

// Handler is an http.Handler that may return an error, removing the
// "write the error and forget to return" failure mode from every handler.
type Handler func(http.ResponseWriter, *http.Request) error

// Wrap adapts a Handler for use with net/http and gorilla/mux.
func Wrap(h Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			WriteError(w, r, err)
		}
	}
}

// DecodeJSON reads a JSON request body into dst with a size cap, content-type
// check and rejection of trailing content.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mediaType, _, err := mime.ParseMediaType(ct)
		if err != nil || mediaType != "application/json" {
			return newError(http.StatusUnsupportedMediaType, CodeUnsupportedMedia,
				"content-type must be application/json")
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return decodeError(err)
	}
	if dec.More() {
		return BadRequest("request body must contain exactly one JSON object")
	}
	return nil
}

func decodeError(err error) error {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	var maxBytesErr *http.MaxBytesError

	switch {
	case errors.Is(err, io.EOF):
		return BadRequest("request body is required")
	// A truncated body surfaces as ErrUnexpectedEOF rather than a SyntaxError,
	// so it needs its own branch to avoid the generic fallback message.
	case errors.Is(err, io.ErrUnexpectedEOF):
		return BadRequest("malformed JSON: request body ended unexpectedly")
	case errors.As(err, &maxBytesErr):
		return newError(http.StatusRequestEntityTooLarge, CodePayloadTooLarge,
			fmt.Sprintf("request body must not exceed %d bytes", MaxBodyBytes))
	case errors.As(err, &syntaxErr):
		return BadRequest(fmt.Sprintf("malformed JSON at byte %d", syntaxErr.Offset))
	case errors.As(err, &typeErr):
		if typeErr.Field != "" {
			return BadRequest(fmt.Sprintf("field %q must be of type %s", typeErr.Field, typeErr.Type.String()))
		}
		return BadRequest("request body has the wrong shape")
	default:
		return BadRequest("request body could not be parsed").WithCause(err)
	}
}

// PathUUID extracts and parses a mux path variable as a UUID.
func PathUUID(r *http.Request, name string) (uuid.UUID, error) {
	raw := strings.TrimSpace(mux.Vars(r)[name])
	if raw == "" {
		return uuid.Nil, BadRequest(fmt.Sprintf("%s is required", name))
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, BadRequest(fmt.Sprintf("%s must be a valid UUID", name))
	}
	if id == uuid.Nil {
		return uuid.Nil, BadRequest(fmt.Sprintf("%s must be a valid UUID", name))
	}
	return id, nil
}

// QueryLimit reads a bounded `limit` query parameter.
func QueryLimit(r *http.Request, def, max int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, BadRequest("limit must be an integer")
	}
	if n < 1 || n > max {
		return 0, BadRequest(fmt.Sprintf("limit must be between 1 and %d", max))
	}
	return n, nil
}
