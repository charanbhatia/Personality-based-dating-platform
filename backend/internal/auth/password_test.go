package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"minimum length", strings.Repeat("a", MinPasswordLength), false},
		{"typical", "correct horse battery", false},
		{"maximum bytes", strings.Repeat("a", MaxPasswordLength), false},
		{"empty", "", true},
		{"one short", strings.Repeat("a", MinPasswordLength-1), true},
		{"one byte over", strings.Repeat("a", MaxPasswordLength+1), true},
		// Length is counted in runes for the minimum so a short multi-byte
		// passphrase is not accepted as long enough...
		{"short but multibyte", "日本語パス", true},
		// ...and in bytes for the maximum, because that is where bcrypt truncates.
		{"under 72 runes but over 72 bytes", strings.Repeat("é", 40), true},
		{"exactly 72 bytes of multibyte", strings.Repeat("é", 36), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePassword(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidatePassword(%q) error = %v, wantErr %v", tc.in, err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrPasswordTooWeak) {
				t.Fatalf("error %v does not wrap ErrPasswordTooWeak", err)
			}
		})
	}
}

func TestValidatePasswordRejectsInvalidUTF8(t *testing.T) {
	if err := ValidatePassword("password\xff\xfe"); err == nil {
		t.Fatal("invalid UTF-8 was accepted")
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	const password = "correct horse battery staple"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == password || strings.Contains(hash, password) {
		t.Fatal("the hash contains the plaintext")
	}
	if !CheckPassword(hash, password) {
		t.Fatal("CheckPassword rejected the correct password")
	}
	if CheckPassword(hash, password+"x") {
		t.Fatal("CheckPassword accepted a wrong password")
	}
	if CheckPassword(hash, strings.ToUpper(password)) {
		t.Fatal("CheckPassword is case-insensitive")
	}
}

func TestHashPasswordIsSalted(t *testing.T) {
	const password = "correct horse battery staple"
	first, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	second, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if first == second {
		t.Fatal("two hashes of the same password are identical; the hash is unsalted")
	}
	// Both must still verify.
	if !CheckPassword(first, password) || !CheckPassword(second, password) {
		t.Fatal("a salted hash failed to verify")
	}
}

func TestHashPasswordEnforcesThePolicy(t *testing.T) {
	for _, weak := range []string{"", "short", strings.Repeat("a", MaxPasswordLength+1)} {
		if _, err := HashPassword(weak); err == nil {
			t.Errorf("HashPassword(%q) succeeded; the policy must be enforced at the hashing boundary too", weak)
		}
	}
}

func TestCheckPasswordRejectsEmptyInputs(t *testing.T) {
	hash, err := HashPassword("a-valid-password")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if CheckPassword("", "a-valid-password") {
		t.Error("an empty hash was accepted")
	}
	if CheckPassword(hash, "") {
		t.Error("an empty password was accepted")
	}
	if CheckPassword("", "") {
		t.Error("two empty inputs were accepted")
	}
	if CheckPassword("not-a-bcrypt-hash", "a-valid-password") {
		t.Error("a malformed hash was accepted")
	}
}

func TestBurnPasswordComparisonIsSafeToCall(t *testing.T) {
	// It must never panic and must be safe concurrently: login calls it on the
	// unauthenticated path for every unknown address.
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("BurnPasswordComparison panicked: %v", r)
				}
				done <- struct{}{}
			}()
			BurnPasswordComparison()
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
}
