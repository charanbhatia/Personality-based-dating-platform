package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// response is a decoded API reply. Tests assert on Status and Code rather than
// on message wording, which is not part of the contract.
type response struct {
	Status  int
	Body    []byte
	Headers http.Header
}

// Code returns the machine-readable error code, or "" for a success.
func (r response) Code() string {
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.Body, &envelope)
	return envelope.Error.Code
}

// Details returns the per-field validation details.
func (r response) Details() map[string]any {
	var envelope struct {
		Error struct {
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.Body, &envelope)
	return envelope.Error.Details
}

// hasFieldError reports whether the validation details name a field, which is
// what lets the client highlight the offending input.
func (r response) hasFieldError(field string) bool {
	_, ok := r.Details()[field]
	return ok
}

func (r response) decode(t *testing.T, dst any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, dst); err != nil {
		t.Fatalf("decode %T from %s: %v", dst, r.Body, err)
	}
}

// requireStatus fails with the response body attached, which is what makes a
// failing assertion diagnosable without a rerun.
func (r response) requireStatus(t *testing.T, want int) response {
	t.Helper()
	if r.Status != want {
		t.Fatalf("status = %d, want %d (body: %s)", r.Status, want, r.Body)
	}
	return r
}

// requireError asserts both the status and the error code clients branch on.
func (r response) requireError(t *testing.T, wantStatus int, wantCode string) response {
	t.Helper()
	if r.Status != wantStatus || r.Code() != wantCode {
		t.Fatalf("got %d/%q, want %d/%q (body: %s)", r.Status, r.Code(), wantStatus, wantCode, r.Body)
	}
	return r
}

func do(t *testing.T, method, path, token string, body any) response {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, testServer.URL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := testServer.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response{Status: resp.StatusCode, Body: raw, Headers: resp.Header}
}

// emailSeq keeps generated addresses unique across a whole run, so a test that
// deliberately skips resetDB still cannot collide with another.
var emailSeq atomic.Uint64

func uniqueEmail(prefix string) string {
	return fmt.Sprintf("%s-%d-%s@integration.test", prefix, emailSeq.Add(1), uuid.NewString()[:8])
}

// authResult mirrors the register/login/refresh payload.
type authResult struct {
	User struct {
		ID            uuid.UUID `json:"id"`
		Email         string    `json:"email"`
		Name          string    `json:"name"`
		DateOfBirth   string    `json:"date_of_birth"`
		Age           *int      `json:"age"`
		EmailVerified bool      `json:"email_verified"`
	} `json:"user"`
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	RefreshToken string    `json:"refresh_token"`
	SessionID    uuid.UUID `json:"session_id"`
}

// user is a registered account plus its live credentials.
type user struct {
	ID      uuid.UUID
	Email   string
	Name    string
	Token   string
	Refresh string
	Session uuid.UUID
}

const defaultPassword = "integration-password"

// register creates an account with nothing else set up.
func register(t *testing.T, name, dateOfBirth string) *user {
	t.Helper()
	email := uniqueEmail("user")
	resp := do(t, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email":         email,
		"password":      defaultPassword,
		"name":          name,
		"date_of_birth": dateOfBirth,
	}).requireStatus(t, http.StatusCreated)

	var result authResult
	resp.decode(t, &result)
	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatalf("register returned an incomplete token pair: %s", resp.Body)
	}
	return &user{
		ID:      result.User.ID,
		Email:   email,
		Name:    name,
		Token:   result.AccessToken,
		Refresh: result.RefreshToken,
		Session: result.SessionID,
	}
}

// login authenticates an existing account, returning a second live session.
func login(t *testing.T, email, password string) authResult {
	t.Helper()
	resp := do(t, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email":    email,
		"password": password,
	}).requireStatus(t, http.StatusOK)

	var result authResult
	resp.decode(t, &result)
	return result
}

type question struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	TraitKey  string    `json:"trait_key"`
	Direction int       `json:"direction"`
}

type questionList struct {
	Questions []question `json:"questions"`
	CanSubmit bool       `json:"can_submit"`
	ScaleMin  int        `json:"scale_min"`
	ScaleMax  int        `json:"scale_max"`
}

func fetchQuestions(t *testing.T, u *user) questionList {
	t.Helper()
	resp := do(t, http.MethodGet, "/api/v1/personality/assessment", u.Token, nil).
		requireStatus(t, http.StatusOK)

	var list questionList
	resp.decode(t, &list)
	if len(list.Questions) == 0 {
		t.Fatal("the question bank is empty; migration 004 did not seed it")
	}
	return list
}

// Levels are the 1..5 range a trait can be driven to. The question bank is
// balanced (equal forward and reverse items per trait), so answering every item
// with the same number always yields 0.5 — a fixture must answer reverse-keyed
// items with the mirrored value to land anywhere else.
const (
	LevelLowest  = 1
	LevelMiddle  = 3
	LevelHighest = 5
)

// answerForLevel mirrors the value for reverse-keyed items so that a whole trait
// resolves to traitValueForLevel(level).
func answerForLevel(q question, level int) int {
	if q.Direction < 0 {
		return (LevelLowest + LevelHighest) - level
	}
	return level
}

// traitValueForLevel is the normalized trait value a level produces, matching
// personality.Score.
func traitValueForLevel(level int) float64 {
	return float64(level-LevelLowest) / float64(LevelHighest-LevelLowest)
}

// submitAssessment places every trait at the same level.
func submitAssessment(t *testing.T, u *user, level int) {
	t.Helper()
	list := fetchQuestions(t, u)
	answers := make([]map[string]any, 0, len(list.Questions))
	for _, q := range list.Questions {
		answers = append(answers, map[string]any{"question_id": q.ID, "value": answerForLevel(q, level)})
	}
	do(t, http.MethodPost, "/api/v1/personality/assessment/submit", u.Token,
		map[string]any{"answers": answers}).requireStatus(t, http.StatusOK)
}

// submitAssessmentPerTrait places each trait at its own level, so a test can put
// a user at a chosen point in trait space.
func submitAssessmentPerTrait(t *testing.T, u *user, levels map[string]int) {
	t.Helper()
	list := fetchQuestions(t, u)
	answers := make([]map[string]any, 0, len(list.Questions))
	for _, q := range list.Questions {
		level, ok := levels[q.TraitKey]
		if !ok {
			t.Fatalf("no level supplied for trait %q", q.TraitKey)
		}
		answers = append(answers, map[string]any{"question_id": q.ID, "value": answerForLevel(q, level)})
	}
	do(t, http.MethodPost, "/api/v1/personality/assessment/submit", u.Token,
		map[string]any{"answers": answers}).requireStatus(t, http.StatusOK)
}

// submitUniformAssessment answers every item with the literal value, ignoring
// direction. Used where the submission itself is under test rather than the
// resulting vector.
func submitUniformAssessment(t *testing.T, u *user, value int) response {
	t.Helper()
	list := fetchQuestions(t, u)
	answers := make([]map[string]any, 0, len(list.Questions))
	for _, q := range list.Questions {
		answers = append(answers, map[string]any{"question_id": q.ID, "value": value})
	}
	return do(t, http.MethodPost, "/api/v1/personality/assessment/submit", u.Token,
		map[string]any{"answers": answers})
}

func setPreferences(t *testing.T, u *user, body map[string]any) {
	t.Helper()
	do(t, http.MethodPut, "/api/v1/preferences", u.Token, body).requireStatus(t, http.StatusOK)
}

func setProfile(t *testing.T, u *user, body map[string]any) {
	t.Helper()
	do(t, http.MethodPut, "/api/v1/profile", u.Token, body).requireStatus(t, http.StatusOK)
}

func setPhotos(t *testing.T, u *user, urls ...string) {
	t.Helper()
	do(t, http.MethodPut, "/api/v1/profile/photos", u.Token,
		map[string]any{"photo_urls": urls}).requireStatus(t, http.StatusOK)
}

// onboardOptions tune a fully onboarded fixture user.
type onboardOptions struct {
	Name        string
	DateOfBirth string
	Gender      string
	// Level places every trait at one point; TraitLevels overrides it per trait.
	Level       int
	TraitLevels map[string]int
	// Genders defaults to every canonical value, so fixtures see each other.
	Genders []string
	AgeMin  int
	AgeMax  int
}

// onboard creates an account that satisfies every discovery prerequisite:
// assessed, with preferences, a gender and at least one photo.
func onboard(t *testing.T, opts onboardOptions) *user {
	t.Helper()
	if opts.Name == "" {
		opts.Name = "Test User"
	}
	if opts.DateOfBirth == "" {
		opts.DateOfBirth = "1995-06-15"
	}
	if opts.Gender == "" {
		opts.Gender = "other"
	}
	if opts.Level == 0 {
		opts.Level = LevelMiddle
	}
	if len(opts.Genders) == 0 {
		opts.Genders = []string{"man", "woman", "nonbinary", "other"}
	}
	if opts.AgeMin == 0 {
		opts.AgeMin = 18
	}
	if opts.AgeMax == 0 {
		opts.AgeMax = 99
	}

	u := register(t, opts.Name, opts.DateOfBirth)
	if len(opts.TraitLevels) > 0 {
		submitAssessmentPerTrait(t, u, opts.TraitLevels)
	} else {
		submitAssessment(t, u, opts.Level)
	}
	setPreferences(t, u, map[string]any{
		"age_min": opts.AgeMin,
		"age_max": opts.AgeMax,
		"genders": opts.Genders,
	})
	setProfile(t, u, map[string]any{
		"gender":   opts.Gender,
		"bio":      "Fixture profile for " + opts.Name,
		"location": "Bengaluru",
	})
	setPhotos(t, u, "https://cdn.example.test/"+strings.ToLower(strings.ReplaceAll(opts.Name, " ", "-"))+".jpg")
	return u
}

type discoverItem struct {
	UserID             uuid.UUID          `json:"user_id"`
	Name               string             `json:"name"`
	Age                *int               `json:"age"`
	Gender             string             `json:"gender"`
	CompatibilityScore float64            `json:"compatibility_score"`
	PhotoURLs          []string           `json:"photo_urls"`
	Traits             map[string]float64 `json:"traits"`
}

type discoverPage struct {
	Items      []discoverItem `json:"items"`
	NextCursor *string        `json:"next_cursor"`
}

func discover(t *testing.T, u *user, query string) discoverPage {
	t.Helper()
	path := "/api/v1/discover"
	if query != "" {
		path += "?" + query
	}
	resp := do(t, http.MethodGet, path, u.Token, nil).requireStatus(t, http.StatusOK)

	var page discoverPage
	resp.decode(t, &page)
	return page
}

func (p discoverPage) ids() []uuid.UUID {
	out := make([]uuid.UUID, 0, len(p.Items))
	for _, item := range p.Items {
		out = append(out, item.UserID)
	}
	return out
}

func (p discoverPage) contains(id uuid.UUID) bool {
	for _, item := range p.Items {
		if item.UserID == id {
			return true
		}
	}
	return false
}

type swipeResult struct {
	OK        bool       `json:"ok"`
	Action    string     `json:"action"`
	Matched   bool       `json:"matched"`
	MatchID   *uuid.UUID `json:"match_id"`
	Duplicate bool       `json:"duplicate"`
}

func swipe(t *testing.T, from *user, to uuid.UUID, action string) swipeResult {
	t.Helper()
	resp := do(t, http.MethodPost, "/api/v1/likes", from.Token,
		map[string]any{"user_id": to, "action": action}).requireStatus(t, http.StatusOK)

	var result swipeResult
	resp.decode(t, &result)
	return result
}

type matchPage struct {
	Items []struct {
		MatchID uuid.UUID `json:"match_id"`
		User    struct {
			UserID    uuid.UUID `json:"user_id"`
			Name      string    `json:"name"`
			IsMatched bool      `json:"is_matched"`
		} `json:"user"`
		CompatibilityScore *float64 `json:"compatibility_score"`
		CreatedAt          string   `json:"created_at"`
	} `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func listMatches(t *testing.T, u *user, query string) matchPage {
	t.Helper()
	path := "/api/v1/matches"
	if query != "" {
		path += "?" + query
	}
	resp := do(t, http.MethodGet, path, u.Token, nil).requireStatus(t, http.StatusOK)

	var page matchPage
	resp.decode(t, &page)
	return page
}

// countRows is used to assert on side effects the API does not expose, such as
// outbox writes.
func countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var count int
	if err := testPool.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count query %q: %v", query, err)
	}
	return count
}
