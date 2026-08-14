package domain

import (
	"strings"
	"testing"
)

func TestNormalizeGender(t *testing.T) {
	cases := []struct {
		in     string
		want   Gender
		wantOK bool
	}{
		{"man", GenderMan, true},
		{"Male", GenderMan, true},
		{"  MALE  ", GenderMan, true},
		{"M", GenderMan, true},
		{"woman", GenderWoman, true},
		{"Female", GenderWoman, true},
		{"f", GenderWoman, true},
		{"Non-binary", GenderNonbinary, true},
		{"non binary", GenderNonbinary, true},
		{"non   binary", GenderNonbinary, true},
		{"NB", GenderNonbinary, true},
		{"enby", GenderNonbinary, true},
		{"Other", GenderOther, true},
		// Empty means "not set" rather than invalid, so an unfinished profile
		// does not fail validation.
		{"", "", true},
		{"   ", "", true},
		{"\t\n", "", true},
		{"martian", "", false},
		{"male female", "", false},
		{"m.", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := NormalizeGender(tc.in)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("NormalizeGender(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestNormalizeGenderProducesCanonicalValuesOnly(t *testing.T) {
	canonical := make(map[Gender]bool, len(canonicalGenders))
	for _, g := range Genders() {
		canonical[g] = true
	}
	for alias := range genderAliases {
		got, ok := NormalizeGender(alias)
		if !ok {
			t.Fatalf("alias %q does not normalize", alias)
		}
		if !canonical[got] {
			t.Fatalf("alias %q maps to %q, which is not in the canonical set; the CHECK constraint would reject it", alias, got)
		}
		if got != Gender(strings.ToLower(string(got))) {
			t.Fatalf("canonical value %q is not lowercase", got)
		}
	}
}

func TestNormalizeGenderList(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		want    []string
		wantBad string
		wantOK  bool
	}{
		{"canonical passthrough", []string{"man", "woman"}, []string{"man", "woman"}, "", true},
		{"aliases", []string{"Male", "Non-binary"}, []string{"man", "nonbinary"}, "", true},
		{"deduplicates across aliases", []string{"man", "Male", "M"}, []string{"man"}, "", true},
		{"preserves caller ordering", []string{"other", "woman", "man"}, []string{"other", "woman", "man"}, "", true},
		{"empty input", nil, []string{}, "", true},
		{"rejects unknown", []string{"man", "martian"}, nil, "martian", false},
		// An empty entry is meaningless inside a preference list, unlike a
		// profile's unset gender.
		{"rejects blank entry", []string{"man", ""}, nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, bad, ok := NormalizeGenderList(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				if bad != tc.wantBad {
					t.Fatalf("rejected value = %q, want %q", bad, tc.wantBad)
				}
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestGendersReturnsACopy(t *testing.T) {
	list := Genders()
	list[0] = "tampered"
	if Genders()[0] != GenderMan {
		t.Fatal("Genders() exposed the package slice")
	}
	strs := GenderStrings()
	strs[0] = "tampered"
	if GenderStrings()[0] != string(GenderMan) {
		t.Fatal("GenderStrings() exposed the package slice")
	}
}

func TestGenderStringsMatchesGenders(t *testing.T) {
	genders := Genders()
	strs := GenderStrings()
	if len(genders) != len(strs) {
		t.Fatalf("GenderStrings() has %d entries, Genders() has %d", len(strs), len(genders))
	}
	for i := range genders {
		if strs[i] != string(genders[i]) {
			t.Fatalf("entry %d: %q != %q", i, strs[i], genders[i])
		}
	}
}
