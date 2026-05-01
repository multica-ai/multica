package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"sync"
)

// MinJWTSecretBytes is the minimum length we accept for JWT_SECRET.
// 32 bytes (256 bits) matches HS256 output size.
const MinJWTSecretBytes = 32

var (
	jwtSecret     []byte
	jwtSecretOnce sync.Once
)

// resolveJWTSecret reads JWT_SECRET from the environment and validates it.
// Returns an error if the value is unset or shorter than MinJWTSecretBytes.
func resolveJWTSecret() ([]byte, error) {
	s := os.Getenv("JWT_SECRET")
	if s == "" {
		return nil, fmt.Errorf("JWT_SECRET is not set — refusing to boot. Generate one with: openssl rand -hex 32")
	}
	if len(s) < MinJWTSecretBytes {
		return nil, fmt.Errorf("JWT_SECRET must be at least %d bytes (got %d) — refusing to boot. Generate one with: openssl rand -hex 32", MinJWTSecretBytes, len(s))
	}
	return []byte(s), nil
}

// JWTSecret returns the configured JWT signing secret. It calls log.Fatal
// if JWT_SECRET is unset or shorter than MinJWTSecretBytes — there is no
// fallback to a default value.
func JWTSecret() []byte {
	jwtSecretOnce.Do(func() {
		secret, err := resolveJWTSecret()
		if err != nil {
			log.Fatalf("auth: %v", err)
		}
		jwtSecret = secret
	})
	return jwtSecret
}

// GeneratePATToken creates a new personal access token: "mul_" + 40 random hex chars.
func GeneratePATToken() (string, error) {
	b := make([]byte, 20) // 20 bytes = 40 hex chars
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate PAT token: %w", err)
	}
	return "mul_" + hex.EncodeToString(b), nil
}

// GenerateDaemonToken creates a new daemon auth token: "mdt_" + 40 random hex chars.
func GenerateDaemonToken() (string, error) {
	b := make([]byte, 20) // 20 bytes = 40 hex chars
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate daemon token: %w", err)
	}
	return "mdt_" + hex.EncodeToString(b), nil
}

// HashToken returns the hex-encoded SHA-256 hash of a token string.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
