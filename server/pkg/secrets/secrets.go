// Package secrets implements AES-256-GCM encryption-at-rest for
// workspace-scoped secrets like the GitHub PAT and the webhook HMAC
// secret introduced in Ship Hub Phase 2.
//
// The wire format is intentionally simple: a 12-byte random nonce is
// prepended to the GCM-sealed ciphertext. Decryption splits at offset
// 12 and authenticates with the same key + AAD-less GCM mode. This
// matches the Go stdlib's standard pattern and lets `Decrypt` recover
// any value `Encrypt` produced without an out-of-band header.
//
// Key handling lives in `LoadKey`, which reads the
// `MULTICA_SECRET_ENCRYPTION_KEY` env var as 32 bytes of hex (64 hex
// chars). When unset we WARN and derive a deterministic key from the
// existing JWT_SECRET fallback path so dev environments keep working
// without forcing every contributor to generate a key. Production
// deployments MUST set the env var explicitly — the warning makes that
// non-negotiable visible at boot.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
)

// EncryptionKeyEnv is the env var read by LoadKey. Exported so tests +
// docs can reference the canonical name without re-stringing it.
const EncryptionKeyEnv = "MULTICA_SECRET_ENCRYPTION_KEY"

// keyLen is the AES-256 key size in bytes. AES-256 because we want the
// strongest stdlib option; the perf delta vs AES-128 is negligible for
// the < 1KB payloads we encrypt.
const keyLen = 32

// nonceLen matches the GCM standard 12-byte nonce. Embedded directly in
// the output to avoid a separate header table.
const nonceLen = 12

// ErrCiphertextTooShort indicates a stored value that's smaller than
// the prefixed nonce — usually a corrupted row or a value written by a
// different (non-secrets) code path.
var ErrCiphertextTooShort = errors.New("secrets: ciphertext shorter than nonce")

var (
	keyOnce sync.Once
	cached  []byte
	keyErr  error
	// devKeyWarned ensures the "deterministic dev key" warning fires
	// exactly once per process even if LoadKey is called repeatedly.
	devKeyWarned bool
)

// LoadKey reads the encryption key from MULTICA_SECRET_ENCRYPTION_KEY.
// Returns the cached value on subsequent calls so repeated callers
// (every Encrypt/Decrypt) avoid re-parsing on every request.
//
// If the env var is missing we derive a deterministic 32-byte key from
// SHA-256(JWT_SECRET || "multica:secrets:v1") and emit a one-time WARN
// so operators see the issue without local dev breaking. Production
// MUST set the env var; the warning is a forcing function for
// deployment review.
func LoadKey() ([]byte, error) {
	keyOnce.Do(func() {
		raw := os.Getenv(EncryptionKeyEnv)
		if raw == "" {
			cached, keyErr = deriveDevKey()
			return
		}
		decoded, err := hex.DecodeString(raw)
		if err != nil {
			keyErr = fmt.Errorf("secrets: %s is not hex: %w", EncryptionKeyEnv, err)
			return
		}
		if len(decoded) != keyLen {
			keyErr = fmt.Errorf("secrets: %s must be %d bytes (%d hex chars), got %d",
				EncryptionKeyEnv, keyLen, keyLen*2, len(decoded))
			return
		}
		cached = decoded
	})
	return cached, keyErr
}

// deriveDevKey produces a deterministic 32-byte key from JWT_SECRET (or
// a static fallback when JWT_SECRET is also missing — i.e. clean dev
// machine). Logs once at WARN so the operator sees that secrets are
// effectively NOT encrypted for any real-world threat.
func deriveDevKey() ([]byte, error) {
	if !devKeyWarned {
		slog.Warn(
			"secrets: MULTICA_SECRET_ENCRYPTION_KEY is unset; deriving a deterministic dev key. "+
				"Workspace secrets are NOT cryptographically protected at rest in this state. "+
				"Set MULTICA_SECRET_ENCRYPTION_KEY before deploying to production.",
			"env_var", EncryptionKeyEnv,
		)
		devKeyWarned = true
	}
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		// Clean dev machine — use a stable but obviously-not-prod string
		// so we don't randomize on every boot (that would brick stored
		// secrets across server restarts).
		jwtSecret = "multica-dev-fallback-never-use-in-production"
	}
	sum := sha256.Sum256([]byte(jwtSecret + "|multica:secrets:v1"))
	return sum[:], nil
}

// Encrypt seals plaintext with AES-256-GCM and returns
// nonce || ciphertext. key must be exactly 32 bytes.
func Encrypt(plaintext []byte, key []byte) ([]byte, error) {
	if len(key) != keyLen {
		return nil, fmt.Errorf("secrets: key must be %d bytes, got %d", keyLen, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: new gcm: %w", err)
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("secrets: read nonce: %w", err)
	}
	// Pre-allocate one buffer so the nonce + sealed ciphertext are
	// contiguous in memory. Cleaner than concat-then-allocate.
	out := make([]byte, nonceLen, nonceLen+len(plaintext)+gcm.Overhead())
	copy(out, nonce)
	out = gcm.Seal(out, nonce, plaintext, nil)
	return out, nil
}

// Decrypt reverses Encrypt. Returns ErrCiphertextTooShort when the
// payload is shorter than the nonce header — the caller should treat
// that as a corrupt or uninitialized secret.
func Decrypt(ciphertext []byte, key []byte) (string, error) {
	if len(key) != keyLen {
		return "", fmt.Errorf("secrets: key must be %d bytes, got %d", keyLen, len(key))
	}
	if len(ciphertext) < nonceLen {
		return "", ErrCiphertextTooShort
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("secrets: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("secrets: new gcm: %w", err)
	}
	nonce, sealed := ciphertext[:nonceLen], ciphertext[nonceLen:]
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("secrets: gcm open: %w", err)
	}
	return string(plaintext), nil
}

// EncryptString is a convenience wrapper for callers that already hold
// the plaintext as a string. The key is fetched via LoadKey so call
// sites don't have to thread it through manually.
func EncryptString(plaintext string) ([]byte, error) {
	key, err := LoadKey()
	if err != nil {
		return nil, err
	}
	return Encrypt([]byte(plaintext), key)
}

// DecryptString is the round-trip mate of EncryptString.
func DecryptString(ciphertext []byte) (string, error) {
	key, err := LoadKey()
	if err != nil {
		return "", err
	}
	return Decrypt(ciphertext, key)
}

// GenerateRandomURLSafe returns a 32-byte random secret encoded as
// 64 hex chars. Used by the regenerate-webhook-secret endpoint. We
// pick hex (not base64URL) because GitHub's signature header gives us
// the same hex-encoded HMAC, and keeping both formats consistent avoids
// the kind of "did you base64 the secret first?" support thread that
// burns hours.
func GenerateRandomURLSafe() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("secrets: random read: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ResetForTest clears the cached key + warning flag so unit tests can
// exercise different env-var configurations without process restarts.
// The function is a deliberate seam — production code never calls it.
func ResetForTest() {
	keyOnce = sync.Once{}
	cached = nil
	keyErr = nil
	devKeyWarned = false
}
