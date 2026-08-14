package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/google/uuid"
)

func TestDiscoverRequiresAnAssessment(t *testing.T) {
	resetDB(t)
	u := register(t, "No Quiz", "1990-01-01")

	do(t, http.MethodGet, "/api/v1/discover", u.Token, nil).
		requireError(t, http.StatusConflict, "assessment_required")

	submitAssessment(t, u, 3)
	do(t, http.MethodGet, "/api/v1/discover", u.Token, nil).requireStatus(t, http.StatusOK)
}

func TestDiscoverIncludesCandidateTraits(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})
	onboard(t, onboardOptions{Name: "Ada"})

	page := discover(t, viewer, "")
	if len(page.Items) == 0 {
		t.Fatal("feed is empty")
	}
	item := page.Items[0]
	for _, k := range domain.TraitKeys() {
		if _, ok := item.Traits[k]; !ok {
			t.Errorf("discover card is missing trait %q: %#v", k, item.Traits)
		}
	}
}

func TestDiscoverExcludesSelf(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})
	onboard(t, onboardOptions{Name: "Other"})

	page := discover(t, viewer, "")
	if page.contains(viewer.ID) {
		t.Fatal("the viewer appears in their own feed")
	}
	if len(page.Items) != 1 {
		t.Fatalf("feed has %d items, want 1", len(page.Items))
	}
}

func TestDiscoverNeverExposesEmail(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})
	candidate := onboard(t, onboardOptions{Name: "Candidate"})

	resp := do(t, http.MethodGet, "/api/v1/discover", viewer.Token, nil).requireStatus(t, http.StatusOK)
	body := string(resp.Body)
	for _, forbidden := range []string{candidate.Email, "email", "password"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("the discovery payload contains %q: %s", forbidden, body)
		}
	}
}

func TestDiscoverRequiresCandidatesToBeScored(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})

	// Registered but has not taken the assessment: cannot be ranked, so must not
	// appear rather than be ranked on a fabricated score.
	unscored := register(t, "Unscored", "1992-02-02")
	setProfile(t, unscored, map[string]any{"gender": "woman"})
	setPhotos(t, unscored, "https://cdn.example.test/unscored.jpg")

	if discover(t, viewer, "").contains(unscored.ID) {
		t.Fatal("an unassessed user appears in the feed")
	}

	submitAssessment(t, unscored, 4)
	if !discover(t, viewer, "").contains(unscored.ID) {
		t.Fatal("the user is still absent after being assessed")
	}
}

func TestDiscoverFiltersByGenderPreference(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer", Genders: []string{"woman"}})
	woman := onboard(t, onboardOptions{Name: "Wanted", Gender: "woman"})
	man := onboard(t, onboardOptions{Name: "Filtered", Gender: "man"})

	page := discover(t, viewer, "")
	if !page.contains(woman.ID) {
		t.Error("a candidate matching the gender preference is missing")
	}
	if page.contains(man.ID) {
		t.Error("a candidate outside the gender preference is present")
	}

	// Widening the preference must reveal them.
	setPreferences(t, viewer, map[string]any{"age_min": 18, "age_max": 99, "genders": []string{"woman", "man"}})
	if !discover(t, viewer, "").contains(man.ID) {
		t.Error("widening the preference did not reveal the candidate")
	}
}

func TestDiscoverFiltersByAgeRange(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer", AgeMin: 30, AgeMax: 40})

	// Ages are computed from the date of birth against the current date.
	young := onboard(t, onboardOptions{Name: "Too Young", DateOfBirth: yearsAgo(t, 22)})
	inRange := onboard(t, onboardOptions{Name: "In Range", DateOfBirth: yearsAgo(t, 35)})
	old := onboard(t, onboardOptions{Name: "Too Old", DateOfBirth: yearsAgo(t, 55)})

	page := discover(t, viewer, "")
	if !page.contains(inRange.ID) {
		t.Error("a candidate inside the age range is missing")
	}
	if page.contains(young.ID) {
		t.Error("a candidate below age_min is present")
	}
	if page.contains(old.ID) {
		t.Error("a candidate above age_max is present")
	}
}

func TestDiscoverIncludesTheAgeBoundaries(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer", AgeMin: 30, AgeMax: 40})
	atMin := onboard(t, onboardOptions{Name: "Exactly Thirty", DateOfBirth: yearsAgo(t, 30)})
	atMax := onboard(t, onboardOptions{Name: "Exactly Forty", DateOfBirth: yearsAgo(t, 40)})

	page := discover(t, viewer, "")
	if !page.contains(atMin.ID) {
		t.Error("age_min is exclusive; it should include candidates of exactly that age")
	}
	if !page.contains(atMax.ID) {
		t.Error("age_max is exclusive; it should include candidates of exactly that age")
	}
}

func TestDiscoverExcludesAlreadySwipedUsers(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})
	liked := onboard(t, onboardOptions{Name: "Liked"})
	passed := onboard(t, onboardOptions{Name: "Passed"})
	untouched := onboard(t, onboardOptions{Name: "Untouched"})

	swipe(t, viewer, liked.ID, "like")
	swipe(t, viewer, passed.ID, "pass")

	page := discover(t, viewer, "")
	if page.contains(liked.ID) || page.contains(passed.ID) {
		t.Fatalf("a swiped candidate reappeared: %v", page.ids())
	}
	if !page.contains(untouched.ID) {
		t.Fatal("an un-swiped candidate is missing")
	}
}

func TestDiscoverIsSortedByCompatibilityDescending(t *testing.T) {
	resetDB(t)
	// The viewer sits at the bottom of every trait, so a candidate's score falls
	// as their level rises: agreement is 1 - |0 - candidate|.
	viewer := onboard(t, onboardOptions{Name: "Viewer", Level: LevelLowest})
	levels := []int{1, 2, 3, 4, 5}
	byName := make(map[string]int, len(levels))
	for _, level := range levels {
		name := fmt.Sprintf("Level %d", level)
		onboard(t, onboardOptions{Name: name, Level: level})
		byName[name] = level
	}

	page := discover(t, viewer, "")
	if len(page.Items) != len(levels) {
		t.Fatalf("feed has %d items, want %d", len(page.Items), len(levels))
	}
	for i := 1; i < len(page.Items); i++ {
		if page.Items[i-1].CompatibilityScore < page.Items[i].CompatibilityScore {
			t.Fatalf("scores are not descending: %v then %v",
				page.Items[i-1].CompatibilityScore, page.Items[i].CompatibilityScore)
		}
	}

	// The exact expected score per candidate, which also proves the ordering is
	// driven by compatibility rather than insertion order.
	for _, item := range page.Items {
		level, known := byName[item.Name]
		if !known {
			t.Fatalf("unexpected candidate %q", item.Name)
		}
		want := 1 - traitValueForLevel(level)
		if diff := item.CompatibilityScore - want; diff > 1e-6 || diff < -1e-6 {
			t.Errorf("%s scored %v, want %v", item.Name, item.CompatibilityScore, want)
		}
	}
	if page.Items[0].Name != "Level 1" {
		t.Errorf("top of the feed is %q, want the identical candidate", page.Items[0].Name)
	}
}

func TestDiscoverScoreMatchesTheGoScorer(t *testing.T) {
	resetDB(t)
	// The SQL expression in discoverSQL and domain.Compatibility must agree, or
	// the feed order would not match anything computed in the application.
	viewerLevels := map[string]int{
		domain.TraitOpenness:          1,
		domain.TraitConscientiousness: 2,
		domain.TraitExtraversion:      3,
		domain.TraitAgreeableness:     4,
		domain.TraitNeuroticism:       5,
	}
	candidateLevels := map[string]int{
		domain.TraitOpenness:          5,
		domain.TraitConscientiousness: 4,
		domain.TraitExtraversion:      2,
		domain.TraitAgreeableness:     1,
		domain.TraitNeuroticism:       3,
	}

	viewer := onboard(t, onboardOptions{Name: "Viewer", TraitLevels: viewerLevels})
	candidate := onboard(t, onboardOptions{Name: "Candidate", TraitLevels: candidateLevels})

	viewerTraits := fetchTraits(t, viewer)
	candidateTraits := traitsOf(t, candidate.ID)

	weights := domain.TraitWeights{
		domain.TraitOpenness:     2.5,
		domain.TraitNeuroticism:  0.25,
		domain.TraitExtraversion: 0,
	}
	setPreferences(t, viewer, map[string]any{
		"age_min":       18,
		"age_max":       99,
		"genders":       []string{"man", "woman", "nonbinary", "other"},
		"trait_weights": weights,
	})

	page := discover(t, viewer, "")
	if len(page.Items) != 1 {
		t.Fatalf("feed has %d items, want 1", len(page.Items))
	}

	want := domain.RoundScore(domain.Compatibility(viewerTraits, candidateTraits, weights))
	got := domain.RoundScore(page.Items[0].CompatibilityScore)
	if got != want {
		t.Fatalf("SQL scored %v but domain.Compatibility gives %v (viewer %v, candidate %v, weights %v)",
			got, want, viewerTraits, candidateTraits, weights)
	}
}

func TestDiscoverAppliesTraitWeights(t *testing.T) {
	resetDB(t)
	// Identical on four traits, maximally opposed on neuroticism.
	base := map[string]int{
		domain.TraitOpenness:          LevelMiddle,
		domain.TraitConscientiousness: LevelMiddle,
		domain.TraitExtraversion:      LevelMiddle,
		domain.TraitAgreeableness:     LevelMiddle,
		domain.TraitNeuroticism:       LevelLowest,
	}
	opposite := map[string]int{
		domain.TraitOpenness:          LevelMiddle,
		domain.TraitConscientiousness: LevelMiddle,
		domain.TraitExtraversion:      LevelMiddle,
		domain.TraitAgreeableness:     LevelMiddle,
		domain.TraitNeuroticism:       LevelHighest,
	}
	viewer := onboard(t, onboardOptions{Name: "Viewer", TraitLevels: base})
	onboard(t, onboardOptions{Name: "Opposite", TraitLevels: opposite})

	neutral := discover(t, viewer, "").Items[0].CompatibilityScore

	setPreferences(t, viewer, map[string]any{
		"age_min": 18, "age_max": 99,
		"genders":       []string{"man", "woman", "nonbinary", "other"},
		"trait_weights": map[string]float64{domain.TraitNeuroticism: 0},
	})
	ignored := discover(t, viewer, "").Items[0].CompatibilityScore

	setPreferences(t, viewer, map[string]any{
		"age_min": 18, "age_max": 99,
		"genders":       []string{"man", "woman", "nonbinary", "other"},
		"trait_weights": map[string]float64{domain.TraitNeuroticism: 5},
	})
	emphasised := discover(t, viewer, "").Items[0].CompatibilityScore

	if !(ignored > neutral && neutral > emphasised) {
		t.Fatalf("weights had no ordered effect: ignored=%v neutral=%v emphasised=%v",
			ignored, neutral, emphasised)
	}
	if ignored != 1 {
		t.Errorf("with the only disagreement zero-weighted the score is %v, want 1", ignored)
	}
	// Unweighted: four traits agree exactly, one disagrees completely.
	if diff := neutral - 0.8; diff > 1e-6 || diff < -1e-6 {
		t.Errorf("unweighted score is %v, want 0.8", neutral)
	}
}

func TestDiscoverPaginationVisitsEveryCandidateExactlyOnce(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer", Level: LevelMiddle})

	const total = 12
	expected := make(map[uuid.UUID]bool, total)
	for i := 0; i < total; i++ {
		// Repeat answer values so several candidates tie on score, which is what
		// exercises the user_id tiebreaker in the keyset comparison.
		c := onboard(t, onboardOptions{
			Name:  fmt.Sprintf("Candidate %02d", i),
			Level: (i % 3) + 2,
		})
		expected[c.ID] = true
	}

	seen := make(map[uuid.UUID]int, total)
	var lastScore float64 = 2 // above any possible score
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > total+2 {
			t.Fatal("pagination did not terminate")
		}
		query := "limit=5"
		if cursor != "" {
			query += "&cursor=" + cursor
		}
		page := discover(t, viewer, query)
		for _, item := range page.Items {
			seen[item.UserID]++
			if item.CompatibilityScore > lastScore {
				t.Fatalf("score %v on a later page exceeds %v from an earlier one; the ordering is unstable",
					item.CompatibilityScore, lastScore)
			}
			lastScore = item.CompatibilityScore
		}
		if page.NextCursor == nil {
			if len(page.Items) > 5 {
				t.Fatalf("final page returned %d items, want at most 5", len(page.Items))
			}
			break
		}
		if len(page.Items) != 5 {
			t.Fatalf("a non-final page returned %d items, want exactly 5", len(page.Items))
		}
		cursor = *page.NextCursor
	}

	if len(seen) != total {
		t.Fatalf("walked %d distinct candidates, want %d", len(seen), total)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("candidate %v appeared %d times", id, count)
		}
		if !expected[id] {
			t.Errorf("unexpected candidate %v in the feed", id)
		}
	}
}

func TestDiscoverLastPageHasNoCursor(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})
	onboard(t, onboardOptions{Name: "Only Candidate"})

	page := discover(t, viewer, "limit=5")
	if len(page.Items) != 1 {
		t.Fatalf("feed has %d items, want 1", len(page.Items))
	}
	if page.NextCursor != nil {
		t.Fatalf("next_cursor = %q on a partial page, want null", *page.NextCursor)
	}
}

func TestDiscoverEmptyFeedIsAnEmptyArray(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Alone"})

	resp := do(t, http.MethodGet, "/api/v1/discover", viewer.Token, nil).requireStatus(t, http.StatusOK)
	// A null items array would break clients that iterate without a nil check.
	if !strings.Contains(string(resp.Body), `"items":[]`) {
		t.Fatalf("expected an empty array, got %s", resp.Body)
	}
}

func TestDiscoverRejectsBadParameters(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer"})

	for _, query := range []string{
		"limit=0", "limit=-1", "limit=9999", "limit=abc",
		"cursor=not-a-cursor", "cursor=" + uuid.NewString(),
	} {
		t.Run(query, func(t *testing.T) {
			do(t, http.MethodGet, "/api/v1/discover?"+query, viewer.Token, nil).
				requireStatus(t, http.StatusBadRequest)
		})
	}
}

func TestDiscoverCursorFromAnotherViewerIsHarmless(t *testing.T) {
	resetDB(t)
	first := onboard(t, onboardOptions{Name: "First"})
	second := onboard(t, onboardOptions{Name: "Second"})
	for i := 0; i < 6; i++ {
		onboard(t, onboardOptions{Name: fmt.Sprintf("Candidate %d", i), Level: (i % 5) + 1})
	}

	page := discover(t, first, "limit=2")
	if page.NextCursor == nil {
		t.Fatal("expected a further page")
	}
	// A cursor is only a position, not a capability: replaying someone else's
	// must still produce a well-formed page scoped to the caller.
	other := discover(t, second, "limit=2&cursor="+*page.NextCursor)
	if other.contains(second.ID) {
		t.Fatal("the second viewer sees themselves")
	}
}

// yearsAgo builds a date of birth using the database's clock, so an age
// assertion cannot fail because the test process and Postgres disagree about
// today's date.
func yearsAgo(t *testing.T, years int) string {
	t.Helper()
	var dob string
	err := testPool.QueryRow(context.Background(),
		`SELECT to_char(current_date - make_interval(years => $1), 'YYYY-MM-DD')`, years).Scan(&dob)
	if err != nil {
		t.Fatalf("compute date of birth: %v", err)
	}
	return dob
}

func fetchTraits(t *testing.T, u *user) domain.Traits {
	t.Helper()
	var payload struct {
		Traits domain.Traits `json:"traits"`
	}
	do(t, http.MethodGet, "/api/v1/personality/me", u.Token, nil).
		requireStatus(t, http.StatusOK).decode(t, &payload)
	return payload.Traits
}

func traitsOf(t *testing.T, userID uuid.UUID) domain.Traits {
	t.Helper()
	var raw []byte
	err := testPool.QueryRow(context.Background(),
		`SELECT traits FROM personality_scores WHERE user_id = $1`, userID).Scan(&raw)
	if err != nil {
		t.Fatalf("load traits for %v: %v", userID, err)
	}
	var traits domain.Traits
	if err := json.Unmarshal(raw, &traits); err != nil {
		t.Fatalf("decode traits: %v", err)
	}
	return traits
}
