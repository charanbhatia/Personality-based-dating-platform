package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/bits-assignment/dating-platform/backend/internal/preferences"
)

type preferencesDTO struct {
	AgeMin               *int                `json:"age_min"`
	AgeMax               *int                `json:"age_max"`
	Genders              []string            `json:"genders"`
	MaxDistanceKM        *int                `json:"max_distance_km"`
	TraitWeights         domain.TraitWeights `json:"trait_weights"`
	Complete             bool                `json:"complete"`
	DistanceFilterActive bool                `json:"distance_filter_active"`
}

func getPreferences(t *testing.T, u *user) preferencesDTO {
	t.Helper()
	var dto preferencesDTO
	do(t, http.MethodGet, "/api/v1/preferences", u.Token, nil).
		requireStatus(t, http.StatusOK).decode(t, &dto)
	return dto
}

func TestPreferencesStartEmptyButPresent(t *testing.T) {
	resetDB(t)
	u := register(t, "Fresh", "1994-05-05")

	// Registration creates the row, so the client always gets a 200 with an
	// explicitly incomplete object rather than a 404 it has to special-case. The
	// age range is pre-filled with a permissive default; only genders are unset.
	dto := getPreferences(t, u)
	if dto.AgeMin == nil || *dto.AgeMin != auth.MinimumAge {
		t.Errorf("age_min = %v, want %d", dto.AgeMin, auth.MinimumAge)
	}
	if dto.AgeMax == nil || *dto.AgeMax != auth.DefaultSeekAgeMax {
		t.Errorf("age_max = %v, want %d", dto.AgeMax, auth.DefaultSeekAgeMax)
	}
	if dto.MaxDistanceKM != nil {
		t.Errorf("max_distance_km = %v, want null (anywhere) by default", *dto.MaxDistanceKM)
	}
	if dto.Genders == nil {
		t.Error("genders is null; the client would have to guard before iterating")
	}
	if len(dto.Genders) != 0 {
		t.Errorf("genders = %v, want empty", dto.Genders)
	}
	if dto.Complete {
		t.Error("complete = true with no age range or genders")
	}
}

func TestUpdatePreferencesRoundTrips(t *testing.T) {
	resetDB(t)
	u := register(t, "Setter", "1994-05-05")

	distance := 50
	var saved preferencesDTO
	do(t, http.MethodPut, "/api/v1/preferences", u.Token, map[string]any{
		"age_min":         25,
		"age_max":         40,
		"genders":         []string{"woman", "nonbinary"},
		"max_distance_km": distance,
		"trait_weights":   map[string]float64{domain.TraitOpenness: 2.5},
	}).requireStatus(t, http.StatusOK).decode(t, &saved)

	if saved.AgeMin == nil || *saved.AgeMin != 25 || saved.AgeMax == nil || *saved.AgeMax != 40 {
		t.Fatalf("age range = %v..%v, want 25..40", saved.AgeMin, saved.AgeMax)
	}
	if len(saved.Genders) != 2 || saved.Genders[0] != "woman" || saved.Genders[1] != "nonbinary" {
		t.Errorf("genders = %v, want [woman nonbinary]", saved.Genders)
	}
	if saved.MaxDistanceKM == nil || *saved.MaxDistanceKM != distance {
		t.Errorf("max_distance_km = %v, want %d", saved.MaxDistanceKM, distance)
	}
	if saved.TraitWeights[domain.TraitOpenness] != 2.5 {
		t.Errorf("trait_weights = %v, want openness 2.5", saved.TraitWeights)
	}
	if !saved.Complete {
		t.Error("complete = false after setting an age range and genders")
	}
	if !saved.DistanceFilterActive {
		t.Error("distance_filter_active = false after setting max_distance_km")
	}

	if fetched := getPreferences(t, u); fetched.TraitWeights[domain.TraitOpenness] != 2.5 {
		t.Errorf("weights did not survive a re-read: %v", fetched.TraitWeights)
	}
}

func TestUpdatePreferencesNormalizesGenders(t *testing.T) {
	resetDB(t)
	u := register(t, "Aliases", "1994-05-05")

	var saved preferencesDTO
	do(t, http.MethodPut, "/api/v1/preferences", u.Token, map[string]any{
		"age_min": 18,
		"age_max": 99,
		// Mixed case, legacy aliases and a duplicate: all must collapse to the
		// canonical values the discovery query compares against.
		"genders": []string{"Male", "female", "MAN", "non-binary"},
	}).requireStatus(t, http.StatusOK).decode(t, &saved)

	want := []string{"man", "woman", "nonbinary"}
	if len(saved.Genders) != len(want) {
		t.Fatalf("genders = %v, want %v", saved.Genders, want)
	}
	for i := range want {
		if saved.Genders[i] != want[i] {
			t.Fatalf("genders = %v, want %v", saved.Genders, want)
		}
	}

	// The stored array must be canonical too, since discovery filters in SQL.
	var stored []string
	if err := testPool.QueryRow(context.Background(),
		`SELECT genders FROM preferences WHERE user_id = $1`, u.ID).Scan(&stored); err != nil {
		t.Fatalf("read stored genders: %v", err)
	}
	for _, g := range stored {
		if _, ok := domain.NormalizeGender(g); !ok {
			t.Errorf("stored gender %q is not canonical", g)
		}
	}
}

func TestUpdatePreferencesReplacesRatherThanMerges(t *testing.T) {
	resetDB(t)
	u := register(t, "Replacer", "1994-05-05")

	setPreferences(t, u, map[string]any{
		"age_min":         25,
		"age_max":         40,
		"genders":         []string{"woman"},
		"max_distance_km": 50,
		"trait_weights":   map[string]float64{domain.TraitOpenness: 2},
	})

	// Omitting optional fields clears them; this is how the UI expresses
	// "anywhere" and "weight every trait equally".
	var saved preferencesDTO
	do(t, http.MethodPut, "/api/v1/preferences", u.Token, map[string]any{
		"age_min": 30,
		"age_max": 45,
		"genders": []string{"man"},
	}).requireStatus(t, http.StatusOK).decode(t, &saved)

	if saved.MaxDistanceKM != nil {
		t.Errorf("max_distance_km = %v, want null after an update that omitted it", *saved.MaxDistanceKM)
	}
	if len(saved.TraitWeights) != 0 {
		t.Errorf("trait_weights = %v, want cleared after an update that omitted them", saved.TraitWeights)
	}
	if len(saved.Genders) != 1 || saved.Genders[0] != "man" {
		t.Errorf("genders = %v, want [man]", saved.Genders)
	}
}

func TestUpdatePreferencesValidation(t *testing.T) {
	resetDB(t)
	u := register(t, "Invalid", "1994-05-05")
	setPreferences(t, u, map[string]any{
		"age_min": 25,
		"age_max": 40,
		"genders": []string{"woman"},
	})
	before := getPreferences(t, u)

	cases := map[string]struct {
		body  map[string]any
		field string
	}{
		"missing age_min": {
			map[string]any{"age_max": 40, "genders": []string{"woman"}}, "age_min"},
		"missing age_max": {
			map[string]any{"age_min": 25, "genders": []string{"woman"}}, "age_max"},
		"under the legal minimum": {
			map[string]any{"age_min": preferences.MinAge - 1, "age_max": 40, "genders": []string{"woman"}}, "age_min"},
		"age above the ceiling": {
			map[string]any{"age_min": 25, "age_max": preferences.MaxAge + 1, "genders": []string{"woman"}}, "age_max"},
		"inverted range": {
			map[string]any{"age_min": 40, "age_max": 25, "genders": []string{"woman"}}, "age_max"},
		"no genders": {
			map[string]any{"age_min": 25, "age_max": 40, "genders": []string{}}, "genders"},
		"unknown gender": {
			map[string]any{"age_min": 25, "age_max": 40, "genders": []string{"martian"}}, "genders"},
		"distance below the floor": {
			map[string]any{"age_min": 25, "age_max": 40, "genders": []string{"woman"},
				"max_distance_km": preferences.MinDistanceKM - 1}, "max_distance_km"},
		"distance above the ceiling": {
			map[string]any{"age_min": 25, "age_max": 40, "genders": []string{"woman"},
				"max_distance_km": preferences.MaxDistanceKM + 1}, "max_distance_km"},
		"unknown trait weight": {
			map[string]any{"age_min": 25, "age_max": 40, "genders": []string{"woman"},
				"trait_weights": map[string]float64{"charisma": 1}}, "trait_weights"},
		"negative trait weight": {
			map[string]any{"age_min": 25, "age_max": 40, "genders": []string{"woman"},
				"trait_weights": map[string]float64{domain.TraitOpenness: -1}}, "trait_weights"},
		"trait weight above the cap": {
			map[string]any{"age_min": 25, "age_max": 40, "genders": []string{"woman"},
				"trait_weights": map[string]float64{domain.TraitOpenness: domain.MaxTraitWeight + 1}}, "trait_weights"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resp := do(t, http.MethodPut, "/api/v1/preferences", u.Token, tc.body).
				requireError(t, http.StatusUnprocessableEntity, "validation_failed")
			// The offending field must be named, or the form cannot show the error
			// next to the input.
			if !resp.hasFieldError(tc.field) {
				t.Errorf("error details = %s, want an entry for %q", resp.Body, tc.field)
			}
		})
	}

	// Every rejection must have left the stored settings alone.
	after := getPreferences(t, u)
	if *after.AgeMin != *before.AgeMin || *after.AgeMax != *before.AgeMax ||
		len(after.Genders) != len(before.Genders) {
		t.Fatalf("preferences changed after rejected updates: %+v -> %+v", before, after)
	}
}

func TestPreferencesRequireAuthentication(t *testing.T) {
	resetDB(t)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		do(t, method, "/api/v1/preferences", "", map[string]any{
			"age_min": 25, "age_max": 40, "genders": []string{"woman"},
		}).requireStatus(t, http.StatusUnauthorized)
	}
}

func TestPreferencesAreScopedToTheCaller(t *testing.T) {
	resetDB(t)
	a := register(t, "Alice", "1994-05-05")
	b := register(t, "Bob", "1993-04-04")

	setPreferences(t, a, map[string]any{
		"age_min": 25, "age_max": 40, "genders": []string{"man"},
	})
	setPreferences(t, b, map[string]any{
		"age_min": 30, "age_max": 50, "genders": []string{"woman"},
	})

	if got := getPreferences(t, a); *got.AgeMax != 40 || got.Genders[0] != "man" {
		t.Errorf("alice sees %+v, want her own settings", got)
	}
	if got := getPreferences(t, b); *got.AgeMax != 50 || got.Genders[0] != "woman" {
		t.Errorf("bob sees %+v, want his own settings", got)
	}
}
