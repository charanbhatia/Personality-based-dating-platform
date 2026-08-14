package integration

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/bits-assignment/dating-platform/backend/internal/profile"
	"github.com/google/uuid"
)

type profileDTO struct {
	UserID          uuid.UUID `json:"user_id"`
	Name            string    `json:"name"`
	Bio             string    `json:"bio"`
	Gender          string    `json:"gender"`
	Location        string    `json:"location"`
	Interests       []string  `json:"interests"`
	PhotoURLs       []string  `json:"photo_urls"`
	PrimaryPhotoURL string    `json:"primary_photo_url"`
	PhotoURL        string    `json:"photo_url"`
	DateOfBirth     *string   `json:"date_of_birth"`
	Age             *int      `json:"age"`
}

func getProfile(t *testing.T, u *user) profileDTO {
	t.Helper()
	var dto profileDTO
	do(t, http.MethodGet, "/api/v1/profile", u.Token, nil).
		requireStatus(t, http.StatusOK).decode(t, &dto)
	return dto
}

func updateProfile(t *testing.T, u *user, body map[string]any) response {
	t.Helper()
	return do(t, http.MethodPut, "/api/v1/profile", u.Token, body)
}

func TestProfileStartsEmptyButWellFormed(t *testing.T) {
	resetDB(t)
	u := register(t, "Fresh User", "1994-05-05")

	dto := getProfile(t, u)
	if dto.UserID != u.ID {
		t.Errorf("user_id = %v, want %v", dto.UserID, u.ID)
	}
	if dto.Name != "Fresh User" {
		t.Errorf("name = %q, want the registered name", dto.Name)
	}
	// Empty collections must serialize as arrays, not null.
	if dto.Interests == nil || dto.PhotoURLs == nil {
		t.Errorf("interests = %v, photo_urls = %v; both should be empty arrays", dto.Interests, dto.PhotoURLs)
	}
	if dto.Age == nil {
		t.Error("age was not derived from the date of birth")
	}
}

func TestProfileUpdateMergesRatherThanReplaces(t *testing.T) {
	resetDB(t)
	u := register(t, "Merge User", "1994-05-05")

	var afterFirst profileDTO
	updateProfile(t, u, map[string]any{
		"gender":    "Female",
		"location":  "Bengaluru",
		"interests": []string{"coffee", "hiking"},
	}).requireStatus(t, http.StatusOK).decode(t, &afterFirst)

	if afterFirst.Gender != "woman" {
		t.Errorf("gender = %q, want the canonical %q", afterFirst.Gender, "woman")
	}

	// Sending only a bio must not clear the other fields.
	var afterSecond profileDTO
	updateProfile(t, u, map[string]any{"bio": "Just the bio."}).
		requireStatus(t, http.StatusOK).decode(t, &afterSecond)

	if afterSecond.Bio != "Just the bio." {
		t.Errorf("bio = %q", afterSecond.Bio)
	}
	if afterSecond.Gender != "woman" || afterSecond.Location != "Bengaluru" {
		t.Errorf("an omitted field was cleared: %+v", afterSecond)
	}
	if len(afterSecond.Interests) != 2 {
		t.Errorf("interests = %v, want the two previously saved", afterSecond.Interests)
	}
}

func TestProfileUpdateDistinguishesOmittedFromEmpty(t *testing.T) {
	resetDB(t)
	u := register(t, "Explicit User", "1994-05-05")
	updateProfile(t, u, map[string]any{"bio": "Something", "location": "Pune"}).
		requireStatus(t, http.StatusOK)

	// An explicit empty string is a clear, unlike an omitted field.
	var cleared profileDTO
	updateProfile(t, u, map[string]any{"bio": ""}).
		requireStatus(t, http.StatusOK).decode(t, &cleared)

	if cleared.Bio != "" {
		t.Errorf("bio = %q, want it cleared", cleared.Bio)
	}
	if cleared.Location != "Pune" {
		t.Errorf("location = %q, want it untouched", cleared.Location)
	}
}

func TestProfileNormalizesInput(t *testing.T) {
	resetDB(t)
	u := register(t, "Normalize", "1994-05-05")

	var dto profileDTO
	updateProfile(t, u, map[string]any{
		"bio":      "  Padded bio with a \x00null byte.  ",
		"location": "  Multi   space \n city  ",
		// Duplicates in different cases, plus blanks.
		"interests": []string{"Coffee", "coffee", "  COFFEE  ", "hiking", "", "   "},
	}).requireStatus(t, http.StatusOK).decode(t, &dto)

	if strings.Contains(dto.Bio, "\x00") {
		t.Error("a control character survived into the stored bio")
	}
	if dto.Bio != "Padded bio with a null byte." {
		t.Errorf("bio = %q", dto.Bio)
	}
	if strings.Contains(dto.Location, "\n") || strings.Contains(dto.Location, "  ") {
		t.Errorf("location = %q, want a single line with collapsed spaces", dto.Location)
	}
	// Deduplication is case-insensitive, but the spelling the user first typed is
	// what gets displayed back.
	if len(dto.Interests) != 2 {
		t.Fatalf("interests = %v, want duplicates and blanks removed", dto.Interests)
	}
	if dto.Interests[0] != "Coffee" {
		t.Errorf("interests[0] = %q, want the first spelling preserved", dto.Interests[0])
	}
}

func TestProfileUpdateValidation(t *testing.T) {
	resetDB(t)
	u := register(t, "Validation", "1994-05-05")

	tooLongBio := strings.Repeat("a", profile.MaxBioRunes+1)
	tooLongLocation := strings.Repeat("a", profile.MaxLocationRunes+1)
	tooManyInterests := make([]string, profile.MaxInterests+1)
	for i := range tooManyInterests {
		tooManyInterests[i] = fmt.Sprintf("interest-%d", i)
	}

	cases := []struct {
		name  string
		body  map[string]any
		field string
	}{
		{"bio too long", map[string]any{"bio": tooLongBio}, "bio"},
		{"location too long", map[string]any{"location": tooLongLocation}, "location"},
		{"unknown gender", map[string]any{"gender": "martian"}, "gender"},
		{"too many interests", map[string]any{"interests": tooManyInterests}, "interests"},
		{"interest too long", map[string]any{"interests": []string{strings.Repeat("a", profile.MaxInterestRunes+1)}}, "interests"},
		{"underage dob", map[string]any{"date_of_birth": "2015-01-01"}, "date_of_birth"},
		{"malformed dob", map[string]any{"date_of_birth": "05/05/1994"}, "date_of_birth"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := updateProfile(t, u, tc.body).requireStatus(t, http.StatusUnprocessableEntity)
			if _, ok := resp.Details()[tc.field]; !ok {
				t.Fatalf("no detail for %q: %s", tc.field, resp.Body)
			}
		})
	}
}

func TestProfileUpdateIsAtomicAcrossTables(t *testing.T) {
	resetDB(t)
	u := register(t, "Atomic", "1994-05-05")
	updateProfile(t, u, map[string]any{"bio": "Original bio"}).requireStatus(t, http.StatusOK)

	// The date of birth lives on users and the bio on profiles. A rejected age
	// must leave neither written.
	updateProfile(t, u, map[string]any{
		"bio":           "Should not be saved",
		"date_of_birth": "2015-01-01",
	}).requireStatus(t, http.StatusUnprocessableEntity)

	if got := getProfile(t, u).Bio; got != "Original bio" {
		t.Fatalf("bio = %q, want the update rolled back", got)
	}
}

func TestProfileAcceptsAValidDateOfBirthChange(t *testing.T) {
	resetDB(t)
	u := register(t, "Birthday", "1994-05-05")

	var dto profileDTO
	updateProfile(t, u, map[string]any{"date_of_birth": "1988-02-29"}).
		requireStatus(t, http.StatusOK).decode(t, &dto)

	if dto.DateOfBirth == nil || *dto.DateOfBirth != "1988-02-29" {
		t.Fatalf("date_of_birth = %v, want the leap-day value to round-trip", dto.DateOfBirth)
	}
	// /auth/me must agree.
	var me struct {
		User struct {
			DateOfBirth string `json:"date_of_birth"`
		} `json:"user"`
	}
	do(t, http.MethodGet, "/api/v1/auth/me", u.Token, nil).
		requireStatus(t, http.StatusOK).decode(t, &me)
	if me.User.DateOfBirth != "1988-02-29" {
		t.Fatalf("/auth/me reports %q, want the updated date", me.User.DateOfBirth)
	}
}

func TestPhotoGallery(t *testing.T) {
	resetDB(t)
	u := register(t, "Gallery", "1994-05-05")

	var dto profileDTO
	do(t, http.MethodPut, "/api/v1/profile/photos", u.Token, map[string]any{
		"photo_urls": []string{
			"https://cdn.example.test/first.jpg",
			"https://cdn.example.test/second.jpg",
		},
	}).requireStatus(t, http.StatusOK).decode(t, &dto)

	if len(dto.PhotoURLs) != 2 {
		t.Fatalf("photo_urls = %v, want 2 entries", dto.PhotoURLs)
	}
	// Order is significant: the first photo is the one the feed shows.
	if dto.PrimaryPhotoURL != "https://cdn.example.test/first.jpg" {
		t.Errorf("primary_photo_url = %q, want the first entry", dto.PrimaryPhotoURL)
	}
	if dto.PhotoURL != dto.PrimaryPhotoURL {
		t.Errorf("the legacy photo_url (%q) is out of sync with primary_photo_url (%q)",
			dto.PhotoURL, dto.PrimaryPhotoURL)
	}

	// Reordering must move the primary.
	do(t, http.MethodPut, "/api/v1/profile/photos", u.Token, map[string]any{
		"photo_urls": []string{
			"https://cdn.example.test/second.jpg",
			"https://cdn.example.test/first.jpg",
		},
	}).requireStatus(t, http.StatusOK).decode(t, &dto)
	if dto.PrimaryPhotoURL != "https://cdn.example.test/second.jpg" {
		t.Errorf("primary_photo_url = %q after reordering", dto.PrimaryPhotoURL)
	}

	// Clearing the gallery must clear both representations.
	do(t, http.MethodPut, "/api/v1/profile/photos", u.Token, map[string]any{
		"photo_urls": []string{},
	}).requireStatus(t, http.StatusOK).decode(t, &dto)
	if len(dto.PhotoURLs) != 0 || dto.PrimaryPhotoURL != "" || dto.PhotoURL != "" {
		t.Errorf("clearing left %+v", dto)
	}
}

func TestPhotoGalleryDeduplicatesPreservingOrder(t *testing.T) {
	resetDB(t)
	u := register(t, "Dedupe", "1994-05-05")

	var dto profileDTO
	do(t, http.MethodPut, "/api/v1/profile/photos", u.Token, map[string]any{
		"photo_urls": []string{
			"https://cdn.example.test/a.jpg",
			"https://cdn.example.test/b.jpg",
			"https://cdn.example.test/a.jpg",
		},
	}).requireStatus(t, http.StatusOK).decode(t, &dto)

	if len(dto.PhotoURLs) != 2 {
		t.Fatalf("photo_urls = %v, want duplicates removed", dto.PhotoURLs)
	}
	if dto.PhotoURLs[0] != "https://cdn.example.test/a.jpg" {
		t.Errorf("photo_urls = %v, want the first occurrence kept in place", dto.PhotoURLs)
	}
}

func TestPhotoGalleryRejectsUnsafeURLs(t *testing.T) {
	resetDB(t)
	u := register(t, "Unsafe", "1994-05-05")

	tooMany := make([]string, profile.MaxPhotos+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("https://cdn.example.test/%d.jpg", i)
	}

	// Anything that could become an <img src> executing script, or point off-origin
	// over plaintext, must be refused.
	cases := map[string][]string{
		"javascript scheme":     {"javascript:alert(1)"},
		"uppercase javascript":  {"JavaScript:alert(1)"},
		"data uri":              {"data:text/html;base64,PHNjcmlwdD4="},
		"vbscript scheme":       {"vbscript:msgbox(1)"},
		"plain http off origin": {"http://cdn.example.test/a.jpg"},
		"no scheme":             {"cdn.example.test/a.jpg"},
		"protocol relative":     {"//evil.example.test/a.jpg"},
		"not a url":             {"not a url at all"},
		"overlong":              {"https://cdn.example.test/" + strings.Repeat("a", profile.MaxPhotoURLLength) + ".jpg"},
		"too many":              tooMany,
	}
	for name, urls := range cases {
		t.Run(name, func(t *testing.T) {
			resp := do(t, http.MethodPut, "/api/v1/profile/photos", u.Token,
				map[string]any{"photo_urls": urls})
			if resp.Status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body: %s)", resp.Status, resp.Body)
			}
		})
	}

	// None of the rejections may have partially applied.
	if got := getProfile(t, u).PhotoURLs; len(got) != 0 {
		t.Fatalf("photo_urls = %v, want the gallery untouched", got)
	}
}

func TestPhotoGalleryAcceptsSameOriginAndLoopbackURLs(t *testing.T) {
	resetDB(t)
	u := register(t, "Allowed", "1994-05-05")

	// Relative paths are how Person C's media service will serve uploads, and
	// loopback HTTP is needed to run the stack locally.
	cases := map[string]string{
		"same-origin path": "/media/photo.jpg",
		"loopback http":    "http://localhost:9000/bucket/photo.jpg",
		"https":            "https://cdn.example.test/photo.jpg",
	}
	for name, url := range cases {
		t.Run(name, func(t *testing.T) {
			var dto profileDTO
			do(t, http.MethodPut, "/api/v1/profile/photos", u.Token,
				map[string]any{"photo_urls": []string{url}}).
				requireStatus(t, http.StatusOK).decode(t, &dto)
			if len(dto.PhotoURLs) != 1 || dto.PhotoURLs[0] != url {
				t.Fatalf("photo_urls = %v, want [%s]", dto.PhotoURLs, url)
			}
		})
	}
}

func TestPhotoGalleryDropsBlankEntries(t *testing.T) {
	resetDB(t)
	u := register(t, "Blanks", "1994-05-05")

	// Blank entries are dropped rather than rejected, matching how interests
	// treat empty strings.
	var dto profileDTO
	do(t, http.MethodPut, "/api/v1/profile/photos", u.Token, map[string]any{
		"photo_urls": []string{"", "  ", "https://cdn.example.test/real.jpg"},
	}).requireStatus(t, http.StatusOK).decode(t, &dto)

	if len(dto.PhotoURLs) != 1 || dto.PhotoURLs[0] != "https://cdn.example.test/real.jpg" {
		t.Fatalf("photo_urls = %v, want only the real entry", dto.PhotoURLs)
	}
}

func TestPhotoGalleryRequiresExactlyOneSource(t *testing.T) {
	resetDB(t)
	u := register(t, "Sources", "1994-05-05")

	do(t, http.MethodPut, "/api/v1/profile/photos", u.Token, map[string]any{}).
		requireStatus(t, http.StatusBadRequest)
	do(t, http.MethodPut, "/api/v1/profile/photos", u.Token, map[string]any{
		"photo_urls": []string{"https://cdn.example.test/a.jpg"},
		"asset_ids":  []string{uuid.NewString()},
	}).requireStatus(t, http.StatusBadRequest)
}

func TestPhotoAssetIDsRejectUnknownAssets(t *testing.T) {
	resetDB(t)
	u := register(t, "Assets", "1994-05-05")

	do(t, http.MethodPut, "/api/v1/profile/photos", u.Token,
		map[string]any{"asset_ids": []string{uuid.NewString()}}).
		requireError(t, http.StatusNotFound, "not_found")
}

func TestPhotoAssetIDsResolveReadyOwnedAssets(t *testing.T) {
	resetDB(t)
	u := register(t, "Gallery", "1994-05-05")
	other := register(t, "Other", "1994-05-05")

	ready := insertReadyAsset(t, u.ID, "https://cdn.example.test/from-asset.jpg")
	foreign := insertReadyAsset(t, other.ID, "https://cdn.example.test/foreign.jpg")
	pending := insertAsset(t, u.ID, "pending", "")

	var dto profileDTO
	do(t, http.MethodPut, "/api/v1/profile/photos", u.Token,
		map[string]any{"asset_ids": []string{ready.String()}}).
		requireStatus(t, http.StatusOK).decode(t, &dto)
	if len(dto.PhotoURLs) != 1 || dto.PhotoURLs[0] != "https://cdn.example.test/from-asset.jpg" {
		t.Fatalf("photo_urls = %v, want the resolved asset URL", dto.PhotoURLs)
	}

	do(t, http.MethodPut, "/api/v1/profile/photos", u.Token,
		map[string]any{"asset_ids": []string{foreign.String()}}).
		requireError(t, http.StatusNotFound, "not_found")
	do(t, http.MethodPut, "/api/v1/profile/photos", u.Token,
		map[string]any{"asset_ids": []string{pending.String()}}).
		requireError(t, http.StatusConflict, profile.CodeAssetNotReady)
}

func TestProfileOptionsDescribeTheContract(t *testing.T) {
	resetDB(t)
	u := register(t, "Options", "1994-05-05")

	var options struct {
		Genders []string       `json:"genders"`
		Limits  map[string]int `json:"limits"`
	}
	do(t, http.MethodGet, "/api/v1/profile/options", u.Token, nil).
		requireStatus(t, http.StatusOK).decode(t, &options)

	if len(options.Genders) == 0 {
		t.Fatal("no genders returned; the client would have to hardcode them")
	}
	// Every advertised value must actually be accepted.
	for _, gender := range options.Genders {
		updateProfile(t, u, map[string]any{"gender": gender}).requireStatus(t, http.StatusOK)
	}
	if options.Limits["bio_max_length"] != profile.MaxBioRunes {
		t.Errorf("bio_max_length = %d, want %d", options.Limits["bio_max_length"], profile.MaxBioRunes)
	}
	if options.Limits["max_photos"] != profile.MaxPhotos {
		t.Errorf("max_photos = %d, want %d", options.Limits["max_photos"], profile.MaxPhotos)
	}
}

func TestProfileUpdateCannotChangeAnotherUser(t *testing.T) {
	resetDB(t)
	victim := onboard(t, onboardOptions{Name: "Victim"})
	attacker := register(t, "Attacker", "1994-05-05")

	// The profile routes take no id: they always act on the token's subject.
	updateProfile(t, attacker, map[string]any{"bio": "attacker bio"}).requireStatus(t, http.StatusOK)

	if got := getProfile(t, victim).Bio; got == "attacker bio" {
		t.Fatal("one user's update modified another's profile")
	}
}

func insertReadyAsset(t *testing.T, userID uuid.UUID, url string) uuid.UUID {
	t.Helper()
	return insertAsset(t, userID, "ready", url)
}

func insertAsset(t *testing.T, userID uuid.UUID, status, originalURL string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO media_assets (id, user_id, status, content_type, original_key, original_url, thumb_url)
		VALUES ($1, $2, $3, 'image/jpeg', $4, NULLIF($5, ''), NULLIF($5, ''))`,
		id, userID, status, "public/users/"+userID.String()+"/"+id.String()+"/original", originalURL)
	if err != nil {
		t.Fatalf("insert media asset: %v", err)
	}
	return id
}
