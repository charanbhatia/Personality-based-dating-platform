package httpx

import (
	"net/http"
	"net/mail"
	"sort"
	"strings"
	"unicode/utf8"
)

// Validator accumulates field-level problems so a client sees every issue in
// one response instead of fixing them one round-trip at a time.
type Validator struct {
	fields map[string]string
	order  []string
}

func NewValidator() *Validator {
	return &Validator{fields: make(map[string]string)}
}

// Add records a problem for field. The first message for a field wins, which
// keeps the most specific check (usually the earliest) as the reported cause.
func (v *Validator) Add(field, message string) {
	if _, exists := v.fields[field]; exists {
		return
	}
	v.fields[field] = message
	v.order = append(v.order, field)
}

// Require records message when ok is false.
func (v *Validator) Require(ok bool, field, message string) {
	if !ok {
		v.Add(field, message)
	}
}

func (v *Validator) HasErrors() bool { return len(v.fields) > 0 }

// Err returns nil when valid, otherwise a 422 with per-field details.
func (v *Validator) Err() error {
	if !v.HasErrors() {
		return nil
	}
	details := make(map[string]any, len(v.fields))
	names := make([]string, len(v.order))
	copy(names, v.order)
	sort.Strings(names)
	for _, f := range names {
		details[f] = v.fields[f]
	}
	return newError(http.StatusUnprocessableEntity, CodeValidationFailed,
		"one or more fields are invalid").WithDetails(details)
}

// NormalizeEmail lowercases and trims an address for storage and lookup.
func NormalizeEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// ValidEmail applies a deliberately conservative check: parseable by net/mail,
// exactly one "@", a dot-bearing domain and no display-name form.
func ValidEmail(email string) bool {
	if email == "" || len(email) > 254 {
		return false
	}
	if strings.Count(email, "@") != 1 {
		return false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return false
	}
	domain := email[strings.LastIndex(email, "@")+1:]
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}
	local := email[:strings.Index(email, "@")]
	return local != ""
}

// TrimmedRuneLen measures user-visible length after trimming, so a 500-char
// limit means 500 characters rather than 500 bytes.
func TrimmedRuneLen(s string) int {
	return utf8.RuneCountInString(strings.TrimSpace(s))
}

// CleanText trims surrounding whitespace and strips control characters that
// would corrupt logs or render oddly in the UI.
func CleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// CleanLine is CleanText for single-line values: newlines and tabs collapse to
// spaces.
func CleanLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}
