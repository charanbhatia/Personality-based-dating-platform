package handlers

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/bits-assignment/dating-platform/backend/internal/profile"
	"github.com/bits-assignment/dating-platform/backend/internal/repository"
)

// MatchHandler serves the legacy /api/matches preview.
//
// Superseded by GET /api/v1/discover, which filters by preferences, excludes
// blocks and prior swipes, scores in SQL and paginates by cursor. This shim
// remains only until Person A migrates.
type MatchHandler struct {
	ProfileRepo    *repository.ProfileRepo
	ProfileService *profile.Service
}

// legacyMatchItem is the pre-v1 card shape.
//
// The email field the original implementation returned has been removed: it
// exposed every other user's address to any authenticated caller (roadmap F16).
type legacyMatchItem struct {
	UserID   string  `json:"user_id"`
	Name     string  `json:"name"`
	Bio      string  `json:"bio"`
	Gender   string  `json:"gender"`
	Location string  `json:"location"`
	PhotoURL string  `json:"photo_url"`
	Score    float64 `json:"score"`
}

const (
	legacyDefaultLimit = 20
	legacyMaxLimit     = 100
)

func (h *MatchHandler) List(w http.ResponseWriter, r *http.Request) error {
	userID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	markDeprecated(w, "/api/v1/discover")

	limit, err := httpx.QueryLimit(r, legacyDefaultLimit, legacyMaxLimit)
	if err != nil {
		return err
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		parsed, convErr := strconv.Atoi(raw)
		if convErr != nil || parsed < 0 {
			return httpx.BadRequest("offset must be a non-negative integer")
		}
		offset = parsed
	}

	candidates, err := h.ProfileRepo.ListCandidates(r.Context(), userID, limit, offset)
	if err != nil {
		return httpx.Internal(err)
	}
	viewerTraits, _ := h.ProfileRepo.GetPersonality(r.Context(), userID)
	viewer := parseTraits(viewerTraits)

	items := make([]legacyMatchItem, 0, len(candidates))
	for _, c := range candidates {
		items = append(items, legacyMatchItem{
			UserID:   c.UserID.String(),
			Name:     c.Name,
			Bio:      c.Bio,
			Gender:   c.Gender,
			Location: c.Location,
			PhotoURL: c.PhotoURL,
			Score:    domain.Compatibility(viewer, parseTraits([]byte(c.TraitsJSON)), nil),
		})
	}
	// Sorted within the page only. Offset paging cannot order globally by a
	// computed score; /api/v1/discover does that correctly with a keyset cursor.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].UserID < items[j].UserID
	})

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"matches": items})
	return nil
}

// Get returns one user's card. Now delegates to the profile service so the
// public payload and its block rules are enforced in one place.
func (h *MatchHandler) Get(w http.ResponseWriter, r *http.Request) error {
	viewerID, err := auth.RequireUser(r.Context())
	if err != nil {
		return err
	}
	markDeprecated(w, "/api/v1/users/{id}/public")

	targetID, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	public, err := h.ProfileService.GetPublic(r.Context(), viewerID, targetID)
	if err != nil {
		return err
	}

	viewerTraits, _ := h.ProfileRepo.GetPersonality(r.Context(), viewerID)
	targetTraits, _ := h.ProfileRepo.GetPersonality(r.Context(), targetID)

	httpx.WriteJSON(w, http.StatusOK, legacyMatchItem{
		UserID:   public.UserID.String(),
		Name:     public.Name,
		Bio:      public.Bio,
		Gender:   public.Gender,
		Location: public.Location,
		PhotoURL: public.PrimaryPhotoURL,
		Score:    domain.Compatibility(parseTraits(viewerTraits), parseTraits(targetTraits), nil),
	})
	return nil
}

func parseTraits(raw []byte) domain.Traits {
	if len(raw) == 0 {
		return domain.Traits{}
	}
	var traits domain.Traits
	if err := json.Unmarshal(raw, &traits); err != nil {
		return domain.Traits{}
	}
	return traits
}

// markDeprecated advertises the replacement endpoint per RFC 8594, so the
// migration is discoverable from the API itself rather than only from the docs.
func markDeprecated(w http.ResponseWriter, successor string) {
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Link", `<`+successor+`>; rel="successor-version"`)
}
