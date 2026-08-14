package handlers

import (
	"net/http"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/bits-assignment/dating-platform/backend/internal/profile"
)

// ProfileHandler serves the legacy profile endpoints.
type ProfileHandler struct {
	Service *profile.Service
}

func (h *ProfileHandler) Get(w http.ResponseWriter, r *http.Request) error {
	userID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	dto, err := h.Service.Get(r.Context(), userID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, dto)
	return nil
}

type legacyProfileUpdateRequest struct {
	Bio      string `json:"bio"`
	Gender   string `json:"gender"`
	Location string `json:"location"`
	PhotoURL string `json:"photo_url"`
}

// Update applies replace semantics: the legacy form posts every field, so an
// empty value means "clear this". The v1 endpoint uses merge semantics instead.
func (h *ProfileHandler) Update(w http.ResponseWriter, r *http.Request) error {
	userID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	var req legacyProfileUpdateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	dto, err := h.Service.UpdateLegacy(r.Context(), userID, req.Bio, req.Gender, req.Location, req.PhotoURL)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, dto)
	return nil
}
