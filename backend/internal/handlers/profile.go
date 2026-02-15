package handlers

import (
	"net/http"

	"github.com/bits-assignment/dating-platform/backend/internal/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/repository"
)

type ProfileHandler struct {
	ProfileRepo *repository.ProfileRepo
}

func (h *ProfileHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	p, err := h.ProfileRepo.GetByUserID(r.Context(), userID)
	if err != nil {
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found"})
		return
	}
	WriteJSON(w, http.StatusOK, p)
}

func (h *ProfileHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	if r.Method != http.MethodPut {
		WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req struct {
		Bio      string `json:"bio"`
		Gender   string `json:"gender"`
		Location string `json:"location"`
		PhotoURL string `json:"photo_url"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	p, err := h.ProfileRepo.GetByUserID(r.Context(), userID)
	if err != nil {
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found"})
		return
	}
	p.Bio = req.Bio
	p.Gender = req.Gender
	p.Location = req.Location
	p.PhotoURL = req.PhotoURL
	if err := h.ProfileRepo.Update(r.Context(), p); err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "update failed"})
		return
	}
	WriteJSON(w, http.StatusOK, p)
}
