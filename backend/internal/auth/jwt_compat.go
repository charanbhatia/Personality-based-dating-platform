package auth

import (
	"time"

	"github.com/google/uuid"
)

// NewJWT and ParseJWT are the pre-v1 helpers Person C's websocket gateway and
// tests still call. They wrap TokenManager so there is a single verifier.
func NewJWT(secret string, userID uuid.UUID) (string, error) {
	m, err := NewTokenManager(secret, "", 24*time.Hour)
	if err != nil {
		return "", err
	}
	token, _, err := m.IssueAccess(userID, uuid.Nil)
	return token, err
}

func ParseJWT(secret, tokenString string) (uuid.UUID, error) {
	m, err := NewTokenManager(secret, "", 24*time.Hour)
	if err != nil {
		return uuid.Nil, err
	}
	principal, err := m.Verify(tokenString)
	if err != nil {
		return uuid.Nil, ErrInvalidToken
	}
	return principal.UserID, nil
}
