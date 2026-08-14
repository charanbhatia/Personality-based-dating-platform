// Package preferences owns the discovery filter settings (roadmap F09).
//
// Age, gender, trait weights and max_distance_km are all applied. Distance
// filtering uses profile lat/lng; if the viewer has no coordinates the distance
// preference is stored but not used until they share a location.
package preferences

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/google/uuid"
)

// Bounds enforced on user input. These mirror the CHECK constraints in
// 005_b_preferences.sql so a bad request fails validation with a field message
// rather than surfacing as a database error.
const (
	MinAge         = 18
	MaxAge         = 120
	MinDistanceKM  = 1
	MaxDistanceKM  = 20000
	MaxGenderCount = 4
)

// ErrNotFound means no preferences row exists. Registration creates one, and
// migration 005 backfills older accounts, so this indicates a deleted row.
var ErrNotFound = errors.New("preferences not found")

// Preferences is the stored settings row.
type Preferences struct {
	UserID        uuid.UUID
	AgeMin        *int
	AgeMax        *int
	Genders       []string
	MaxDistanceKM *int
	TraitWeights  domain.TraitWeights
	UpdatedAt     time.Time
}

// Complete reports the `preferences_done` onboarding condition: an age range and
// at least one sought gender. Kept next to the type so the definition used by
// /auth/me and by the UI cannot drift.
func (p Preferences) Complete() bool {
	return p.AgeMin != nil && p.AgeMax != nil && len(p.Genders) > 0
}

// DTO is the API representation.
type DTO struct {
	AgeMin        *int                `json:"age_min"`
	AgeMax        *int                `json:"age_max"`
	Genders       []string            `json:"genders"`
	MaxDistanceKM *int                `json:"max_distance_km"`
	TraitWeights  domain.TraitWeights `json:"trait_weights"`
	Complete      bool                `json:"complete"`
	// DistanceFilterActive is true when max_distance_km is set. Discovery applies
	// it once the viewer has profile coordinates.
	DistanceFilterActive bool      `json:"distance_filter_active"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func toDTO(p *Preferences) *DTO {
	genders := p.Genders
	if genders == nil {
		genders = []string{}
	}
	return &DTO{
		AgeMin:               p.AgeMin,
		AgeMax:               p.AgeMax,
		Genders:              genders,
		MaxDistanceKM:        p.MaxDistanceKM,
		TraitWeights:         p.TraitWeights,
		Complete:             p.Complete(),
		DistanceFilterActive: p.MaxDistanceKM != nil,
		UpdatedAt:            p.UpdatedAt,
	}
}

// Repo reads and writes the preferences table.
type Repo struct{}

// Get loads a user's preferences.
func (Repo) Get(ctx context.Context, q db.Querier, userID uuid.UUID) (*Preferences, error) {
	p := &Preferences{UserID: userID}
	var weights []byte
	err := q.QueryRow(ctx, `
		SELECT age_min, age_max, genders, max_distance_km, trait_weights, updated_at
		FROM preferences WHERE user_id = $1`, userID,
	).Scan(&p.AgeMin, &p.AgeMax, &p.Genders, &p.MaxDistanceKM, &weights, &p.UpdatedAt)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load preferences: %w", err)
	}
	if len(weights) > 0 {
		if err := json.Unmarshal(weights, &p.TraitWeights); err != nil {
			return nil, fmt.Errorf("decode trait_weights for user %s: %w", userID, err)
		}
	}
	if p.Genders == nil {
		p.Genders = []string{}
	}
	return p, nil
}

// Upsert writes the full settings object. Registration inserts the row, but the
// upsert keeps this safe for accounts whose row was removed.
func (Repo) Upsert(ctx context.Context, q db.Querier, p *Preferences) (*Preferences, error) {
	var weights any
	if p.TraitWeights != nil {
		encoded, err := json.Marshal(p.TraitWeights)
		if err != nil {
			return nil, fmt.Errorf("encode trait_weights: %w", err)
		}
		weights = encoded
	}

	out := &Preferences{UserID: p.UserID}
	var stored []byte
	err := q.QueryRow(ctx, `
		INSERT INTO preferences (user_id, age_min, age_max, genders, max_distance_km, trait_weights, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (user_id) DO UPDATE SET
			age_min         = EXCLUDED.age_min,
			age_max         = EXCLUDED.age_max,
			genders         = EXCLUDED.genders,
			max_distance_km = EXCLUDED.max_distance_km,
			trait_weights   = EXCLUDED.trait_weights,
			updated_at      = now()
		RETURNING age_min, age_max, genders, max_distance_km, trait_weights, updated_at`,
		p.UserID, p.AgeMin, p.AgeMax, p.Genders, p.MaxDistanceKM, weights,
	).Scan(&out.AgeMin, &out.AgeMax, &out.Genders, &out.MaxDistanceKM, &stored, &out.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("save preferences: %w", err)
	}
	if len(stored) > 0 {
		if err := json.Unmarshal(stored, &out.TraitWeights); err != nil {
			return nil, fmt.Errorf("decode stored trait_weights: %w", err)
		}
	}
	if out.Genders == nil {
		out.Genders = []string{}
	}
	return out, nil
}

// Service implements the preferences use cases.
type Service struct {
	pool db.Querier
	repo Repo
}

func NewService(pool db.Querier) (*Service, error) {
	if pool == nil {
		return nil, errors.New("preferences: pool is required")
	}
	return &Service{pool: pool}, nil
}

// Get returns the caller's preferences.
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (*DTO, error) {
	p, err := s.Load(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, httpx.NotFound("preferences not found")
		}
		return nil, httpx.Internal(err)
	}
	return toDTO(p), nil
}

// Load returns the stored preferences for internal callers such as discovery.
func (s *Service) Load(ctx context.Context, userID uuid.UUID) (*Preferences, error) {
	return s.repo.Get(ctx, s.pool, userID)
}

// UpdateInput is a full replacement of the settings object.
type UpdateInput struct {
	AgeMin        *int
	AgeMax        *int
	Genders       []string
	MaxDistanceKM *int
	TraitWeights  domain.TraitWeights
}

// Update validates and stores preferences.
//
// PUT replaces the whole object rather than merging: these are settings a user
// edits as a single form, and replace semantics keep "clear my distance limit"
// expressible as null.
func (s *Service) Update(ctx context.Context, userID uuid.UUID, in UpdateInput) (*DTO, error) {
	v := httpx.NewValidator()

	if in.AgeMin == nil {
		v.Add("age_min", "age_min is required")
	} else if *in.AgeMin < MinAge || *in.AgeMin > MaxAge {
		v.Add("age_min", fmt.Sprintf("must be between %d and %d", MinAge, MaxAge))
	}
	if in.AgeMax == nil {
		v.Add("age_max", "age_max is required")
	} else if *in.AgeMax < MinAge || *in.AgeMax > MaxAge {
		v.Add("age_max", fmt.Sprintf("must be between %d and %d", MinAge, MaxAge))
	}
	if in.AgeMin != nil && in.AgeMax != nil && *in.AgeMin > *in.AgeMax {
		v.Add("age_max", "must be greater than or equal to age_min")
	}

	genders, invalid, ok := domain.NormalizeGenderList(in.Genders)
	if !ok {
		v.Add("genders", fmt.Sprintf("%q is not a recognised gender", invalid))
	} else if len(genders) == 0 {
		v.Add("genders", "select at least one gender")
	} else if len(genders) > MaxGenderCount {
		v.Add("genders", fmt.Sprintf("must not contain more than %d entries", MaxGenderCount))
	}

	if in.MaxDistanceKM != nil && (*in.MaxDistanceKM < MinDistanceKM || *in.MaxDistanceKM > MaxDistanceKM) {
		v.Add("max_distance_km", fmt.Sprintf("must be between %d and %d, or null for anywhere", MinDistanceKM, MaxDistanceKM))
	}
	if in.TraitWeights != nil {
		if err := in.TraitWeights.Validate(); err != nil {
			v.Add("trait_weights", err.Error())
		}
	}
	if err := v.Err(); err != nil {
		return nil, err
	}

	saved, err := s.repo.Upsert(ctx, s.pool, &Preferences{
		UserID:        userID,
		AgeMin:        in.AgeMin,
		AgeMax:        in.AgeMax,
		Genders:       genders,
		MaxDistanceKM: in.MaxDistanceKM,
		TraitWeights:  in.TraitWeights,
	})
	if err != nil {
		if db.IsForeignKeyViolation(err) {
			return nil, httpx.NotFound("account not found")
		}
		return nil, httpx.Internal(err)
	}
	return toDTO(saved), nil
}
