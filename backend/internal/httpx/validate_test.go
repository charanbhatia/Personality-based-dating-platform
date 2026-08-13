package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestValidatorEmptyIsValid(t *testing.T) {
	v := NewValidator()
	if v.HasErrors() {
		t.Error("a fresh validator reports errors")
	}
	if err := v.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestValidatorCollectsEveryField(t *testing.T) {
	v := NewValidator()
	v.Add("bio", "too long")
	v.Add("gender", "unknown value")
	v.Require(false, "age_min", "must be at least 18")
	v.Require(true, "location", "never recorded")

	err := v.Err()
	if err == nil {
		t.Fatal("expected an error")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is %T, want *Error", err)
	}
	if apiErr.Status != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want %d", apiErr.Status, http.StatusUnprocessableEntity)
	}
	if apiErr.Code != CodeValidationFailed {
		t.Errorf("code = %q, want %q", apiErr.Code, CodeValidationFailed)
	}
	if len(apiErr.Details) != 3 {
		t.Fatalf("details = %v, want 3 entries so the client can fix everything at once", apiErr.Details)
	}
	if _, reported := apiErr.Details["location"]; reported {
		t.Error("Require(true) recorded a problem")
	}
	if apiErr.Details["age_min"] != "must be at least 18" {
		t.Errorf("age_min = %v", apiErr.Details["age_min"])
	}
}

func TestValidatorKeepsTheFirstMessagePerField(t *testing.T) {
	v := NewValidator()
	v.Add("bio", "first")
	v.Add("bio", "second")

	var apiErr *Error
	if !errors.As(v.Err(), &apiErr) {
		t.Fatal("expected an *Error")
	}
	if apiErr.Details["bio"] != "first" {
		t.Fatalf("bio = %v, want the first (most specific) message", apiErr.Details["bio"])
	}
	if len(apiErr.Details) != 1 {
		t.Fatalf("details = %v, want a single entry per field", apiErr.Details)
	}
}

func TestValidatorDetailsSerializeDeterministically(t *testing.T) {
	build := func() string {
		v := NewValidator()
		for _, f := range []string{"zeta", "alpha", "mu", "beta"} {
			v.Add(f, "invalid")
		}
		var apiErr *Error
		if !errors.As(v.Err(), &apiErr) {
			t.Fatal("expected an *Error")
		}
		raw, err := json.Marshal(apiErr)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(raw)
	}
	first := build()
	for i := 0; i < 20; i++ {
		if got := build(); got != first {
			t.Fatalf("validation details are not stable across runs:\n%s\n%s", first, got)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	cases := map[string]string{
		"USER@EXAMPLE.COM":     "user@example.com",
		"  user@example.com  ": "user@example.com",
		"User@Example.Com":     "user@example.com",
		"\tuser@example.com\n": "user@example.com",
		"":                     "",
	}
	for in, want := range cases {
		if got := NormalizeEmail(in); got != want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidEmail(t *testing.T) {
	valid := []string{
		"user@example.com",
		"user.name+tag@example.co.uk",
		"u@a.io",
		"user_name@sub.domain.example.com",
		"123@example.com",
	}
	for _, email := range valid {
		if !ValidEmail(email) {
			t.Errorf("ValidEmail(%q) = false, want true", email)
		}
	}

	invalid := []string{
		"",
		"user",
		"user@",
		"@example.com",
		"user@example",          // no dot in the domain
		"user@.com",             // domain starts with a dot
		"user@example.",         // domain ends with a dot
		"user@@example.com",     // two @
		"a@b@example.com",       // two @
		"Ada <ada@example.com>", // display-name form
		"user @example.com",     // space
		"user@exam ple.com",     // space in the domain
		strings.Repeat("a", 250) + "@example.com", // over 254 characters
	}
	for _, email := range invalid {
		if ValidEmail(email) {
			t.Errorf("ValidEmail(%q) = true, want false", email)
		}
	}
}

func TestValidEmailRejectsWhatNormalizeCannotFix(t *testing.T) {
	// The service normalizes before validating, so the pair must agree on case.
	if !ValidEmail(NormalizeEmail("  USER@EXAMPLE.COM  ")) {
		t.Fatal("a normalized uppercase address failed validation")
	}
}

func TestTrimmedRuneLen(t *testing.T) {
	cases := map[string]int{
		"":         0,
		"   ":      0,
		"abc":      3,
		"  abc  ":  3,
		"héllo":    5,
		"日本語":      3,
		"a\u0301":  2, // combining accent counts as its own rune
		"emoji: 👍": 8,
	}
	for in, want := range cases {
		if got := TrimmedRuneLen(in); got != want {
			t.Errorf("TrimmedRuneLen(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestCleanText(t *testing.T) {
	cases := map[string]string{
		"  hello  ":          "hello",
		"line1\nline2":       "line1\nline2", // newlines survive in multi-line text
		"tab\there":          "tab\there",
		"null\x00byte":       "nullbyte",
		"bell\x07":           "bell",
		"del\x7f":            "del",
		"\x1b[31mred\x1b[0m": "[31mred[0m", // ANSI escapes cannot forge log lines
		"":                   "",
		"  \n\n  ":           "",
		"日本語":                "日本語",
	}
	for in, want := range cases {
		if got := CleanText(in); got != want {
			t.Errorf("CleanText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanLine(t *testing.T) {
	cases := map[string]string{
		"  hello  ":          "hello",
		"line1\nline2":       "line1 line2",
		"line1\r\nline2":     "line1 line2",
		"tab\there":          "tab here",
		"lots    of   space": "lots of space",
		"null\x00byte":       "nullbyte",
		"\n\n":               "",
		"":                   "",
	}
	for in, want := range cases {
		if got := CleanLine(in); got != want {
			t.Errorf("CleanLine(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanLineNeverKeepsNewlines(t *testing.T) {
	// Single-line fields end up in log lines and one-line UI slots, so a newline
	// must never survive.
	for _, in := range []string{"a\nb", "a\r\nb", "a\rb", "\na", "a\n"} {
		if got := CleanLine(in); strings.ContainsAny(got, "\n\r") {
			t.Errorf("CleanLine(%q) = %q, which still contains a line break", in, got)
		}
	}
}
