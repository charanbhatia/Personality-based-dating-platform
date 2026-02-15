package handlers

import (
	"net/http"
	"strconv"

	"github.com/bits-assignment/dating-platform/backend/internal/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/repository"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type MessagingHandler struct {
	ConvRepo *repository.ConversationRepo
}

func (h *MessagingHandler) ListConversations(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	list, err := h.ConvRepo.ListByUserID(r.Context(), userID)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list conversations"})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{"conversations": list})
}

func (h *MessagingHandler) GetMessages(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	vars := mux.Vars(r)
	idStr := vars["id"]
	convID, err := uuid.Parse(idStr)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation id"})
		return
	}
	inConv, convErr := h.ConvRepo.UserInConversation(r.Context(), convID, userID)
	if convErr != nil || !inConv {
		WriteJSON(w, http.StatusForbidden, map[string]string{"error": "not in conversation"})
		return
	}
	limit, offset := 100, 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, e := strconv.Atoi(l); e == nil {
			limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, e := strconv.Atoi(o); e == nil {
			offset = n
		}
	}
	msgs, err := h.ConvRepo.Messages(r.Context(), convID, limit, offset)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get messages"})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{"messages": msgs})
}

func (h *MessagingHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	vars := mux.Vars(r)
	idStr := vars["id"]
	convID, err := uuid.Parse(idStr)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid conversation id"})
		return
	}
	inConv, convErr := h.ConvRepo.UserInConversation(r.Context(), convID, userID)
	if convErr != nil || !inConv {
		WriteJSON(w, http.StatusForbidden, map[string]string{"error": "not in conversation"})
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Content == "" {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "content required"})
		return
	}
	msg, err := h.ConvRepo.SendMessage(r.Context(), convID, userID, req.Content)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to send"})
		return
	}
	WriteJSON(w, http.StatusCreated, msg)
}

func (h *MessagingHandler) StartConversation(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	otherID, err := uuid.Parse(req.UserID)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user_id"})
		return
	}
	if otherID == userID {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot message yourself"})
		return
	}
	conv, err := h.ConvRepo.CreateOrGet(r.Context(), userID, otherID)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create conversation"})
		return
	}
	WriteJSON(w, http.StatusCreated, conv)
}
