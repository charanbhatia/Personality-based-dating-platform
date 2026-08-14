package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func refreshCookieFrom(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == RefreshCookieName {
			return c
		}
	}
	return nil
}

// A Set-Cookie added after the status line is written never reaches the client,
// which left every session unable to survive a page reload.
func TestRefreshCookieMustBeSetBeforeTheBody(t *testing.T) {
	rec := httptest.NewRecorder()
	SetRefreshCookie(rec, "rt_example", time.Hour, true)
	rec.WriteHeader(http.StatusOK)

	if refreshCookieFrom(t, rec) == nil {
		t.Fatal("no refresh cookie on the response")
	}

	late := httptest.NewRecorder()
	late.WriteHeader(http.StatusOK)
	SetRefreshCookie(late, "rt_example", time.Hour, true)
	if refreshCookieFrom(t, late) != nil {
		t.Fatal("expected a cookie set after WriteHeader to be dropped; " +
			"if this passes, the ordering guarantee no longer holds")
	}
}

func TestRefreshCookieAttributes(t *testing.T) {
	// Secure deployments serve the frontend from another origin, so the cookie
	// has to be SameSite=None to be sent on the cross-site refresh call.
	rec := httptest.NewRecorder()
	SetRefreshCookie(rec, "rt_example", time.Hour, true)
	c := refreshCookieFrom(t, rec)
	if c == nil {
		t.Fatal("no refresh cookie")
	}
	if !c.HttpOnly {
		t.Error("refresh cookie must be httpOnly so scripts cannot read it")
	}
	if !c.Secure {
		t.Error("refresh cookie must be Secure when secure cookies are enabled")
	}
	if c.SameSite != http.SameSiteNoneMode {
		t.Errorf("SameSite = %v, want None so a cross-origin frontend can refresh", c.SameSite)
	}

	// Plain HTTP local development cannot use None, which browsers only accept
	// with Secure, so it stays on the stricter Lax.
	dev := httptest.NewRecorder()
	SetRefreshCookie(dev, "rt_example", time.Hour, false)
	if got := refreshCookieFrom(t, dev).SameSite; got != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax without Secure", got)
	}
}

func TestClearRefreshCookieExpiresIt(t *testing.T) {
	rec := httptest.NewRecorder()
	ClearRefreshCookie(rec, true)

	c := refreshCookieFrom(t, rec)
	if c == nil {
		t.Fatal("no refresh cookie")
	}
	if c.MaxAge >= 0 {
		t.Errorf("MaxAge = %d, want negative so the browser drops it", c.MaxAge)
	}
	if c.Value != "" {
		t.Errorf("Value = %q, want empty", c.Value)
	}
}
