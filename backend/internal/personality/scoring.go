package personality

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/google/uuid"
)

// Likert response bounds.
const (
	MinAnswerValue = 1
	MaxAnswerValue = 5
)

// TraitDecimals is the precision trait scores are stored at. Four places is far
// beyond questionnaire resolution and keeps the JSON compact.
const TraitDecimals = 4

// Question is one item in the assessment bank.
type Question struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	Prompt    string    `json:"prompt"`
	TraitKey  string    `json:"trait_key"`
	Direction int       `json:"direction"`
	SortOrder int       `json:"sort_order"`
}

// Answer is a submitted response.
type Answer struct {
	QuestionID uuid.UUID `json:"question_id"`
	Value      int       `json:"value"`
}

// ScoreError describes why a submission was rejected, in terms the API can pass
// straight through to the client.
type ScoreError struct {
	Field   string
	Message string
}

func (e *ScoreError) Error() string { return e.Field + ": " + e.Message }

func scoreErr(field, format string, args ...any) *ScoreError {
	return &ScoreError{Field: field, Message: fmt.Sprintf(format, args...)}
}

// Score converts Likert answers into a normalized Big Five vector.
//
// Every active question must be answered exactly once. Partial submissions are
// rejected rather than scored over whatever arrived, because a trait averaged
// over a different number of items is not comparable with other users' — and
// compatibility ranking assumes the vectors are comparable.
//
// Normalization maps 1..5 onto 0..1, inverted for reverse-keyed items:
//
//	direction  1: (value - 1) / 4
//	direction -1: (5 - value) / 4
func Score(questions []Question, answers []Answer) (domain.Traits, error) {
	if len(questions) == 0 {
		return nil, errors.New("personality: question bank is empty")
	}

	byID := make(map[uuid.UUID]Question, len(questions))
	for _, q := range questions {
		byID[q.ID] = q
	}

	seen := make(map[uuid.UUID]struct{}, len(answers))
	sums := make(map[string]float64, len(domain.TraitKeys()))
	counts := make(map[string]int, len(domain.TraitKeys()))

	for i, a := range answers {
		question, known := byID[a.QuestionID]
		if !known {
			return nil, scoreErr(fmt.Sprintf("answers[%d].question_id", i),
				"%s is not part of the current assessment", a.QuestionID)
		}
		if _, duplicate := seen[a.QuestionID]; duplicate {
			return nil, scoreErr(fmt.Sprintf("answers[%d].question_id", i),
				"question %s was answered more than once", question.Code)
		}
		if a.Value < MinAnswerValue || a.Value > MaxAnswerValue {
			return nil, scoreErr(fmt.Sprintf("answers[%d].value", i),
				"must be between %d and %d", MinAnswerValue, MaxAnswerValue)
		}
		seen[a.QuestionID] = struct{}{}

		normalized := float64(a.Value-MinAnswerValue) / float64(MaxAnswerValue-MinAnswerValue)
		if question.Direction < 0 {
			normalized = 1 - normalized
		}
		sums[question.TraitKey] += normalized
		counts[question.TraitKey]++
	}

	if missing := missingCodes(questions, seen); len(missing) > 0 {
		return nil, scoreErr("answers",
			"all %d questions must be answered; missing: %s",
			len(questions), strings.Join(missing, ", "))
	}

	traits := make(domain.Traits, len(domain.TraitKeys()))
	for _, key := range domain.TraitKeys() {
		count := counts[key]
		if count == 0 {
			// Only reachable if the bank itself lacks a trait, which is a server
			// misconfiguration rather than bad input.
			return nil, fmt.Errorf("personality: question bank has no items for trait %q", key)
		}
		traits[key] = roundTo(sums[key]/float64(count), TraitDecimals)
	}
	if err := traits.Validate(); err != nil {
		return nil, fmt.Errorf("personality: computed traits are invalid: %w", err)
	}
	return traits, nil
}

func missingCodes(questions []Question, seen map[uuid.UUID]struct{}) []string {
	var missing []string
	for _, q := range questions {
		if _, ok := seen[q.ID]; !ok {
			missing = append(missing, q.Code)
		}
	}
	sort.Strings(missing)
	// Keep the message bounded; the client only needs to know which are absent,
	// and it already has the full question list.
	const maxListed = 5
	if len(missing) > maxListed {
		listed := append([]string{}, missing[:maxListed]...)
		return append(listed, fmt.Sprintf("and %d more", len(missing)-maxListed))
	}
	return missing
}

func roundTo(value float64, decimals int) float64 {
	factor := math.Pow(10, float64(decimals))
	return math.Round(value*factor) / factor
}
