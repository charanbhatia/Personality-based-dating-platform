package personality

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/google/uuid"
)

// bank builds a deterministic question bank with itemsPerTrait items for each of
// the Big Five, alternating forward and reverse keying.
func bank(itemsPerTrait int) []Question {
	var out []Question
	order := 0
	for _, trait := range domain.TraitKeys() {
		for i := 0; i < itemsPerTrait; i++ {
			direction := 1
			if i%2 == 1 {
				direction = -1
			}
			order++
			out = append(out, Question{
				ID:        uuid.New(),
				Code:      strings.ToUpper(trait[:1]) + string(rune('0'+i)),
				Prompt:    "prompt",
				TraitKey:  trait,
				Direction: direction,
				SortOrder: order,
			})
		}
	}
	return out
}

func answerAll(questions []Question, value int) []Answer {
	out := make([]Answer, 0, len(questions))
	for _, q := range questions {
		out = append(out, Answer{QuestionID: q.ID, Value: value})
	}
	return out
}

func TestScoreNormalizesToUnitInterval(t *testing.T) {
	// A single forward item per trait makes the mapping directly observable.
	questions := bank(1)
	cases := map[int]float64{1: 0, 2: 0.25, 3: 0.5, 4: 0.75, 5: 1}
	for value, want := range cases {
		traits, err := Score(questions, answerAll(questions, value))
		if err != nil {
			t.Fatalf("value %d: unexpected error: %v", value, err)
		}
		for _, key := range domain.TraitKeys() {
			if math.Abs(traits[key]-want) > 1e-9 {
				t.Errorf("value %d: trait %s = %v, want %v", value, key, traits[key], want)
			}
		}
	}
}

func TestScoreInvertsReverseKeyedItems(t *testing.T) {
	forward := Question{ID: uuid.New(), Code: "F", TraitKey: domain.TraitOpenness, Direction: 1}
	reverse := Question{ID: uuid.New(), Code: "R", TraitKey: domain.TraitOpenness, Direction: -1}

	// Answering "strongly agree" to both a forward and a reverse item on the same
	// trait must cancel out to the midpoint.
	questions := []Question{forward, reverse}
	for _, key := range domain.TraitKeys()[1:] {
		questions = append(questions, Question{ID: uuid.New(), Code: "X" + key, TraitKey: key, Direction: 1})
	}
	traits, err := Score(questions, answerAll(questions, 5))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(traits[domain.TraitOpenness]-0.5) > 1e-9 {
		t.Fatalf("openness = %v, want 0.5 (a forward and a reverse item must cancel)", traits[domain.TraitOpenness])
	}
	if math.Abs(traits[domain.TraitConscientiousness]-1) > 1e-9 {
		t.Fatalf("conscientiousness = %v, want 1", traits[domain.TraitConscientiousness])
	}
}

func TestScoreAveragesWithinATrait(t *testing.T) {
	// Two forward items on one trait, answered 1 and 5, average to the midpoint.
	a := Question{ID: uuid.New(), Code: "A", TraitKey: domain.TraitExtraversion, Direction: 1}
	b := Question{ID: uuid.New(), Code: "B", TraitKey: domain.TraitExtraversion, Direction: 1}
	questions := []Question{a, b}
	answers := []Answer{{QuestionID: a.ID, Value: 1}, {QuestionID: b.ID, Value: 5}}
	for _, key := range domain.TraitKeys() {
		if key == domain.TraitExtraversion {
			continue
		}
		q := Question{ID: uuid.New(), Code: "X" + key, TraitKey: key, Direction: 1}
		questions = append(questions, q)
		answers = append(answers, Answer{QuestionID: q.ID, Value: 3})
	}
	traits, err := Score(questions, answers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(traits[domain.TraitExtraversion]-0.5) > 1e-9 {
		t.Fatalf("extraversion = %v, want 0.5", traits[domain.TraitExtraversion])
	}
}

func TestScoreResultIsAlwaysAValidVector(t *testing.T) {
	questions := bank(6)
	for value := MinAnswerValue; value <= MaxAnswerValue; value++ {
		traits, err := Score(questions, answerAll(questions, value))
		if err != nil {
			t.Fatalf("value %d: unexpected error: %v", value, err)
		}
		if err := traits.Validate(); err != nil {
			t.Fatalf("value %d: produced an invalid vector: %v", value, err)
		}
		if len(traits) != len(domain.TraitKeys()) {
			t.Fatalf("value %d: got %d traits, want %d", value, len(traits), len(domain.TraitKeys()))
		}
	}
}

func TestScoreIsDeterministic(t *testing.T) {
	questions := bank(6)
	answers := make([]Answer, 0, len(questions))
	for i, q := range questions {
		answers = append(answers, Answer{QuestionID: q.ID, Value: (i % MaxAnswerValue) + 1})
	}
	first, err := Score(questions, answers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 25; i++ {
		again, err := Score(questions, answers)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for _, key := range domain.TraitKeys() {
			if again[key] != first[key] {
				t.Fatalf("trait %s varies between runs: %v then %v", key, first[key], again[key])
			}
		}
	}
}

func TestScoreRejectsPartialSubmission(t *testing.T) {
	questions := bank(2)
	answers := answerAll(questions, 3)[:len(questions)-1]

	_, err := Score(questions, answers)
	if err == nil {
		t.Fatal("expected an error for a partial submission")
	}
	var scoreErr *ScoreError
	if !errors.As(err, &scoreErr) {
		t.Fatalf("error is %T, want *ScoreError so the API can report the field", err)
	}
	if scoreErr.Field != "answers" {
		t.Fatalf("field = %q, want %q", scoreErr.Field, "answers")
	}
	if !strings.Contains(scoreErr.Message, "missing") {
		t.Fatalf("message %q does not say which items are missing", scoreErr.Message)
	}
}

func TestScoreMissingListIsBoundedAndSorted(t *testing.T) {
	questions := bank(6) // 30 items
	_, err := Score(questions, nil)
	if err == nil {
		t.Fatal("expected an error when nothing is answered")
	}
	var scoreErr *ScoreError
	if !errors.As(err, &scoreErr) {
		t.Fatalf("error is %T, want *ScoreError", err)
	}
	if !strings.Contains(scoreErr.Message, "and 25 more") {
		t.Fatalf("message should elide the tail of a 30-item list, got %q", scoreErr.Message)
	}
	listed := strings.SplitN(scoreErr.Message, "missing: ", 2)[1]
	codes := strings.Split(listed, ", ")
	for i := 1; i < len(codes)-1; i++ {
		if codes[i-1] > codes[i] {
			t.Fatalf("missing codes are not sorted: %v", codes)
		}
	}
}

func TestScoreRejectsOutOfRangeValues(t *testing.T) {
	questions := bank(1)
	for _, value := range []int{-1, 0, MaxAnswerValue + 1, 100} {
		answers := answerAll(questions, 3)
		answers[0].Value = value

		_, err := Score(questions, answers)
		if err == nil {
			t.Fatalf("value %d was accepted", value)
		}
		var scoreErr *ScoreError
		if !errors.As(err, &scoreErr) {
			t.Fatalf("value %d: error is %T, want *ScoreError", value, err)
		}
		if scoreErr.Field != "answers[0].value" {
			t.Fatalf("value %d: field = %q, want answers[0].value", value, scoreErr.Field)
		}
	}
}

func TestScoreRejectsUnknownQuestion(t *testing.T) {
	questions := bank(1)
	answers := answerAll(questions, 3)
	answers = append(answers, Answer{QuestionID: uuid.New(), Value: 3})

	_, err := Score(questions, answers)
	if err == nil {
		t.Fatal("an answer to a question outside the bank was accepted")
	}
	var scoreErr *ScoreError
	if !errors.As(err, &scoreErr) {
		t.Fatalf("error is %T, want *ScoreError", err)
	}
	if !strings.HasSuffix(scoreErr.Field, ".question_id") {
		t.Fatalf("field = %q, want a question_id field", scoreErr.Field)
	}
}

func TestScoreRejectsDuplicateAnswer(t *testing.T) {
	questions := bank(1)
	answers := answerAll(questions, 3)
	answers = append(answers, Answer{QuestionID: questions[0].ID, Value: 5})

	_, err := Score(questions, answers)
	if err == nil {
		t.Fatal("a duplicate answer was accepted; it would skew that trait's mean")
	}
	if !strings.Contains(err.Error(), "more than once") {
		t.Fatalf("error %q does not explain the duplicate", err)
	}
}

func TestScoreRejectsEmptyBank(t *testing.T) {
	if _, err := Score(nil, nil); err == nil {
		t.Fatal("expected an error for an empty question bank")
	}
}

func TestScoreRejectsBankMissingATrait(t *testing.T) {
	// Drop every neuroticism item: this is a server misconfiguration, not client
	// error, and must not silently produce a four-trait vector.
	var questions []Question
	for _, q := range bank(2) {
		if q.TraitKey != domain.TraitNeuroticism {
			questions = append(questions, q)
		}
	}
	_, err := Score(questions, answerAll(questions, 3))
	if err == nil {
		t.Fatal("expected an error when the bank covers only four traits")
	}
	var scoreErr *ScoreError
	if errors.As(err, &scoreErr) {
		t.Fatal("a malformed bank must not surface as a client validation error")
	}
	if !strings.Contains(err.Error(), domain.TraitNeuroticism) {
		t.Fatalf("error %q does not name the missing trait", err)
	}
}

func TestScoreRoundsToFixedPrecision(t *testing.T) {
	// Three items on one trait produce a repeating decimal.
	var questions []Question
	var answers []Answer
	for i := 0; i < 3; i++ {
		q := Question{ID: uuid.New(), Code: "O" + string(rune('0'+i)), TraitKey: domain.TraitOpenness, Direction: 1}
		questions = append(questions, q)
		answers = append(answers, Answer{QuestionID: q.ID, Value: i + 1})
	}
	for _, key := range domain.TraitKeys()[1:] {
		q := Question{ID: uuid.New(), Code: "X" + key, TraitKey: key, Direction: 1}
		questions = append(questions, q)
		answers = append(answers, Answer{QuestionID: q.ID, Value: 2})
	}
	traits, err := Score(questions, answers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// (0 + 0.25 + 0.5) / 3 = 0.25
	if traits[domain.TraitOpenness] != 0.25 {
		t.Fatalf("openness = %v, want 0.25", traits[domain.TraitOpenness])
	}

	factor := math.Pow(10, TraitDecimals)
	for key, value := range traits {
		if value*factor != math.Trunc(value*factor) {
			t.Fatalf("trait %s = %v, which carries more than %d decimal places", key, value, TraitDecimals)
		}
	}
}

func TestScoreTreatsZeroDirectionAsForward(t *testing.T) {
	// direction is a smallint in the bank; anything non-negative scores forward.
	questions := bank(1)
	for i := range questions {
		questions[i].Direction = 0
	}
	traits, err := Score(questions, answerAll(questions, 5))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if traits[domain.TraitOpenness] != 1 {
		t.Fatalf("openness = %v, want 1", traits[domain.TraitOpenness])
	}
}
