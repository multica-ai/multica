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
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	AuthCookieName = "multica_auth"
	// CSRFCookieName carries the token-bound CSRF value, and keeps carrying
	// it: every server that has ever run this code understands this cookie,
	// which is what makes rolling BACK past MUL-7436 safe. See
	// SessionCSRFCookieName.
	CSRFCookieName = "multica_csrf"
	// SessionCSRFCookieName carries the session-bound CSRF value added by
	// MUL-7436. It is the one the server prefers, because it survives a
	// sliding renewal; the token-bound cookie above cannot, since renewal
	// replaces the very token it is keyed to.
	SessionCSRFCookieName = "multica_csrf_session"

	// CSRFHeaderName is where clients echo ONE of the two cookies back, and
	// there is deliberately only one header name.
	//
	// A second header would have to be added to corsAllowedHeaders, and that
	// list is version-specific: a browser holding a cookie issued by this
	// release, talking to a rolled-back server, would send a header that
	// server does not allowlist. The preflight fails, the request never
	// reaches a handler, and no amount of client retrying helps — which is
	// exactly the half-logged-in state the second cookie exists to prevent.
	// Keeping one header name means the rollback path has no preflight
	// dependency at all; the server simply tries both bindings against
	// whatever value arrives.
	CSRFHeaderName = "X-CSRF-Token"

	defaultAuthTokenTTL = 30 * 24 * time.Hour // 30 days

	// MinAuthTokenTTL is the shortest session lifetime this system can serve
	// correctly, and what sets it is the CLIENT FLOOR, not the wire format.
	//
	// The renewal cadence is TTL/10, and every client floors what the server
	// sends at five seconds so a nonsense value cannot turn activity into a
	// request per event. Once TTL/10 drops below that floor the floor wins,
	// and the client is checking on a schedule the server did not choose —
	// which is exactly how a cadence ends up longer than the window it has to
	// land in. TTL/10 >= 5s means TTL >= 50 seconds; one minute is that bound
	// rounded to something an operator would actually write, and it leaves
	// the cadence (six seconds) comfortably above the floor.
	//
	// Whole-second serialisation (RefreshSessionResponse.CheckAgainInSeconds)
	// is a second, looser constraint: it only bites below about four seconds
	// of cadence, i.e. a TTL under 40 seconds, so the client floor is the one
	// that actually decides this number.
	//
	// Anything shorter is clamped up rather than honoured, because honouring
	// it would mean silently logging active users out.
	MinAuthTokenTTL = time.Minute
)

var (
	ipCookieDomainWarnOnce sync.Once
	authTokenTTLOnce       sync.Once
	authTokenTTLCached     time.Duration
)

// parseAuthTokenTTL parses a raw AUTH_TOKEN_TTL value into a duration.
// It first tries time.ParseDuration (e.g. "8760h", "720h30m"), then falls
// back to parsing as integer seconds. Returns the parsed duration and true
// on success; zero and false when the input is empty or invalid.
func parseAuthTokenTTL(raw string) (time.Duration, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}

	// Try Go duration string first (e.g. "8760h", "720h30m").
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

	// Fall back to plain integer seconds.
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
// AUTH_TOKEN_TTL environment variable (Go duration string or integer seconds) on first call and caches
// the result. When the variable is unset or invalid the default of 30 days
// is used.
func AuthTokenTTL() time.Duration {
	authTokenTTLOnce.Do(func() {
		raw := os.Getenv("AUTH_TOKEN_TTL")
		if ttl, ok := parseAuthTokenTTL(raw); ok {
			if ttl < MinAuthTokenTTL {
				// Clamping up, not falling back to the default: someone who
				// asked for 30 seconds is far better served by 1 minute than
				// by the 30-day default they did not ask for, and the warning
				// says exactly what happened.
				slog.Warn("AUTH_TOKEN_TTL is below the shortest supported session lifetime; using the minimum",
					"value", raw, "minimum_seconds", int(MinAuthTokenTTL.Seconds()),
					"reason", "a shorter TTL derives a renewal cadence below the floor every client applies, so clients would check on a schedule this server did not choose")
				ttl = MinAuthTokenTTL
			}
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

// Two CSRF bindings exist side by side, and the pair is deliberate.
//
//   - Session-bound: HMAC-SHA256 keyed by the server's JWT secret over
//     (sessionID, nonce). `sid` survives renewal, so re-signing the auth
//     cookie leaves every CSRF token already issued for that session valid —
//     which is what keeps sliding renewal from racing other tabs (MUL-7436).
//     This is the HMAC-based Token Pattern OWASP recommends.
//   - Token-bound: HMAC-SHA256 keyed by the auth token itself over the nonce.
//     This is the original scheme, and it is still ISSUED, not merely still
//     accepted. A server running the previous release can verify only this
//     one, so dropping it would make a rollback strand every signed-in user
//     able to read but unable to write: their session would authenticate on
//     GET and 403 on every POST, with no client-side retry able to help. It
//     is the only binding either version can agree on, so both are written.
//
// Both defend the same thing: an attacker who can write cookies on a sibling
// subdomain cannot produce a CSRF value matching the auth cookie without
// already holding the session it belongs to.
//
// The token-bound cookie becomes removable one release after this ships, when
// no deployable version still needs it.
func sessionCSRFSignature(sessionID string, nonce []byte) []byte {
	mac := hmac.New(sha256.New, JWTSecret())
	mac.Write([]byte(sessionID))
	// Domain separator: without it a (sessionID, nonce) pair could be
	// re-split, letting one session's token be read as another's.
	mac.Write([]byte{0})
	mac.Write(nonce)
	return mac.Sum(nil)
}

func tokenCSRFSignature(authToken string, nonce []byte) []byte {
	mac := hmac.New(sha256.New, []byte(authToken))
	mac.Write(nonce)
	return mac.Sum(nil)
}

// csrfToken formats a CSRF value as hex(nonce) + "." + hex(signature).
func csrfToken(sign func(nonce []byte) []byte) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce) + "." + hex.EncodeToString(sign(nonce)), nil
}

// verifyCSRFToken checks a presented "nonce.signature" value against sign.
func verifyCSRFToken(presented string, sign func(nonce []byte) []byte) bool {
	parts := strings.SplitN(presented, ".", 2)
	if len(parts) != 2 {
		return false
	}
	nonce, err := hex.DecodeString(parts[0])
	if err != nil {
		return false
	}
	sig, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	return hmac.Equal(sign(nonce), sig)
}

// SetAuthCookies sets the HttpOnly auth cookie and both readable CSRF cookies
// on the response.
func SetAuthCookies(w http.ResponseWriter, token string) error {
	secure := isSecureCookie()
	domain := cookieDomain()
	ttl := AuthTokenTTL()
	now := time.Now()

	readable := func(name, value string) {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    value,
			Path:     "/",
			Domain:   domain,
			MaxAge:   int(ttl.Seconds()),
			Expires:  now.Add(ttl),
			HttpOnly: false,
			Secure:   secure,
			SameSite: http.SameSiteStrictMode,
		})
	}

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

	tokenBound, err := csrfToken(func(nonce []byte) []byte {
		return tokenCSRFSignature(token, nonce)
	})
	if err != nil {
		return err
	}
	readable(CSRFCookieName, tokenBound)

	// Only a token carrying `sid` can have a session-bound value. A token
	// minted before MUL-7436 has none until it renews, and until then the
	// token-bound cookie above is the only one it can use.
	if sid := SessionIDFromToken(token); sid != "" {
		sessionBound, err := csrfToken(func(nonce []byte) []byte {
			return sessionCSRFSignature(sid, nonce)
		})
		if err != nil {
			return err
		}
		readable(SessionCSRFCookieName, sessionBound)
	}

	return nil
}

// ClearAuthCookies removes the auth cookie and both CSRF cookies.
func ClearAuthCookies(w http.ResponseWriter) {
	domain := cookieDomain()
	secure := isSecureCookie()

	expire := func(name string, httpOnly bool) {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			Domain:   domain,
			MaxAge:   -1,
			Expires:  time.Unix(0, 0),
			HttpOnly: httpOnly,
			Secure:   secure,
			SameSite: http.SameSiteStrictMode,
		})
	}

	expire(AuthCookieName, true)
	expire(CSRFCookieName, false)
	// Logout must clear BOTH, or a stale session-bound value survives the
	// logout and is presented alongside the next login's cookies.
	expire(SessionCSRFCookieName, false)
}

// IsSafeMethod reports whether a method is read-only under RFC 9110, i.e. one
// that carries no CSRF requirement. Exported because the session-renewal
// middleware reuses it: re-issuing the auth cookie is itself a write to the
// client's cookie jar, and doing it only on safe requests keeps a rotation
// from ever racing the CSRF token of the request that triggered it.
func IsSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

// ValidateCSRF checks the CSRF header against the auth cookie. The value is
// HMAC-signed, so the server verifies a signature rather than comparing
// cookie == header.
// Returns true if validation passes (including for safe methods that don't need CSRF).
//
// One header, two acceptable bindings. Clients prefer the session-bound
// cookie, which survives a sliding renewal; a client that has not picked up
// that cookie, or is talking to us right after a rollback, sends the
// token-bound one. Trying both here is what lets a single header name serve
// every combination of client and server version — see CSRFHeaderName.
//
// The `sid` is read from the cookie WITHOUT enforcing expiry
// (SessionIDFromToken). That is what keeps an expired session from failing
// here: this function must not be the thing that answers a user whose session
// just ended, because a 403 leaves the client retrying a write instead of
// returning them to the login page. Authentication — and the 401 — happens
// immediately after, in the auth middleware.
func ValidateCSRF(r *http.Request) bool {
	if IsSafeMethod(r.Method) {
		return true
	}

	presented := r.Header.Get(CSRFHeaderName)
	if presented == "" {
		return false
	}

	authCookie, err := r.Cookie(AuthCookieName)
	if err != nil || authCookie.Value == "" {
		return false
	}

	if sid := SessionIDFromToken(authCookie.Value); sid != "" {
		if verifyCSRFToken(presented, func(nonce []byte) []byte {
			return sessionCSRFSignature(sid, nonce)
		}) {
			return true
		}
	}

	return verifyCSRFToken(presented, func(nonce []byte) []byte {
		return tokenCSRFSignature(authCookie.Value, nonce)
	})
}
