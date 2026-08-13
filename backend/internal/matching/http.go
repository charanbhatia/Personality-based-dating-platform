package matching

import (
	"net/http"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// Handler exposes the discovery, likes, matches, blocks and reports endpoints.
type Handler struct {
	Service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{Service: service} }

// RegisterRoutes mounts the matching endpoints on an /api/v1 router.
func (h *Handler) RegisterRoutes(r *mux.Router, requireAuth func(http.Handler) http.Handler) {
	r.Handle("/discover", requireAuth(httpx.Wrap(h.Discover))).Methods(http.MethodGet)
	r.Handle("/likes", requireAuth(httpx.Wrap(h.Swipe))).Methods(http.MethodPost)
	r.Handle("/matches", requireAuth(httpx.Wrap(h.Matches))).Methods(http.MethodGet)

	r.Handle("/blocks", requireAuth(httpx.Wrap(h.ListBlocks))).Methods(http.MethodGet)
	r.Handle("/blocks", requireAuth(httpx.Wrap(h.Block))).Methods(http.MethodPost)
	r.Handle("/blocks/{id}", requireAuth(httpx.Wrap(h.Unblock))).Methods(http.MethodDelete)
	r.Handle("/reports", requireAuth(httpx.Wrap(h.Report))).Methods(http.MethodPost)
}

func (h *Handler) Discover(w http.ResponseWriter, r *http.Request) error {
	viewerID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	limit, err := httpx.QueryLimit(r, h.Service.DefaultLimit(), h.Service.MaxLimit())
	if err != nil {
		return err
	}
	page, err := h.Service.Discover(r.Context(), viewerID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, page)
	return nil
}

type swipeRequest struct {
	UserID string `json:"user_id"`
	Action string `json:"action"`
}

func (h *Handler) Swipe(w http.ResponseWriter, r *http.Request) error {
	viewerID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	var req swipeRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	targetID, err := parseBodyUUID(req.UserID, "user_id")
	if err != nil {
		return err
	}
	result, err := h.Service.Swipe(r.Context(), viewerID, targetID, req.Action)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}

func (h *Handler) Matches(w http.ResponseWriter, r *http.Request) error {
	viewerID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	limit, err := httpx.QueryLimit(r, h.Service.DefaultLimit(), h.Service.MaxLimit())
	if err != nil {
		return err
	}
	page, err := h.Service.Matches(r.Context(), viewerID, r.URL.Query().Get("cursor"), limit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, page)
	return nil
}

type blockRequest struct {
	UserID string `json:"user_id"`
}

func (h *Handler) Block(w http.ResponseWriter, r *http.Request) error {
	blockerID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	var req blockRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	blockedID, err := parseBodyUUID(req.UserID, "user_id")
	if err != nil {
		return err
	}
	if err := h.Service.Block(r.Context(), blockerID, blockedID); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (h *Handler) Unblock(w http.ResponseWriter, r *http.Request) error {
	blockerID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	blockedID, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.Service.Unblock(r.Context(), blockerID, blockedID); err != nil {
		return err
	}
	httpx.NoContent(w)
	return nil
}

func (h *Handler) ListBlocks(w http.ResponseWriter, r *http.Request) error {
	blockerID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	items, err := h.Service.Blocks(r.Context(), blockerID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

type reportRequest struct {
	UserID  string `json:"user_id"`
	Reason  string `json:"reason"`
	Details string `json:"details"`
}

func (h *Handler) Report(w http.ResponseWriter, r *http.Request) error {
	reporterID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	var req reportRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	reportedID, err := parseBodyUUID(req.UserID, "user_id")
	if err != nil {
		return err
	}
	result, err := h.Service.Report(r.Context(), reporterID, reportedID, req.Reason, req.Details)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusAccepted, result)
	return nil
}

func parseBodyUUID(raw, field string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		v := httpx.NewValidator()
		v.Add(field, "must be a valid UUID")
		return uuid.Nil, v.Err()
	}
	return id, nil
}
