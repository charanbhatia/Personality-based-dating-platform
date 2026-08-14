package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/bits-assignment/dating-platform/backend/internal/auth"
	"github.com/google/uuid"
)

func TestRegisterCreatesAUsableAccount(t *testing.T) {
	resetDB(t)
	email := uniqueEmail("register")

	resp := do(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":         strings.ToUpper(email),
		"password":      defaultPassword,
		"name":          "  Ada Lovelace  ",
		"date_of_birth": "1990-12-10",
	}).requireStatus(t, http.StatusCreated)

	var result authResult
	resp.decode(t, &result)

	if result.User.Email != email {
		t.Errorf("email = %q, want the lowercased %q", result.User.Email, email)
	}
	if result.User.Name != "Ada Lovelace" {
		t.Errorf("name = %q, want it trimmed", result.User.Name)
	}
	if result.User.Age == nil || *result.User.Age < 18 {
		t.Errorf("age = %v, want a computed value", result.User.Age)
	}
	if result.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", result.TokenType)
	}
	if result.ExpiresIn <= 0 {
		t.Errorf("expires_in = %d, want a positive TTL", result.ExpiresIn)
	}
	if result.SessionID == uuid.Nil {
		t.Error("session_id is nil; the refresh token is not bound to a session")
	}
	if result.User.EmailVerified {
		t.Error("a new account must start unverified")
	}

	// Registration must also create the dependent rows the other domains read.
	if got := countRows(t, `SELECT count(*) FROM profiles WHERE user_id = $1`, result.User.ID); got != 1 {
		t.Errorf("profiles rows = %d, want 1", got)
	}
	if got := countRows(t, `SELECT count(*) FROM preferences WHERE user_id = $1`, result.User.ID); got != 1 {
		t.Errorf("preferences rows = %d, want 1", got)
	}
	if got := countRows(t, `SELECT count(*) FROM auth_sessions WHERE user_id = $1`, result.User.ID); got != 1 {
		t.Errorf("auth_sessions rows = %d, want 1", got)
	}

	// The returned token must work immediately.
	do(t, http.MethodGet, "/api/v1/auth/me", result.AccessToken, nil).requireStatus(t, http.StatusOK)
}

func TestRegisterStoresOnlyAHashedPassword(t *testing.T) {
	resetDB(t)
	u := register(t, "Hash Check", "1990-01-01")

	var stored string
	err := testPool.QueryRow(context.Background(),
		`SELECT password_hash FROM users WHERE id = $1`, u.ID).Scan(&stored)
	if err != nil {
		t.Fatalf("load password hash: %v", err)
	}
	if stored == defaultPassword || strings.Contains(stored, defaultPassword) {
		t.Fatal("the plaintext password is recoverable from the users table")
	}
	if !strings.HasPrefix(stored, "$2") {
		t.Fatalf("password hash %q is not a bcrypt hash", stored)
	}
	if !auth.CheckPassword(stored, defaultPassword) {
		t.Fatal("the stored hash does not verify the original password")
	}
}

func TestRegisterRejectsDuplicateEmailRegardlessOfCase(t *testing.T) {
	resetDB(t)
	u := register(t, "First", "1990-01-01")

	for _, variant := range []string{u.Email, strings.ToUpper(u.Email), "  " + u.Email + "  "} {
		do(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
			"email":         variant,
			"password":      defaultPassword,
			"name":          "Second",
			"date_of_birth": "1990-01-01",
		}).requireError(t, http.StatusConflict, "email_already_registered")
	}
}

func TestRegisterValidation(t *testing.T) {
	resetDB(t)
	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing email", map[string]any{"password": defaultPassword, "name": "X", "date_of_birth": "1990-01-01"}},
		{"malformed email", map[string]any{"email": "not-an-email", "password": defaultPassword, "name": "X", "date_of_birth": "1990-01-01"}},
		{"short password", map[string]any{"email": uniqueEmail("v"), "password": "short", "name": "X", "date_of_birth": "1990-01-01"}},
		{"missing name", map[string]any{"email": uniqueEmail("v"), "password": defaultPassword, "date_of_birth": "1990-01-01"}},
		{"missing dob", map[string]any{"email": uniqueEmail("v"), "password": defaultPassword, "name": "X"}},
		{"malformed dob", map[string]any{"email": uniqueEmail("v"), "password": defaultPassword, "name": "X", "date_of_birth": "10-12-1990"}},
		{"future dob", map[string]any{"email": uniqueEmail("v"), "password": defaultPassword, "name": "X", "date_of_birth": "2999-01-01"}},
		{"underage", map[string]any{"email": uniqueEmail("v"), "password": defaultPassword, "name": "X", "date_of_birth": "2015-01-01"}},
		{"implausibly old", map[string]any{"email": uniqueEmail("v"), "password": defaultPassword, "name": "X", "date_of_birth": "1080-01-01"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := do(t, http.MethodPost, "/api/v1/auth/register", "", tc.body)
			if resp.Status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body: %s)", resp.Status, resp.Body)
			}
			if len(resp.Details()) == 0 {
				t.Fatalf("no per-field details returned: %s", resp.Body)
			}
		})
	}
}

func TestRegisterRejectsMalformedBodies(t *testing.T) {
	resetDB(t)
	cases := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"empty", "", http.StatusBadRequest},
		{"truncated", `{"email":`, http.StatusBadRequest},
		{"array", `[]`, http.StatusBadRequest},
		{"two objects", `{}{}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, testServer.URL+"/api/v1/auth/register",
				strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := testServer.Client().Do(req)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
		})
	}
}

func TestLogin(t *testing.T) {
	resetDB(t)
	u := register(t, "Login User", "1990-01-01")

	t.Run("succeeds with the right password", func(t *testing.T) {
		result := login(t, u.Email, defaultPassword)
		if result.AccessToken == "" {
			t.Fatal("no access token returned")
		}
		if result.SessionID == u.Session {
			t.Fatal("login reused the registration session; each sign-in must get its own")
		}
	})

	t.Run("is case insensitive on email", func(t *testing.T) {
		login(t, strings.ToUpper(u.Email), defaultPassword)
	})

	t.Run("rejects a wrong password", func(t *testing.T) {
		do(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": u.Email, "password": defaultPassword + "x",
		}).requireError(t, http.StatusUnauthorized, "invalid_credentials")
	})

	t.Run("does not reveal whether an address is registered", func(t *testing.T) {
		known := do(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": u.Email, "password": "definitely-wrong",
		})
		unknown := do(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": uniqueEmail("ghost"), "password": "definitely-wrong",
		})
		if known.Status != unknown.Status || known.Code() != unknown.Code() {
			t.Fatalf("a wrong password (%d/%s) and an unknown account (%d/%s) are distinguishable",
				known.Status, known.Code(), unknown.Status, unknown.Code())
		}
	})
}

func TestMeReportsOnboardingProgress(t *testing.T) {
	resetDB(t)
	u := register(t, "Onboarding", "1993-03-03")

	type meResponse struct {
		User struct {
			ID uuid.UUID `json:"id"`
		} `json:"user"`
		Onboarding struct {
			QuizDone        bool `json:"quiz_done"`
			PreferencesDone bool `json:"preferences_done"`
			ProfileDone     bool `json:"profile_done"`
			PhotosDone      bool `json:"photos_done"`
		} `json:"onboarding"`
	}

	fetch := func() meResponse {
		var out meResponse
		do(t, http.MethodGet, "/api/v1/auth/me", u.Token, nil).
			requireStatus(t, http.StatusOK).decode(t, &out)
		return out
	}

	initial := fetch()
	if initial.User.ID != u.ID {
		t.Fatalf("user id = %v, want %v", initial.User.ID, u.ID)
	}
	if initial.Onboarding.QuizDone || initial.Onboarding.PreferencesDone ||
		initial.Onboarding.PhotosDone {
		t.Fatalf("a fresh account reports progress: %+v", initial.Onboarding)
	}

	submitAssessment(t, u, 3)
	if !fetch().Onboarding.QuizDone {
		t.Error("quiz_done is still false after submitting the assessment")
	}

	setPreferences(t, u, map[string]any{"age_min": 25, "age_max": 45, "genders": []string{"woman"}})
	if !fetch().Onboarding.PreferencesDone {
		t.Error("preferences_done is still false after saving preferences")
	}

	setProfile(t, u, map[string]any{"gender": "man", "bio": "Hello"})
	if !fetch().Onboarding.ProfileDone {
		t.Error("profile_done is still false after completing the profile")
	}

	setPhotos(t, u, "https://cdn.example.test/a.jpg")
	final := fetch()
	if !final.Onboarding.PhotosDone {
		t.Error("photos_done is still false after adding a photo")
	}
	if !final.Onboarding.QuizDone || !final.Onboarding.PreferencesDone || !final.Onboarding.ProfileDone {
		t.Errorf("earlier flags regressed: %+v", final.Onboarding)
	}
}

func TestAuthenticationIsRequired(t *testing.T) {
	resetDB(t)
	u := onboard(t, onboardOptions{Name: "Protected"})

	protected := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/api/v1/auth/me", nil},
		{http.MethodGet, "/api/v1/auth/sessions", nil},
		{http.MethodPost, "/api/v1/auth/logout", nil},
		{http.MethodGet, "/api/v1/profile", nil},
		{http.MethodPut, "/api/v1/profile", map[string]any{"bio": "x"}},
		{http.MethodPut, "/api/v1/profile/photos", map[string]any{"photo_urls": []string{}}},
		{http.MethodGet, "/api/v1/personality/assessment", nil},
		{http.MethodPost, "/api/v1/personality/assessment/submit", map[string]any{"answers": []any{}}},
		{http.MethodGet, "/api/v1/personality/me", nil},
		{http.MethodGet, "/api/v1/preferences", nil},
		{http.MethodPut, "/api/v1/preferences", map[string]any{"age_min": 20}},
		{http.MethodGet, "/api/v1/discover", nil},
		{http.MethodPost, "/api/v1/likes", map[string]any{"user_id": u.ID, "action": "like"}},
		{http.MethodGet, "/api/v1/matches", nil},
		{http.MethodGet, "/api/v1/blocks", nil},
		{http.MethodPost, "/api/v1/blocks", map[string]any{"user_id": u.ID}},
		{http.MethodPost, "/api/v1/reports", map[string]any{"user_id": u.ID, "reason": "spam"}},
		{http.MethodGet, "/api/v1/users/" + u.ID.String() + "/public", nil},
	}

	for _, tc := range protected {
		name := tc.method + " " + tc.path
		t.Run(name, func(t *testing.T) {
			do(t, tc.method, tc.path, "", tc.body).requireStatus(t, http.StatusUnauthorized)
			do(t, tc.method, tc.path, "not-a-jwt", tc.body).requireStatus(t, http.StatusUnauthorized)
		})
	}
}

func TestRefreshRotatesTheToken(t *testing.T) {
	resetDB(t)
	u := register(t, "Rotate", "1990-01-01")

	resp := do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{
		"refresh_token": u.Refresh,
	}).requireStatus(t, http.StatusOK)

	var rotated authResult
	resp.decode(t, &rotated)
	if rotated.RefreshToken == u.Refresh {
		t.Fatal("the refresh token was not rotated")
	}
	if rotated.SessionID != u.Session {
		t.Fatalf("session changed from %v to %v; rotation must stay within one session", u.Session, rotated.SessionID)
	}
	if rotated.AccessToken == "" {
		t.Fatal("no new access token issued")
	}
	do(t, http.MethodGet, "/api/v1/auth/me", rotated.AccessToken, nil).requireStatus(t, http.StatusOK)

	// The new token must itself be usable for a further rotation.
	do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{
		"refresh_token": rotated.RefreshToken,
	}).requireStatus(t, http.StatusOK)
}

func TestRefreshReplayRevokesTheWholeSession(t *testing.T) {
	resetDB(t)
	u := register(t, "Replay", "1990-01-01")

	var first authResult
	do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refresh_token": u.Refresh}).
		requireStatus(t, http.StatusOK).decode(t, &first)

	// Replaying the consumed token is the signal that it leaked.
	do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refresh_token": u.Refresh}).
		requireError(t, http.StatusUnauthorized, "refresh_token_reused")

	// The whole session must die, including the token the legitimate client holds:
	// the server cannot tell which party is the attacker.
	do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refresh_token": first.RefreshToken}).
		requireStatus(t, http.StatusUnauthorized)

	var revokedAt *string
	err := testPool.QueryRow(context.Background(),
		`SELECT revoked_reason FROM auth_sessions WHERE id = $1`, u.Session).Scan(&revokedAt)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	if revokedAt == nil || *revokedAt != "refresh_token_reuse" {
		t.Fatalf("revoked_reason = %v, want refresh_token_reuse; the revocation was rolled back", revokedAt)
	}
}

func TestRefreshRejectsBadInput(t *testing.T) {
	resetDB(t)
	cases := []struct {
		name       string
		body       map[string]any
		wantStatus int
	}{
		{"missing", map[string]any{}, http.StatusBadRequest},
		{"empty", map[string]any{"refresh_token": ""}, http.StatusBadRequest},
		{"unknown", map[string]any{"refresh_token": "rt_" + strings.Repeat("a", 43)}, http.StatusUnauthorized},
		{"an access token", map[string]any{"refresh_token": register(t, "X", "1990-01-01").Token}, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			do(t, http.MethodPost, "/api/v1/auth/refresh", "", tc.body).requireStatus(t, tc.wantStatus)
		})
	}
}

func TestLogoutRevokesTheSessionAndIsIdempotent(t *testing.T) {
	resetDB(t)
	u := register(t, "Logout", "1990-01-01")

	do(t, http.MethodPost, "/api/v1/auth/logout", u.Token, nil).requireStatus(t, http.StatusNoContent)
	do(t, http.MethodPost, "/api/v1/auth/logout", u.Token, nil).requireStatus(t, http.StatusNoContent)

	do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refresh_token": u.Refresh}).
		requireStatus(t, http.StatusUnauthorized)
}

func TestSessionsListAndRevoke(t *testing.T) {
	resetDB(t)
	u := register(t, "Sessions", "1990-01-01")
	second := login(t, u.Email, defaultPassword)

	type sessionList struct {
		Items []struct {
			ID      uuid.UUID `json:"id"`
			Current bool      `json:"current"`
			IP      string    `json:"ip"`
		} `json:"items"`
	}

	var list sessionList
	do(t, http.MethodGet, "/api/v1/auth/sessions", second.AccessToken, nil).
		requireStatus(t, http.StatusOK).decode(t, &list)

	if len(list.Items) != 2 {
		t.Fatalf("listed %d sessions, want 2", len(list.Items))
	}
	var currentCount int
	for _, item := range list.Items {
		if item.Current {
			currentCount++
			if item.ID != second.SessionID {
				t.Errorf("current session = %v, want the caller's %v", item.ID, second.SessionID)
			}
		}
	}
	if currentCount != 1 {
		t.Errorf("%d sessions marked current, want exactly 1", currentCount)
	}

	// Revoking the other session must kill its refresh token but not this one.
	do(t, http.MethodDelete, "/api/v1/auth/sessions/"+u.Session.String(), second.AccessToken, nil).
		requireStatus(t, http.StatusNoContent)
	do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refresh_token": u.Refresh}).
		requireStatus(t, http.StatusUnauthorized)
	do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refresh_token": second.RefreshToken}).
		requireStatus(t, http.StatusOK)
}

func TestRevokeSessionCannotTouchAnotherAccount(t *testing.T) {
	resetDB(t)
	victim := register(t, "Victim", "1990-01-01")
	attacker := register(t, "Attacker", "1990-01-01")

	// A session id belonging to someone else must be indistinguishable from one
	// that does not exist.
	do(t, http.MethodDelete, "/api/v1/auth/sessions/"+victim.Session.String(), attacker.Token, nil).
		requireStatus(t, http.StatusNotFound)
	do(t, http.MethodDelete, "/api/v1/auth/sessions/"+uuid.NewString(), attacker.Token, nil).
		requireStatus(t, http.StatusNotFound)
	do(t, http.MethodDelete, "/api/v1/auth/sessions/not-a-uuid", attacker.Token, nil).
		requireStatus(t, http.StatusBadRequest)

	// The victim's session must still work.
	do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refresh_token": victim.Refresh}).
		requireStatus(t, http.StatusOK)
}

func TestPasswordResetFlow(t *testing.T) {
	resetDB(t)
	u := register(t, "Reset", "1990-01-01")
	otherSession := login(t, u.Email, defaultPassword)

	// The response must be identical for known and unknown addresses.
	known := do(t, http.MethodPost, "/api/v1/auth/password/forgot", "", map[string]any{"email": u.Email}).
		requireStatus(t, http.StatusAccepted)
	unknown := do(t, http.MethodPost, "/api/v1/auth/password/forgot", "", map[string]any{"email": uniqueEmail("nobody")}).
		requireStatus(t, http.StatusAccepted)
	if string(known.Body) != string(unknown.Body) {
		t.Fatalf("forgot-password discloses account existence:\nknown:   %s\nunknown: %s", known.Body, unknown.Body)
	}

	// Only one token was actually issued.
	if got := countRows(t, `SELECT count(*) FROM password_reset_tokens`); got != 1 {
		t.Fatalf("issued %d reset tokens, want 1", got)
	}

	token := resetTokenFromOutbox(t, u.ID)
	const newPassword = "brand-new-password"

	do(t, http.MethodPost, "/api/v1/auth/password/reset", "", map[string]any{
		"token": token, "password": "short",
	}).requireStatus(t, http.StatusUnprocessableEntity)

	do(t, http.MethodPost, "/api/v1/auth/password/reset", "", map[string]any{
		"token": token, "password": newPassword,
	}).requireStatus(t, http.StatusNoContent)

	// Single use.
	do(t, http.MethodPost, "/api/v1/auth/password/reset", "", map[string]any{
		"token": token, "password": "another-password",
	}).requireStatus(t, http.StatusBadRequest)

	// The old password must stop working and the new one must start.
	do(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email": u.Email, "password": defaultPassword,
	}).requireStatus(t, http.StatusUnauthorized)
	login(t, u.Email, newPassword)

	// A reset means the account may be compromised, so every pre-existing
	// session must be gone.
	for name, refresh := range map[string]string{"registration": u.Refresh, "second login": otherSession.RefreshToken} {
		do(t, http.MethodPost, "/api/v1/auth/refresh", "", map[string]any{"refresh_token": refresh}).
			requireStatus(t, http.StatusUnauthorized)
		_ = name
	}
}

func TestPasswordResetRejectsUnknownTokens(t *testing.T) {
	resetDB(t)
	for _, token := range []string{"", "garbage", strings.Repeat("a", 43)} {
		resp := do(t, http.MethodPost, "/api/v1/auth/password/reset", "", map[string]any{
			"token": token, "password": "a-valid-password",
		})
		if resp.Status != http.StatusBadRequest && resp.Status != http.StatusUnauthorized {
			t.Fatalf("token %q: status = %d, want 400 or 401 (body: %s)", token, resp.Status, resp.Body)
		}
	}
}

// resetTokenFromOutbox reads the token out of the event the service enqueued,
// which is how the notification worker will receive it.
func resetTokenFromOutbox(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	var token string
	err := testPool.QueryRow(context.Background(), `
		SELECT payload->>'reset_token'
		FROM outbox_events
		WHERE event_type = 'auth.password_reset_requested'
		  AND payload->>'user_id' = $1::text
		ORDER BY created_at DESC
		LIMIT 1`, userID.String()).Scan(&token)
	if err != nil {
		t.Fatalf("read reset token from the outbox: %v", err)
	}
	if token == "" {
		t.Fatal("the outbox event carries no reset token")
	}
	return token
}

func TestPasswordResetEventDoesNotCarryTheHash(t *testing.T) {
	resetDB(t)
	u := register(t, "Event", "1990-01-01")
	do(t, http.MethodPost, "/api/v1/auth/password/forgot", "", map[string]any{"email": u.Email}).
		requireStatus(t, http.StatusAccepted)

	var payload string
	err := testPool.QueryRow(context.Background(), `
		SELECT payload::text FROM outbox_events
		WHERE event_type = 'auth.password_reset_requested' LIMIT 1`).Scan(&payload)
	if err != nil {
		t.Fatalf("load event: %v", err)
	}
	if !strings.Contains(payload, u.Email) {
		t.Errorf("the event omits the recipient address, so no email can be sent: %s", payload)
	}
	if strings.Contains(payload, "password_hash") || strings.Contains(payload, "token_hash") {
		t.Errorf("the event carries stored credential material: %s", payload)
	}
}

func TestEmailVerificationFlow(t *testing.T) {
	resetDB(t)
	u := register(t, "Verify", "1990-01-01")

	if got := countRows(t, `SELECT count(*) FROM email_verification_tokens WHERE user_id = $1`, u.ID); got != 1 {
		t.Fatalf("issued %d verification tokens, want 1", got)
	}
	if got := countRows(t, `
		SELECT count(*) FROM outbox_events
		WHERE event_type = 'auth.email_verification_requested'
		  AND payload->>'user_id' = $1`, u.ID.String()); got != 1 {
		t.Fatalf("outbox verification events = %d, want 1", got)
	}

	token := verifyTokenFromOutbox(t, u.ID)

	// Login is not gated on verification.
	login(t, u.Email, defaultPassword)

	do(t, http.MethodPost, "/api/v1/auth/email/verify", "", map[string]any{"token": "bogus"}).
		requireError(t, http.StatusBadRequest, "verify_token_invalid")

	do(t, http.MethodPost, "/api/v1/auth/email/verify", "", map[string]any{"token": token}).
		requireStatus(t, http.StatusNoContent)

	var me struct {
		User struct {
			EmailVerified bool `json:"email_verified"`
		} `json:"user"`
	}
	do(t, http.MethodGet, "/api/v1/auth/me", u.Token, nil).
		requireStatus(t, http.StatusOK).decode(t, &me)
	if !me.User.EmailVerified {
		t.Fatal("GET /auth/me still reports email_verified=false after a successful verify")
	}

	do(t, http.MethodPost, "/api/v1/auth/email/verify", "", map[string]any{"token": token}).
		requireError(t, http.StatusBadRequest, "verify_token_invalid")

	do(t, http.MethodPost, "/api/v1/auth/email/resend", u.Token, nil).
		requireStatus(t, http.StatusAccepted)
	if got := countRows(t, `
		SELECT count(*) FROM outbox_events
		WHERE event_type = 'auth.email_verification_requested'
		  AND payload->>'user_id' = $1`, u.ID.String()); got != 1 {
		t.Fatalf("resend after verify issued another event (%d), want the original 1", got)
	}
}

func TestEmailVerificationResendReplacesTheOutstandingToken(t *testing.T) {
	resetDB(t)
	u := register(t, "Resend", "1990-01-01")
	first := verifyTokenFromOutbox(t, u.ID)

	do(t, http.MethodPost, "/api/v1/auth/email/resend", u.Token, nil).
		requireStatus(t, http.StatusAccepted)

	second := verifyTokenFromOutbox(t, u.ID)
	if second == first {
		t.Fatal("resend reused the previous plaintext token")
	}

	do(t, http.MethodPost, "/api/v1/auth/email/verify", "", map[string]any{"token": first}).
		requireError(t, http.StatusBadRequest, "verify_token_invalid")
	do(t, http.MethodPost, "/api/v1/auth/email/verify", "", map[string]any{"token": second}).
		requireStatus(t, http.StatusNoContent)
}

func verifyTokenFromOutbox(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	var token string
	err := testPool.QueryRow(context.Background(), `
		SELECT payload->>'verify_token'
		FROM outbox_events
		WHERE event_type = 'auth.email_verification_requested'
		  AND payload->>'user_id' = $1::text
		ORDER BY created_at DESC
		LIMIT 1`, userID.String()).Scan(&token)
	if err != nil {
		t.Fatalf("read verification token from the outbox: %v", err)
	}
	if token == "" {
		t.Fatal("the outbox event carries no verify token")
	}
	return token
}
