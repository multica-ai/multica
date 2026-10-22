package auth

import (
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// resetAuthTokenTTLForTest makes AuthTokenTTL() re-read the environment.
// The production path caches it behind a sync.Once on purpose — the TTL is
// deployment configuration, not per-request state — so a test that wants a
// specific TTL has to clear the cache on both sides of itself.
func resetAuthTokenTTLForTest(t *testing.T) {
	t.Helper()
	clear := func() {
		authTokenTTLOnce = sync.Once{}
		authTokenTTLCached = 0
	}
	clear()
	t.Cleanup(clear)
}

func sessionClaims(t *testing.T, exp time.Time, sid string) jwt.MapClaims {
	t.Helper()
	// float64, not int64: every claims map the production code sees comes out
	// of jwt.Parse, which JSON-decodes numeric dates into float64. An int64
	// here would make MapClaims.GetExpirationTime fail in a way it never does
	// at runtime, and the test would be asserting against a shape that does
	// not exist.
	claims := jwt.MapClaims{
		"sub":   "user-1",
		"email": "user-1@multica.ai",
		"name":  "User One",
		"exp":   float64(exp.Unix()),
		"iat":   float64(time.Now().Add(-time.Hour).Unix()),
	}
	if sid != "" {
		claims["sid"] = sid
	}
	return claims
}

func signSession(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(JWTSecret())
	if err != nil {
		t.Fatalf("sign session token: %v", err)
	}
	return signed
}

// The renewal window is the defining behaviour of a sliding session: too
// eager and every response carries a Set-Cookie, too lazy and an active user
// still gets logged out. Half the TTL is the contract Bohan approved on
// MUL-7436, so pin both sides of the boundary.
func TestShouldRenewSession_HalfTTLBoundary(t *testing.T) {
	t.Setenv("AUTH_TOKEN_TTL", "720h") // 30 days
	resetAuthTokenTTLForTest(t)

	now := time.Now()
	ttl := AuthTokenTTL()

	cases := []struct {
		name      string
		remaining time.Duration
		want      bool
	}{
		{"freshly issued", ttl, false},
		{"just above half", ttl/2 + time.Minute, false},
		{"exactly half", ttl / 2, true},
		{"just below half", ttl/2 - time.Minute, true},
		{"nearly expired", time.Minute, true},
		// An expired session is dead, not stale. Renewal extends a live
		// session; it must never be a way to resurrect one.
		{"expired a second ago", -time.Second, false},
		{"long expired", -30 * 24 * time.Hour, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldRenewSession(now, now.Add(tc.remaining)); got != tc.want {
				t.Errorf("ShouldRenewSession(remaining=%s) = %v, want %v", tc.remaining, got, tc.want)
			}
		})
	}
}

func TestShouldRenewSession_NoExpiryIsNotRenewable(t *testing.T) {
	if ShouldRenewSession(time.Now(), time.Time{}) {
		t.Error("a token with no exp must not be renewable")
	}
}

// Clients must never hardcode a cadence: a deployment that shortens the TTL
// has to get proportionally more frequent checks, or its sessions die between
// two of them.
func TestSessionRenewCheckInterval_TracksTTL(t *testing.T) {
	cases := []struct {
		ttl  string
		want time.Duration
		why  string
	}{
		{"720h", 72 * time.Hour, "30d default → TTL/10 = 3d, same as the daemon's PAT cadence"},
		{"24h", 144 * time.Minute, "short TTL → proportionally short cadence"},
		{"30m", 3 * time.Minute, "TTL/10, comfortably inside the 15m window"},
		{"10m", time.Minute, "the TTL the PR's manual test plan uses"},
		{"1m", 6 * time.Second, "the shortest supported TTL; 6s is expressible in whole seconds"},
		{"20s", 6 * time.Second, "below MinAuthTokenTTL, so the TTL itself is clamped up to 1m first"},
		{"87600h", 7 * 24 * time.Hour, "10y → clamped at the ceiling"},
	}
	for _, tc := range cases {
		t.Run(tc.ttl, func(t *testing.T) {
			t.Setenv("AUTH_TOKEN_TTL", tc.ttl)
			resetAuthTokenTTLForTest(t)
			if got := SessionRenewCheckInterval(); got != tc.want {
				t.Errorf("SessionRenewCheckInterval() = %s, want %s (TTL=%s: %s)", got, tc.want, tc.ttl, tc.why)
			}
		})
	}
}

// The cadence has exactly one property that must hold for every configurable
// TTL: a client that follows it gets at least two chances inside the renewal
// window. Without that a short TTL produces an interval LONGER than the window
// — and a client doing everything right is logged out while actively using the
// app. A flat five-minute floor did that for any TTL under ten minutes.
//
// The assertion is made against the value that actually reaches a client —
// whole seconds, as it is serialised — not the internal time.Duration. Those
// differ, and only the serialised one governs client behaviour.
func TestSessionRenewCheckInterval_WireValueFitsInsideTheRenewalWindow(t *testing.T) {
	for _, ttl := range []string{"1s", "10s", "1m", "5m", "10m", "30m", "1h", "24h", "720h", "8760h", "87600h"} {
		t.Run(ttl, func(t *testing.T) {
			t.Setenv("AUTH_TOKEN_TTL", ttl)
			resetAuthTokenTTLForTest(t)

			// Exactly what handler.RefreshSession puts on the wire.
			wireSeconds := int(SessionRenewCheckInterval().Seconds())
			if wireSeconds < 1 {
				t.Fatalf("cadence serialises to %ds — a client is told to check immediately, forever (TTL=%s)", wireSeconds, ttl)
			}

			asClientSeesIt := time.Duration(wireSeconds) * time.Second
			threshold := AuthRenewThreshold()
			if asClientSeesIt > threshold/2 {
				t.Errorf("client-visible cadence %s leaves fewer than two attempts inside a %s window (TTL=%s)",
					asClientSeesIt, threshold, ttl)
			}
		})
	}
}

// Clients floor the server's cadence to protect themselves from a nonsense
// value. That floor must never be the thing that decides the cadence, so the
// server's own smallest possible value has to sit above it.
func TestSessionRenewCheckInterval_StaysAboveTheClientFloors(t *testing.T) {
	// packages/core/platform/session-renewal.ts and
	// apps/mobile/data/session-renewal.ts both floor at 5 seconds.
	const clientFloor = 5 * time.Second

	for _, ttl := range []string{"1s", "30s", "1m", "10m", "720h"} {
		t.Run(ttl, func(t *testing.T) {
			t.Setenv("AUTH_TOKEN_TTL", ttl)
			resetAuthTokenTTLForTest(t)

			asClientSeesIt := time.Duration(int(SessionRenewCheckInterval().Seconds())) * time.Second
			if asClientSeesIt < clientFloor {
				t.Errorf("server cadence %s is below the %s client floor, so clients would silently override it (TTL=%s)",
					asClientSeesIt, clientFloor, ttl)
			}
		})
	}
}

// A TTL too short to serve correctly is clamped up rather than honoured or
// replaced by the default. Honouring it would mean telling clients to check
// on a cadence the wire cannot carry; falling back to 30 days would be a far
// bigger surprise than the one-minute floor.
func TestAuthTokenTTL_ClampsBelowTheSupportedMinimum(t *testing.T) {
	cases := map[string]time.Duration{
		"1s":  MinAuthTokenTTL,
		"30s": MinAuthTokenTTL,
		"59s": MinAuthTokenTTL,
		"1m":  time.Minute,
		"10m": 10 * time.Minute,
	}
	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("AUTH_TOKEN_TTL", raw)
			resetAuthTokenTTLForTest(t)
			if got := AuthTokenTTL(); got != want {
				t.Errorf("AuthTokenTTL() = %s, want %s (AUTH_TOKEN_TTL=%s)", got, want, raw)
			}
		})
	}
}

// Renewal reads no database, so the claims it copies forward ARE the identity
// the next request authenticates as. Dropping one would silently downgrade
// the session.
func TestRenewSessionToken_CarriesIdentityForward(t *testing.T) {
	t.Setenv("AUTH_TOKEN_TTL", "720h")
	resetAuthTokenTTLForTest(t)

	old := sessionClaims(t, time.Now().Add(24*time.Hour), "session-abc")
	token, expiresAt, err := RenewSessionToken(old)
	if err != nil {
		t.Fatalf("RenewSessionToken: %v", err)
	}

	next, err := ParseSessionToken(token)
	if err != nil {
		t.Fatalf("renewed token does not parse: %v", err)
	}
	for _, claim := range []string{"sub", "email", "name", "sid"} {
		if next[claim] != old[claim] {
			t.Errorf("claim %q = %v, want %v", claim, next[claim], old[claim])
		}
	}

	// exp moved to a full TTL from now, not from the old expiry.
	want := time.Now().Add(AuthTokenTTL())
	if delta := expiresAt.Sub(want); delta > time.Minute || delta < -time.Minute {
		t.Errorf("new expiry %s is not ~now+TTL (%s)", expiresAt, want)
	}
	if got := SessionExpiry(next); got.Unix() != expiresAt.Unix() {
		t.Errorf("exp claim %s does not match returned expiry %s", got, expiresAt)
	}
}

// Sessions that predate MUL-7436 carry no `sid`. They must still renew — this
// is the migration path for everyone logged in when this ships — and the
// renewed token has to acquire one so its CSRF binding becomes stable.
func TestRenewSessionToken_MintsSessionIDForLegacyToken(t *testing.T) {
	legacy := sessionClaims(t, time.Now().Add(24*time.Hour), "")

	token, _, err := RenewSessionToken(legacy)
	if err != nil {
		t.Fatalf("RenewSessionToken: %v", err)
	}
	if sid := SessionIDFromToken(token); sid == "" {
		t.Error("renewed legacy token must carry a session id")
	}
}

// Renewal is the one place a live session's lifetime is decided after login,
// so the disabled-user check has to live there too — otherwise disabling an
// abusive account leaves their session renewing itself forever.
func TestRenewSessionToken_RejectsDisabledUser(t *testing.T) {
	// One of the ids on the standing emergency denylist.
	var disabledID string
	for id := range temporarilyDisabledUserIDs {
		disabledID = id
		break
	}
	claims := sessionClaims(t, time.Now().Add(time.Hour), "sid-1")
	claims["sub"] = disabledID

	_, _, err := RenewSessionToken(claims)
	if err != ErrTemporarilyDisabledUser {
		t.Fatalf("err = %v, want ErrTemporarilyDisabledUser", err)
	}
}

func TestRenewSessionToken_RejectsClaimsWithoutSubject(t *testing.T) {
	_, _, err := RenewSessionToken(jwt.MapClaims{"email": "nobody@multica.ai"})
	if err != ErrNotSessionToken {
		t.Fatalf("err = %v, want ErrNotSessionToken", err)
	}
}

// Every non-JWT credential the auth middleware accepts must be unable to
// become an interactive session. These all authenticate fine; none of them is
// a session.
func TestParseSessionToken_RejectsNonSessionCredentials(t *testing.T) {
	cases := map[string]string{
		"personal access token": "mul_0123456789abcdef0123456789abcdef01234567",
		"agent task token":      "mat_0123456789abcdef0123456789abcdef01234567",
		"cloud node pat":        "mcn_0123456789abcdef0123456789abcdef01234567",
		"empty":                 "",
		"garbage":               "not-a-token",
		"wrong secret": func() string {
			s, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
				"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix(),
			}).SignedString([]byte("some-other-secret"))
			return s
		}(),
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseSessionToken(token); err != ErrNotSessionToken {
				t.Errorf("err = %v, want ErrNotSessionToken", err)
			}
		})
	}
}

// An expired JWT must not parse, which is what keeps the refresh endpoint
// from being a resurrection mechanism even before ShouldRenewSession looks at
// the clock.
func TestParseSessionToken_RejectsExpiredToken(t *testing.T) {
	expired := signSession(t, sessionClaims(t, time.Now().Add(-time.Minute), "sid-1"))
	if _, err := ParseSessionToken(expired); err != ErrNotSessionToken {
		t.Errorf("err = %v, want ErrNotSessionToken", err)
	}
}

func TestSessionIDFromToken(t *testing.T) {
	withSID := signSession(t, sessionClaims(t, time.Now().Add(time.Hour), "sid-xyz"))
	if got := SessionIDFromToken(withSID); got != "sid-xyz" {
		t.Errorf("SessionIDFromToken() = %q, want %q", got, "sid-xyz")
	}

	legacy := signSession(t, sessionClaims(t, time.Now().Add(time.Hour), ""))
	if got := SessionIDFromToken(legacy); got != "" {
		t.Errorf("legacy token session id = %q, want empty", got)
	}

	if got := SessionIDFromToken("mul_deadbeef"); got != "" {
		t.Errorf("PAT session id = %q, want empty", got)
	}
}

// A session that is used at least once inside every renewal window never
// expires. This is the behaviour the issue asks for, stated as a simulation
// rather than as a property of one call.
func TestSlidingSession_ContinuousUseOutlivesTTL(t *testing.T) {
	t.Setenv("AUTH_TOKEN_TTL", "720h") // 30 days
	resetAuthTokenTTLForTest(t)

	ttl := AuthTokenTTL()
	claims := sessionClaims(t, time.Now().Add(ttl), "sid-live")

	// Use the app every 10 days for 120 days — past both the old 30-day wall
	// and the 90-day absolute cap that was considered and dropped.
	now := time.Now()
	for day := 10; day <= 120; day += 10 {
		now = now.Add(10 * 24 * time.Hour)
		expiresAt := SessionExpiry(claims)
		if !expiresAt.After(now) {
			t.Fatalf("session expired on day %d — an actively used session must not", day)
		}
		if !ShouldRenewSession(now, expiresAt) {
			continue
		}
		token, _, err := RenewSessionToken(claims)
		if err != nil {
			t.Fatalf("day %d: RenewSessionToken: %v", day, err)
		}
		renewed, err := ParseSessionToken(token)
		if err != nil {
			t.Fatalf("day %d: renewed token does not parse: %v", day, err)
		}
		// RenewSessionToken always dates from the real clock, so re-stamp the
		// simulated expiry to keep the loop honest about the TTL it grants.
		renewed["exp"] = float64(now.Add(ttl).Unix())
		claims = renewed
	}

	if sid, _ := claims["sid"].(string); sid != "sid-live" {
		t.Errorf("session id changed across renewals: %q — CSRF binding would break", sid)
	}
}

// The flip side: stop using the app and the session does die. Renewal removes
// the scheduled logout, not expiry itself.
func TestSlidingSession_IdleSessionStillExpires(t *testing.T) {
	t.Setenv("AUTH_TOKEN_TTL", "720h")
	resetAuthTokenTTLForTest(t)

	ttl := AuthTokenTTL()
	lastUsed := time.Now()
	expiresAt := lastUsed.Add(ttl)

	// Last request came in while the token still had more than half its life
	// left, so nothing was renewed...
	if ShouldRenewSession(lastUsed, expiresAt) {
		t.Fatal("a freshly issued session must not be renewable")
	}
	// ...and the session is gone one TTL after it was issued.
	if ShouldRenewSession(lastUsed.Add(ttl+time.Second), expiresAt) {
		t.Error("an expired idle session must not be renewable")
	}
}

// SessionIDFromToken must answer "which session is this" for a token whose
// signature is ours, even once it has expired. It feeds CSRF validation, which
// runs before authentication — returning "" for an expired token made a valid
// CSRF value fail and turned session expiry into a 403 on the user's next
// write instead of the 401 that logs them out.
func TestSessionIDFromToken_ReadsExpiredTokens(t *testing.T) {
	expired := signSession(t, sessionClaims(t, time.Now().Add(-24*time.Hour), "sid-expired"))
	if got := SessionIDFromToken(expired); got != "sid-expired" {
		t.Errorf("SessionIDFromToken(expired) = %q, want sid-expired", got)
	}

	// Still only OUR tokens: a forged one has no session to name.
	forged, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user-1", "sid": "forged", "exp": float64(time.Now().Add(-time.Hour).Unix()),
	}).SignedString([]byte("some-other-secret"))
	if got := SessionIDFromToken(forged); got != "" {
		t.Errorf("SessionIDFromToken(forged) = %q, want empty", got)
	}
}

// The authentication parser keeps enforcing expiry, which is what stops the
// relaxed read above from leaking into anything that decides access. These two
// answers must stay different for the same token.
func TestParseSessionToken_StillEnforcesExpiryAfterTheSplit(t *testing.T) {
	expired := signSession(t, sessionClaims(t, time.Now().Add(-time.Minute), "sid-x"))

	if _, err := ParseSessionToken(expired); err != ErrNotSessionToken {
		t.Errorf("ParseSessionToken(expired) err = %v, want ErrNotSessionToken", err)
	}
	if SessionIDFromToken(expired) == "" {
		t.Error("SessionIDFromToken must still read the same token")
	}
}
