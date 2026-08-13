package httpx

import (
	"fmt"
	"net/http"
)

// Machine-readable error codes returned in the `error.code` field. Person A
// branches on these, so treat them as part of the public API contract.
const (
	CodeBadRequest       = "bad_request"
	CodeValidationFailed = "validation_failed"
	CodeUnauthorized     = "unauthorized"
	CodeForbidden        = "forbidden"
	CodeNotFound         = "not_found"
	CodeConflict         = "conflict"
	CodeMethodNotAllowed = "method_not_allowed"
	CodeUnsupportedMedia = "unsupported_media_type"
	CodePayloadTooLarge  = "payload_too_large"
	CodeRateLimited      = "rate_limited"
	CodeInternal         = "internal_error"
	CodeNotImplemented   = "not_implemented"
)

// Error is the wire format from the shared roadmap §6:
//
//	{ "error": { "code": "...", "message": "...", "details": {} } }
type Error struct {
	Status  int            `json:"-"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`

	// cause carries the internal error for logging. It is never serialized.
	cause error
}

type errorEnvelope struct {
	Error *Error `json:"error"`
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.cause }

// WithCause attaches an internal error that is logged but not returned to the
// client.
func (e *Error) WithCause(err error) *Error {
	e.cause = err
	return e
}

// WithDetails attaches structured, client-safe context.
func (e *Error) WithDetails(details map[string]any) *Error {
	e.Details = details
	return e
}

func newError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func BadRequest(message string) *Error {
	return newError(http.StatusBadRequest, CodeBadRequest, message)
}

func Unauthorized(message string) *Error {
	return newError(http.StatusUnauthorized, CodeUnauthorized, message)
}

func Forbidden(message string) *Error {
	return newError(http.StatusForbidden, CodeForbidden, message)
}

func NotFound(message string) *Error {
	return newError(http.StatusNotFound, CodeNotFound, message)
}

func Conflict(code, message string) *Error {
	if code == "" {
		code = CodeConflict
	}
	return newError(http.StatusConflict, code, message)
}

func NotImplemented(code, message string) *Error {
	if code == "" {
		code = CodeNotImplemented
	}
	return newError(http.StatusNotImplemented, code, message)
}

// CodedError builds an error with a domain-specific code, e.g.
// "assessment_required" or "refresh_token_reused".
func CodedError(status int, code, message string) *Error {
	return newError(status, code, message)
}

// Internal wraps an unexpected failure. The cause is logged; the client only
// ever sees a generic message.
func Internal(cause error) *Error {
	return (&Error{
		Status:  http.StatusInternalServerError,
		Code:    CodeInternal,
		Message: "an unexpected error occurred",
	}).WithCause(cause)
}
