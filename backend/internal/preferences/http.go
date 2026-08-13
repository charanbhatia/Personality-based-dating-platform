package preferences

import (
	"net/http"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/gorilla/mux"
)

// Handler exposes the preferences endpoints.
type Handler struct {
	Service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{Service: service} }

// RegisterRoutes mounts the preferences endpoints on an /api/v1 router.
func (h *Handler) RegisterRoutes(r *mux.Router, requireAuth func(http.Handler) http.Handler) {
	r.Handle("/preferences", requireAuth(httpx.Wrap(h.Get))).Methods(http.MethodGet)
	r.Handle("/preferences", requireAuth(httpx.Wrap(h.Update))).Methods(http.MethodPut)
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) error {
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

type updateRequest struct {
	AgeMin        *int                `json:"age_min"`
	AgeMax        *int                `json:"age_max"`
	Genders       []string            `json:"genders"`
	MaxDistanceKM *int                `json:"max_distance_km"`
	TraitWeights  domain.TraitWeights `json:"trait_weights"`
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) error {
	userID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	var req updateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	dto, err := h.Service.Update(r.Context(), userID, UpdateInput{
		AgeMin:        req.AgeMin,
		AgeMax:        req.AgeMax,
		Genders:       req.Genders,
		MaxDistanceKM: req.MaxDistanceKM,
		TraitWeights:  req.TraitWeights,
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, dto)
	return nil
}
