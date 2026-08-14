package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Access-token claim contract. Person C's shared middleware validates these
// exact fields (roadmap §7, "Both validate JWT via shared platform/auth helpers
// (claims defined by B)"), so treat the JSON tags as a cross-team interface.
const (
	// TokenTypeAccess marks a short-lived bearer token. Refresh tokens are opaque
	// and never JWTs, so this is the only type that appears in a signed token —
	// the claim exists so a future token kind cannot be replayed as an access
	// token.
	TokenTypeAccess = "access"

	signingAlgorithm = "HS256"
)

var (
	// ErrInvalidToken covers every unusable-token case except expiry.
	ErrInvalidToken = errors.New("invalid token")
	// ErrTokenExpired is separated out so the API can answer with a code that
	// tells the client to refresh rather than to log in again.
	ErrTokenExpired = errors.New("token expired")
)

// Claims is the access-token payload.
type Claims struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"sid,omitempty"`
	TokenType string `json:"typ,omitempty"`
	jwt.RegisteredClaims
}

// Principal is the authenticated caller extracted from a verified token.
type Principal struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	ExpiresAt time.Time
}

// TokenManager issues and verifies access tokens.
type TokenManager struct {
	secret    []byte
	issuer    string
	accessTTL time.Duration
	now       func() time.Time
}

// NewTokenManager validates its inputs up front so a misconfigured secret fails
// at boot rather than on the first login.
func NewTokenManager(secret, issuer string, accessTTL time.Duration) (*TokenManager, error) {
	if secret == "" {
		return nil, errors.New("auth: JWT secret is required")
	}
	if accessTTL <= 0 {
		return nil, errors.New("auth: access token TTL must be positive")
	}
	return &TokenManager{
		secret:    []byte(secret),
		issuer:    issuer,
		accessTTL: accessTTL,
		now:       time.Now,
	}, nil
}

// AccessTTL is exposed so handlers can report expires_in without duplicating it.
func (m *TokenManager) AccessTTL() time.Duration { return m.accessTTL }

// IssueAccess mints a signed access token bound to a refresh session, returning
// the token and its expiry.
func (m *TokenManager) IssueAccess(userID, sessionID uuid.UUID) (string, time.Time, error) {
	if userID == uuid.Nil {
		return "", time.Time{}, errors.New("auth: user id is required")
	}
	issuedAt := m.now().UTC()
	expiresAt := issuedAt.Add(m.accessTTL)

	claims := Claims{
		UserID:    userID.String(),
		SessionID: sessionID.String(),
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			// Subject mirrors user_id so generic JWT tooling and Person C's
			// middleware can read either.
			Subject:   userID.String(),
			Issuer:    m.issuer,
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			NotBefore: jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

// Verify parses and validates an access token.
//
// The algorithm is pinned to HS256, so a token re-signed with "none" or an
// asymmetric header is rejected before the signature is checked. `exp` is
// mandatory: a token without one would never expire.
func (m *TokenManager) Verify(raw string) (Principal, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Principal{}, ErrInvalidToken
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (any, error) { return m.secret, nil },
		jwt.WithValidMethods([]string{signingAlgorithm}),
		jwt.WithTimeFunc(m.now),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Principal{}, ErrTokenExpired
		}
		return Principal{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if !token.Valid {
		return Principal{}, ErrInvalidToken
	}
	if claims.ExpiresAt == nil {
		return Principal{}, fmt.Errorf("%w: missing exp claim", ErrInvalidToken)
	}
	// Tokens minted before the issuer claim existed carry no `iss`; accept those
	// but reject a mismatch, so rotating the issuer name does not force a
	// flag-day logout while still catching cross-environment token reuse.
	if claims.Issuer != "" && m.issuer != "" && claims.Issuer != m.issuer {
		return Principal{}, fmt.Errorf("%w: unexpected issuer", ErrInvalidToken)
	}
	if claims.TokenType != "" && claims.TokenType != TokenTypeAccess {
		return Principal{}, fmt.Errorf("%w: %s tokens are not accepted here", ErrInvalidToken, claims.TokenType)
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil || userID == uuid.Nil {
		return Principal{}, fmt.Errorf("%w: malformed user_id", ErrInvalidToken)
	}

	principal := Principal{UserID: userID, ExpiresAt: claims.ExpiresAt.Time}
	if claims.SessionID != "" {
		sessionID, err := uuid.Parse(claims.SessionID)
		if err != nil {
			return Principal{}, fmt.Errorf("%w: malformed sid", ErrInvalidToken)
		}
		principal.SessionID = sessionID
	}
	return principal, nil
}

// refreshTokenPrefix makes leaked credentials recognisable in logs and secret
// scanners.
const refreshTokenPrefix = "rt_"

// refreshTokenEntropyBytes exceeds the 32 bytes required by the spec.
const refreshTokenEntropyBytes = 32

// NewRefreshToken returns a fresh opaque refresh token and the hash to persist.
// Only the hash is stored, so a database disclosure does not yield usable
// credentials.
func NewRefreshToken() (token string, hash string, err error) {
	buf := make([]byte, refreshTokenEntropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("auth: generate refresh token: %w", err)
	}
	token = refreshTokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	return token, HashOpaqueToken(token), nil
}

// NewResetToken returns a password-reset token and its stored hash.
func NewResetToken() (token string, hash string, err error) {
	buf := make([]byte, refreshTokenEntropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("auth: generate reset token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, HashOpaqueToken(token), nil
}

// HashOpaqueToken derives the stored form of an opaque token.
//
// SHA-256 rather than bcrypt is correct here: these tokens are 256 bits of
// uniform randomness, so there is no low-entropy secret to slow an attacker
// down, and a fast digest is what allows the indexed lookup by hash.
func HashOpaqueToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
