// Package handlers holds the pre-v1 /api endpoints kept alive while Person A
// migrates the SPA to /api/v1.
//
// These are thin shims over the domain services in internal/auth, internal/profile
// and internal/matching: the response shapes the existing frontend reads are
// preserved, but there is no duplicated logic behind them.
package handlers

import (
	"net/http"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
)

// AuthHandler serves the legacy auth endpoints.
type AuthHandler struct {
	Service *auth.Service
}

// legacyAuthResponse keeps the `token` field the current frontend reads from
// localStorage while also returning the v1 token pair, so a client can migrate
// to refresh tokens without a coordinated release.
type legacyAuthResponse struct {
	User  auth.UserDTO `json:"user"`
	Token string       `json:"token"`
	auth.TokenPair
}

func newLegacyAuthResponse(result *auth.AuthResult) legacyAuthResponse {
	return legacyAuthResponse{
		User:      result.User,
		Token:     result.AccessToken,
		TokenPair: result.TokenPair,
	}
}

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	Name        string `json:"name"`
	DateOfBirth string `json:"date_of_birth"`
}

// Register creates an account.
//
// Unlike /api/v1/auth/register this does not insist on a date of birth, because
// the existing registration form does not collect one. Such accounts are not
// discoverable until it is set on the profile.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) error {
	var req registerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	result, err := h.Service.Register(r.Context(), auth.RegisterInput{
		Email:       req.Email,
		Password:    req.Password,
		Name:        req.Name,
		DateOfBirth: req.DateOfBirth,
		Meta:        auth.SessionMetaFromRequest(r),
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, newLegacyAuthResponse(result))
	return nil
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) error {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	result, err := h.Service.Login(r.Context(), auth.LoginInput{
		Email:    req.Email,
		Password: req.Password,
		Meta:     auth.SessionMetaFromRequest(r),
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, newLegacyAuthResponse(result))
	return nil
}
