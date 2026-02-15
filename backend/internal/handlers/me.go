package handlers

import (
	"net/http"

	"github.com/bits-assignment/dating-platform/backend/internal/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/repository"
)

type MeHandler struct {
	UserRepo *repository.UserRepo
}

func (h *MeHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	u, err := h.UserRepo.GetByID(r.Context(), userID)
	if err != nil {
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	WriteJSON(w, http.StatusOK, u)
}
