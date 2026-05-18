package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

const defaultJWTSecret = "multica-dev-secret-change-in-production"
const placeholderJWTSecret = "change-me-in-production"
const defaultSessionDuration = 30 * 24 * time.Hour

var (
	jwtSecret     []byte
	jwtSecretOnce sync.Once
)

func JWTSecret() []byte {
	jwtSecretOnce.Do(func() {
		secret := os.Getenv("JWT_SECRET")
		if secret == "" {
			secret = defaultJWTSecret
		}
		jwtSecret = []byte(secret)
	})

	return jwtSecret
}

func JWTSecretIsConfigured() bool {
	secret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	return secret != "" && secret != defaultJWTSecret && secret != placeholderJWTSecret
}

func ValidateJWTSecretConfiguration() error {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") && !JWTSecretIsConfigured() {
		return fmt.Errorf("JWT_SECRET is required in production and must be a persistent random value")
	}
	return nil
}

func SessionDuration() time.Duration {
	raw := strings.TrimSpace(os.Getenv("MULTICA_SESSION_TTL"))
	if raw == "" {
		return defaultSessionDuration
	}
	duration, err := time.ParseDuration(raw)
	if err != nil || duration <= 0 {
		slog.Warn("MULTICA_SESSION_TTL is invalid; using default browser session lifetime")
		return defaultSessionDuration
	}
	return duration
}

func SessionMaxAgeSeconds() int {
	return int(SessionDuration().Seconds())
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
