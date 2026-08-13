package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const testSecret = "unit-test-secret-material-0123456789"

func newTestManager(t *testing.T, ttl time.Duration) *TokenManager {
	t.Helper()
	m, err := NewTokenManager(testSecret, "dating-platform", ttl)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	return m
}

func TestNewTokenManagerRejectsBadConfig(t *testing.T) {
	if _, err := NewTokenManager("", "iss", time.Minute); err == nil {
		t.Error("an empty secret was accepted; unsigned tokens would be forgeable")
	}
	if _, err := NewTokenManager("secret", "iss", 0); err == nil {
		t.Error("a zero TTL was accepted")
	}
	if _, err := NewTokenManager("secret", "iss", -time.Minute); err == nil {
		t.Error("a negative TTL was accepted")
	}
}

func TestIssueAndVerifyRoundTrip(t *testing.T) {
	m := newTestManager(t, 15*time.Minute)
	userID, sessionID := uuid.New(), uuid.New()

	token, expiresAt, err := m.IssueAccess(userID, sessionID)
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	principal, err := m.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if principal.UserID != userID {
		t.Errorf("UserID = %v, want %v", principal.UserID, userID)
	}
	if principal.SessionID != sessionID {
		t.Errorf("SessionID = %v, want %v", principal.SessionID, sessionID)
	}
	if !principal.ExpiresAt.Equal(expiresAt.Truncate(time.Second)) {
		t.Errorf("ExpiresAt = %v, want %v", principal.ExpiresAt, expiresAt.Truncate(time.Second))
	}
	if time.Until(expiresAt) > 15*time.Minute+time.Second {
		t.Errorf("token outlives the configured TTL: expires in %v", time.Until(expiresAt))
	}
}

func TestIssueAccessRequiresAUser(t *testing.T) {
	m := newTestManager(t, time.Minute)
	if _, _, err := m.IssueAccess(uuid.Nil, uuid.New()); err == nil {
		t.Fatal("a token was minted for the nil user id")
	}
}

func TestVerifyRejectsEmptyAndMalformed(t *testing.T) {
	m := newTestManager(t, time.Minute)
	for _, raw := range []string{"", "   ", "not-a-jwt", "a.b", "a.b.c", "Bearer x.y.z"} {
		if _, err := m.Verify(raw); err == nil {
			t.Errorf("Verify(%q) succeeded", raw)
		} else if !errors.Is(err, ErrInvalidToken) {
			t.Errorf("Verify(%q) error = %v, want ErrInvalidToken", raw, err)
		}
	}
}

func TestVerifyTrimsSurroundingWhitespace(t *testing.T) {
	m := newTestManager(t, time.Minute)
	token, _, err := m.IssueAccess(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	if _, err := m.Verify("  " + token + "\n"); err != nil {
		t.Fatalf("Verify rejected a padded token: %v", err)
	}
}

func TestVerifyRejectsAnotherSecret(t *testing.T) {
	issuer := newTestManager(t, time.Minute)
	token, _, err := issuer.IssueAccess(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	other, err := NewTokenManager("a-completely-different-secret-value", "dating-platform", time.Minute)
	if err != nil {
		t.Fatalf("NewTokenManager: %v", err)
	}
	if _, err := other.Verify(token); err == nil {
		t.Fatal("a token signed with a different secret was accepted")
	}
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	m := newTestManager(t, time.Minute)
	token, _, err := m.IssueAccess(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	parts := strings.Split(token, ".")
	// Swap the payload for one naming a different user, keeping the signature.
	forged, _, err := m.IssueAccess(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	forgedParts := strings.Split(forged, ".")
	spliced := parts[0] + "." + forgedParts[1] + "." + parts[2]

	if _, err := m.Verify(spliced); err == nil {
		t.Fatal("a token with a swapped payload was accepted")
	}
}

func TestVerifyRejectsUnsignedAlgorithm(t *testing.T) {
	m := newTestManager(t, time.Minute)
	claims := Claims{
		UserID:    uuid.NewString(),
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "dating-platform",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	// alg=none, the classic JWT downgrade attack.
	unsigned, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign unsigned token: %v", err)
	}
	if _, err := m.Verify(unsigned); err == nil {
		t.Fatal("an alg=none token was accepted")
	}
}

func TestVerifyRejectsExpiredWithADistinctError(t *testing.T) {
	m := newTestManager(t, time.Minute)
	base := time.Now()
	m.now = func() time.Time { return base }

	token, _, err := m.IssueAccess(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	m.now = func() time.Time { return base.Add(2 * time.Minute) }

	_, err = m.Verify(token)
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("error = %v, want ErrTokenExpired so the client is told to refresh", err)
	}
	if errors.Is(err, ErrInvalidToken) {
		t.Fatal("expiry must not also report ErrInvalidToken; the two drive different client behaviour")
	}
}

func TestVerifyAcceptsATokenRightBeforeExpiry(t *testing.T) {
	m := newTestManager(t, time.Minute)
	base := time.Now()
	m.now = func() time.Time { return base }

	token, _, err := m.IssueAccess(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	m.now = func() time.Time { return base.Add(59 * time.Second) }
	if _, err := m.Verify(token); err != nil {
		t.Fatalf("a token one second from expiry was rejected: %v", err)
	}
}

func TestVerifyRejectsNonAccessTokenType(t *testing.T) {
	m := newTestManager(t, time.Minute)
	claims := Claims{
		UserID:    uuid.NewString(),
		TokenType: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "dating-platform",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	_, err = m.Verify(signed)
	if err == nil {
		t.Fatal("a refresh-typed token was accepted as a bearer token")
	}
	if !strings.Contains(err.Error(), "refresh") {
		t.Fatalf("error %q should name the rejected token type", err)
	}
}

func TestVerifyRequiresAnExpiry(t *testing.T) {
	m := newTestManager(t, time.Minute)
	claims := Claims{
		UserID:    uuid.NewString(),
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: "dating-platform",
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := m.Verify(signed); err == nil {
		t.Fatal("a token with no exp claim was accepted; it would never expire")
	}
}

func TestVerifyRejectsForeignIssuer(t *testing.T) {
	m := newTestManager(t, time.Minute)
	claims := Claims{
		UserID:    uuid.NewString(),
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "some-other-service",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := m.Verify(signed); err == nil {
		t.Fatal("a token issued by another service was accepted")
	}
}

func TestVerifyAcceptsAMissingIssuerForBackwardCompatibility(t *testing.T) {
	m := newTestManager(t, time.Minute)
	claims := Claims{
		UserID:    uuid.NewString(),
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := m.Verify(signed); err != nil {
		t.Fatalf("a legacy token without iss was rejected: %v", err)
	}
}

func TestVerifyRejectsMalformedIdentifiers(t *testing.T) {
	m := newTestManager(t, time.Minute)
	cases := []struct {
		name   string
		claims Claims
	}{
		{"missing user id", Claims{TokenType: TokenTypeAccess}},
		{"non-uuid user id", Claims{UserID: "not-a-uuid", TokenType: TokenTypeAccess}},
		{"nil user id", Claims{UserID: uuid.Nil.String(), TokenType: TokenTypeAccess}},
		{"non-uuid sid", Claims{UserID: uuid.NewString(), SessionID: "nope", TokenType: TokenTypeAccess}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.claims.RegisteredClaims = jwt.RegisteredClaims{
				Issuer:    "dating-platform",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			}
			signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, tc.claims).SignedString([]byte(testSecret))
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			if _, err := m.Verify(signed); err == nil {
				t.Fatal("token was accepted")
			}
		})
	}
}

func TestNewRefreshToken(t *testing.T) {
	seen := make(map[string]bool, 200)
	for i := 0; i < 200; i++ {
		token, hash, err := NewRefreshToken()
		if err != nil {
			t.Fatalf("NewRefreshToken: %v", err)
		}
		if !strings.HasPrefix(token, refreshTokenPrefix) {
			t.Fatalf("token %q lacks the %q prefix that makes leaks recognisable", token, refreshTokenPrefix)
		}
		if seen[token] {
			t.Fatalf("NewRefreshToken repeated a value after %d calls", i)
		}
		seen[token] = true

		if hash == token {
			t.Fatal("the stored hash equals the token; a database leak would yield usable credentials")
		}
		if hash != HashOpaqueToken(token) {
			t.Fatal("returned hash does not match HashOpaqueToken; lookups by hash would miss")
		}
		if strings.Contains(hash, token) {
			t.Fatal("the hash embeds the raw token")
		}
	}
}

func TestNewResetTokenIsOpaqueAndUnprefixed(t *testing.T) {
	token, hash, err := NewResetToken()
	if err != nil {
		t.Fatalf("NewResetToken: %v", err)
	}
	// Reset tokens travel in emails and URLs, so they must not carry the
	// refresh-token prefix that secret scanners key on.
	if strings.HasPrefix(token, refreshTokenPrefix) {
		t.Fatalf("reset token %q carries the refresh-token prefix", token)
	}
	if hash != HashOpaqueToken(token) {
		t.Fatal("returned hash does not match HashOpaqueToken")
	}
	for _, r := range token {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			t.Fatalf("token %q contains %q, which needs URL escaping", token, r)
		}
	}
}

func TestHashOpaqueTokenIsStableAndDistinct(t *testing.T) {
	if HashOpaqueToken("abc") != HashOpaqueToken("abc") {
		t.Fatal("HashOpaqueToken is not deterministic; stored hashes would never match")
	}
	if HashOpaqueToken("abc") == HashOpaqueToken("abd") {
		t.Fatal("HashOpaqueToken collided on a one-character difference")
	}
	if got := len(HashOpaqueToken("abc")); got != 64 {
		t.Fatalf("hash length = %d, want 64 hex characters for SHA-256", got)
	}
	if HashOpaqueToken("") == "" {
		t.Fatal("hashing the empty string produced an empty hash")
	}
}
