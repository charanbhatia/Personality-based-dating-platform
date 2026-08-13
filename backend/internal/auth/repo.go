package auth

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/google/uuid"
)

// ErrNotFound is returned by repository lookups with no matching row.
var ErrNotFound = errors.New("not found")

// User is the identity record. PasswordHash never leaves this package.
type User struct {
	ID              uuid.UUID
	Email           string
	PasswordHash    string
	Name            string
	DateOfBirth     *time.Time
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Session is one refresh-token family.
type Session struct {
	ID                uuid.UUID
	UserID            uuid.UUID
	RefreshTokenHash  string
	PreviousTokenHash *string
	RotationCount     int
	UserAgent         string
	IP                *netip.Addr
	ExpiresAt         time.Time
	RevokedAt         *time.Time
	RevokedReason     *string
	LastUsedAt        time.Time
	CreatedAt         time.Time
}

// Active reports whether the session can still be used at t.
func (s Session) Active(t time.Time) bool {
	return s.RevokedAt == nil && s.ExpiresAt.After(t)
}

// Onboarding are the derived completion flags returned by /auth/me.
type Onboarding struct {
	QuizDone        bool `json:"quiz_done"`
	PreferencesDone bool `json:"preferences_done"`
	ProfileDone     bool `json:"profile_done"`
	PhotosDone      bool `json:"photos_done"`
}

// Complete reports whether the user can use the discovery feed.
func (o Onboarding) Complete() bool {
	return o.QuizDone && o.PreferencesDone && o.ProfileDone
}

const userColumns = `id, email, password_hash, name, date_of_birth, email_verified_at, created_at, updated_at`

// Repo reads and writes the auth tables. Every method takes a Querier so the
// caller decides whether it runs inside a transaction.
type Repo struct{}

func (Repo) scanUser(row interface {
	Scan(dest ...any) error
}) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.DateOfBirth,
		&u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

// CreateUser inserts an identity. The caller must pass an already-normalized
// email and a bcrypt hash.
func (r Repo) CreateUser(ctx context.Context, q db.Querier, email, passwordHash, name string, dob *time.Time) (*User, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, name, date_of_birth)
		VALUES ($1, $2, $3, $4)
		RETURNING `+userColumns, email, passwordHash, name, dob)
	return r.scanUser(row)
}

// GetUserByEmail looks up an identity case-insensitively, matching the
// lower(email) unique index.
func (r Repo) GetUserByEmail(ctx context.Context, q db.Querier, email string) (*User, error) {
	row := q.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE lower(email) = lower($1)`, email)
	return r.scanUser(row)
}

func (r Repo) GetUserByID(ctx context.Context, q db.Querier, id uuid.UUID) (*User, error) {
	row := q.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	return r.scanUser(row)
}

// UpdatePasswordHash sets a new password hash.
func (Repo) UpdatePasswordHash(ctx context.Context, q db.Querier, userID uuid.UUID, hash string) error {
	tag, err := q.Exec(ctx,
		`UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, userID, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateDateOfBirth sets the date of birth, used by the profile update path.
func (Repo) UpdateDateOfBirth(ctx context.Context, q db.Querier, userID uuid.UUID, dob *time.Time) error {
	tag, err := q.Exec(ctx,
		`UPDATE users SET date_of_birth = $2, updated_at = now() WHERE id = $1`, userID, dob)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetOnboarding derives the completion flags in a single round trip.
//
// profile_done additionally requires date_of_birth: without it a user cannot be
// age-filtered and would be invisible in every feed, so treating the profile as
// complete would strand the account.
func (Repo) GetOnboarding(ctx context.Context, q db.Querier, userID uuid.UUID, traitKeys []string) (Onboarding, error) {
	var o Onboarding
	err := q.QueryRow(ctx, `
		SELECT
			(ps.user_id IS NOT NULL AND ps.traits ?& $2::text[])                     AS quiz_done,
			(pr.age_min IS NOT NULL AND pr.age_max IS NOT NULL
			   AND coalesce(array_length(pr.genders, 1), 0) > 0)                     AS preferences_done,
			(coalesce(p.bio, '') <> '' AND coalesce(p.gender, '') <> ''
			   AND u.date_of_birth IS NOT NULL)                                      AS profile_done,
			(coalesce(jsonb_array_length(p.photo_urls), 0) > 0)                      AS photos_done
		FROM users u
		LEFT JOIN profiles p            ON p.user_id  = u.id
		LEFT JOIN preferences pr        ON pr.user_id = u.id
		LEFT JOIN personality_scores ps ON ps.user_id = u.id
		WHERE u.id = $1`, userID, traitKeys,
	).Scan(&o.QuizDone, &o.PreferencesDone, &o.ProfileDone, &o.PhotosDone)
	if err != nil {
		if db.IsNoRows(err) {
			return Onboarding{}, ErrNotFound
		}
		return Onboarding{}, err
	}
	return o, nil
}

const sessionColumns = `id, user_id, refresh_token_hash, previous_token_hash, rotation_count,
	user_agent, ip, expires_at, revoked_at, revoked_reason, last_used_at, created_at`

func (Repo) scanSession(row interface {
	Scan(dest ...any) error
}) (*Session, error) {
	s := &Session{}
	err := row.Scan(&s.ID, &s.UserID, &s.RefreshTokenHash, &s.PreviousTokenHash, &s.RotationCount,
		&s.UserAgent, &s.IP, &s.ExpiresAt, &s.RevokedAt, &s.RevokedReason, &s.LastUsedAt, &s.CreatedAt)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s, nil
}

// SessionMeta is the request context recorded against a session so a user can
// recognise their own devices in the session list.
type SessionMeta struct {
	UserAgent string
	IP        *netip.Addr
}

// CreateSession starts a refresh-token family.
func (r Repo) CreateSession(ctx context.Context, q db.Querier, userID uuid.UUID, tokenHash string, expiresAt time.Time, meta SessionMeta) (*Session, error) {
	row := q.QueryRow(ctx, `
		INSERT INTO auth_sessions (user_id, refresh_token_hash, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+sessionColumns,
		userID, tokenHash, meta.UserAgent, meta.IP, expiresAt)
	return r.scanSession(row)
}

// FindSessionByToken matches either the current or the immediately previous
// refresh-token hash, and reports which one matched.
//
// The previous-hash lookback is what makes replay detectable: a token that was
// already rotated away can only be presented by someone who captured it, so
// Refresh treats that match as a compromise rather than a retry.
func (r Repo) FindSessionByToken(ctx context.Context, q db.Querier, tokenHash string) (session *Session, isCurrent bool, err error) {
	row := q.QueryRow(ctx, `
		SELECT `+sessionColumns+`, (refresh_token_hash = $1) AS is_current
		FROM auth_sessions
		WHERE refresh_token_hash = $1 OR previous_token_hash = $1
		-- A rotated hash briefly exists as some other row's previous value;
		-- prefer the row where it is current.
		ORDER BY (refresh_token_hash = $1) DESC
		LIMIT 1
		FOR UPDATE`, tokenHash)

	s := &Session{}
	err = row.Scan(&s.ID, &s.UserID, &s.RefreshTokenHash, &s.PreviousTokenHash, &s.RotationCount,
		&s.UserAgent, &s.IP, &s.ExpiresAt, &s.RevokedAt, &s.RevokedReason, &s.LastUsedAt, &s.CreatedAt,
		&isCurrent)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, false, ErrNotFound
		}
		return nil, false, err
	}
	return s, isCurrent, nil
}

// RotateSession swaps in a new refresh-token hash, retaining the retired one for
// replay detection. The WHERE clause re-asserts the expected current hash so two
// concurrent refreshes cannot both succeed.
func (Repo) RotateSession(ctx context.Context, q db.Querier, sessionID uuid.UUID, expectedHash, newHash string, expiresAt time.Time, meta SessionMeta) error {
	tag, err := q.Exec(ctx, `
		UPDATE auth_sessions
		SET previous_token_hash = refresh_token_hash,
		    refresh_token_hash  = $3,
		    rotation_count      = rotation_count + 1,
		    expires_at          = $4,
		    last_used_at        = now(),
		    user_agent          = CASE WHEN $5 <> '' THEN $5 ELSE user_agent END,
		    ip                  = coalesce($6, ip)
		WHERE id = $1 AND refresh_token_hash = $2 AND revoked_at IS NULL`,
		sessionID, expectedHash, newHash, expiresAt, meta.UserAgent, meta.IP)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeSession revokes one session. It is idempotent: revoking an
// already-revoked session reports no rows and the caller treats that as success.
func (Repo) RevokeSession(ctx context.Context, q db.Querier, sessionID uuid.UUID, reason string) (bool, error) {
	tag, err := q.Exec(ctx, `
		UPDATE auth_sessions
		SET revoked_at = now(), revoked_reason = $2
		WHERE id = $1 AND revoked_at IS NULL`, sessionID, reason)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// RevokeUserSessions revokes every live session for a user, optionally keeping
// one (used when a password change should not log out the device performing it).
func (Repo) RevokeUserSessions(ctx context.Context, q db.Querier, userID uuid.UUID, reason string, except uuid.UUID) (int64, error) {
	tag, err := q.Exec(ctx, `
		UPDATE auth_sessions
		SET revoked_at = now(), revoked_reason = $2
		WHERE user_id = $1 AND revoked_at IS NULL AND ($3::uuid IS NULL OR id <> $3)`,
		userID, reason, nullableUUID(except))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ListSessions returns live sessions, most recently used first.
func (Repo) ListSessions(ctx context.Context, q db.Querier, userID uuid.UUID) ([]Session, error) {
	rows, err := q.Query(ctx, `
		SELECT `+sessionColumns+`
		FROM auth_sessions
		WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
		ORDER BY last_used_at DESC, created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.RefreshTokenHash, &s.PreviousTokenHash,
			&s.RotationCount, &s.UserAgent, &s.IP, &s.ExpiresAt, &s.RevokedAt, &s.RevokedReason,
			&s.LastUsedAt, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSession fetches a session scoped to its owner, so one user can never revoke
// another user's session by guessing an id.
func (r Repo) GetSession(ctx context.Context, q db.Querier, userID, sessionID uuid.UUID) (*Session, error) {
	row := q.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM auth_sessions WHERE id = $1 AND user_id = $2`, sessionID, userID)
	return r.scanSession(row)
}

// DeleteExpiredSessions prunes rows that can no longer be used. Retains revoked
// rows briefly so the session list and any audit trail stay meaningful.
func (Repo) DeleteExpiredSessions(ctx context.Context, q db.Querier, retainRevokedFor time.Duration) (int64, error) {
	tag, err := q.Exec(ctx, `
		DELETE FROM auth_sessions
		WHERE expires_at < now()
		   OR (revoked_at IS NOT NULL AND revoked_at < now() - make_interval(secs => $1))`,
		retainRevokedFor.Seconds())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PasswordResetToken is a single-use reset grant.
type PasswordResetToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// CreatePasswordResetToken stores a hashed reset token.
func (Repo) CreatePasswordResetToken(ctx context.Context, q db.Querier, userID uuid.UUID, tokenHash string, expiresAt time.Time) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `
		INSERT INTO password_reset_tokens (user_id, token_hash, expires_at)
		VALUES ($1, $2, $3) RETURNING id`, userID, tokenHash, expiresAt).Scan(&id)
	return id, err
}

// InvalidatePasswordResetTokens burns any outstanding tokens for a user, so
// requesting a new link silently retires the previous one.
func (Repo) InvalidatePasswordResetTokens(ctx context.Context, q db.Querier, userID uuid.UUID) error {
	_, err := q.Exec(ctx,
		`UPDATE password_reset_tokens SET used_at = now() WHERE user_id = $1 AND used_at IS NULL`, userID)
	return err
}

// ClaimPasswordResetToken atomically consumes an unused, unexpired token.
//
// The single UPDATE ... RETURNING is what makes the token single-use under
// concurrency: two simultaneous submissions of the same link contend on the row
// and exactly one sees a result.
func (Repo) ClaimPasswordResetToken(ctx context.Context, q db.Querier, tokenHash string) (*PasswordResetToken, error) {
	t := &PasswordResetToken{}
	err := q.QueryRow(ctx, `
		UPDATE password_reset_tokens
		SET used_at = now()
		WHERE id = (
			SELECT id FROM password_reset_tokens
			WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now()
			FOR UPDATE
		)
		RETURNING id, user_id, expires_at, used_at`, tokenHash,
	).Scan(&t.ID, &t.UserID, &t.ExpiresAt, &t.UsedAt)
	if err != nil {
		if db.IsNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("claim reset token: %w", err)
	}
	return t, nil
}

func nullableUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}
