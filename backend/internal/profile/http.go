package profile

import (
	"net/http"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/gorilla/mux"
)

func domainGenders() []string { return domain.GenderStrings() }

// Handler exposes the profile endpoints.
type Handler struct {
	Service *Service
}

func NewHandler(service *Service) *Handler { return &Handler{Service: service} }

// RegisterRoutes mounts the profile endpoints on an /api/v1 router.
func (h *Handler) RegisterRoutes(r *mux.Router, requireAuth func(http.Handler) http.Handler) {
	r.Handle("/profile", requireAuth(httpx.Wrap(h.Get))).Methods(http.MethodGet)
	r.Handle("/profile", requireAuth(httpx.Wrap(h.Update))).Methods(http.MethodPut)
	r.Handle("/profile/photos", requireAuth(httpx.Wrap(h.SetPhotos))).Methods(http.MethodPut)
	r.Handle("/users/{id}/public", requireAuth(httpx.Wrap(h.GetPublic))).Methods(http.MethodGet)
	// Lets the frontend build the gender selector from the server's canonical set
	// instead of hardcoding a list that can drift out of sync.
	r.Handle("/profile/options", requireAuth(httpx.Wrap(h.Options))).Methods(http.MethodGet)
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

// updateRequest uses pointers so an omitted field is distinguishable from an
// explicit empty value; PUT therefore merges rather than overwriting the whole
// profile with blanks.
type updateRequest struct {
	Bio         *string   `json:"bio"`
	Gender      *string   `json:"gender"`
	Location    *string   `json:"location"`
	Interests   *[]string `json:"interests"`
	DateOfBirth *string   `json:"date_of_birth"`
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
		Bio:         req.Bio,
		Gender:      req.Gender,
		Location:    req.Location,
		Interests:   req.Interests,
		DateOfBirth: req.DateOfBirth,
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, dto)
	return nil
}

type setPhotosRequest struct {
	PhotoURLs *[]string `json:"photo_urls"`
	AssetIDs  *[]string `json:"asset_ids"`
}

func (h *Handler) SetPhotos(w http.ResponseWriter, r *http.Request) error {
	userID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	var req setPhotosRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		return err
	}
	dto, err := h.Service.SetPhotos(r.Context(), userID, SetPhotosInput{
		PhotoURLs: req.PhotoURLs,
		AssetIDs:  req.AssetIDs,
	})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, dto)
	return nil
}

func (h *Handler) GetPublic(w http.ResponseWriter, r *http.Request) error {
	viewerID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	targetID, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	dto, err := h.Service.GetPublic(r.Context(), viewerID, targetID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, dto)
	return nil
}

func (h *Handler) Options(w http.ResponseWriter, r *http.Request) error {
	if _, err := auth.RequireUser(r.Context()); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"genders": domainGenders(),
		"limits": map[string]int{
			"bio_max_length":      MaxBioRunes,
			"location_max_length": MaxLocationRunes,
			"max_interests":       MaxInterests,
			"interest_max_length": MaxInterestRunes,
			"max_photos":          MaxPhotos,
		},
	})
	return nil
}
