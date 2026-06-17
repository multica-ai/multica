package util

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// RandomToken returns n bytes of CSPRNG entropy encoded as URL-safe, unpadded
// base64 — safe to embed in a URL without escaping. Used for short-lived
// binding tokens, where the raw value is shown once and only its hash is stored.
func RandomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashTokenSHA256 returns the hex-encoded SHA-256 of raw. Binding tokens store
// only this hash (never the raw token) so a database leak cannot be replayed.
func HashTokenSHA256(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
