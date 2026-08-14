package handlers

import (
	"net/http"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
)

// MeHandler serves the legacy current-user endpoint.
type MeHandler struct {
	Service *auth.Service
}

// legacyMeResponse inlines the user fields at the top level, which is the shape
// the current frontend stores as its user object, and adds the onboarding flags
// alongside them.
type legacyMeResponse struct {
	auth.UserDTO
	Onboarding auth.Onboarding `json:"onboarding"`
}

func (h *MeHandler) Me(w http.ResponseWriter, r *http.Request) error {
	userID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	result, err := h.Service.Me(r.Context(), userID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, legacyMeResponse{
		UserDTO:    result.User,
		Onboarding: result.Onboarding,
	})
	return nil
}
