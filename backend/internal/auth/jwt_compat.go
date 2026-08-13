package auth

import (
	"time"

	"github.com/google/uuid"
)

// NewJWT and ParseJWT keep the pre-v1 handlers compiling while the new token
// manager lands. Removed once the legacy handlers switch to TokenManager.
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