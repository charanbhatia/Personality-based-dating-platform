package matching

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/google/uuid"
)

// ErrNotFound is returned when a lookup finds no row.
var ErrNotFound = errors.New("not found")

// discoverSQL is the scored, filtered, keyset-paginated candidate query.
//
// Scoring happens in SQL rather than in Go on purpose. The feed is ordered by
// score, so pagination is only correct if the whole candidate set is ordered
// consistently; scoring a "top N" window in the application would let page 2
// contain higher scores than page 1. Computing the score in the database means
// ORDER BY ... LIMIT sees every candidate and the keyset cursor is stable.
//
// The expression must stay equivalent to domain.Compatibility — the weighted mean
// of (1 - |a - b|) across the Big Five. TestCompatibilityMatchesSQL asserts that.
const discoverSQL = `
WITH scored AS (
    SELECT
        u.id            AS user_id,
        u.name          AS name,
        u.date_of_birth AS date_of_birth,
        p.bio           AS bio,
        p.gender        AS gender,
        p.location      AS location,
        p.interests     AS interests,
        p.photo_urls    AS photo_urls,
        coalesce(p.primary_photo_url, '') AS primary_photo_url,
        ps.traits       AS traits,
        round(
            ((  $7::float8  * (1 - abs($2::float8 - (ps.traits->>'openness')::float8))
              + $8::float8  * (1 - abs($3::float8 - (ps.traits->>'conscientiousness')::float8))
              + $9::float8  * (1 - abs($4::float8 - (ps.traits->>'extraversion')::float8))
              + $10::float8 * (1 - abs($5::float8 - (ps.traits->>'agreeableness')::float8))
              + $11::float8 * (1 - abs($6::float8 - (ps.traits->>'neuroticism')::float8))
             ) / ($7::float8 + $8::float8 + $9::float8 + $10::float8 + $11::float8)
            )::numeric, 6) AS score
    FROM users u
    JOIN profiles p            ON p.user_id  = u.id
    JOIN personality_scores ps ON ps.user_id = u.id
    WHERE u.id <> $1
      -- Require a complete, numeric trait vector: an unscored candidate would
      -- otherwise rank on a fabricated value, and a non-numeric JSON value would
      -- abort the query on the float cast.
      AND jsonb_typeof(ps.traits->'openness')          = 'number'
      AND jsonb_typeof(ps.traits->'conscientiousness') = 'number'
      AND jsonb_typeof(ps.traits->'extraversion')      = 'number'
      AND jsonb_typeof(ps.traits->'agreeableness')     = 'number'
      AND jsonb_typeof(ps.traits->'neuroticism')       = 'number'
      -- Age cannot be evaluated without a date of birth, so such accounts are
      -- not discoverable. /auth/me reports this via onboarding.profile_done.
      AND u.date_of_birth IS NOT NULL
      AND NOT EXISTS (
          SELECT 1 FROM swipes s WHERE s.from_user_id = $1 AND s.to_user_id = u.id
      )
      AND NOT EXISTS (
          SELECT 1 FROM blocks b
          WHERE (b.blocker_id = $1 AND b.blocked_id = u.id)
             OR (b.blocker_id = u.id AND b.blocked_id = $1)
      )
      AND (coalesce(array_length($14::text[], 1), 0) = 0 OR p.gender = ANY ($14::text[]))
      AND ($12::int IS NULL OR u.date_of_birth <= (current_date - make_interval(years => $12::int)))
      AND ($13::int IS NULL OR u.date_of_birth >  (current_date - make_interval(years => $13::int + 1)))
      AND (
            $18::float8 IS NULL
         OR (
              p.lat IS NOT NULL AND p.lng IS NOT NULL
              AND (6371 * acos(LEAST(1.0, GREATEST(-1.0,
                    cos(radians($18::float8)) * cos(radians(p.lat))
                    * cos(radians(p.lng) - radians($19::float8))
                    + sin(radians($18::float8)) * sin(radians(p.lat))
              )))) <= $20::float8
            )
      )
)
SELECT s.user_id, s.name, s.date_of_birth, s.bio, s.gender, s.location, s.interests,
       s.photo_urls, s.primary_photo_url, s.traits,
       s.score::float8 AS score_num,
       s.score::text   AS score_text
FROM scored s
WHERE $15::text IS NULL
   OR s.score < $15::text::numeric
   OR (s.score = $15::text::numeric AND s.user_id > $16::uuid)
ORDER BY s.score DESC, s.user_id ASC
LIMIT $17`

// Candidate is one scored discovery row.
type Candidate struct {
	UserID          uuid.UUID
	Name            string
	DateOfBirth     *time.Time
	Bio             string
	Gender          string
	Location        string
	Interests       []string
	PhotoURLs       []string
	PrimaryPhotoURL string
	Traits          domain.Traits
	Score           float64
	// ScoreText is the exact decimal Postgres produced, carried into the next
	// cursor so the keyset comparison is exact.
	ScoreText string
}

// DiscoverQuery is the resolved filter for one page of the feed.
type DiscoverQuery struct {
	ViewerID      uuid.UUID
	Traits        []float64
	Weights       []float64
	AgeMin        *int
	AgeMax        *int
	Genders       []string
	Cursor        *discoverCursor
	Limit         int
	ViewerLat     *float64
	ViewerLng     *float64
	MaxDistanceKM *int
}

// Repo holds the matching-domain queries.
type Repo struct{}

// Discover returns the next page of scored candidates.
func (Repo) Discover(ctx context.Context, q db.Querier, query DiscoverQuery) ([]Candidate, error) {
	if len(query.Traits) != 5 || len(query.Weights) != 5 {
		return nil, fmt.Errorf("matching: expected 5 traits and 5 weights, got %d and %d",
			len(query.Traits), len(query.Weights))
	}
	var cursorScore any
	var cursorUserID any
	if query.Cursor != nil {
		cursorScore = query.Cursor.Score
		cursorUserID = query.Cursor.UserID
	}

	genders := query.Genders
	if genders == nil {
		genders = []string{}
	}

	rows, err := q.Query(ctx, discoverSQL,
		query.ViewerID,
		query.Traits[0], query.Traits[1], query.Traits[2], query.Traits[3], query.Traits[4],
		query.Weights[0], query.Weights[1], query.Weights[2], query.Weights[3], query.Weights[4],
		query.AgeMin, query.AgeMax, genders,
		cursorScore, cursorUserID,
		query.Limit,
		query.ViewerLat, query.ViewerLng, query.MaxDistanceKM,
	)
	if err != nil {
		return nil, fmt.Errorf("discover candidates: %w", err)
	}
	defer rows.Close()

	var out []Candidate
	for rows.Next() {
		var c Candidate
		var photoURLs []byte
		var traitsRaw []byte
		if err := rows.Scan(&c.UserID, &c.Name, &c.DateOfBirth, &c.Bio, &c.Gender, &c.Location,
			&c.Interests, &photoURLs, &c.PrimaryPhotoURL, &traitsRaw, &c.Score, &c.ScoreText); err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		urls, err := decodeStringArray(photoURLs)
		if err != nil {
			return nil, err
		}
		c.PhotoURLs = urls
		if c.Interests == nil {
			c.Interests = []string{}
		}
		if len(traitsRaw) > 0 {
			var traits domain.Traits
			if err := json.Unmarshal(traitsRaw, &traits); err == nil && traits.Complete() {
				c.Traits = traits
			}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Coords returns a user's stored map position, used to apply max_distance_km.
func (Repo) Coords(ctx context.Context, q db.Querier, userID uuid.UUID) (lat, lng *float64, err error) {
	err = q.QueryRow(ctx, `SELECT lat, lng FROM profiles WHERE user_id = $1`, userID).Scan(&lat, &lng)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	return lat, lng, nil
}

// UserExists reports whether a user id refers to a real account.
func (Repo) UserExists(ctx context.Context, q db.Querier, userID uuid.UUID) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists)
	return exists, err
}

// BlockExistsEitherWay reports whether either user has blocked the other.
func (Repo) BlockExistsEitherWay(ctx context.Context, q db.Querier, a, b uuid.UUID) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM blocks
			WHERE (blocker_id = $1 AND blocked_id = $2)
			   OR (blocker_id = $2 AND blocked_id = $1)
		)`, a, b).Scan(&exists)
	return exists, err
}

// LockPair takes a transaction-scoped advisory lock on an unordered pair.
//
// This closes a write-skew hole in mutual-match detection. Under READ COMMITTED,
// A liking B and B liking A concurrently can each fail to see the other's
// uncommitted like, leaving a reciprocal pair with no match row. Serializing on
// the pair means the second transaction observes the first one's like.
//
// The lock is released when the transaction ends, so no explicit unlock is
// needed. Hash collisions merely serialize unrelated pairs.
func (Repo) LockPair(ctx context.Context, q db.Querier, a, b uuid.UUID) error {
	first, second := orderPair(a, b)
	hash := fnv.New64a()
	_, _ = hash.Write(first[:])
	_, _ = hash.Write(second[:])
	sum := hash.Sum64()
	// pg_advisory_xact_lock(int4, int4) keyed on the pair.
	key1 := int32(sum >> 32)
	key2 := int32(sum & 0xffffffff)
	_, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, key1, key2)
	if err != nil {
		return fmt.Errorf("lock user pair: %w", err)
	}
	return nil
}

// Swipe is a recorded like or pass.
type Swipe struct {
	Action    string
	CreatedAt time.Time
}

// InsertSwipe records a decision, reporting whether it was newly inserted.
// A conflict means the user already swiped this candidate, which the service
// treats as an idempotent replay rather than an error.
func (Repo) InsertSwipe(ctx context.Context, q db.Querier, from, to uuid.UUID, action string) (bool, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `
		INSERT INTO swipes (from_user_id, to_user_id, action)
		VALUES ($1, $2, $3)
		ON CONFLICT (from_user_id, to_user_id) DO NOTHING
		RETURNING id`, from, to, action).Scan(&id)
	if err != nil {
		if db.IsNoRows(err) {
			return false, nil
		}
		return false, fmt.Errorf("insert swipe: %w", err)
	}
	return true, nil
}

// GetSwipe loads an existing decision.
func (Repo) GetSwipe(ctx context.Context, q db.Querier, from, to uuid.UUID) (*Swipe, error) {
	s := &Swipe{}
	err := q.QueryRow(ctx,
		`SELECT action, created_at FROM swipes WHERE from_user_id = $1 AND to_user_id = $2`,
		from, to).Scan(&s.Action, &s.CreatedAt)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load swipe: %w", err)
	}
	return s, nil
}

// HasLiked reports whether `from` has liked `to`.
func (Repo) HasLiked(ctx context.Context, q db.Querier, from, to uuid.UUID) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM swipes
			WHERE from_user_id = $1 AND to_user_id = $2 AND action = 'like'
		)`, from, to).Scan(&exists)
	return exists, err
}

// Match identifies a mutual match.
type Match struct {
	ID        uuid.UUID
	UserAID   uuid.UUID
	UserBID   uuid.UUID
	CreatedAt time.Time
}

// InsertMatch creates the mutual match, normalising the pair order to satisfy the
// user_a_id < user_b_id constraint. The bool reports whether this call created the
// row, so exactly one match.created event is emitted even under a concurrent
// double insert.
func (Repo) InsertMatch(ctx context.Context, q db.Querier, a, b uuid.UUID, score *float64) (*Match, bool, error) {
	first, second := orderPair(a, b)
	m := &Match{UserAID: first, UserBID: second}

	err := q.QueryRow(ctx, `
		INSERT INTO matches (user_a_id, user_b_id, compatibility_score)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_a_id, user_b_id) DO NOTHING
		RETURNING id, created_at`, first, second, score).Scan(&m.ID, &m.CreatedAt)
	if err == nil {
		return m, true, nil
	}
	if !db.IsNoRows(err) {
		return nil, false, fmt.Errorf("insert match: %w", err)
	}

	// Another transaction won the race; return its row so the response is still
	// correct for this caller, without re-emitting the event.
	err = q.QueryRow(ctx,
		`SELECT id, created_at FROM matches WHERE user_a_id = $1 AND user_b_id = $2`,
		first, second).Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return nil, false, fmt.Errorf("load existing match: %w", err)
	}
	return m, false, nil
}

// BlockDirections reports which side of a pair has blocked the other.
//
// The two directions are distinguished because the API answers differently: "you
// blocked this person" is safe to state, whereas "this person blocked you" must
// look identical to the account not existing.
func (Repo) BlockDirections(ctx context.Context, q db.Querier, viewerID, targetID uuid.UUID) (viewerBlocked, blockedByTarget bool, err error) {
	err = q.QueryRow(ctx, `
		SELECT
			EXISTS (SELECT 1 FROM blocks WHERE blocker_id = $1 AND blocked_id = $2),
			EXISTS (SELECT 1 FROM blocks WHERE blocker_id = $2 AND blocked_id = $1)`,
		viewerID, targetID).Scan(&viewerBlocked, &blockedByTarget)
	if err != nil {
		return false, false, fmt.Errorf("load block state: %w", err)
	}
	return viewerBlocked, blockedByTarget, nil
}

// FindMatch returns the match id for a pair, in either order.
func (Repo) FindMatch(ctx context.Context, q db.Querier, a, b uuid.UUID) (uuid.UUID, time.Time, error) {
	first, second := orderPair(a, b)
	var id uuid.UUID
	var createdAt time.Time
	err := q.QueryRow(ctx,
		`SELECT id, created_at FROM matches WHERE user_a_id = $1 AND user_b_id = $2`,
		first, second).Scan(&id, &createdAt)
	if err != nil {
		if db.IsNoRows(err) {
			return uuid.Nil, time.Time{}, ErrNotFound
		}
		return uuid.Nil, time.Time{}, fmt.Errorf("find match: %w", err)
	}
	return id, createdAt, nil
}

// TraitsFor loads complete trait vectors for the given users. Users without a
// usable vector are simply absent from the result.
func (Repo) TraitsFor(ctx context.Context, q db.Querier, userIDs ...uuid.UUID) (map[uuid.UUID]domain.Traits, error) {
	rows, err := q.Query(ctx,
		`SELECT user_id, traits FROM personality_scores WHERE user_id = ANY($1)`, userIDs)
	if err != nil {
		return nil, fmt.Errorf("load traits: %w", err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]domain.Traits, len(userIDs))
	for rows.Next() {
		var userID uuid.UUID
		var raw []byte
		if err := rows.Scan(&userID, &raw); err != nil {
			return nil, fmt.Errorf("scan traits: %w", err)
		}
		var traits domain.Traits
		if err := json.Unmarshal(raw, &traits); err != nil {
			continue
		}
		if traits.Complete() {
			out[userID] = traits
		}
	}
	return out, rows.Err()
}

// matchListSQL pages a user's matches newest first.
//
// The OR over the two participant columns lets Postgres combine the per-side
// indexes, and the row-wise comparison implements the keyset walk. Blocked
// counterparties are filtered here as well as in discovery, so blocking hides an
// existing match immediately.
const matchListSQL = `
SELECT m.id, m.created_at, m.compatibility_score,
       o.id, o.name, o.date_of_birth,
       coalesce(p.bio, ''), coalesce(p.gender, ''), coalesce(p.location, ''),
       coalesce(p.interests, '{}'::text[]), coalesce(p.photo_urls, '[]'::jsonb),
       coalesce(p.primary_photo_url, '')
FROM matches m
JOIN users o
  ON o.id = CASE WHEN m.user_a_id = $1 THEN m.user_b_id ELSE m.user_a_id END
LEFT JOIN profiles p ON p.user_id = o.id
WHERE (m.user_a_id = $1 OR m.user_b_id = $1)
  AND NOT EXISTS (
      SELECT 1 FROM blocks b
      WHERE (b.blocker_id = $1 AND b.blocked_id = o.id)
         OR (b.blocker_id = o.id AND b.blocked_id = $1)
  )
  AND ($2::timestamptz IS NULL OR (m.created_at, m.id) < ($2::timestamptz, $3::uuid))
ORDER BY m.created_at DESC, m.id DESC
LIMIT $4`

// MatchRow is one joined match record.
type MatchRow struct {
	MatchID            uuid.UUID
	CreatedAt          time.Time
	CompatibilityScore *float64
	UserID             uuid.UUID
	Name               string
	DateOfBirth        *time.Time
	Bio                string
	Gender             string
	Location           string
	Interests          []string
	PhotoURLs          []string
	PrimaryPhotoURL    string
}

// ListMatches returns a page of matches for a user.
func (Repo) ListMatches(ctx context.Context, q db.Querier, userID uuid.UUID, cursor *matchCursor, limit int) ([]MatchRow, error) {
	var cursorTime any
	var cursorID any
	if cursor != nil {
		cursorTime = cursor.CreatedAt
		cursorID = cursor.ID
	}

	rows, err := q.Query(ctx, matchListSQL, userID, cursorTime, cursorID, limit)
	if err != nil {
		return nil, fmt.Errorf("list matches: %w", err)
	}
	defer rows.Close()

	var out []MatchRow
	for rows.Next() {
		var m MatchRow
		var photoURLs []byte
		if err := rows.Scan(&m.MatchID, &m.CreatedAt, &m.CompatibilityScore,
			&m.UserID, &m.Name, &m.DateOfBirth, &m.Bio, &m.Gender, &m.Location,
			&m.Interests, &photoURLs, &m.PrimaryPhotoURL); err != nil {
			return nil, fmt.Errorf("scan match: %w", err)
		}
		urls, err := decodeStringArray(photoURLs)
		if err != nil {
			return nil, err
		}
		m.PhotoURLs = urls
		if m.Interests == nil {
			m.Interests = []string{}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// IsMatched reports whether two users have a mutual match.
func (Repo) IsMatched(ctx context.Context, q db.Querier, a, b uuid.UUID) (bool, error) {
	if a == b {
		return false, nil
	}
	first, second := orderPair(a, b)
	var exists bool
	err := q.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM matches WHERE user_a_id = $1 AND user_b_id = $2)`,
		first, second).Scan(&exists)
	return exists, err
}

// InsertBlock records a block, reporting whether it was newly created.
func (Repo) InsertBlock(ctx context.Context, q db.Querier, blockerID, blockedID uuid.UUID) (bool, error) {
	tag, err := q.Exec(ctx, `
		INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)
		ON CONFLICT (blocker_id, blocked_id) DO NOTHING`, blockerID, blockedID)
	if err != nil {
		return false, fmt.Errorf("insert block: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// DeleteBlock removes a block. Absence is not an error: unblocking is idempotent.
func (Repo) DeleteBlock(ctx context.Context, q db.Querier, blockerID, blockedID uuid.UUID) error {
	_, err := q.Exec(ctx,
		`DELETE FROM blocks WHERE blocker_id = $1 AND blocked_id = $2`, blockerID, blockedID)
	if err != nil {
		return fmt.Errorf("delete block: %w", err)
	}
	return nil
}

// ListBlocks returns the users the caller has blocked, newest first.
func (Repo) ListBlocks(ctx context.Context, q db.Querier, blockerID uuid.UUID, limit int) ([]BlockItem, error) {
	rows, err := q.Query(ctx, `
		SELECT b.blocked_id, u.name, b.created_at
		FROM blocks b
		JOIN users u ON u.id = b.blocked_id
		WHERE b.blocker_id = $1
		ORDER BY b.created_at DESC
		LIMIT $2`, blockerID, limit)
	if err != nil {
		return nil, fmt.Errorf("list blocks: %w", err)
	}
	defer rows.Close()

	out := make([]BlockItem, 0)
	for rows.Next() {
		var item BlockItem
		if err := rows.Scan(&item.UserID, &item.Name, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan block: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// InsertReport files a report. A partial unique index collapses repeat
// submissions while a case is open, in which case the existing id is returned.
func (Repo) InsertReport(ctx context.Context, q db.Querier, reporterID, reportedID uuid.UUID, reason, details string) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `
		INSERT INTO reports (reporter_id, reported_id, reason, details)
		VALUES ($1, $2, $3, $4)
		RETURNING id`, reporterID, reportedID, reason, details).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if !db.IsUniqueViolation(err) {
		return uuid.Nil, false, fmt.Errorf("insert report: %w", err)
	}

	err = q.QueryRow(ctx, `
		SELECT id FROM reports
		WHERE reporter_id = $1 AND reported_id = $2 AND status IN ('open', 'reviewing')
		ORDER BY created_at DESC LIMIT 1`, reporterID, reportedID).Scan(&id)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("load existing report: %w", err)
	}
	return id, true, nil
}

// orderPair returns the pair in ascending order, matching the canonical ordering
// enforced by the matches table.
func orderPair(a, b uuid.UUID) (uuid.UUID, uuid.UUID) {
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return a, b
			}
			return b, a
		}
	}
	return a, b
}

func decodeStringArray(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	var values []any
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decode json array: %w", err)
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}
