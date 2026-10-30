package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Sliding session renewal (MUL-7436).
//
// A UI session token used to have its lifetime fixed at login: `exp` was
// stamped once and never moved, so every continuously active user was logged
// out on a schedule — 30 days after login, always while they were mid-task.
// Renewal turns that into an IDLE bound instead: a token inside its renewal
// window is re-signed with a fresh `exp` on the next authenticated request.
//
// The model is deliberately stateless. Renewal copies the identity claims of
// a token we ourselves signed and moves `exp`; there is no session row, no
// refresh token, and no DB read on the hot path. What that buys in
// simplicity it gives up in revocation — which the JWT path never had
// anyway (logout clears the cookie; the token itself stays valid until
// `exp`). Real revocation needs server-side session records and is tracked
// separately.
//
// The idle bound is approximate, and the approximation is one-sided: a
// session is renewed only once it drops below half its TTL, so the guarantee
// is "at least TTL/2 of idleness is survivable", not "exactly TTL after the
// last request". A token signed on day 0 and last used on day 14 was not yet
// eligible, so it still expires on day 30 — 16 idle days, not 30. Sessions
// used at any point inside the second half of their lifetime get the full
// TTL from that moment.

const (
	// sessionIDClaim carries a per-login identifier that is STABLE across
	// renewals and changes only when the user authenticates again. It is
	// what the CSRF token is bound to, so re-signing the auth cookie does
	// not invalidate CSRF tokens other tabs are already holding.
	sessionIDClaim = "sid"

	// sessionRenewCheckDivisor derives the client-side re-check cadence
	// from the configured TTL: a client that checks every TTL/10 gets
	// roughly five attempts inside the renewal window, so several
	// consecutive failures still leave the session recoverable. At the
	// default 30-day TTL this is 3 days, matching the daemon's PAT
	// renewal cadence (DefaultTokenRenewalInterval).
	sessionRenewCheckDivisor = 10

	// minSessionRenewCheckInterval is a pure anti-busy-loop floor, not a
	// policy. With MinAuthTokenTTL enforced the smallest cadence this can
	// ever produce is six seconds, so this floor never actually binds; it
	// stays as a backstop, and the window cap below outranks it either way,
	// because an interval that cannot fit inside the renewal window is not a
	// cadence, it is a guaranteed logout.
	//
	// Every client floor must be at or below this value, or the client would
	// silently override a cadence the server chose for correctness. See
	// packages/core/platform/session-renewal.ts and
	// apps/mobile/data/session-renewal.ts.
	minSessionRenewCheckInterval = 5 * time.Second

	// maxSessionRenewCheckInterval bounds the other end: a client that has
	// not asked in a week is far enough from its own state that checking is
	// cheap insurance, whatever the TTL says.
	maxSessionRenewCheckInterval = 7 * 24 * time.Hour
)

// ErrNotSessionToken is returned by RenewSessionToken when handed something
// that is not one of our own UI session JWTs. PATs (mul_), agent task tokens
// (mat_) and cloud-node PATs (mcn_) authenticate fine at the middleware but
// must never be exchanged for an interactive session.
var ErrNotSessionToken = errors.New("not an interactive session token")

// AuthRenewThreshold is the remaining lifetime at or below which a session
// token becomes eligible for renewal. Half the TTL is the largest threshold
// that still leaves renewal rare (one re-sign per TTL/2 of continuous use)
// while giving an intermittently used client a window measured in days
// rather than minutes to land a successful renewal.
func AuthRenewThreshold() time.Duration { return AuthTokenTTL() / 2 }

// SessionRenewCheckInterval is how long a token-mode client should wait
// before asking whether its session is renewable again. Derived from the
// configured TTL so a deployment that shortens AUTH_TOKEN_TTL automatically
// gets proportionally more frequent checks — clients must never hardcode a
// cadence tuned for the 30-day default.
//
// The cadence has one hard requirement: it must fit inside the renewal
// window, or a client that follows it can miss the window entirely and be
// logged out while actively using the app. Capping at half the threshold
// guarantees at least two attempts inside it, so a single failed check is
// never fatal. That cap is applied last, after both clamps, because it is
// the correctness constraint and they are only guard rails.
func SessionRenewCheckInterval() time.Duration {
	interval := AuthTokenTTL() / sessionRenewCheckDivisor
	if interval < minSessionRenewCheckInterval {
		interval = minSessionRenewCheckInterval
	}
	if interval > maxSessionRenewCheckInterval {
		interval = maxSessionRenewCheckInterval
	}
	if cap := AuthRenewThreshold() / 2; interval > cap {
		interval = cap
	}
	return interval
}

// ShouldRenewSession reports whether a token expiring at expiresAt is inside
// the renewal window at now.
//
// An already-expired token is never renewable: `exp` in the past means the
// credential is dead, and renewal must not be a way to resurrect one. The
// middleware rejects those before this is ever reached; this is the second
// lock on the same door.
func ShouldRenewSession(now, expiresAt time.Time) bool {
	if expiresAt.IsZero() {
		return false
	}
	remaining := expiresAt.Sub(now)
	if remaining <= 0 {
		return false
	}
	return remaining <= AuthRenewThreshold()
}

// NewSessionID mints the per-login identifier stamped into the `sid` claim.
func NewSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// SessionExpiry reads the `exp` claim as a time. Zero when absent or
// malformed — callers treat that as "not renewable" rather than "never
// expires", because a session token we signed always has one.
func SessionExpiry(claims jwt.MapClaims) time.Time {
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return time.Time{}
	}
	return exp.Time
}

func parseSignedToken(tokenString string, opts ...jwt.ParserOption) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenString, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return JWTSecret(), nil
	}, opts...)
	if err != nil || !token.Valid {
		return nil, ErrNotSessionToken
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, ErrNotSessionToken
	}
	return claims, nil
}

// ParseSessionToken verifies a raw token string as one of our UI session
// JWTs and returns its claims. Anything that is not a validly signed,
// unexpired HS256 JWT — including every opaque token prefix — comes back as
// ErrNotSessionToken.
//
// This is the AUTHENTICATION parser: use it wherever the answer decides
// whether the caller is logged in or whether a session may be extended.
func ParseSessionToken(tokenString string) (jwt.MapClaims, error) {
	return parseSignedToken(tokenString)
}

// SessionIDFromToken returns the `sid` claim of a token we signed, or "" for
// anything else — an opaque PAT, a forged string, or one of the pre-MUL-7436
// JWTs minted before the claim existed. "" is the signal to fall back to the
// legacy CSRF binding; see ValidateCSRF.
//
// Expiry is deliberately NOT enforced here, and that distinction is
// load-bearing. This function answers "which session does this cookie
// belong to", not "may this cookie authenticate" — the signature alone
// settles the first question, and the auth middleware settles the second a
// few lines later. Enforcing expiry here instead meant an EXPIRED cookie
// lost its `sid`, so its perfectly valid CSRF token failed to verify and the
// user's first action after expiry was answered with 403 "CSRF validation
// failed" rather than the 401 that ends the session and sends them to the
// login page. A rejected CSRF token must never be how a user learns their
// session ended.
func SessionIDFromToken(tokenString string) string {
	claims, err := parseSignedToken(tokenString, jwt.WithoutClaimsValidation())
	if err != nil {
		return ""
	}
	sid, _ := claims[sessionIDClaim].(string)
	return sid
}

// RenewSessionToken re-signs the identity carried by claims with a fresh
// lifetime, returning the new token and its expiry.
//
// Only `exp` and `iat` move. `sub`, `email`, `name` and `sid` are copied
// forward verbatim, which is what makes renewal free of any database read:
// the claims were signed by this server, so they are already authoritative
// for exactly the fields the auth middleware reads back out.
//
// A pre-MUL-7436 token carries no `sid`. Renewal mints one, which is the
// migration path for sessions that predate this code — and the one moment
// where a CSRF token already read by another tab stops matching. It happens
// once per legacy session, on a safe-method request; clients retry a CSRF
// rejection once (packages/core/api/client.ts).
func RenewSessionToken(claims jwt.MapClaims) (string, time.Time, error) {
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return "", time.Time{}, ErrNotSessionToken
	}
	email, _ := claims["email"].(string)

	// A user disabled since their token was signed must not have that token
	// extended — renewal is the one place a live session's lifetime is
	// decided after login, so the check belongs here as much as at issuance.
	if IsTemporarilyDisabledUser(sub, email) {
		return "", time.Time{}, ErrTemporarilyDisabledUser
	}

	sid, _ := claims[sessionIDClaim].(string)
	if sid == "" {
		var err error
		if sid, err = NewSessionID(); err != nil {
			return "", time.Time{}, err
		}
	}

	now := time.Now()
	expiresAt := now.Add(AuthTokenTTL())
	next := jwt.MapClaims{
		"sub":          sub,
		"exp":          expiresAt.Unix(),
		"iat":          now.Unix(),
		sessionIDClaim: sid,
	}
	if email != "" {
		next["email"] = email
	}
	if name, ok := claims["name"].(string); ok && name != "" {
		next["name"] = name
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, next).SignedString(JWTSecret())
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}
