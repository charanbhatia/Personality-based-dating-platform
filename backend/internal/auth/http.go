package auth

import (
	"net"
	"net/http"
	"strings"

	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/gorilla/mux"
)

// Handler exposes the auth use cases over HTTP.
type Handler struct {
	Service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{Service: service} }

// RegisterRoutes mounts the auth endpoints on an /api/v1 router. requireAuth is
// applied per-route rather than via a subrouter so public and protected paths can
// share the same prefix without mux matching ambiguity.
func (h *Handler) RegisterRoutes(r *mux.Router, requireAuth, limitAuth func(http.Handler) http.Handler) {
	if limitAuth == nil {
		limitAuth = func(next http.Handler) http.Handler { return next }
	}
	r.Handle("/auth/register", limitAuth(httpx.Wrap(h.Register))).Methods(http.MethodPost)
	r.Handle("/auth/login", limitAuth(httpx.Wrap(h.Login))).Methods(http.MethodPost)
	r.Handle("/auth/refresh", limitAuth(httpx.Wrap(h.Refresh))).Methods(http.MethodPost)
	r.Handle("/auth/password/forgot", limitAuth(httpx.Wrap(h.ForgotPassword))).Methods(http.MethodPost)
	r.Handle("/auth/password/reset", limitAuth(httpx.Wrap(h.ResetPassword))).Methods(http.MethodPost)
	r.Handle("/auth/email/verify", limitAuth(httpx.Wrap(h.VerifyEmail))).Methods(http.MethodPost)

	r.Handle("/auth/logout", requireAuth(httpx.Wrap(h.Logout))).Methods(http.MethodPost)
	r.Handle("/auth/me", requireAuth(httpx.Wrap(h.Me))).Methods(http.MethodGet)
	r.Handle("/auth/sessions", requireAuth(httpx.Wrap(h.ListSessions))).Methods(http.MethodGet)
	r.Handle("/auth/sessions/{id}", requireAuth(httpx.Wrap(h.RevokeSession))).Methods(http.MethodDelete)
	r.Handle("/auth/email/resend", requireAuth(httpx.Wrap(h.ResendEmailVerification))).Methods(http.MethodPost)
}

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	Name        string `json:"name"`
	DateOfBirth string `json:"date_of_birth"`
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request) error {
	var req registerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	result, err := h.Service.Register(r.Context(), RegisterInput{
		Email:              req.Email,
		Password:           req.Password,
		Name:               req.Name,
		DateOfBirth:        req.DateOfBirth,
		Meta:               SessionMetaFromRequest(r),
		RequireDateOfBirth: true,
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, result)
	return nil
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) error {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	result, err := h.Service.Login(r.Context(), LoginInput{
		Email:    req.Email,
		Password: req.Password,
		Meta:     SessionMetaFromRequest(r),
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) error {
	var req refreshRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	result, err := h.Service.Refresh(r.Context(), strings.TrimSpace(req.RefreshToken), SessionMetaFromRequest(r))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) error {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		return httpx.Unauthorized("authentication is required")
	}
	if err := h.Service.Logout(r.Context(), principal); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) error {
	userID, err := RequireUser(r.Context())
	if err != nil {
		return err
	}
	result, err := h.Service.Me(r.Context(), userID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}

func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) error {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		return httpx.Unauthorized("authentication is required")
	}
	sessions, err := h.Service.ListSessions(r.Context(), principal)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": sessions})
	return nil
}

func (h *Handler) RevokeSession(w http.ResponseWriter, r *http.Request) error {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		return httpx.Unauthorized("authentication is required")
	}
	sessionID, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.Service.RevokeSession(r.Context(), principal, sessionID); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

// ForgotPassword always answers 202 so the response cannot be used to discover
// which addresses have accounts.
func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) error {
	var req forgotPasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := h.Service.RequestPasswordReset(r.Context(), req.Email); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"message": "if an account exists for that address, a reset link has been sent",
	})
	return nil
}

type resetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) error {
	var req resetPasswordRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := h.Service.ResetPassword(r.Context(), strings.TrimSpace(req.Token), req.Password); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

type verifyEmailRequest struct {
	Token string `json:"token"`
}

func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) error {
	var req verifyEmailRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if err := h.Service.VerifyEmail(r.Context(), strings.TrimSpace(req.Token)); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (h *Handler) ResendEmailVerification(w http.ResponseWriter, r *http.Request) error {
	userID, err := RequireUser(r.Context())
	if err != nil {
		return err
	}
	if err := h.Service.ResendEmailVerification(r.Context(), userID); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
		"message": "if the address is still unverified, a confirmation link has been sent",
	})
	return nil
}

// SessionMetaFromRequest captures the device details shown in the session list.
//
// X-Forwarded-For is client-controlled unless a trusted proxy overwrites it. The
// value is only ever displayed back to the account owner and never drives an
// authorization decision, so a spoofed entry is cosmetic.
func SessionMetaFromRequest(r *http.Request) SessionMeta {
	meta := SessionMeta{UserAgent: truncateRunes(httpx.CleanLine(r.UserAgent()), 300)}
	if ip := clientIP(r); ip != "" {
		meta.IP = ParseIP(ip)
	}
	return meta
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if first, _, found := strings.Cut(forwarded, ","); found {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(forwarded)
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-Ip")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
