package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
)

// HMAC verification errors. These are sentinel values rather than
// strings so the router can map them to specific status codes (401 for
// signature problems, 400 for envelope problems) without a string
// match.
var (
	// ErrSignatureMissing — request did not include the configured
	// signature header. 401, alert on rate > 5/min (per Observability
	// section: attack signal).
	ErrSignatureMissing = errors.New("webhooks: signature header missing")

	// ErrSignatureMalformed — header present but does not match the
	// expected encoding (e.g. GitHub uses `sha256=<hex>` and we got
	// `<hex>` alone, or a non-hex character snuck in). 400.
	ErrSignatureMalformed = errors.New("webhooks: signature malformed")

	// ErrSignatureInvalid — header well-formed, but the HMAC over the
	// body computed with either current or previous secret did not
	// match. 401. Constant-time compare under the hood.
	ErrSignatureInvalid = errors.New("webhooks: signature invalid")
)

// VerifyHMACSHA256 implements the GitHub-style "sha256=<hex>" signature
// scheme used by github, linear, and slack v0 (slack's pre-amble is
// different but the body computation is identical). It is the only
// signature scheme PR2 supports; vendors that use a different scheme
// (gitlab's plain shared-secret header, jwt-based providers) return
// empty SignatureHeader and own their auth path internally.
//
// Inputs:
//   - signatureHeader: raw header value, e.g. "sha256=ab12cd…"
//   - body:            raw HTTP request body bytes (hashed verbatim).
//   - current, previous: secrets returned by Source.Secrets. Either may
//                        validate the signature; `previous` is non-empty
//                        only during the 24h rotation window.
//
// Returns nil on a valid signature, ErrSignatureMissing /
// ErrSignatureMalformed / ErrSignatureInvalid otherwise. Constant-time
// comparison via hmac.Equal — never short-circuits on length mismatch
// for the actual compare step.
func VerifyHMACSHA256(signatureHeader string, body, current, previous []byte) error {
	if signatureHeader == "" {
		return ErrSignatureMissing
	}
	// Expected envelope: "sha256=<64-char hex>". Strict prefix match;
	// no whitespace tolerance, no case-folding on the algorithm tag
	// (matches what GitHub actually sends — being permissive here only
	// makes the auth boundary surface larger).
	const prefix = "sha256="
	if !strings.HasPrefix(signatureHeader, prefix) {
		return ErrSignatureMalformed
	}
	hexSig := signatureHeader[len(prefix):]
	if len(hexSig) != sha256.Size*2 {
		return ErrSignatureMalformed
	}
	wantSig, err := hex.DecodeString(hexSig)
	if err != nil {
		return ErrSignatureMalformed
	}

	// Try the current secret first — the steady-state path. Computing
	// twice when previous is empty would be a waste, so the previous
	// path is gated on `len(previous) > 0`. Both branches use
	// hmac.Equal for constant-time compare; the surrounding if/else is
	// fine to short-circuit on because the timing leak between
	// "current valid" and "fell through to previous" only reveals that
	// rotation is in progress, which is already public info (the
	// `previous` env var is rotated by ops).
	mac := hmac.New(sha256.New, current)
	mac.Write(body)
	if hmac.Equal(mac.Sum(nil), wantSig) {
		return nil
	}
	if len(previous) > 0 {
		mac = hmac.New(sha256.New, previous)
		mac.Write(body)
		if hmac.Equal(mac.Sum(nil), wantSig) {
			return nil
		}
	}
	return ErrSignatureInvalid
}

// hmacStatusCode maps a VerifyHMACSHA256 sentinel error to its
// HTTP status. Centralised here so the router and any future
// per-source bypass path stay consistent.
func hmacStatusCode(err error) int {
	switch {
	case errors.Is(err, ErrSignatureMissing):
		return http.StatusUnauthorized
	case errors.Is(err, ErrSignatureMalformed):
		return http.StatusBadRequest
	case errors.Is(err, ErrSignatureInvalid):
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}
