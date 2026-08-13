package domain

import "strings"

// Gender is the canonical, storage-level gender token. Values are lowercase and
// stable: profiles store them, preferences filter on them, and the discovery
// query compares them directly, so they must never be localized or re-cased.
type Gender string

const (
	GenderMan       Gender = "man"
	GenderWoman     Gender = "woman"
	GenderNonbinary Gender = "nonbinary"
	GenderOther     Gender = "other"
)

// canonicalGenders is the ordered, exhaustive set exposed to clients.
var canonicalGenders = []Gender{GenderMan, GenderWoman, GenderNonbinary, GenderOther}

// genderAliases maps the spellings we accept from clients (and from the
// pre-existing seed data, which used "Male"/"Female"/"Non-binary") onto the
// canonical tokens.
var genderAliases = map[string]Gender{
	"man":         GenderMan,
	"male":        GenderMan,
	"m":           GenderMan,
	"woman":       GenderWoman,
	"female":      GenderWoman,
	"f":           GenderWoman,
	"nonbinary":   GenderNonbinary,
	"non-binary":  GenderNonbinary,
	"non binary":  GenderNonbinary,
	"nb":          GenderNonbinary,
	"enby":        GenderNonbinary,
	"genderqueer": GenderNonbinary,
	"other":       GenderOther,
}

// Genders returns the canonical set. The slice is a copy so callers cannot
// mutate the package state.
func Genders() []Gender {
	out := make([]Gender, len(canonicalGenders))
	copy(out, canonicalGenders)
	return out
}

// GenderStrings returns the canonical set as plain strings, for validation
// messages and API discovery responses.
func GenderStrings() []string {
	out := make([]string, len(canonicalGenders))
	for i, g := range canonicalGenders {
		out[i] = string(g)
	}
	return out
}

// NormalizeGender maps free-form client input onto a canonical token. The empty
// string means "not set" and normalizes to ("", true) so an unset profile gender
// is not treated as invalid.
func NormalizeGender(raw string) (Gender, bool) {
	key := strings.ToLower(strings.Join(strings.Fields(raw), " "))
	if key == "" {
		return "", true
	}
	if g, ok := genderAliases[key]; ok {
		return g, true
	}
	return "", false
}

// NormalizeGenderList canonicalizes a preference list, dropping duplicates and
// preserving the caller's ordering. It reports the first unrecognised value.
func NormalizeGenderList(raw []string) ([]string, string, bool) {
	seen := make(map[Gender]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		g, ok := NormalizeGender(item)
		if !ok || g == "" {
			return nil, item, false
		}
		if _, dup := seen[g]; dup {
			continue
		}
		seen[g] = struct{}{}
		out = append(out, string(g))
	}
	return out, "", true
}
