package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/bits-assignment/dating-platform/backend/internal/db"
	"github.com/bits-assignment/dating-platform/backend/internal/domain"
	"github.com/bits-assignment/dating-platform/backend/internal/httpx"
	"github.com/bits-assignment/dating-platform/backend/internal/outbox"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Domain error codes surfaced to clients.
const (
	CodeEmailTaken           = "email_already_registered"
	CodeInvalidCredentials   = "invalid_credentials"
	CodeRefreshTokenInvalid  = "refresh_token_invalid"
	CodeRefreshTokenReused   = "refresh_token_reused"
	CodeResetTokenInvalid    = "reset_token_invalid"
	CodeVerifyTokenInvalid   = "verify_token_invalid"
	CodeUnderageRegistration = "underage"
)

// errRefreshReuse signals a replayed refresh token from inside the rotation
// transaction so the caller can revoke the session after the rollback.
var errRefreshReuse = errors.New("refresh token reused")

// MinimumAge gates registration and profile updates. A dating product must not
// hold accounts for minors, so this is enforced server-side rather than trusted
// from the client.
const MinimumAge = 18

// MaximumAge rejects obvious typos such as a birth year of 1080.
const MaximumAge = 120

// DefaultSeekAgeMax is the upper bound of the age range registration writes into
// preferences, so a new account has a usable filter before onboarding. Migration
// 005 backfills older accounts with the same value; changing one without the
// other would give new and existing users different feeds.
const DefaultSeekAgeMax = 99

// revokedSessionRetention keeps revoked sessions listed briefly for audit before
// the sweeper deletes them.
const revokedSessionRetention = 7 * 24 * time.Hour

// Pool is the database surface the service needs.
type Pool interface {
	db.Querier
	db.Beginner
}

// EventKicker lets the service nudge the outbox drainer after a commit so events
// are published promptly instead of on the next poll tick.
type EventKicker interface{ Kick() }

// Service implements the auth use cases (F01–F04).
type Service struct {
	pool                 Pool
	repo                 Repo
	tokens               *TokenManager
	refreshTTL           time.Duration
	passwordResetTTL     time.Duration
	emailVerificationTTL time.Duration
	kicker               EventKicker
	now                  func() time.Time
}

// ServiceConfig configures NewService.
type ServiceConfig struct {
	Pool                 Pool
	Tokens               *TokenManager
	RefreshTTL           time.Duration
	PasswordResetTTL     time.Duration
	EmailVerificationTTL time.Duration
	Kicker               EventKicker
	Now                  func() time.Time
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Pool == nil {
		return nil, errors.New("auth: pool is required")
	}
	if cfg.Tokens == nil {
		return nil, errors.New("auth: token manager is required")
	}
	if cfg.RefreshTTL <= 0 {
		return nil, errors.New("auth: refresh TTL must be positive")
	}
	if cfg.PasswordResetTTL <= 0 {
		return nil, errors.New("auth: password reset TTL must be positive")
	}
	if cfg.EmailVerificationTTL <= 0 {
		return nil, errors.New("auth: email verification TTL must be positive")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		pool:                 cfg.Pool,
		tokens:               cfg.Tokens,
		refreshTTL:           cfg.RefreshTTL,
		passwordResetTTL:     cfg.PasswordResetTTL,
		emailVerificationTTL: cfg.EmailVerificationTTL,
		kicker:               cfg.Kicker,
		now:                  now,
	}, nil
}

// UserDTO is the caller's own identity. Only ever returned to the account owner;
// public and discovery payloads use profile.PublicProfile, which has no email.
type UserDTO struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	Name          string    `json:"name"`
	DateOfBirth   *string   `json:"date_of_birth"`
	Age           *int      `json:"age,omitempty"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
}

func toUserDTO(u *User) UserDTO {
	dto := UserDTO{
		ID:            u.ID,
		Email:         u.Email,
		Name:          u.Name,
		EmailVerified: u.EmailVerifiedAt != nil,
		CreatedAt:     u.CreatedAt,
	}
	if u.DateOfBirth != nil {
		formatted := u.DateOfBirth.Format(DateLayout)
		dto.DateOfBirth = &formatted
		age := AgeAt(*u.DateOfBirth, time.Now())
		dto.Age = &age
	}
	return dto
}

// TokenPair is the credential set handed to a client after authenticating.
type TokenPair struct {
	AccessToken           string    `json:"access_token"`
	TokenType             string    `json:"token_type"`
	ExpiresIn             int       `json:"expires_in"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
	SessionID             uuid.UUID `json:"session_id"`
}

// AuthResult is the register/login/refresh response body.
type AuthResult struct {
	User UserDTO `json:"user"`
	TokenPair
}

// RegisterInput is the validated registration request.
type RegisterInput struct {
	Email       string
	Password    string
	Name        string
	DateOfBirth string
	Meta        SessionMeta

	// RequireDateOfBirth is set by the /api/v1 handler. The pre-v1 endpoint keeps
	// it optional so the existing frontend continues to work during migration.
	RequireDateOfBirth bool
}

// DateLayout is the wire format for dates (RFC 3339 full-date).
const DateLayout = "2006-01-02"

// Register creates the user together with the empty profile and preferences rows
// its onboarding depends on.
//
// All three inserts share one transaction: the previous implementation created
// the user first and could leave an account with no profile if the second insert
// failed, which then broke every profile read for that user.
func (s *Service) Register(ctx context.Context, in RegisterInput) (*AuthResult, error) {
	email := httpx.NormalizeEmail(in.Email)
	name := httpx.CleanLine(in.Name)

	v := httpx.NewValidator()
	v.Require(email != "", "email", "email is required")
	if email != "" {
		v.Require(httpx.ValidEmail(email), "email", "must be a valid email address")
	}
	v.Require(name != "", "name", "name is required")
	v.Require(httpx.TrimmedRuneLen(name) <= 120, "name", "must not exceed 120 characters")
	if err := ValidatePassword(in.Password); err != nil {
		v.Add("password", passwordMessage(err))
	}

	dob, dobErr := parseDateOfBirth(in.DateOfBirth, in.RequireDateOfBirth, s.now())
	if dobErr != "" {
		v.Add("date_of_birth", dobErr)
	}
	if err := v.Err(); err != nil {
		return nil, err
	}

	passwordHash, err := HashPassword(in.Password)
	if err != nil {
		return nil, httpx.Internal(err)
	}

	refreshToken, refreshHash, err := NewRefreshToken()
	if err != nil {
		return nil, httpx.Internal(err)
	}
	refreshExpiry := s.now().Add(s.refreshTTL)

	verifyToken, verifyHash, err := NewResetToken()
	if err != nil {
		return nil, httpx.Internal(err)
	}
	verifyExpiry := s.now().Add(s.emailVerificationTTL)

	var user *User
	var session *Session
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		created, err := s.repo.CreateUser(ctx, tx, email, passwordHash, name, dob)
		if err != nil {
			if db.IsUniqueViolation(err) {
				return httpx.Conflict(CodeEmailTaken, "an account with this email already exists")
			}
			return fmt.Errorf("create user: %w", err)
		}
		user = created

		// Onboarding reads and updates these rows, so create them up front rather
		// than lazily on first access.
		if _, err := tx.Exec(ctx, `INSERT INTO profiles (user_id) VALUES ($1)`, created.ID); err != nil {
			return fmt.Errorf("create profile: %w", err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO preferences (user_id, age_min, age_max) VALUES ($1, $2, $3)`,
			created.ID, MinimumAge, DefaultSeekAgeMax); err != nil {
			return fmt.Errorf("create preferences: %w", err)
		}

		session, err = s.repo.CreateSession(ctx, tx, created.ID, refreshHash, refreshExpiry, in.Meta)
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}

		if _, err := s.repo.CreateEmailVerificationToken(ctx, tx, created.ID, verifyHash, verifyExpiry); err != nil {
			return fmt.Errorf("create verification token: %w", err)
		}
		_, err = outbox.Enqueue(ctx, tx, outbox.EventEmailVerificationRequested, map[string]any{
			"user_id":      created.ID,
			"email":        created.Email,
			"verify_token": verifyToken,
			"expires_at":   verifyExpiry.UTC().Format(time.RFC3339),
		})
		return err
	})
	if err != nil {
		return nil, wrapDomainError(err)
	}

	pair, err := s.newTokenPair(user.ID, session.ID, refreshToken, refreshExpiry)
	if err != nil {
		return nil, err
	}
	s.kick()
	slog.InfoContext(ctx, "user registered", "user_id", user.ID)
	return &AuthResult{User: toUserDTO(user), TokenPair: *pair}, nil
}

// LoginInput is a credential submission.
type LoginInput struct {
	Email    string
	Password string
	Meta     SessionMeta
}

// Login verifies credentials and starts a session.
//
// Both failure paths return the same generic error and take comparable time, so
// neither the response body nor its latency reveals whether an email is
// registered.
func (s *Service) Login(ctx context.Context, in LoginInput) (*AuthResult, error) {
	email := httpx.NormalizeEmail(in.Email)
	if email == "" || in.Password == "" {
		return nil, httpx.CodedError(http.StatusBadRequest, httpx.CodeBadRequest,
			"email and password are required")
	}

	user, err := s.repo.GetUserByEmail(ctx, s.pool, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			BurnPasswordComparison()
			return nil, invalidCredentials()
		}
		return nil, httpx.Internal(fmt.Errorf("lookup user: %w", err))
	}
	if !CheckPassword(user.PasswordHash, in.Password) {
		return nil, invalidCredentials()
	}

	refreshToken, refreshHash, err := NewRefreshToken()
	if err != nil {
		return nil, httpx.Internal(err)
	}
	refreshExpiry := s.now().Add(s.refreshTTL)

	session, err := s.repo.CreateSession(ctx, s.pool, user.ID, refreshHash, refreshExpiry, in.Meta)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("create session: %w", err))
	}

	pair, err := s.newTokenPair(user.ID, session.ID, refreshToken, refreshExpiry)
	if err != nil {
		return nil, err
	}
	return &AuthResult{User: toUserDTO(user), TokenPair: *pair}, nil
}

// Refresh rotates a refresh token and issues a new access token.
//
// Rotation is single-use. Presenting a token that was already rotated away means
// it leaked, so the whole session is revoked instead of being refreshed. A client
// whose refresh request failed mid-flight therefore has to log in again; that is
// the deliberate trade, because the alternative is letting a captured token stay
// valid indefinitely.
func (s *Service) Refresh(ctx context.Context, refreshToken string, meta SessionMeta) (*AuthResult, error) {
	if refreshToken == "" {
		return nil, httpx.CodedError(http.StatusBadRequest, httpx.CodeBadRequest, "refresh_token is required")
	}
	presentedHash := HashOpaqueToken(refreshToken)

	newToken, newHash, err := NewRefreshToken()
	if err != nil {
		return nil, httpx.Internal(err)
	}
	newExpiry := s.now().Add(s.refreshTTL)

	var user *User
	var sessionID uuid.UUID
	var reused *Session
	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		session, isCurrent, err := s.repo.FindSessionByToken(ctx, tx, presentedHash)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return httpx.CodedError(http.StatusUnauthorized, CodeRefreshTokenInvalid,
					"refresh token is not valid")
			}
			return fmt.Errorf("find session: %w", err)
		}

		if !isCurrent {
			// Revoking here would be undone by this transaction's rollback, so
			// the replay is only recorded and acted on after it unwinds.
			reused = session
			return errRefreshReuse
		}
		if !session.Active(s.now()) {
			return httpx.CodedError(http.StatusUnauthorized, CodeRefreshTokenInvalid,
				"session has expired or been revoked")
		}

		if err := s.repo.RotateSession(ctx, tx, session.ID, presentedHash, newHash, newExpiry, meta); err != nil {
			if errors.Is(err, ErrNotFound) {
				// Another refresh for the same session won the race.
				return httpx.CodedError(http.StatusUnauthorized, CodeRefreshTokenInvalid,
					"refresh token is no longer valid")
			}
			return fmt.Errorf("rotate session: %w", err)
		}

		user, err = s.repo.GetUserByID(ctx, tx, session.UserID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return httpx.CodedError(http.StatusUnauthorized, CodeRefreshTokenInvalid,
					"account no longer exists")
			}
			return fmt.Errorf("load user: %w", err)
		}
		sessionID = session.ID
		return nil
	})
	if errors.Is(err, errRefreshReuse) {
		if _, rerr := s.repo.RevokeSession(ctx, s.pool, reused.ID, "refresh_token_reuse"); rerr != nil {
			// The client is rejected either way; a failed revoke only leaves the
			// live token usable, so it is logged loudly rather than surfaced.
			slog.ErrorContext(ctx, "failed to revoke replayed session",
				"user_id", reused.UserID, "session_id", reused.ID, "error", rerr)
		}
		slog.WarnContext(ctx, "refresh token replay detected; session revoked",
			"user_id", reused.UserID, "session_id", reused.ID)
		return nil, httpx.CodedError(http.StatusUnauthorized, CodeRefreshTokenReused,
			"refresh token has already been used; sign in again")
	}
	if err != nil {
		return nil, wrapDomainError(err)
	}

	pair, err := s.newTokenPair(user.ID, sessionID, newToken, newExpiry)
	if err != nil {
		return nil, err
	}
	return &AuthResult{User: toUserDTO(user), TokenPair: *pair}, nil
}

// Logout revokes the session backing the caller's access token. It is idempotent
// so a client retrying after a network failure still sees success.
func (s *Service) Logout(ctx context.Context, principal Principal) error {
	if principal.SessionID == uuid.Nil {
		// Tokens minted before sessions existed carry no sid; there is nothing
		// server-side to revoke and the client discards the token either way.
		return nil
	}
	if _, err := s.repo.RevokeSession(ctx, s.pool, principal.SessionID, "logout"); err != nil {
		return httpx.Internal(fmt.Errorf("revoke session: %w", err))
	}
	return nil
}

// MeResponse is the /auth/me body.
type MeResponse struct {
	User       UserDTO    `json:"user"`
	Onboarding Onboarding `json:"onboarding"`
}

// Me returns the caller's identity plus derived onboarding flags.
func (s *Service) Me(ctx context.Context, userID uuid.UUID) (*MeResponse, error) {
	user, err := s.repo.GetUserByID(ctx, s.pool, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, httpx.NotFound("account not found")
		}
		return nil, httpx.Internal(fmt.Errorf("load user: %w", err))
	}
	onboarding, err := s.repo.GetOnboarding(ctx, s.pool, userID, domain.TraitKeys())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, httpx.NotFound("account not found")
		}
		return nil, httpx.Internal(fmt.Errorf("load onboarding flags: %w", err))
	}
	return &MeResponse{User: toUserDTO(user), Onboarding: onboarding}, nil
}

// GetUser exposes the identity record to sibling B domains (profile needs the
// date of birth) without them reaching into the users table directly.
func (s *Service) GetUser(ctx context.Context, userID uuid.UUID) (*User, error) {
	return s.repo.GetUserByID(ctx, s.pool, userID)
}

// SessionDTO is one entry in the session list.
type SessionDTO struct {
	ID         uuid.UUID `json:"id"`
	UserAgent  string    `json:"user_agent"`
	IP         *string   `json:"ip"`
	Current    bool      `json:"current"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// ListSessions returns the caller's live sessions, flagging the current one.
func (s *Service) ListSessions(ctx context.Context, principal Principal) ([]SessionDTO, error) {
	sessions, err := s.repo.ListSessions(ctx, s.pool, principal.UserID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list sessions: %w", err))
	}
	out := make([]SessionDTO, 0, len(sessions))
	for _, session := range sessions {
		dto := SessionDTO{
			ID:         session.ID,
			UserAgent:  session.UserAgent,
			Current:    session.ID == principal.SessionID,
			CreatedAt:  session.CreatedAt,
			LastUsedAt: session.LastUsedAt,
			ExpiresAt:  session.ExpiresAt,
		}
		if session.IP != nil {
			ip := session.IP.String()
			dto.IP = &ip
		}
		out = append(out, dto)
	}
	return out, nil
}

// RevokeSession revokes one of the caller's own sessions.
func (s *Service) RevokeSession(ctx context.Context, principal Principal, sessionID uuid.UUID) error {
	if _, err := s.repo.GetSession(ctx, s.pool, principal.UserID, sessionID); err != nil {
		if errors.Is(err, ErrNotFound) {
			// Scoped by user id, so an id belonging to somebody else is reported as
			// not found rather than forbidden — no cross-account probing.
			return httpx.NotFound("session not found")
		}
		return httpx.Internal(fmt.Errorf("load session: %w", err))
	}
	if _, err := s.repo.RevokeSession(ctx, s.pool, sessionID, "revoked_by_user"); err != nil {
		return httpx.Internal(fmt.Errorf("revoke session: %w", err))
	}
	return nil
}

// RequestPasswordReset issues a reset token and enqueues the email event.
//
// Returns success regardless of whether the address exists, so the endpoint
// cannot be used to enumerate accounts.
func (s *Service) RequestPasswordReset(ctx context.Context, rawEmail string) error {
	email := httpx.NormalizeEmail(rawEmail)
	if email == "" || !httpx.ValidEmail(email) {
		// Still a no-op response: an invalid address cannot match an account, and
		// reporting the difference would leak the same information.
		return nil
	}

	user, err := s.repo.GetUserByEmail(ctx, s.pool, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			slog.InfoContext(ctx, "password reset requested for unknown email")
			return nil
		}
		return httpx.Internal(fmt.Errorf("lookup user: %w", err))
	}

	token, tokenHash, err := NewResetToken()
	if err != nil {
		return httpx.Internal(err)
	}
	expiresAt := s.now().Add(s.passwordResetTTL)

	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.InvalidatePasswordResetTokens(ctx, tx, user.ID); err != nil {
			return fmt.Errorf("invalidate previous reset tokens: %w", err)
		}
		if _, err := s.repo.CreatePasswordResetToken(ctx, tx, user.ID, tokenHash, expiresAt); err != nil {
			return fmt.Errorf("create reset token: %w", err)
		}
		// Enqueued in the same transaction so the email can never be sent for a
		// token that was not persisted, nor lost after one was.
		_, err := outbox.Enqueue(ctx, tx, outbox.EventPasswordResetRequired, map[string]any{
			"user_id":     user.ID,
			"email":       user.Email,
			"reset_token": token,
			"expires_at":  expiresAt.UTC().Format(time.RFC3339),
		})
		return err
	})
	if err != nil {
		return httpx.Internal(err)
	}
	s.kick()
	return nil
}

// ResetPassword consumes a reset token, sets the new password and signs every
// device out, since a reset usually follows a suspected compromise.
func (s *Service) ResetPassword(ctx context.Context, token, newPassword string) error {
	if token == "" {
		return httpx.CodedError(http.StatusBadRequest, httpx.CodeBadRequest, "token is required")
	}
	if err := ValidatePassword(newPassword); err != nil {
		v := httpx.NewValidator()
		v.Add("password", passwordMessage(err))
		return v.Err()
	}

	passwordHash, err := HashPassword(newPassword)
	if err != nil {
		return httpx.Internal(err)
	}

	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		claimed, err := s.repo.ClaimPasswordResetToken(ctx, tx, HashOpaqueToken(token))
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return httpx.CodedError(http.StatusBadRequest, CodeResetTokenInvalid,
					"reset token is invalid, expired, or already used")
			}
			return err
		}
		if err := s.repo.UpdatePasswordHash(ctx, tx, claimed.UserID, passwordHash); err != nil {
			return fmt.Errorf("update password: %w", err)
		}
		if _, err := s.repo.RevokeUserSessions(ctx, tx, claimed.UserID, "password_reset", uuid.Nil); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		slog.InfoContext(ctx, "password reset completed", "user_id", claimed.UserID)
		return nil
	})
	if err != nil {
		return wrapDomainError(err)
	}
	return nil
}

// VerifyEmail consumes a verification token and marks the account verified.
// Login is never gated on this; the flag is informational for the client.
func (s *Service) VerifyEmail(ctx context.Context, token string) error {
	if token == "" {
		return httpx.CodedError(http.StatusBadRequest, httpx.CodeBadRequest, "token is required")
	}

	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		claimed, err := s.repo.ClaimEmailVerificationToken(ctx, tx, HashOpaqueToken(token))
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return httpx.CodedError(http.StatusBadRequest, CodeVerifyTokenInvalid,
					"verification token is invalid, expired, or already used")
			}
			return err
		}
		if err := s.repo.MarkEmailVerified(ctx, tx, claimed.UserID); err != nil {
			return fmt.Errorf("mark email verified: %w", err)
		}
		slog.InfoContext(ctx, "email verified", "user_id", claimed.UserID)
		return nil
	})
	if err != nil {
		return wrapDomainError(err)
	}
	return nil
}

// ResendEmailVerification issues a fresh token for the caller. Already-verified
// accounts are a no-op so the endpoint cannot be used to spam the inbox.
func (s *Service) ResendEmailVerification(ctx context.Context, userID uuid.UUID) error {
	user, err := s.repo.GetUserByID(ctx, s.pool, userID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return httpx.NotFound("user not found")
		}
		return httpx.Internal(fmt.Errorf("lookup user: %w", err))
	}
	if user.EmailVerifiedAt != nil {
		return nil
	}

	token, tokenHash, err := NewResetToken()
	if err != nil {
		return httpx.Internal(err)
	}
	expiresAt := s.now().Add(s.emailVerificationTTL)

	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.repo.InvalidateEmailVerificationTokens(ctx, tx, user.ID); err != nil {
			return fmt.Errorf("invalidate previous verification tokens: %w", err)
		}
		if _, err := s.repo.CreateEmailVerificationToken(ctx, tx, user.ID, tokenHash, expiresAt); err != nil {
			return fmt.Errorf("create verification token: %w", err)
		}
		_, err := outbox.Enqueue(ctx, tx, outbox.EventEmailVerificationRequested, map[string]any{
			"user_id":      user.ID,
			"email":        user.Email,
			"verify_token": token,
			"expires_at":   expiresAt.UTC().Format(time.RFC3339),
		})
		return err
	})
	if err != nil {
		return wrapDomainError(err)
	}
	s.kick()
	return nil
}

// SweepExpiredSessions deletes unusable session rows. Run periodically by the
// server; safe to call concurrently.
func (s *Service) SweepExpiredSessions(ctx context.Context) (int64, error) {
	return s.repo.DeleteExpiredSessions(ctx, s.pool, revokedSessionRetention)
}

func (s *Service) newTokenPair(userID, sessionID uuid.UUID, refreshToken string, refreshExpiry time.Time) (*TokenPair, error) {
	accessToken, accessExpiry, err := s.tokens.IssueAccess(userID, sessionID)
	if err != nil {
		return nil, httpx.Internal(err)
	}
	return &TokenPair{
		AccessToken:           accessToken,
		TokenType:             "Bearer",
		ExpiresIn:             int(s.tokens.AccessTTL().Seconds()),
		AccessTokenExpiresAt:  accessExpiry.UTC(),
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshExpiry.UTC(),
		SessionID:             sessionID,
	}, nil
}

func (s *Service) kick() {
	if s.kicker != nil {
		s.kicker.Kick()
	}
}

func invalidCredentials() error {
	return httpx.CodedError(http.StatusUnauthorized, CodeInvalidCredentials, "email or password is incorrect")
}

// wrapDomainError passes through errors already shaped for the API and wraps
// anything else as an internal failure.
func wrapDomainError(err error) error {
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return httpx.Internal(err)
}

func passwordMessage(err error) string {
	if errors.Is(err, ErrPasswordTooWeak) {
		// Strip the sentinel prefix, leaving the actionable part.
		msg := err.Error()
		if idx := len(ErrPasswordTooWeak.Error()) + 2; idx < len(msg) {
			return msg[idx:]
		}
	}
	return "password is not acceptable"
}

// parseDateOfBirth validates a YYYY-MM-DD date of birth, returning a
// client-facing message on failure.
func parseDateOfBirth(raw string, required bool, now time.Time) (*time.Time, string) {
	if raw == "" {
		if required {
			return nil, "date_of_birth is required"
		}
		return nil, ""
	}
	parsed, err := time.Parse(DateLayout, raw)
	if err != nil {
		return nil, "must be a date in YYYY-MM-DD format"
	}
	parsed = parsed.UTC()
	if parsed.After(now) {
		return nil, "must not be in the future"
	}
	age := AgeAt(parsed, now)
	if age < MinimumAge {
		return nil, fmt.Sprintf("you must be at least %d years old", MinimumAge)
	}
	if age > MaximumAge {
		return nil, "must be a realistic date of birth"
	}
	return &parsed, ""
}

// AgeAt returns completed years between dob and t.
func AgeAt(dob, t time.Time) int {
	dob = dob.UTC()
	t = t.UTC()
	years := t.Year() - dob.Year()
	// Subtract a year when the birthday has not occurred yet this year.
	if t.Month() < dob.Month() || (t.Month() == dob.Month() && t.Day() < dob.Day()) {
		years--
	}
	if years < 0 {
		return 0
	}
	return years
}

// ParseIP extracts a comparable client address for the session record.
func ParseIP(raw string) *netip.Addr {
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return nil
	}
	// Unmap so an IPv4-in-IPv6 address is stored the same way as its plain form.
	addr = addr.Unmap()
	return &addr
}
