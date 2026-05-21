package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	AuthCookieName      = "multica_auth"
	CSRFCookieName      = "multica_csrf"
	defaultAuthTokenTTL = 30 * 24 * time.Hour // 30 days
)

var (
	invalidCookieDomainWarnOnce sync.Once
	authTokenTTLOnce           sync.Once
	authTokenTTLCached         time.Duration
)

var cookieDomainLabelRE = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// parseAuthTokenTTL parses a raw AUTH_TOKEN_TTL value into a duration.
// It first tries time.ParseDuration (e.g. "8760h", "720h30m"), then falls
// back to parsing as integer seconds.
func parseAuthTokenTTL(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if d, err := time.ParseDuration(raw); err == nil {
		if d <= 0 {
			return 0, false
		}
		if d > 10*365*24*time.Hour {
			slog.Warn("AUTH_TOKEN_TTL exceeds 10 years; accepting but verify this is intentional",
				"value", raw, "hours", d.Hours())
		}
		return d, true
	}
	secs, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || secs <= 0 {
		return 0, false
	}
	if secs > int64(math.MaxInt64/int64(time.Second)) {
		return 0, false
	}
	d := time.Duration(secs) * time.Second
	if d > 10*365*24*time.Hour {
		slog.Warn("AUTH_TOKEN_TTL exceeds 10 years; accepting but verify this is intentional",
			"value", raw, "hours", d.Hours())
	}
	return d, true
}

// AuthTokenTTL returns the configured auth token lifetime. It reads the
// AUTH_TOKEN_TTL env var and caches the result. Default: 30 days.
func AuthTokenTTL() time.Duration {
	authTokenTTLOnce.Do(func() {
		raw := os.Getenv("AUTH_TOKEN_TTL")
		if ttl, ok := parseAuthTokenTTL(raw); ok {
			authTokenTTLCached = ttl
			slog.Info("auth token TTL configured", "seconds", int(ttl.Seconds()))
			return
		}
		authTokenTTLCached = defaultAuthTokenTTL
		if strings.TrimSpace(raw) != "" {
			slog.Warn("AUTH_TOKEN_TTL is not a valid duration or positive integer; using default",
				"value", raw, "default_seconds", int(defaultAuthTokenTTL.Seconds()))
		}
	})
	return authTokenTTLCached
}

// cookieDomain returns the trimmed COOKIE_DOMAIN env value, or "" if the value
// is unsafe for the browser cookie Domain attribute. RFC 6265 §4.1.2.3 forbids
// IP literals, and browsers can drop Set-Cookie headers that carry invalid
// Domain attributes. Invalid values become host-only cookies instead.
func cookieDomain() string {
	raw := strings.TrimSpace(os.Getenv("COOKIE_DOMAIN"))
	if raw == "" {
		return ""
	}
	// A leading dot ("." for subdomain matching) is legal syntax but doesn't
	// change whether the remainder is an IP literal.
	if ok, reason := validCookieDomain(raw); !ok {
		invalidCookieDomainWarnOnce.Do(func() {
			slog.Warn("COOKIE_DOMAIN is invalid for browser cookie Domain; ignoring so session cookies remain host-only", "reason", reason)
		})
		return ""
	}
	return raw
}

func validCookieDomain(raw string) (bool, string) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "."))
	if trimmed == "" {
		return false, "empty"
	}
	if strings.Contains(trimmed, "://") || strings.ContainsAny(trimmed, "/\\") {
		return false, "not-a-domain"
	}
	if ip := net.ParseIP(strings.Trim(trimmed, "[]")); ip != nil {
		return false, "ip-literal"
	}
	if strings.EqualFold(trimmed, "localhost") || !strings.Contains(trimmed, ".") {
		return false, "single-label"
	}
	labels := strings.Split(trimmed, ".")
	for _, label := range labels {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") || !cookieDomainLabelRE.MatchString(label) {
			return false, "invalid-label"
		}
	}
	return true, ""
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
	ttl := AuthTokenTTL()
	now := time.Now()

	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    token,
		Path:     "/",
		Domain:   domain,
		MaxAge:   int(ttl.Seconds()),
		Expires:  now.Add(ttl),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
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
		MaxAge:   int(ttl.Seconds()),
		Expires:  now.Add(ttl),
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})

	return nil
}

// ClearAuthCookies removes the auth and CSRF cookies.
func ClearAuthCookies(w http.ResponseWriter) {
	domain := cookieDomain()
	secure := isSecureCookie()

	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    "",
		Path:     "/",
		Domain:   domain,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
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
		SameSite: http.SameSiteStrictMode,
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
