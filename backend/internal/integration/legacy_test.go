// Legacy coverage pins the pre-v1 /api endpoints the current SPA still calls.
//
// These now delegate to the same services as /api/v1, so the risk is not missing
// logic but a changed response shape. Each test asserts on the field names the
// existing frontend reads, so a refactor of the domain services cannot silently
// break it before Person A migrates.
package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestLegacyRegisterReturnsTheTokenFieldTheSPAReads(t *testing.T) {
	resetDB(t)
	email := uniqueEmail("legacy")

	resp := do(t, http.MethodPost, "/api/auth/register", "", map[string]any{
		"email":    email,
		"password": "correct horse battery",
		"name":     "Legacy User",
	}).requireStatus(t, http.StatusCreated)

	var body struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"user"`
		Token        string `json:"token"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	resp.decode(t, &body)

	if body.Token == "" {
		t.Error("token is empty; the SPA stores this field and would log the user straight out")
	}
	if body.Token != body.AccessToken {
		t.Error("token and access_token differ; the legacy field must alias the v1 access token")
	}
	// The refresh token is offered so a client can migrate to rotation without a
	// coordinated release.
	if body.RefreshToken == "" {
		t.Error("refresh_token is empty")
	}
	if body.User.ID == "" || body.User.Email != strings.ToLower(email) || body.User.Name != "Legacy User" {
		t.Errorf("user = %+v, want the created account", body.User)
	}

	// The legacy form collects no date of birth, and that must not block signup.
	login := do(t, http.MethodPost, "/api/auth/login", "", map[string]any{
		"email": email, "password": "correct horse battery",
	}).requireStatus(t, http.StatusOK)
	var loginBody struct {
		Token string `json:"token"`
	}
	login.decode(t, &loginBody)
	if loginBody.Token == "" {
		t.Error("login token is empty")
	}
}

func TestLegacyTokenWorksOnV1Endpoints(t *testing.T) {
	resetDB(t)
	email := uniqueEmail("legacy")
	resp := do(t, http.MethodPost, "/api/auth/register", "", map[string]any{
		"email": email, "password": "correct horse battery", "name": "Crossover",
	}).requireStatus(t, http.StatusCreated)
	var body struct {
		Token string `json:"token"`
	}
	resp.decode(t, &body)

	// One token namespace across both mounts: the SPA can migrate endpoint by
	// endpoint instead of all at once.
	do(t, http.MethodGet, "/api/v1/profile", body.Token, nil).requireStatus(t, http.StatusOK)
	do(t, http.MethodGet, "/api/v1/preferences", body.Token, nil).requireStatus(t, http.StatusOK)
}

func TestLegacyMeInlinesUserFields(t *testing.T) {
	resetDB(t)
	u := register(t, "Inline", "1994-05-05")

	resp := do(t, http.MethodGet, "/api/auth/me", u.Token, nil).requireStatus(t, http.StatusOK)

	// The old shape put the user at the top level, not under a "user" key.
	var raw map[string]json.RawMessage
	resp.decode(t, &raw)
	for _, field := range []string{"id", "email", "name", "onboarding"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("field %q missing from /api/auth/me (body: %s)", field, resp.Body)
		}
	}
	if _, nested := raw["user"]; nested {
		t.Error("user is nested; the legacy endpoint must inline it at the top level")
	}

	var body struct {
		ID         string          `json:"id"`
		Email      string          `json:"email"`
		Onboarding map[string]bool `json:"onboarding"`
	}
	resp.decode(t, &body)
	if body.ID != u.ID.String() {
		t.Errorf("id = %q, want %q", body.ID, u.ID)
	}

	// Decoding into named fields would hide a rename, since an absent key reads
	// as false and every flag is expected to be false here.
	want := []string{"quiz_done", "preferences_done", "profile_done", "photos_done"}
	if len(body.Onboarding) != len(want) {
		t.Errorf("onboarding = %v, want exactly %v", body.Onboarding, want)
	}
	for _, flag := range want {
		if _, ok := body.Onboarding[flag]; !ok {
			t.Errorf("onboarding is missing %q (got %v)", flag, body.Onboarding)
		}
		if body.Onboarding[flag] {
			t.Errorf("%s = true on a fresh account", flag)
		}
	}

	do(t, http.MethodGet, "/api/auth/me", "", nil).requireStatus(t, http.StatusUnauthorized)
}

func TestLegacyProfileUpdateReplacesFields(t *testing.T) {
	resetDB(t)
	u := register(t, "Replacer", "1994-05-05")

	var dto profileDTO
	do(t, http.MethodPut, "/api/profile", u.Token, map[string]any{
		"bio":       "First bio",
		"gender":    "Male",
		"location":  "Pune",
		"photo_url": "https://cdn.example.test/one.jpg",
	}).requireStatus(t, http.StatusOK).decode(t, &dto)

	if dto.Bio != "First bio" || dto.Location != "Pune" {
		t.Fatalf("profile = %+v, want the posted values", dto)
	}
	// Gender is canonicalised even on the legacy path, so the value discovery
	// filters on is the same whichever endpoint wrote it.
	if dto.Gender != "man" {
		t.Errorf("gender = %q, want %q", dto.Gender, "man")
	}
	// photo_url must be mirrored into the gallery, or the v1 profile page would
	// show nothing after a legacy save.
	if dto.PrimaryPhotoURL != "https://cdn.example.test/one.jpg" {
		t.Errorf("primary_photo_url = %q, want the posted photo", dto.PrimaryPhotoURL)
	}
	if len(dto.PhotoURLs) != 1 || dto.PhotoURLs[0] != "https://cdn.example.test/one.jpg" {
		t.Errorf("photo_urls = %v, want the posted photo mirrored", dto.PhotoURLs)
	}

	// The legacy form posts every field, so a blank one means "clear it" rather
	// than "leave it alone" — the opposite of the v1 merge semantics.
	var cleared profileDTO
	do(t, http.MethodPut, "/api/profile", u.Token, map[string]any{
		"bio": "Second bio", "gender": "", "location": "", "photo_url": "",
	}).requireStatus(t, http.StatusOK).decode(t, &cleared)

	if cleared.Bio != "Second bio" {
		t.Errorf("bio = %q, want it replaced", cleared.Bio)
	}
	if cleared.Gender != "" || cleared.Location != "" {
		t.Errorf("gender/location = %q/%q, want both cleared", cleared.Gender, cleared.Location)
	}
	if cleared.PrimaryPhotoURL != "" || len(cleared.PhotoURLs) != 0 {
		t.Errorf("photos = %v/%q, want cleared", cleared.PhotoURLs, cleared.PrimaryPhotoURL)
	}

	// Both mounts read the same row.
	if v1 := getProfile(t, u); v1.Bio != "Second bio" {
		t.Errorf("/api/v1/profile bio = %q, want the legacy write to be visible", v1.Bio)
	}
}

func TestLegacyProfileRejectsTheSameBadInputAsV1(t *testing.T) {
	resetDB(t)
	u := register(t, "Strict", "1994-05-05")

	cases := map[string]map[string]any{
		"unknown gender":  {"gender": "martian"},
		"bio too long":    {"bio": strings.Repeat("a", 5000)},
		"unsafe photo":    {"photo_url": "javascript:alert(1)"},
		"overlong bio":    {"bio": strings.Repeat("x", 100000)},
		"bad location":    {"location": strings.Repeat("l", 5000)},
		"protocol photo":  {"photo_url": "//evil.example.test/a.jpg"},
		"data uri  photo": {"photo_url": "data:text/html,<script>"},
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			do(t, http.MethodPut, "/api/profile", u.Token, body).
				requireStatus(t, http.StatusUnprocessableEntity)
		})
	}
}

func TestLegacyMatchesNeverExposeEmail(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer", Level: LevelMiddle})
	target := onboard(t, onboardOptions{Name: "Target", Level: LevelHighest})

	// The original implementation selected u.email into this payload, handing
	// every address to any authenticated caller.
	list := do(t, http.MethodGet, "/api/matches", viewer.Token, nil).
		requireStatus(t, http.StatusOK)
	if strings.Contains(strings.ToLower(string(list.Body)), "@") {
		t.Errorf("/api/matches body contains an address: %s", list.Body)
	}

	var listBody struct {
		Matches []struct {
			UserID string  `json:"user_id"`
			Name   string  `json:"name"`
			Score  float64 `json:"score"`
			Email  string  `json:"email"`
		} `json:"matches"`
	}
	list.decode(t, &listBody)
	if len(listBody.Matches) == 0 {
		t.Fatal("matches is empty; the shim should still return candidates")
	}
	for _, m := range listBody.Matches {
		if m.Email != "" {
			t.Errorf("candidate %s leaked email %q", m.UserID, m.Email)
		}
		if m.Score < 0 || m.Score > 1 {
			t.Errorf("score = %v for %s, want within [0, 1]", m.Score, m.Name)
		}
	}

	single := do(t, http.MethodGet, "/api/matches/"+target.ID.String(), viewer.Token, nil).
		requireStatus(t, http.StatusOK)
	if strings.Contains(string(single.Body), "@") {
		t.Errorf("/api/matches/{id} body contains an address: %s", single.Body)
	}
}

func TestLegacyMatchesAdvertiseTheirSuccessor(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer", Level: LevelMiddle})
	target := onboard(t, onboardOptions{Name: "Target", Level: LevelMiddle})

	cases := map[string]string{
		"/api/matches":                       "/api/v1/discover",
		"/api/matches/" + target.ID.String(): "/api/v1/users/{id}/public",
	}
	for path, successor := range cases {
		t.Run(path, func(t *testing.T) {
			headers := do(t, http.MethodGet, path, viewer.Token, nil).
				requireStatus(t, http.StatusOK).Headers
			if headers.Get("Deprecation") != "true" {
				t.Errorf("Deprecation header = %q, want %q", headers.Get("Deprecation"), "true")
			}
			if !strings.Contains(headers.Get("Link"), successor) {
				t.Errorf("Link header = %q, want it to name %q", headers.Get("Link"), successor)
			}
		})
	}
}

func TestLegacyMatchesRejectBadPaging(t *testing.T) {
	resetDB(t)
	viewer := onboard(t, onboardOptions{Name: "Viewer", Level: LevelMiddle})

	for _, query := range []string{"?limit=0", "?limit=-1", "?limit=abc", "?limit=1000", "?offset=-1", "?offset=abc"} {
		t.Run(query, func(t *testing.T) {
			do(t, http.MethodGet, "/api/matches"+query, viewer.Token, nil).
				requireStatus(t, http.StatusBadRequest)
		})
	}
}

func TestLegacyEndpointsRequireAuthentication(t *testing.T) {
	resetDB(t)
	cases := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/auth/me", nil},
		{http.MethodGet, "/api/profile", nil},
		{http.MethodPut, "/api/profile", map[string]any{"bio": "hi"}},
		{http.MethodGet, "/api/matches", nil},
		{http.MethodGet, "/api/matches/" + uuid.NewString(), nil},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			do(t, tc.method, tc.path, "", tc.body).requireStatus(t, http.StatusUnauthorized)
		})
	}
}

func TestUnknownRoutesReturnTheStandardEnvelope(t *testing.T) {
	resetDB(t)

	// mux's plain-text defaults would break a client that always parses JSON.
	do(t, http.MethodGet, "/api/v1/does-not-exist", "", nil).
		requireError(t, http.StatusNotFound, "not_found")
	do(t, http.MethodGet, "/api/nope", "", nil).
		requireError(t, http.StatusNotFound, "not_found")
}

func TestWrongMethodReports405WithAllow(t *testing.T) {
	resetDB(t)

	// A wrong verb on a real endpoint must be distinguishable from a wrong path,
	// including on the subrouter mounts.
	cases := map[string]struct {
		method, path string
		wantAllow    []string
	}{
		"v1 profile":     {http.MethodDelete, "/api/v1/profile", []string{"GET", "PUT"}},
		"legacy profile": {http.MethodDelete, "/api/profile", []string{"GET", "PUT"}},
		"health":         {http.MethodPost, "/health", []string{"GET"}},
		"login":          {http.MethodGet, "/api/v1/auth/login", []string{"POST"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resp := do(t, tc.method, tc.path, "", nil).
				requireError(t, http.StatusMethodNotAllowed, "method_not_allowed")
			allow := resp.Headers.Get("Allow")
			for _, want := range tc.wantAllow {
				if !strings.Contains(allow, want) {
					t.Errorf("Allow = %q, want it to include %q", allow, want)
				}
			}
			if strings.Contains(allow, tc.method) {
				t.Errorf("Allow = %q, must not include the rejected method %q", allow, tc.method)
			}
		})
	}
}

func TestHealthCheckIsUnauthenticated(t *testing.T) {
	var body struct {
		Status string `json:"status"`
	}
	do(t, http.MethodGet, "/health", "", nil).
		requireStatus(t, http.StatusOK).decode(t, &body)
	if body.Status != "ok" {
		t.Errorf("status = %q, want %q", body.Status, "ok")
	}
}
