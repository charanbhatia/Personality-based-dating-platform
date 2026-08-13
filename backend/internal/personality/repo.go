package personality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/google/uuid"
)

// ErrNotFound means the user has not completed the assessment.
var ErrNotFound = errors.New("personality scores not found")

// Assessment is a stored trait vector.
type Assessment struct {
	UserID     uuid.UUID
	Traits     domain.Traits
	Version    int
	AssessedAt time.Time
	UpdatedAt  time.Time
}

// Repo reads the question bank and reads/writes trait vectors.
type Repo struct{}

// ActiveQuestions returns the live question bank in presentation order.
func (Repo) ActiveQuestions(ctx context.Context, q db.Querier) ([]Question, error) {
	rows, err := q.Query(ctx, `
		SELECT id, code, prompt, trait_key, direction, sort_order
		FROM personality_questions
		WHERE active
		ORDER BY sort_order, code`)
	if err != nil {
		return nil, fmt.Errorf("list questions: %w", err)
	}
	defer rows.Close()

	var out []Question
	for rows.Next() {
		var item Question
		var direction int16
		if err := rows.Scan(&item.ID, &item.Code, &item.Prompt, &item.TraitKey, &direction, &item.SortOrder); err != nil {
			return nil, fmt.Errorf("scan question: %w", err)
		}
		item.Direction = int(direction)
		out = append(out, item)
	}
	return out, rows.Err()
}

// Get loads a user's trait vector.
func (Repo) Get(ctx context.Context, q db.Querier, userID uuid.UUID) (*Assessment, error) {
	var raw []byte
	a := &Assessment{UserID: userID}
	var assessedAt *time.Time

	err := q.QueryRow(ctx, `
		SELECT traits, version, assessed_at, updated_at
		FROM personality_scores WHERE user_id = $1`, userID,
	).Scan(&raw, &a.Version, &assessedAt, &a.UpdatedAt)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load traits: %w", err)
	}

	if err := json.Unmarshal(raw, &a.Traits); err != nil {
		return nil, fmt.Errorf("decode traits for user %s: %w", userID, err)
	}
	// Rows seeded before the assessment API existed have no assessed_at; fall back
	// to updated_at so retake windows still have a reference point.
	if assessedAt != nil {
		a.AssessedAt = *assessedAt
	} else {
		a.AssessedAt = a.UpdatedAt
	}
	if !a.Traits.Complete() {
		// An incomplete vector cannot be scored against, and treating it as
		// "assessment done" would leave the user with an empty feed forever.
		return nil, ErrNotFound
	}
	return a, nil
}

// LastAssessedAt reports when a user last submitted, for the retake window.
func (Repo) LastAssessedAt(ctx context.Context, q db.Querier, userID uuid.UUID) (*time.Time, error) {
	var assessedAt *time.Time
	var updatedAt time.Time
	err := q.QueryRow(ctx,
		`SELECT assessed_at, updated_at FROM personality_scores WHERE user_id = $1`, userID,
	).Scan(&assessedAt, &updatedAt)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load assessment timestamp: %w", err)
	}
	if assessedAt != nil {
		return assessedAt, nil
	}
	return &updatedAt, nil
}

// Upsert stores a trait vector along with the raw answers and scoring version.
func (Repo) Upsert(ctx context.Context, q db.Querier, userID uuid.UUID, traits domain.Traits, answers []Answer, assessedAt time.Time) error {
	traitsJSON, err := json.Marshal(traits)
	if err != nil {
		return fmt.Errorf("encode traits: %w", err)
	}
	answersJSON, err := json.Marshal(answers)
	if err != nil {
		return fmt.Errorf("encode answers: %w", err)
	}
	_, err = q.Exec(ctx, `
		INSERT INTO personality_scores (user_id, traits, version, assessed_at, raw_answers, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, now(), now())
		ON CONFLICT (user_id) DO UPDATE SET
			traits      = EXCLUDED.traits,
			version     = EXCLUDED.version,
			assessed_at = EXCLUDED.assessed_at,
			raw_answers = EXCLUDED.raw_answers,
			updated_at  = now()`,
		userID, traitsJSON, domain.TraitsVersion, assessedAt, answersJSON)
	if err != nil {
		return fmt.Errorf("upsert traits: %w", err)
	}
	return nil
}
