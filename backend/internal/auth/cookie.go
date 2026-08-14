package auth

import (
	"net/http"
	"strings"
	"time"
)

// RefreshCookieName is the httpOnly cookie that carries the refresh token so
// Person A does not have to keep it in JavaScript storage.
const RefreshCookieName = "refresh_token"

// crossSite reports whether the cookie has to survive a request from a
// different site. A single-origin deployment keeps SameSite=Lax, which is the
// stronger default; a browser will not attach a Lax cookie to the cross-origin
// refresh call a separately hosted frontend makes, so those deployments need
// SameSite=None, which browsers only honour alongside Secure.
func sameSite(secure bool) http.SameSite {
	if secure {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}

func refreshCookie(token string, ttl time.Duration, secure bool) *http.Cookie {
	maxAge := int(ttl.Seconds())
	if token == "" {
		maxAge = -1
	}
	return &http.Cookie{
		Name:     RefreshCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite(secure),
	}
}

// SetRefreshCookie stores the rotated refresh token in an httpOnly cookie.
//
// Call this before writing the response body: WriteJSON commits the status
// line, and headers set afterwards are discarded.
func SetRefreshCookie(w http.ResponseWriter, token string, ttl time.Duration, secure bool) {
	http.SetCookie(w, refreshCookie(token, ttl, secure))
}

// ClearRefreshCookie drops the refresh cookie on logout or a failed refresh.
func ClearRefreshCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, refreshCookie("", 0, secure))
}

// RefreshTokenFromRequest prefers the JSON body (tests and older clients) and
// falls back to the httpOnly cookie Person A uses.
func RefreshTokenFromRequest(r *http.Request, bodyToken string) string {
	if t := strings.TrimSpace(bodyToken); t != "" {
		return t
	}
	c, err := r.Cookie(RefreshCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}
