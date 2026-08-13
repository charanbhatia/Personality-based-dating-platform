package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/bits-assignment/dating-platform/backend/internal/personality"
	"github.com/google/uuid"
)

type assessmentResult struct {
	Traits       domain.Traits `json:"traits"`
	Version      int           `json:"version"`
	AssessedAt   string        `json:"assessed_at"`
	NextRetakeAt *string       `json:"next_retake_at"`
	CanRetake    bool          `json:"can_retake"`
}

func TestAssessmentBankIsWellFormed(t *testing.T) {
	resetDB(t)
	u := register(t, "Quiz", "1994-05-05")

	list := fetchQuestions(t, u)
	if list.ScaleMin != personality.MinAnswerValue || list.ScaleMax != personality.MaxAnswerValue {
		t.Errorf("scale = %d..%d, want %d..%d",
			list.ScaleMin, list.ScaleMax, personality.MinAnswerValue, personality.MaxAnswerValue)
	}
	if !list.CanSubmit {
		t.Error("can_submit = false for a user who has never taken the assessment")
	}

	// Every trait must be covered, or a submission could not produce a full
	// vector, and each item needs a usable direction.
	perTrait := make(map[string]int)
	codes := make(map[string]bool)
	ids := make(map[uuid.UUID]bool)
	for _, q := range list.Questions {
		if !domain.IsTraitKey(q.TraitKey) {
			t.Errorf("question %s has unknown trait_key %q", q.Code, q.TraitKey)
		}
		if q.Direction != 1 && q.Direction != -1 {
			t.Errorf("question %s has direction %d, want 1 or -1", q.Code, q.Direction)
		}
		if codes[q.Code] {
			t.Errorf("duplicate question code %q", q.Code)
		}
		if ids[q.ID] {
			t.Errorf("duplicate question id %v", q.ID)
		}
		codes[q.Code] = true
		ids[q.ID] = true
		perTrait[q.TraitKey]++
	}
	for _, trait := range domain.TraitKeys() {
		if perTrait[trait] == 0 {
			t.Errorf("no questions for trait %q", trait)
		}
	}
}

func TestAssessmentQuestionOrderIsStable(t *testing.T) {
	resetDB(t)
	u := register(t, "Stable", "1994-05-05")

	first := fetchQuestions(t, u)
	for i := 0; i < 3; i++ {
		again := fetchQuestions(t, u)
		if len(again.Questions) != len(first.Questions) {
			t.Fatalf("question count changed between requests")
		}
		for j := range first.Questions {
			if again.Questions[j].ID != first.Questions[j].ID {
				t.Fatalf("question %d changed position between requests; a paused quiz would scramble", j)
			}
		}
	}
}

func TestSubmitAssessmentProducesTheExpectedVector(t *testing.T) {
	resetDB(t)
	u := register(t, "Scored", "1994-05-05")

	levels := map[string]int{
		domain.TraitOpenness:          LevelHighest,
		domain.TraitConscientiousness: LevelLowest,
		domain.TraitExtraversion:      LevelMiddle,
		domain.TraitAgreeableness:     4,
		domain.TraitNeuroticism:       2,
	}
	submitAssessmentPerTrait(t, u, levels)

	var result assessmentResult
	do(t, http.MethodGet, "/api/v1/personality/me", u.Token, nil).
		requireStatus(t, http.StatusOK).decode(t, &result)

	if result.Version != domain.TraitsVersion {
		t.Errorf("version = %d, want %d", result.Version, domain.TraitsVersion)
	}
	for trait, level := range levels {
		want := traitValueForLevel(level)
		got, present := result.Traits[trait]
		if !present {
			t.Errorf("trait %q missing from the result", trait)
			continue
		}
		if diff := got - want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("trait %q = %v, want %v", trait, got, want)
		}
	}
	if err := result.Traits.Validate(); err != nil {
		t.Errorf("the stored vector is not valid: %v", err)
	}

	// The raw answers are retained so a future scoring version can be recomputed
	// without asking users to retake the quiz.
	var answerCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT jsonb_array_length(raw_answers) FROM personality_scores WHERE user_id = $1`,
		u.ID).Scan(&answerCount); err != nil {
		t.Fatalf("load raw answers: %v", err)
	}
	if answerCount != len(fetchQuestions(t, u).Questions) {
		t.Errorf("stored %d raw answers, want one per question", answerCount)
	}
}

func TestSubmitAssessmentValidation(t *testing.T) {
	resetDB(t)
	u := register(t, "Invalid", "1994-05-05")
	list := fetchQuestions(t, u)

	full := func(value int) []map[string]any {
		out := make([]map[string]any, 0, len(list.Questions))
		for _, q := range list.Questions {
			out = append(out, map[string]any{"question_id": q.ID, "value": value})
		}
		return out
	}

	cases := []struct {
		name    string
		answers []map[string]any
	}{
		{"empty", nil},
		{"partial", full(3)[:len(list.Questions)-1]},
		{"value too low", withValue(full(3), 0, personality.MinAnswerValue-1)},
		{"value too high", withValue(full(3), 0, personality.MaxAnswerValue+1)},
		{"negative value", withValue(full(3), 0, -3)},
		{"unknown question", append(full(3), map[string]any{"question_id": uuid.New(), "value": 3})},
		{"duplicate question", append(full(3), map[string]any{"question_id": list.Questions[0].ID, "value": 5})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			do(t, http.MethodPost, "/api/v1/personality/assessment/submit", u.Token,
				map[string]any{"answers": tc.answers}).
				requireStatus(t, http.StatusUnprocessableEntity)
		})
	}

	// Nothing may have been persisted by the rejected submissions.
	do(t, http.MethodGet, "/api/v1/personality/me", u.Token, nil).requireStatus(t, http.StatusNotFound)
	if got := countRows(t, `SELECT count(*) FROM personality_scores WHERE user_id = $1`, u.ID); got != 0 {
		t.Fatalf("personality_scores rows = %d, want 0", got)
	}
}

func withValue(answers []map[string]any, index, value int) []map[string]any {
	out := make([]map[string]any, len(answers))
	copy(out, answers)
	replaced := map[string]any{}
	for k, v := range out[index] {
		replaced[k] = v
	}
	replaced["value"] = value
	out[index] = replaced
	return out
}

func TestPersonalityMeBeforeSubmitting(t *testing.T) {
	resetDB(t)
	u := register(t, "Unscored", "1994-05-05")

	// A missing assessment is a 404 rather than a neutral vector, so the client
	// can route the user into onboarding.
	do(t, http.MethodGet, "/api/v1/personality/me", u.Token, nil).
		requireStatus(t, http.StatusNotFound)
}

func TestAssessmentRetakeIsRateLimited(t *testing.T) {
	resetDB(t)
	u := register(t, "Retake", "1994-05-05")
	submitAssessment(t, u, LevelMiddle)

	var first assessmentResult
	do(t, http.MethodGet, "/api/v1/personality/me", u.Token, nil).
		requireStatus(t, http.StatusOK).decode(t, &first)
	if first.CanRetake {
		t.Error("can_retake = true immediately after submitting")
	}
	if first.NextRetakeAt == nil {
		t.Error("next_retake_at is null, so the client cannot tell the user when to come back")
	}

	// A second submission inside the window must be refused, and must not alter
	// the stored vector.
	do(t, http.MethodPost, "/api/v1/personality/assessment/submit", u.Token,
		map[string]any{"answers": answersForLevel(t, u, LevelHighest)}).
		requireError(t, http.StatusConflict, "retake_too_soon")

	var afterAttempt assessmentResult
	do(t, http.MethodGet, "/api/v1/personality/me", u.Token, nil).
		requireStatus(t, http.StatusOK).decode(t, &afterAttempt)
	for trait, value := range first.Traits {
		if afterAttempt.Traits[trait] != value {
			t.Fatalf("trait %q changed despite the refused retake", trait)
		}
	}

	// The assessment listing must advertise the same restriction.
	if fetchQuestions(t, u).CanSubmit {
		t.Error("can_submit = true while the retake window is open")
	}
}

func TestAssessmentRetakeAfterTheWindowUpdatesTraitsAndCache(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer", Level: LevelLowest})
	u := onboard(t, onboardOptions{Name: "Retaker", Level: LevelLowest})

	if got := discover(t, viewer, "").Items[0].CompatibilityScore; got != 1 {
		t.Fatalf("setup: score = %v, want 1", got)
	}

	// Move the recorded assessment outside the retake window.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE personality_scores SET assessed_at = now() - $2::interval WHERE user_id = $1`,
		u.ID, "2 days"); err != nil {
		t.Fatalf("age the assessment: %v", err)
	}
	if !fetchQuestions(t, u).CanSubmit {
		t.Fatal("can_submit = false after the retake window has passed")
	}

	submitAssessment(t, u, LevelHighest)

	var updated assessmentResult
	do(t, http.MethodGet, "/api/v1/personality/me", u.Token, nil).
		requireStatus(t, http.StatusOK).decode(t, &updated)
	for _, trait := range domain.TraitKeys() {
		if updated.Traits[trait] != 1 {
			t.Fatalf("trait %q = %v after the retake, want 1", trait, updated.Traits[trait])
		}
	}

	// Only one row per user: a retake replaces rather than accumulates.
	if got := countRows(t, `SELECT count(*) FROM personality_scores WHERE user_id = $1`, u.ID); got != 1 {
		t.Fatalf("personality_scores rows = %d, want 1", got)
	}

	// The cached vector must have been invalidated, or discovery would keep
	// scoring against the old answers.
	if got := discover(t, viewer, "").Items[0].CompatibilityScore; got != 0 {
		t.Fatalf("discovery score = %v after the retake, want 0; the trait cache is stale", got)
	}
}

func answersForLevel(t *testing.T, u *user, level int) []map[string]any {
	t.Helper()
	list := fetchQuestions(t, u)
	out := make([]map[string]any, 0, len(list.Questions))
	for _, q := range list.Questions {
		out = append(out, map[string]any{"question_id": q.ID, "value": answerForLevel(q, level)})
	}
	return out
}
