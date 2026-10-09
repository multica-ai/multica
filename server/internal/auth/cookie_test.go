package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIsSecureCookie(t *testing.T) {
	cases := []struct {
		name           string
		frontendOrigin string
		want           bool
	}{
		{"https origin → Secure", "https://app.example.com", true},
		{"https with port", "https://app.example.com:8443", true},
		{"http origin → not Secure", "http://192.168.5.5:13000", false},
		{"http localhost → not Secure", "http://localhost:3000", false},
		{"empty → not Secure", "", false},
		{"malformed → not Secure", "::not-a-url", false},
		{"uppercase scheme still matches", "HTTPS://app.example.com", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FRONTEND_ORIGIN", tc.frontendOrigin)
			if got := isSecureCookie(); got != tc.want {
				t.Errorf("isSecureCookie() = %v, want %v (FRONTEND_ORIGIN=%q)", got, tc.want, tc.frontendOrigin)
			}
		})
	}
}

func TestCookieDomain(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"real domain", ".example.com", ".example.com"},
		{"bare domain", "example.com", "example.com"},
		{"IPv4 rejected", "192.168.5.5", ""},
		{"IPv4 with leading dot rejected", ".192.168.5.5", ""},
		{"IPv6 rejected", "::1", ""},
		{"IPv6 bracketed is not a valid IP literal → passthrough", "[::1]", "[::1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("COOKIE_DOMAIN", tc.env)
			if got := cookieDomain(); got != tc.want {
				t.Errorf("cookieDomain() = %q, want %q (COOKIE_DOMAIN=%q)", got, tc.want, tc.env)
			}
		})
	}
}

// TestSetAuthCookies_HTTPSelfHost covers the exact misconfiguration that
// shipped to users on LAN self-host: COOKIE_DOMAIN=<ip> + HTTP FRONTEND_ORIGIN.
// The cookie must land with no Domain attribute and Secure=false so browsers
// actually store it.
func TestSetAuthCookies_HTTPSelfHost(t *testing.T) {
	t.Setenv("FRONTEND_ORIGIN", "http://192.168.5.5:13000")
	t.Setenv("COOKIE_DOMAIN", "192.168.5.5")

	rec := httptest.NewRecorder()
	if err := SetAuthCookies(rec, "test-token"); err != nil {
		t.Fatalf("SetAuthCookies: %v", err)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("expected 2 cookies (auth + csrf), got %d", len(cookies))
	}
	for _, c := range cookies {
		if c.Secure {
			t.Errorf("cookie %q has Secure=true on HTTP origin; browser would reject it", c.Name)
		}
		if c.Domain != "" {
			t.Errorf("cookie %q has Domain=%q; IP-address Domain would be rejected by the browser (RFC 6265)", c.Name, c.Domain)
		}
	}
}

func TestParseAuthTokenTTL(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantDur time.Duration
		wantOK  bool
	}{
		{"empty string", "", 0, false},
		{"valid 3600", "3600", time.Hour, true},
		{"valid 86400", "86400", 24 * time.Hour, true},
		{"negative", "-100", 0, false},
		{"zero", "0", 0, false},
		{"non-numeric", "abc", 0, false},
		{"whitespace trimmed", " 7200 ", 2 * time.Hour, true},
		{"duration hours", "8760h", 8760 * time.Hour, true},
		{"duration compound", "720h30m", 720*time.Hour + 30*time.Minute, true},
		{"duration minutes", "90m", 90 * time.Minute, true},
		{"duration negative", "-1h", 0, false},
		{"duration zero", "0s", 0, false},
		{"integer overflow", "9999999999", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseAuthTokenTTL(tc.raw)
			if ok != tc.wantOK || got != tc.wantDur {
				t.Errorf("parseAuthTokenTTL(%q) = (%v, %v), want (%v, %v)", tc.raw, got, ok, tc.wantDur, tc.wantOK)
			}
		})
	}
}

func TestSetAuthCookies_HTTPSProduction(t *testing.T) {
	t.Setenv("FRONTEND_ORIGIN", "https://app.example.com")
	t.Setenv("COOKIE_DOMAIN", "app.example.com")

	rec := httptest.NewRecorder()
	if err := SetAuthCookies(rec, "test-token"); err != nil {
		t.Fatalf("SetAuthCookies: %v", err)
	}

	for _, c := range rec.Result().Cookies() {
		if !c.Secure {
			t.Errorf("cookie %q missing Secure flag on HTTPS origin", c.Name)
		}
		if c.Domain != "app.example.com" {
			t.Errorf("cookie %q Domain = %q, want %q", c.Name, c.Domain, "app.example.com")
		}
	}
}

// csrfHeaderFor builds the request a browser would send: the auth cookie the
// server set, plus the CSRF cookie's value echoed in the header.
// csrfRequest presents authToken as the auth cookie and echoes csrfValue in
// the single CSRF header. Which of the two cookie values a client puts there
// is the client's choice; the server tries both bindings either way.
func csrfRequest(t *testing.T, authToken, csrfValue string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/issues", nil)
	req.AddCookie(&http.Cookie{Name: AuthCookieName, Value: authToken})
	req.Header.Set(CSRFHeaderName, csrfValue)
	return req
}

// legacyCSRFRequest is the same request; the name records that the value came
// from the token-bound cookie — what a client that has not picked up the new
// cookie sends, and what a rolled-back server would be verifying.
func legacyCSRFRequest(t *testing.T, authToken, csrfValue string) *http.Request {
	t.Helper()
	return csrfRequest(t, authToken, csrfValue)
}

// cookieValues runs the real cookie-setting path and returns the readable
// cookies by name.
func cookieValues(t *testing.T, authToken string) map[string]string {
	t.Helper()
	rec := httptest.NewRecorder()
	if err := SetAuthCookies(rec, authToken); err != nil {
		t.Fatalf("SetAuthCookies: %v", err)
	}
	out := map[string]string{}
	for _, c := range rec.Result().Cookies() {
		out[c.Name] = c.Value
	}
	return out
}

// setAuthCookiesFor runs the real cookie-setting path and returns the CSRF
// token it minted, so tests exercise the same code the login and renewal
// paths use rather than reimplementing the token format.
func setAuthCookiesFor(t *testing.T, authToken string) string {
	t.Helper()
	values := cookieValues(t, authToken)
	if v := values[SessionCSRFCookieName]; v != "" {
		return v
	}
	t.Fatal("no session-bound CSRF cookie was set")
	return ""
}

// The point of binding CSRF to `sid` instead of to the token string: sliding
// renewal re-issues the auth cookie mid-session, and a CSRF token another tab
// read before that must keep working afterwards (MUL-7436).
func TestValidateCSRF_SurvivesSessionRenewal(t *testing.T) {
	sid, err := NewSessionID()
	if err != nil {
		t.Fatalf("NewSessionID: %v", err)
	}
	claims := sessionClaims(t, time.Now().Add(20*24*time.Hour), sid)
	original := signSession(t, claims)

	// A tab reads the CSRF cookie...
	csrfToken := setAuthCookiesFor(t, original)

	// ...the session is renewed on another tab's GET, producing a different
	// auth token for the same session...
	renewed, _, err := RenewSessionToken(claims)
	if err != nil {
		t.Fatalf("RenewSessionToken: %v", err)
	}
	if renewed == original {
		t.Fatal("renewal must produce a different token")
	}

	// ...and the first tab's POST still validates against the new cookie.
	if !ValidateCSRF(csrfRequest(t, renewed, csrfToken)) {
		t.Error("CSRF token issued before renewal must stay valid after it")
	}

	// The reverse also holds: a token minted after renewal works, so both
	// tabs converge without either having to reload.
	if !ValidateCSRF(csrfRequest(t, renewed, setAuthCookiesFor(t, renewed))) {
		t.Error("CSRF token issued after renewal must validate")
	}
}

// Binding to the session must not become a way to use one session's CSRF
// token against another's cookie.
func TestValidateCSRF_RejectsTokenFromAnotherSession(t *testing.T) {
	sidA, _ := NewSessionID()
	sidB, _ := NewSessionID()
	tokenA := signSession(t, sessionClaims(t, time.Now().Add(time.Hour), sidA))
	tokenB := signSession(t, sessionClaims(t, time.Now().Add(time.Hour), sidB))

	csrfA := setAuthCookiesFor(t, tokenA)
	if ValidateCSRF(csrfRequest(t, tokenB, csrfA)) {
		t.Error("a CSRF token from another session must not validate")
	}
}

// Everyone signed in when this ships holds a cookie pair minted under the old
// binding. Those must keep working until that session renews, or the deploy
// logs the whole userbase out of every state-changing action.
func TestValidateCSRF_AcceptsTokenBinding(t *testing.T) {
	legacy := signSession(t, sessionClaims(t, time.Now().Add(20*24*time.Hour), ""))

	values := cookieValues(t, legacy)
	if values[SessionCSRFCookieName] != "" {
		t.Error("a token without `sid` has no session to bind to; no session cookie should be set")
	}
	if !ValidateCSRF(legacyCSRFRequest(t, legacy, values[CSRFCookieName])) {
		t.Error("a pre-MUL-7436 cookie pair must still validate")
	}

	// And the token binding stays bound: it is keyed by the token itself, so
	// it must not validate against a different one.
	other := signSession(t, sessionClaims(t, time.Now().Add(time.Hour), ""))
	if ValidateCSRF(legacyCSRFRequest(t, other, values[CSRFCookieName])) {
		t.Error("a token-bound CSRF value must not validate against a different auth token")
	}
}

// Rolling BACK past MUL-7436 must not strand signed-in users able to read and
// unable to write. A previous release verifies only the token binding, so
// every cookie set here has to include one — that is the sole reason it is
// still issued alongside the session binding.
func TestSetAuthCookies_KeepsTokenBindingForRollback(t *testing.T) {
	sid, _ := NewSessionID()
	token := signSession(t, sessionClaims(t, time.Now().Add(20*24*time.Hour), sid))

	values := cookieValues(t, token)
	if values[CSRFCookieName] == "" {
		t.Fatal("the token-bound cookie must still be issued; without it a rollback cannot verify any write")
	}
	if values[SessionCSRFCookieName] == "" {
		t.Fatal("the session-bound cookie must be issued")
	}
	if values[CSRFCookieName] == values[SessionCSRFCookieName] {
		t.Error("the two cookies must carry different bindings")
	}

	// Exactly what a rolled-back server does: verify the token-bound value
	// against the auth cookie, knowing nothing about `sid`.
	if !verifyCSRFToken(values[CSRFCookieName], func(nonce []byte) []byte {
		return tokenCSRFSignature(token, nonce)
	}) {
		t.Error("the token-bound cookie must verify under the pre-MUL-7436 scheme")
	}

	// And a current server accepts it too, so a client that only ever sends
	// the token-bound header keeps working.
	if !ValidateCSRF(legacyCSRFRequest(t, token, values[CSRFCookieName])) {
		t.Error("a current server must still accept the token-bound header")
	}
}

// After a renewal the token-bound cookie is re-minted for the NEW token, so a
// rollback taken at any point still finds a usable pair — that is what makes
// the rollback path complete rather than only true at login.
func TestSetAuthCookies_TokenBindingTracksRenewal(t *testing.T) {
	sid, _ := NewSessionID()
	claims := sessionClaims(t, time.Now().Add(20*24*time.Hour), sid)
	renewed, _, err := RenewSessionToken(claims)
	if err != nil {
		t.Fatalf("RenewSessionToken: %v", err)
	}

	values := cookieValues(t, renewed)
	if !verifyCSRFToken(values[CSRFCookieName], func(nonce []byte) []byte {
		return tokenCSRFSignature(renewed, nonce)
	}) {
		t.Error("the renewed token-bound cookie must be keyed to the renewed token")
	}
}

// One header, two acceptable bindings: whichever cookie value a client
// chooses to echo, the server has to recognise it. This is what lets the
// single header name serve every client/server version combination without a
// CORS change — see CSRFHeaderName.
func TestValidateCSRF_AcceptsEitherCookieValueInTheOneHeader(t *testing.T) {
	sid, _ := NewSessionID()
	token := signSession(t, sessionClaims(t, time.Now().Add(20*24*time.Hour), sid))
	values := cookieValues(t, token)

	if !ValidateCSRF(csrfRequest(t, token, values[SessionCSRFCookieName])) {
		t.Error("the session-bound value must validate")
	}
	if !ValidateCSRF(csrfRequest(t, token, values[CSRFCookieName])) {
		t.Error("the token-bound value must validate")
	}

	// After a renewal the token-bound value from BEFORE it is stale, and the
	// session-bound one is not — which is the whole reason clients prefer it.
	renewed, _, err := RenewSessionToken(sessionClaims(t, time.Now().Add(20*24*time.Hour), sid))
	if err != nil {
		t.Fatalf("RenewSessionToken: %v", err)
	}
	if !ValidateCSRF(csrfRequest(t, renewed, values[SessionCSRFCookieName])) {
		t.Error("a session-bound value must survive renewal")
	}
	if ValidateCSRF(csrfRequest(t, renewed, values[CSRFCookieName])) {
		t.Error("a pre-renewal token-bound value must NOT still validate; clients fall back to the re-minted one")
	}
}

// Expiry is not this gate's business. SessionIDFromToken reads `sid` without
// enforcing it, so an expired-but-genuine cookie still validates here and the
// auth middleware answers with the 401 that ends the session.
func TestValidateCSRF_IgnoresExpirySoAuthCanAnswer(t *testing.T) {
	sid, _ := NewSessionID()
	live := signSession(t, sessionClaims(t, time.Now().Add(time.Hour), sid))
	expired := signSession(t, sessionClaims(t, time.Now().Add(-time.Hour), sid))

	csrf := setAuthCookiesFor(t, live)
	if !ValidateCSRF(csrfRequest(t, expired, csrf)) {
		t.Error("an expired session must fail authentication, not CSRF — a 403 here never reaches the session-expiry path")
	}
}

func TestValidateCSRF_RejectsMalformedAndMissing(t *testing.T) {
	sid, _ := NewSessionID()
	token := signSession(t, sessionClaims(t, time.Now().Add(time.Hour), sid))

	cases := map[string]string{
		"empty":          "",
		"no separator":   "abcdef",
		"bad nonce hex":  "zz.00",
		"bad sig hex":    "00.zz",
		"wrong sig":      "0011223344556677889900112233445566.00",
		"extra segments": "00.11.22",
	}
	for name, csrf := range cases {
		t.Run(name, func(t *testing.T) {
			if ValidateCSRF(csrfRequest(t, token, csrf)) {
				t.Errorf("malformed CSRF token %q must not validate", csrf)
			}
		})
	}

	// No auth cookie at all: nothing to bind to, so nothing validates.
	req := httptest.NewRequest(http.MethodPost, "/api/issues", nil)
	req.Header.Set("X-CSRF-Token", setAuthCookiesFor(t, token))
	if ValidateCSRF(req) {
		t.Error("CSRF must not validate without an auth cookie")
	}
}

func TestValidateCSRF_SafeMethodsSkipTheCheck(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		req := httptest.NewRequest(method, "/api/issues", nil)
		if !ValidateCSRF(req) {
			t.Errorf("%s must not require a CSRF token", method)
		}
	}
}
