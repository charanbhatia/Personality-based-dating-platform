package messaging

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
	defaultConversationLimit = 20
	defaultMessageLimit      = 50
	maxPageLimit             = 100
)

type Handler struct {
	svc *Service
	log *slog.Logger
	// sendLimit throttles writes per user; nil disables throttling.
	sendLimit func(http.Handler) http.Handler
}

func NewHandler(svc *Service, log *slog.Logger, sendLimit func(http.Handler) http.Handler) *Handler {
	if sendLimit == nil {
		sendLimit = func(next http.Handler) http.Handler { return next }
	}
	return &Handler{svc: svc, log: log, sendLimit: sendLimit}
}

// RegisterRoutes mounts the messaging domain on an already authenticated router.
func (h *Handler) RegisterRoutes(r *mux.Router) {
	r.HandleFunc("/conversations", h.ListConversations).Methods(http.MethodGet)
	r.HandleFunc("/conversations", h.CreateConversation).Methods(http.MethodPost)
	r.HandleFunc("/conversations/{id}", h.GetConversation).Methods(http.MethodGet)
	r.HandleFunc("/conversations/{id}/messages", h.ListMessages).Methods(http.MethodGet)
	r.Handle("/conversations/{id}/messages", h.sendLimit(http.HandlerFunc(h.SendMessage))).Methods(http.MethodPost)
	r.HandleFunc("/conversations/{id}/read", h.MarkRead).Methods(http.MethodPost)
}

func (h *Handler) ListConversations(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "unauthorized")
		return
	}

	after, err := parseCursor(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid cursor")
		return
	}

	items, next, err := h.svc.ListConversations(r.Context(), userID, after, parseLimit(r, defaultConversationLimit))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Page[Conversation]{Items: items, NextCursor: next})
}

func (h *Handler) CreateConversation(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "unauthorized")
		return
	}

	var req struct {
		MatchID string `json:"match_id"`
		UserID  string `json:"user_id"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid body")
		return
	}

	var conv Conversation
	var err error
	switch {
	case req.MatchID != "":
		matchID, parseErr := uuid.Parse(req.MatchID)
		if parseErr != nil {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid match_id")
			return
		}
		conv, err = h.svc.OpenByMatch(r.Context(), userID, matchID)
	case req.UserID != "":
		peerID, parseErr := uuid.Parse(req.UserID)
		if parseErr != nil {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid user_id")
			return
		}
		conv, err = h.svc.OpenByUser(r.Context(), userID, peerID)
	default:
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "match_id is required")
		return
	}

	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, conv)
}

func (h *Handler) GetConversation(w http.ResponseWriter, r *http.Request) {
	userID, convID, ok := h.identify(w, r)
	if !ok {
		return
	}
	conv, err := h.svc.Get(r.Context(), userID, convID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, conv)
}

func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	userID, convID, ok := h.identify(w, r)
	if !ok {
		return
	}

	before, err := parseCursor(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid cursor")
		return
	}

	items, next, err := h.svc.ListMessages(r.Context(), userID, convID, before, parseLimit(r, defaultMessageLimit))
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, httpx.Page[Message]{Items: items, NextCursor: next})
}

func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID, convID, ok := h.identify(w, r)
	if !ok {
		return
	}

	var req struct {
		Content     string `json:"content"`
		ClientMsgID string `json:"client_msg_id"`
	}
	if err := httpx.ReadJSON(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid body")
		return
	}

	msg, created, err := h.svc.Send(r.Context(), userID, convID, req.Content, req.ClientMsgID)
	if err != nil {
		h.writeServiceError(w, r, err)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, msg)
}

func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, convID, ok := h.identify(w, r)
	if !ok {
		return
	}

	if err := h.svc.MarkRead(r.Context(), userID, convID); err != nil {
		h.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) identify(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "unauthorized")
		return uuid.Nil, uuid.Nil, false
	}

	convID, err := uuid.Parse(mux.Vars(r)["id"])
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, "invalid conversation id")
		return uuid.Nil, uuid.Nil, false
	}
	return userID, convID, true
}

func (h *Handler) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrConversationNotFound):
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "conversation not found")
	case errors.Is(err, ErrMatchNotFound):
		httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "match not found")
	case errors.Is(err, ErrForbidden):
		httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, err.Error())
	case errors.Is(err, ErrBlocked):
		httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, err.Error())
	case errors.Is(err, ErrEmptyContent), errors.Is(err, ErrContentTooLong), errors.Is(err, ErrGateRequired):
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeBadRequest, err.Error())
	case errors.Is(err, ErrMatchTableMissing):
		httpx.WriteError(w, http.StatusServiceUnavailable, httpx.CodeInternalError,
			"matching is not available yet; conversations cannot be opened")
	default:
		h.log.Error("messaging request failed",
			"error", err,
			"path", r.URL.Path,
			"request_id", platformmw.RequestIDFrom(r.Context()),
		)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "internal server error")
	}
}

func parseLimit(r *http.Request, fallback int) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	if n > maxPageLimit {
		return maxPageLimit
	}
	return n
}

func parseCursor(r *http.Request) (*cursor.Keyset, error) {
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return nil, nil
	}
	k, err := cursor.Decode(raw)
	if err != nil {
		return nil, err
	}
	return &k, nil
}
