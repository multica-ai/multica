package composio

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Signed-state errors. The handler maps all of them to a generic
// "connect failed" redirect so a tampered/expired state never leaks which
// check failed.
var (
	// ErrStateMalformed is returned when the state token is not the expected
	// "<payload>.<sig>" base64url shape.
	ErrStateMalformed = errors.New("composio: state malformed")
	// ErrStateSignature is returned when the HMAC signature does not match —
	// the state was tampered with or signed by a different secret.
	ErrStateSignature = errors.New("composio: state signature mismatch")
	// ErrStateExpired is returned when the state's exp claim is in the past.
	ErrStateExpired = errors.New("composio: state expired")
)

// stateClaims is the payload embedded in the signed connect-state. It carries
// exactly what CompleteCallback needs to attribute the callback to a user and
// toolkit without a server-side session table — the signature is what makes it
// trustworthy, the short exp is what bounds replay.
//
// Field names are single letters to keep the encoded token compact; they are
// an internal wire format, never exposed to clients.
type stateClaims struct {
	UserID      string `json:"u"`
	ToolkitSlug string `json:"t"`
	// AuthConfigID is the exact Composio auth_config_id resolved at BeginConnect
	// and used to create the connect link. Signing it into the state lets
	// CompleteCallback verify the returned account was created under THIS
	// toolkit's auth config without re-resolving (which could fail-open). It is
	// an opaque config handle (ac_…), not a credential.
	AuthConfigID string `json:"a"`
	Exp          int64  `json:"e"`
}

// signState produces a URL-safe "<payload>.<sig>" token. payload is the
// base64url-encoded JSON claims; sig is the base64url-encoded HMAC-SHA256 of
// the payload under the service secret. We sign the encoded payload (not the
// raw struct) so verification re-derives the exact bytes that were signed.
func signState(secret []byte, claims stateClaims) (string, error) {
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	sig := signPayload(secret, payload)
	return payload + "." + sig, nil
}

// verifyState validates the signature and expiry of a token produced by
// signState and returns the embedded claims. Signature is checked with a
// constant-time compare before the payload is trusted; expiry is checked
// against now.
func verifyState(secret []byte, token string, now time.Time) (stateClaims, error) {
	payload, sig, found := strings.Cut(token, ".")
	if !found || payload == "" || sig == "" {
		return stateClaims{}, ErrStateMalformed
	}
	expected := signPayload(secret, payload)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return stateClaims{}, ErrStateSignature
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return stateClaims{}, ErrStateMalformed
	}
	var claims stateClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return stateClaims{}, ErrStateMalformed
	}
	if now.Unix() > claims.Exp {
		return stateClaims{}, ErrStateExpired
	}
	return claims, nil
}

// signPayload returns the base64url HMAC-SHA256 of payload under secret.
func signPayload(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
