package personality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/cache"
	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/google/uuid"
)

// CodeRetakeTooSoon is returned when the retake window has not elapsed.
const CodeRetakeTooSoon = "retake_too_soon"

// Pool is the database surface the service needs.
type Pool interface{ db.Querier }

// Service implements the assessment use cases (F07, F08).
type Service struct {
	pool     Pool
	repo     Repo
	cache    cache.Cache
	cacheTTL time.Duration
	retakeIn time.Duration
	now      func() time.Time
}

type ServiceConfig struct {
	Pool           Pool
	Cache          cache.Cache
	CacheTTL       time.Duration
	RetakeInterval time.Duration
	Now            func() time.Time
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Pool == nil {
		return nil, errors.New("personality: pool is required")
	}
	traitCache := cfg.Cache
	if traitCache == nil {
		traitCache = cache.Noop{}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		pool:     cfg.Pool,
		cache:    traitCache,
		cacheTTL: cfg.CacheTTL,
		retakeIn: cfg.RetakeInterval,
		now:      now,
	}, nil
}

// AssessmentResponse is the question list handed to the client.
type AssessmentResponse struct {
	Version      int        `json:"version"`
	ScaleMin     int        `json:"scale_min"`
	ScaleMax     int        `json:"scale_max"`
	Questions    []Question `json:"questions"`
	CanSubmit    bool       `json:"can_submit"`
	NextRetakeAt *time.Time `json:"next_retake_at"`
}

// Assessment returns the question bank plus whether the caller may submit now.
func (s *Service) Assessment(ctx context.Context, userID uuid.UUID) (*AssessmentResponse, error) {
	questions, err := s.repo.ActiveQuestions(ctx, s.pool)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	if len(questions) == 0 {
		return nil, httpx.Internal(errors.New("personality: no active questions; run migrations"))
	}

	nextRetakeAt, err := s.nextRetakeAt(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &AssessmentResponse{
		Version:      domain.TraitsVersion,
		ScaleMin:     MinAnswerValue,
		ScaleMax:     MaxAnswerValue,
		Questions:    questions,
		CanSubmit:    nextRetakeAt == nil || !nextRetakeAt.After(s.now()),
		NextRetakeAt: nextRetakeAt,
	}, nil
}

// Result is the response to a submission and to GET /personality/me.
type Result struct {
	Traits       domain.Traits `json:"traits"`
	Version      int           `json:"version"`
	AssessedAt   time.Time     `json:"assessed_at"`
	NextRetakeAt *time.Time    `json:"next_retake_at"`
	CanRetake    bool          `json:"can_retake"`
}

// Submit scores answers and stores the resulting trait vector.
//
// The trait cache is invalidated after the write so a retake is reflected in the
// very next discovery request rather than after the TTL.
func (s *Service) Submit(ctx context.Context, userID uuid.UUID, answers []Answer) (*Result, error) {
	if len(answers) == 0 {
		v := httpx.NewValidator()
		v.Add("answers", "at least one answer is required")
		return nil, v.Err()
	}

	nextRetakeAt, err := s.nextRetakeAt(ctx, userID)
	if err != nil {
		return nil, err
	}
	now := s.now()
	if nextRetakeAt != nil && nextRetakeAt.After(now) {
		return nil, httpx.CodedError(http.StatusConflict, CodeRetakeTooSoon,
			"you cannot retake the assessment yet").WithDetails(map[string]any{
			"next_retake_at": nextRetakeAt.UTC().Format(time.RFC3339),
		})
	}

	questions, err := s.repo.ActiveQuestions(ctx, s.pool)
	if err != nil {
		return nil, httpx.Internal(err)
	}

	traits, err := Score(questions, answers)
	if err != nil {
		var scoreError *ScoreError
		if errors.As(err, &scoreError) {
			v := httpx.NewValidator()
			v.Add(scoreError.Field, scoreError.Message)
			return nil, v.Err()
		}
		return nil, httpx.Internal(err)
	}

	if err := s.repo.Upsert(ctx, s.pool, userID, traits, answers, now); err != nil {
		return nil, httpx.Internal(err)
	}
	s.invalidate(ctx, userID)
	slog.InfoContext(ctx, "assessment submitted", "user_id", userID, "version", domain.TraitsVersion)

	next := s.retakeTime(now)
	return &Result{
		Traits:       traits,
		Version:      domain.TraitsVersion,
		AssessedAt:   now.UTC(),
		NextRetakeAt: next,
		CanRetake:    next == nil,
	}, nil
}

// Me returns the caller's current trait vector.
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (*Result, error) {
	assessment, err := s.repo.Get(ctx, s.pool, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, httpx.NotFound("you have not completed the personality assessment yet")
		}
		return nil, httpx.Internal(err)
	}
	next := s.retakeTime(assessment.AssessedAt)
	return &Result{
		Traits:       assessment.Traits,
		Version:      assessment.Version,
		AssessedAt:   assessment.AssessedAt.UTC(),
		NextRetakeAt: next,
		CanRetake:    next == nil || !next.After(s.now()),
	}, nil
}

// Traits returns a user's trait vector, reading through the cache.
//
// Used by discovery on every request, which is why it is cached; a miss or a
// cache error always falls back to Postgres, so the cache is never load-bearing.
func (s *Service) Traits(ctx context.Context, userID uuid.UUID) (domain.Traits, error) {
	key := traitCacheKey(userID)
	if raw, hit, err := s.cache.Get(ctx, key); err != nil {
		slog.WarnContext(ctx, "trait cache read failed", "user_id", userID, "error", err)
	} else if hit {
		var traits domain.Traits
		if err := json.Unmarshal(raw, &traits); err == nil && traits.Complete() {
			return traits, nil
		}
		// A corrupt entry is dropped rather than trusted.
		_ = s.cache.Delete(ctx, key)
	}

	assessment, err := s.repo.Get(ctx, s.pool, userID)
	if err != nil {
		return nil, err
	}
	if encoded, err := json.Marshal(assessment.Traits); err == nil {
		if err := s.cache.Set(ctx, key, encoded, s.cacheTTL); err != nil {
			slog.WarnContext(ctx, "trait cache write failed", "user_id", userID, "error", err)
		}
	}
	return assessment.Traits, nil
}

// InvalidateTraits drops a user's cached vector.
func (s *Service) InvalidateTraits(ctx context.Context, userID uuid.UUID) {
	s.invalidate(ctx, userID)
}

func (s *Service) invalidate(ctx context.Context, userID uuid.UUID) {
	if err := s.cache.Delete(ctx, traitCacheKey(userID)); err != nil {
		slog.WarnContext(ctx, "trait cache invalidation failed", "user_id", userID, "error", err)
	}
}

func (s *Service) nextRetakeAt(ctx context.Context, userID uuid.UUID) (*time.Time, error) {
	lastAssessedAt, err := s.repo.LastAssessedAt(ctx, s.pool, userID)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	if lastAssessedAt == nil {
		return nil, nil
	}
	return s.retakeTime(*lastAssessedAt), nil
}

// retakeTime returns when a retake becomes available, or nil when retakes are
// unrestricted (interval configured to zero).
func (s *Service) retakeTime(assessedAt time.Time) *time.Time {
	if s.retakeIn <= 0 {
		return nil
	}
	next := assessedAt.Add(s.retakeIn).UTC()
	return &next
}

func traitCacheKey(userID uuid.UUID) string {
	return fmt.Sprintf("traits:%s", userID)
}
