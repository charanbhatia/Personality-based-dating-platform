package auth

import (
	"net/http"
	"strings"
	"time"
)

// RefreshCookieName is the httpOnly cookie that carries the refresh token so
// Person A does not have to keep it in JavaScript storage.
const RefreshCookieName = "refresh_token"

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
		SameSite: http.SameSiteLaxMode,
	}
}

// SetRefreshCookie stores the rotated refresh token in an httpOnly cookie.
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
