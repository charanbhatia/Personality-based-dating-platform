package notifications

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/bits-assignment/dating-platform/backend/internal/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/cursor"
	"github.com/bits-assignment/dating-platform/backend/internal/platform/httpx"
	platformmw "github.com/bits-assignment/dating-platform/backend/internal/platform/middleware"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

func (h *Handler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/notifications", h.List).Methods(http.MethodGet)
	r.HandleFunc("/notifications/unread-count", h.UnreadCount).Methods(http.MethodGet)
	r.HandleFunc("/notifications/read-all", h.MarkAllRead).Methods(http.MethodPost)
	r.HandleFunc("/notifications/{id}/read", h.MarkRead).Methods(http.MethodPost)
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}

	var before *cursor.Keyset
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		k, err := cursor.Decode(raw)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid cursor")
			return
		}
		before = &k
	}

	items, next, err := h.svc.List(r.Context(), userID, before, parseLimit(r))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Page[Notification]{Items: items, NextCursor: next})
}

func (h *Handler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}

	count, err := h.svc.UnreadCount(r.Context(), userID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int64{"unread_count": count})
}

func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid notification id")
		return
	}
	if err := h.svc.MarkRead(r.Context(), userID, id); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.user(w, r)
	if !ok {
		return
	}

	if err := h.svc.MarkAllRead(r.Context(), userID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) user(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "unauthorized")
		return uuid.Nil, false
	}
	return userID, true
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "notification not found")
		return
	}
	h.log.Error("notifications request failed",
		"error", err,
		"path", r.URL.Path,
		"request_id", platformmw.RequestIDFrom(r.Context()),
	)
	httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "internal server error")
}

func parseLimit(r *http.Request) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}
