package personality

import (
	"fmt"
	"net/http"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/gorilla/mux"
)

// maxAnswers bounds a submission so a client cannot force an unbounded scoring
// loop. The bank is 30 items; the headroom allows growth without a code change.
const maxAnswers = 200

// Handler exposes the assessment endpoints.
type Handler struct {
	Service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{Service: service} }

// RegisterRoutes mounts the personality endpoints on an /api/v1 router.
func (h *Handler) RegisterRoutes(r *mux.Router, requireAuth func(http.Handler) http.Handler) {
	r.Handle("/personality/assessment", requireAuth(httpx.Wrap(h.Assessment))).Methods(http.MethodGet)
	r.Handle("/personality/assessment/submit", requireAuth(httpx.Wrap(h.Submit))).Methods(http.MethodPost)
	r.Handle("/personality/me", requireAuth(httpx.Wrap(h.Me))).Methods(http.MethodGet)
}

func (h *Handler) Assessment(w http.ResponseWriter, r *http.Request) error {
	userID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	result, err := h.Service.Assessment(r.Context(), userID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}

type submitRequest struct {
	Answers []Answer `json:"answers"`
}

func (h *Handler) Submit(w http.ResponseWriter, r *http.Request) error {
	userID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	var req submitRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	if len(req.Answers) > maxAnswers {
		return httpx.BadRequest(fmt.Sprintf("answers must not contain more than %d entries", maxAnswers))
	}
	result, err := h.Service.Submit(r.Context(), userID, req.Answers)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, result)
	return nil
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) error {
	userID, err := auth.RequireUser(r.Context())
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
