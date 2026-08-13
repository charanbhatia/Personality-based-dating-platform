package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// Password policy. Document these in the API reference before changing them.
const (
	MinPasswordLength = 8
	// MaxPasswordLength is bcrypt's hard input limit. Longer inputs are rejected
	// with a clear message rather than being silently truncated at 72 bytes,
	// which would make two different passwords interchangeable.
	MaxPasswordLength = 72
)

var ErrPasswordTooWeak = errors.New("password does not meet the policy")

// ValidatePassword enforces the policy and returns a message suitable for
// returning to the client.
func ValidatePassword(password string) error {
	// Measured in bytes because that is the unit bcrypt truncates on: a
	// multi-byte passphrase can be well under 72 characters yet over 72 bytes.
	length := len(password)
	switch {
	case length == 0:
		return fmt.Errorf("%w: password is required", ErrPasswordTooWeak)
	case utf8.RuneCountInString(password) < MinPasswordLength:
		return fmt.Errorf("%w: must be at least %d characters", ErrPasswordTooWeak, MinPasswordLength)
	case length > MaxPasswordLength:
		return fmt.Errorf("%w: must not exceed %d bytes", ErrPasswordTooWeak, MaxPasswordLength)
	}
	if !utf8.ValidString(password) {
		return fmt.Errorf("%w: must be valid UTF-8", ErrPasswordTooWeak)
	}
	return nil
}

// HashPassword applies bcrypt at the library default cost.
func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("auth: hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword verifies a password against its stored hash.
func CheckPassword(hash, password string) bool {
	if hash == "" || password == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// decoyHash is a valid bcrypt hash of a random secret, computed once on demand.
var decoyHash = sync.OnceValue(func() []byte {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// Fall back to a fixed string: the value only needs to be a well-formed
		// bcrypt input, never a secret.
		buf = []byte("decoy-password-material")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(base64.RawStdEncoding.EncodeToString(buf)), bcrypt.DefaultCost)
	if err != nil {
		return nil
	}
	return hash
})

// BurnPasswordComparison performs a throwaway bcrypt comparison.
//
// Login calls this when the email does not exist so that a missing account and a
// wrong password take comparable time. Without it, response latency alone
// discloses which addresses are registered.
func BurnPasswordComparison() {
	hash := decoyHash()
	if len(hash) == 0 {
		return
	}
	_ = bcrypt.CompareHashAndPassword(hash, []byte("incorrect-password"))
}
