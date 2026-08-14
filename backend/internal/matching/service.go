package matching

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/bits-assignment/dating-platform/backend/internal/outbox"
	"github.com/bits-assignment/dating-platform/backend/internal/personality"
	"github.com/bits-assignment/dating-platform/backend/internal/preferences"
	"github.com/bits-assignment/dating-platform/backend/internal/profile"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Domain error codes.
const (
	CodeAssessmentRequired = "assessment_required"
	CodeCannotSwipeSelf    = "cannot_swipe_self"
	CodeCannotBlockSelf    = "cannot_block_self"
	CodeCannotReportSelf   = "cannot_report_self"
	CodeBlockedByYou       = "blocked_by_you"
	CodeInvalidAction      = "invalid_action"
)

// MaxBlockList caps the block list, which has no cursor because it is a settings
// screen rather than a feed.
const MaxBlockList = 200

// Pool is the database surface the service needs.
type Pool interface {
	db.Querier
	db.Beginner
}

// TraitProvider supplies a user's Big Five vector. Implemented by
// personality.Service, which reads through the trait cache.
type TraitProvider interface {
	Traits(ctx context.Context, userID uuid.UUID) (domain.Traits, error)
}

// PreferenceProvider supplies discovery filters. Implemented by
// preferences.Service.
type PreferenceProvider interface {
	Load(ctx context.Context, userID uuid.UUID) (*preferences.Preferences, error)
}

// EventKicker nudges the outbox drainer after a commit.
type EventKicker interface{ Kick() }

// Service implements discovery, likes, matches, blocks and reports
// (F10–F15).
type Service struct {
	pool         Pool
	repo         Repo
	traits       TraitProvider
	prefs        PreferenceProvider
	kicker       EventKicker
	defaultLimit int
	maxLimit     int
	now          func() time.Time
}

type ServiceConfig struct {
	Pool         Pool
	Traits       TraitProvider
	Preferences  PreferenceProvider
	Kicker       EventKicker
	DefaultLimit int
	MaxLimit     int
	Now          func() time.Time
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Pool == nil {
		return nil, errors.New("matching: pool is required")
	}
	if cfg.Traits == nil {
		return nil, errors.New("matching: trait provider is required")
	}
	if cfg.Preferences == nil {
		return nil, errors.New("matching: preference provider is required")
	}
	defaultLimit := cfg.DefaultLimit
	if defaultLimit <= 0 {
		defaultLimit = 20
	}
	maxLimit := cfg.MaxLimit
	if maxLimit < defaultLimit {
		maxLimit = defaultLimit
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		pool:         cfg.Pool,
		traits:       cfg.Traits,
		prefs:        cfg.Preferences,
		kicker:       cfg.Kicker,
		defaultLimit: defaultLimit,
		maxLimit:     maxLimit,
		now:          now,
	}, nil
}

// DefaultLimit and MaxLimit expose the page-size policy to the HTTP layer.
func (s *Service) DefaultLimit() int { return s.defaultLimit }
func (s *Service) MaxLimit() int     { return s.maxLimit }

// Discover returns the next page of the compatibility-sorted feed.
//
// One extra row is requested beyond the page size: its presence is how we know
// another page exists without a second count query, and it is dropped before the
// response is built.
func (s *Service) Discover(ctx context.Context, viewerID uuid.UUID, rawCursor string, limit int) (*Page[DiscoverItem], error) {
	cursor, err := parseDiscoverCursor(rawCursor)
	if err != nil {
		return nil, err
	}

	viewerTraits, err := s.traits.Traits(ctx, viewerID)
	if err != nil {
		if errors.Is(err, personality.ErrNotFound) {
			return nil, httpx.CodedError(http.StatusConflict, CodeAssessmentRequired,
				"complete the personality assessment to see your matches")
		}
		return nil, httpx.Internal(fmt.Errorf("load viewer traits: %w", err))
	}

	query := DiscoverQuery{
		ViewerID: viewerID,
		Traits:   viewerTraits.Ordered(),
		Weights:  domain.TraitWeights(nil).Ordered(),
		Cursor:   cursor,
		Limit:    limit + 1,
	}

	prefs, err := s.prefs.Load(ctx, viewerID)
	switch {
	case err == nil:
		query.AgeMin = prefs.AgeMin
		query.AgeMax = prefs.AgeMax
		query.Genders = prefs.Genders
		query.Weights = prefs.TraitWeights.Ordered()
	case errors.Is(err, preferences.ErrNotFound):
		// Missing preferences should not break discovery; an unfiltered feed is a
		// better failure mode than an error page.
		slog.WarnContext(ctx, "preferences row missing; discovering without filters", "user_id", viewerID)
	default:
		return nil, httpx.Internal(fmt.Errorf("load preferences: %w", err))
	}

	if prefs != nil && prefs.MaxDistanceKM != nil {
		lat, lng, coordErr := s.repo.Coords(ctx, s.pool, viewerID)
		if coordErr != nil {
			return nil, httpx.Internal(fmt.Errorf("load viewer coordinates: %w", coordErr))
		}
		if lat != nil && lng != nil {
			query.ViewerLat = lat
			query.ViewerLng = lng
			query.MaxDistanceKM = prefs.MaxDistanceKM
		}
	}

	candidates, err := s.repo.Discover(ctx, s.pool, query)
	if err != nil {
		return nil, httpx.Internal(err)
	}

	page := &Page[DiscoverItem]{Items: make([]DiscoverItem, 0, limit)}
	hasMore := len(candidates) > limit
	if hasMore {
		candidates = candidates[:limit]
	}
	for _, c := range candidates {
		item := DiscoverItem{
			PublicProfile: profile.PublicProfile{
				UserID:          c.UserID,
				Name:            c.Name,
				Age:             s.age(c.DateOfBirth),
				Bio:             c.Bio,
				Gender:          c.Gender,
				Location:        c.Location,
				Interests:       c.Interests,
				PhotoURLs:       c.PhotoURLs,
				PrimaryPhotoURL: c.PrimaryPhotoURL,
				// Candidates are un-swiped by construction, so they cannot be
				// matched yet.
				IsMatched: false,
			},
			CompatibilityScore: c.Score,
			Traits:             c.Traits,
		}
		page.Items = append(page.Items, item)
	}
	if hasMore && len(candidates) > 0 {
		last := candidates[len(candidates)-1]
		encoded, err := encodeCursor(discoverCursor{
			Version: cursorVersion,
			Score:   last.ScoreText,
			UserID:  last.UserID,
		})
		if err != nil {
			return nil, httpx.Internal(err)
		}
		page.NextCursor = &encoded
	}
	return page, nil
}

// Swipe records a like or pass and creates the match when the like is reciprocal.
//
// The whole decision runs in one transaction holding a pair-scoped advisory lock,
// so two people liking each other at the same moment reliably produce exactly one
// match and one event. Repeating a swipe is idempotent and reports the prior
// outcome rather than failing.
func (s *Service) Swipe(ctx context.Context, viewerID, targetID uuid.UUID, action string) (*SwipeResult, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action != ActionLike && action != ActionPass {
		return nil, httpx.CodedError(http.StatusBadRequest, CodeInvalidAction,
			fmt.Sprintf("action must be %q or %q", ActionLike, ActionPass))
	}
	if viewerID == targetID {
		return nil, httpx.CodedError(http.StatusBadRequest, CodeCannotSwipeSelf,
			"you cannot swipe on yourself")
	}

	result := &SwipeResult{OK: true, Action: action}
	var createdMatch *Match

	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.LockPair(ctx, tx, viewerID, targetID); err != nil {
			return err
		}
		if err := s.assertReachable(ctx, tx, viewerID, targetID); err != nil {
			return err
		}

		inserted, err := s.repo.InsertSwipe(ctx, tx, viewerID, targetID, action)
		if err != nil {
			if db.IsForeignKeyViolation(err) {
				return httpx.NotFound("user not found")
			}
			return err
		}

		if !inserted {
			existing, err := s.repo.GetSwipe(ctx, tx, viewerID, targetID)
			if err != nil {
				return err
			}
			result.Duplicate = true
			result.Action = existing.Action
			if matchID, _, err := s.repo.FindMatch(ctx, tx, viewerID, targetID); err == nil {
				result.Matched = true
				result.MatchID = &matchID
			} else if !errors.Is(err, ErrNotFound) {
				return err
			}
			return nil
		}

		if action != ActionLike {
			return nil
		}
		reciprocated, err := s.repo.HasLiked(ctx, tx, targetID, viewerID)
		if err != nil {
			return err
		}
		if !reciprocated {
			return nil
		}

		match, created, err := s.repo.InsertMatch(ctx, tx, viewerID, targetID, s.pairScore(ctx, tx, viewerID, targetID))
		if err != nil {
			return err
		}
		result.Matched = true
		result.MatchID = &match.ID

		if created {
			// Same transaction as the match row: the event cannot be lost, and it
			// cannot be published for a match that rolled back.
			if _, err := outbox.Enqueue(ctx, tx, outbox.EventMatchCreated, map[string]any{
				"match_id":   match.ID,
				"user_a_id":  match.UserAID,
				"user_b_id":  match.UserBID,
				"created_at": match.CreatedAt.UTC().Format(time.RFC3339Nano),
			}); err != nil {
				return err
			}
			createdMatch = match
		}
		return nil
	})
	if err != nil {
		return nil, wrapError(err)
	}

	if createdMatch != nil {
		slog.InfoContext(ctx, "mutual match created",
			"match_id", createdMatch.ID, "user_a_id", createdMatch.UserAID, "user_b_id", createdMatch.UserBID)
		s.kick()
	}
	return result, nil
}

// pairScore computes the symmetric compatibility stored on the match row.
//
// Deliberately unweighted: each side weights traits differently, so a weighted
// value would only be meaningful to one of them. Discovery applies personal
// weights on top of the same base similarity.
func (s *Service) pairScore(ctx context.Context, q db.Querier, a, b uuid.UUID) *float64 {
	traits, err := s.repo.TraitsFor(ctx, q, a, b)
	if err != nil {
		slog.WarnContext(ctx, "could not load traits for match score", "error", err)
		return nil
	}
	first, second := traits[a], traits[b]
	if !first.Complete() || !second.Complete() {
		return nil
	}
	score := domain.RoundScore(domain.Compatibility(first, second, nil))
	return &score
}

// Matches returns a page of the caller's mutual matches, newest first.
func (s *Service) Matches(ctx context.Context, viewerID uuid.UUID, rawCursor string, limit int) (*Page[MatchItem], error) {
	cursor, err := parseMatchCursor(rawCursor)
	if err != nil {
		return nil, err
	}

	rows, err := s.repo.ListMatches(ctx, s.pool, viewerID, cursor, limit+1)
	if err != nil {
		return nil, httpx.Internal(err)
	}

	page := &Page[MatchItem]{Items: make([]MatchItem, 0, limit)}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	for _, row := range rows {
		page.Items = append(page.Items, MatchItem{
			MatchID: row.MatchID,
			User: profile.PublicProfile{
				UserID:          row.UserID,
				Name:            row.Name,
				Age:             s.age(row.DateOfBirth),
				Bio:             row.Bio,
				Gender:          row.Gender,
				Location:        row.Location,
				Interests:       row.Interests,
				PhotoURLs:       row.PhotoURLs,
				PrimaryPhotoURL: row.PrimaryPhotoURL,
				IsMatched:       true,
			},
			CompatibilityScore: row.CompatibilityScore,
			CreatedAt:          row.CreatedAt,
		})
	}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		encoded, err := encodeCursor(matchCursor{
			Version:   cursorVersion,
			CreatedAt: last.CreatedAt,
			ID:        last.MatchID,
		})
		if err != nil {
			return nil, httpx.Internal(err)
		}
		page.NextCursor = &encoded
	}
	return page, nil
}

// Block hides a user in both directions and notifies Person C's messaging domain.
func (s *Service) Block(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	if blockerID == blockedID {
		return httpx.CodedError(http.StatusBadRequest, CodeCannotBlockSelf, "you cannot block yourself")
	}

	var created bool
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		exists, err := s.repo.UserExists(ctx, tx, blockedID)
		if err != nil {
			return err
		}
		if !exists {
			return httpx.NotFound("user not found")
		}

		created, err = s.repo.InsertBlock(ctx, tx, blockerID, blockedID)
		if err != nil {
			return err
		}
		if !created {
			return nil
		}
		_, err = outbox.Enqueue(ctx, tx, outbox.EventUserBlocked, map[string]any{
			"blocker_id": blockerID,
			"blocked_id": blockedID,
		})
		return err
	})
	if err != nil {
		return wrapError(err)
	}
	if created {
		s.kick()
	}
	return nil
}

// Unblock removes a block. Idempotent.
func (s *Service) Unblock(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	if err := s.repo.DeleteBlock(ctx, s.pool, blockerID, blockedID); err != nil {
		return httpx.Internal(err)
	}
	return nil
}

// Blocks lists the users the caller has blocked.
func (s *Service) Blocks(ctx context.Context, blockerID uuid.UUID) ([]BlockItem, error) {
	items, err := s.repo.ListBlocks(ctx, s.pool, blockerID, MaxBlockList)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	return items, nil
}

// Report files a safety report for Person C's moderation queue to pick up.
func (s *Service) Report(ctx context.Context, reporterID, reportedID uuid.UUID, reason, details string) (*ReportResult, error) {
	if reporterID == reportedID {
		return nil, httpx.CodedError(http.StatusBadRequest, CodeCannotReportSelf, "you cannot report yourself")
	}

	reason = strings.ToLower(strings.TrimSpace(reason))
	details = httpx.CleanText(details)

	v := httpx.NewValidator()
	if !isValidReportReason(reason) {
		v.Add("reason", "must be one of: "+strings.Join(ReportReasons(), ", "))
	}
	if httpx.TrimmedRuneLen(details) > MaxReportDetailRunes {
		v.Add("details", fmt.Sprintf("must not exceed %d characters", MaxReportDetailRunes))
	}
	if err := v.Err(); err != nil {
		return nil, err
	}

	exists, err := s.repo.UserExists(ctx, s.pool, reportedID)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	if !exists {
		return nil, httpx.NotFound("user not found")
	}

	id, duplicate, err := s.repo.InsertReport(ctx, s.pool, reporterID, reportedID, reason, details)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	slog.InfoContext(ctx, "user reported",
		"reporter_id", reporterID, "reported_id", reportedID, "reason", reason, "duplicate", duplicate)
	return &ReportResult{ID: id, Duplicate: duplicate}, nil
}

// IsMatched reports whether two users have a mutual match.
//
// This is the in-process helper Person C's messaging domain calls to gate
// conversations (roadmap §7), so that the `matches` table stays behind this
// package rather than being queried from C directly.
func (s *Service) IsMatched(ctx context.Context, a, b uuid.UUID) (bool, error) {
	return s.repo.IsMatched(ctx, s.pool, a, b)
}

// assertReachable rejects interactions with a missing or blocked account.
func (s *Service) assertReachable(ctx context.Context, q db.Querier, viewerID, targetID uuid.UUID) error {
	exists, err := s.repo.UserExists(ctx, q, targetID)
	if err != nil {
		return err
	}
	if !exists {
		return httpx.NotFound("user not found")
	}

	viewerBlocked, blockedByTarget, err := s.repo.BlockDirections(ctx, q, viewerID, targetID)
	if err != nil {
		return err
	}
	if viewerBlocked {
		return httpx.Conflict(CodeBlockedByYou, "unblock this user before interacting with them")
	}
	if blockedByTarget {
		// Indistinguishable from a non-existent account on purpose: confirming the
		// block would tell the caller they were blocked.
		return httpx.NotFound("user not found")
	}
	return nil
}

func (s *Service) age(dob *time.Time) *int {
	if dob == nil {
		return nil
	}
	age := auth.AgeAt(*dob, s.now())
	return &age
}

func (s *Service) kick() {
	if s.kicker != nil {
		s.kicker.Kick()
	}
}

func wrapError(err error) error {
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return httpx.Internal(err)
}
