package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	AuthCookieName   = "multica_auth"
	CSRFCookieName   = "multica_csrf"
	authCookieMaxAge = 30 * 24 * 60 * 60 // 30 days in seconds
)

var ipCookieDomainWarnOnce sync.Once

// cookieDomain returns the trimmed COOKIE_DOMAIN env value, or "" if it looks
// like an IP address. RFC 6265 §4.1.2.3 forbids IP literals in the cookie
// Domain attribute, so browsers silently drop Set-Cookie headers that carry
// one. An IP value here is almost always a misconfiguration.
func cookieDomain() string {
	raw := strings.TrimSpace(os.Getenv("COOKIE_DOMAIN"))
	if raw == "" {
		return ""
	}
	// A leading dot ("." for subdomain matching) is legal syntax but doesn't
	// change whether the remainder is an IP literal.
	if ip := net.ParseIP(strings.TrimPrefix(raw, ".")); ip != nil {
		ipCookieDomainWarnOnce.Do(func() {
			slog.Warn(
				"COOKIE_DOMAIN looks like an IP address; ignoring. RFC 6265 forbids IP literals in the cookie Domain attribute, so browsers would drop the Set-Cookie. Leave COOKIE_DOMAIN empty for single-host deployments, or use a real domain.",
				"value", raw,
			)
		})
		return ""
	}
	return raw
}

// isSecureCookie reports whether session cookies should carry the Secure flag.
// Derived from the scheme of FRONTEND_ORIGIN — browsers silently drop Secure
// cookies received on a plain-HTTP page, so the flag has to track the actual
// user-facing scheme rather than a coarser environment name.
func isSecureCookie() bool {
	raw := strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "https")
}

// sessionSameSite returns the SameSite policy for session cookies.
// None is required so the cookie is sent when ship is embedded as an iframe
// or called cross-site (e.g. Feishu Project plugin at projectplg.feishupkg.com
// hitting ship.lilithgames.com). None requires Secure, so fall back to Lax on
// plain-HTTP dev where Secure isn't set and browsers would reject None.
func sessionSameSite(secure bool) http.SameSite {
	if secure {
		return http.SameSiteNoneMode
	}
	return http.SameSiteLaxMode
}

// generateCSRFToken creates a CSRF token bound to the auth token via HMAC.
// Format: hex(nonce) + "." + hex(HMAC-SHA256(nonce, authToken)).
// This ensures an attacker who can write cookies on a subdomain cannot forge
// a valid CSRF token without knowing the auth token.
func generateCSRFToken(authToken string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	nonceHex := hex.EncodeToString(nonce)

	mac := hmac.New(sha256.New, []byte(authToken))
	mac.Write(nonce)
	sig := hex.EncodeToString(mac.Sum(nil))

	return nonceHex + "." + sig, nil
}

// SetAuthCookies sets the HttpOnly auth cookie and the readable CSRF cookie on the response.
func SetAuthCookies(w http.ResponseWriter, token string) error {
	secure := isSecureCookie()
	domain := cookieDomain()
	sameSite := sessionSameSite(secure)

	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    token,
		Path:     "/",
		Domain:   domain,
		MaxAge:   authCookieMaxAge,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
	})

	csrfToken, err := generateCSRFToken(token)
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    csrfToken,
		Path:     "/",
		Domain:   domain,
		MaxAge:   authCookieMaxAge,
		Expires:  time.Now().Add(30 * 24 * time.Hour),
		HttpOnly: false,
		Secure:   secure,
		SameSite: sameSite,
	})

	return nil
}

// ClearAuthCookies removes the auth and CSRF cookies.
func ClearAuthCookies(w http.ResponseWriter) {
	domain := cookieDomain()
	secure := isSecureCookie()
	sameSite := sessionSameSite(secure)

	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    "",
		Path:     "/",
		Domain:   domain,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
	})

	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    "",
		Path:     "/",
		Domain:   domain,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: false,
		Secure:   secure,
		SameSite: sameSite,
	})
}

// ValidateCSRF checks the X-CSRF-Token header against the auth cookie.
// The CSRF token is HMAC-signed with the auth token, so the server verifies
// the signature rather than simply comparing cookie == header.
// Returns true if validation passes (including for safe methods that don't need CSRF).
func ValidateCSRF(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}

	csrfHeader := r.Header.Get("X-CSRF-Token")
	if csrfHeader == "" {
		return false
	}

	authCookie, err := r.Cookie(AuthCookieName)
	if err != nil || authCookie.Value == "" {
		return false
	}

	parts := strings.SplitN(csrfHeader, ".", 2)
	if len(parts) != 2 {
		return false
	}

	nonce, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}

	expectedSig, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(authCookie.Value))
	mac.Write(nonce)
	return hmac.Equal(mac.Sum(nil), expectedSig)
}
