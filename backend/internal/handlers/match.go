package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/bits-assignment/dating-platform/backend/internal/middleware"
	"github.com/bits-assignment/dating-platform/backend/internal/repository"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type MatchHandler struct {
	ProfileRepo *repository.ProfileRepo
	UserRepo    *repository.UserRepo
}

type MatchItem struct {
	UserID     string  `json:"user_id"`
	Email      string  `json:"email"`
	Name       string  `json:"name"`
	Bio        string  `json:"bio"`
	Gender     string  `json:"gender"`
	Location   string  `json:"location"`
	PhotoURL   string  `json:"photo_url"`
	Score      float64 `json:"score"`
}

func (h *MatchHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	limit := 20
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}
	candidates, err := h.ProfileRepo.ListCandidates(r.Context(), userID, limit, offset)
	if err != nil {
		WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list matches"})
		return
	}
	myTraits, _ := h.ProfileRepo.GetPersonality(r.Context(), userID)
	myMap := parseTraits(myTraits)
	out := make([]MatchItem, 0, len(candidates))
	for _, c := range candidates {
		score := similarity(myMap, c.TraitsJSON)
		out = append(out, MatchItem{
			UserID:   c.UserID.String(),
			Email:    c.Email,
			Name:     c.Name,
			Bio:      c.Bio,
			Gender:   c.Gender,
			Location: c.Location,
			PhotoURL: c.PhotoURL,
			Score:    score,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]interface{}{"matches": out})
}

func (h *MatchHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		WriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	vars := mux.Vars(r)
	idStr := vars["id"]
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	u, err := h.UserRepo.GetByID(r.Context(), targetID)
	if err != nil {
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	p, err := h.ProfileRepo.GetByUserID(r.Context(), targetID)
	if err != nil {
		WriteJSON(w, http.StatusNotFound, map[string]string{"error": "profile not found"})
		return
	}
	myTraits, _ := h.ProfileRepo.GetPersonality(r.Context(), userID)
	theirTraits, _ := h.ProfileRepo.GetPersonality(r.Context(), targetID)
	score := similarity(parseTraits(myTraits), string(theirTraits))
	WriteJSON(w, http.StatusOK, MatchItem{
		UserID:   u.ID.String(),
		Email:    u.Email,
		Name:     u.Name,
		Bio:      p.Bio,
		Gender:   p.Gender,
		Location: p.Location,
		PhotoURL: p.PhotoURL,
		Score:    score,
	})
}

func parseTraits(b []byte) map[string]float64 {
	var m map[string]float64
	_ = json.Unmarshal(b, &m)
	if m == nil {
		m = make(map[string]float64)
	}
	return m
}

func similarity(a map[string]float64, bJSON string) float64 {
	var b map[string]float64
	_ = json.Unmarshal([]byte(bJSON), &b)
	if b == nil {
		b = make(map[string]float64)
	}
	var sum, count float64
	for k, v1 := range a {
		if v2, ok := b[k]; ok {
			diff := v1 - v2
			if diff < 0 {
				diff = -diff
			}
			sum += 1 - diff
			count++
		}
	}
	if count == 0 {
		return 0.5
	}
	return sum / count
}
