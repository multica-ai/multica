package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// KeyEnvVar is the env var holding the master encryption key for credential
// plaintext. The value is a base64-encoded 32-byte (AES-256) key. If unset,
// the registry refuses to start — there is no implicit insecure default.
const KeyEnvVar = "MULTICA_CREDENTIALS_KEY"

// ErrCipherMissing is surfaced when no Cipher has been wired (production
// build without MULTICA_CREDENTIALS_KEY). It exists so the handler can map
// it to a 503-style error instead of a generic 500.
var ErrCipherMissing = errors.New("credentials encryption is not configured")

// ErrInvalidCiphertext signals that the BYTEA payload could not be
// authenticated — either the key changed or the row was tampered with.
var ErrInvalidCiphertext = errors.New("credential ciphertext is invalid or has been tampered with")

// Cipher wraps an AES-256-GCM AEAD primitive. The same instance is reused
// across every encrypt/decrypt call; cipher.AEAD is safe for concurrent
// use. The key never leaves this struct.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher constructs a Cipher from a 32-byte raw key. Use NewCipherFromEnv
// for the production path; this constructor is exposed for tests that need a
// deterministic in-memory key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("credential cipher key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes.NewCipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher.NewGCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// MustNewCipherFromEnv is the one-line variant of NewCipherFromEnv used by
// the router so the CEREBRO-PATCH on cmd/server/router.go stays small.
// Returns nil (no panic) when the env var is unset — the handler maps that
// to 503 at request time. Panics only on a malformed key (operator typo);
// failing startup on that is the right behavior.
func MustNewCipherFromEnv() *Cipher {
	c, err := NewCipherFromEnv()
	if err != nil {
		panic(fmt.Errorf("credentials: %w", err))
	}
	return c
}

// NewCipherFromEnv reads MULTICA_CREDENTIALS_KEY, base64-decodes it, and
// returns a Cipher. Returns (nil, nil) when the env var is unset — callers
// must decide whether that is acceptable (dev) or fatal (prod).
func NewCipherFromEnv() (*Cipher, error) {
	raw := strings.TrimSpace(os.Getenv(KeyEnvVar))
	if raw == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		// Accept URL-safe base64 too — operators paste from various sources.
		key, err = base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("%s must be base64-encoded: %w", KeyEnvVar, err)
		}
	}
	return NewCipher(key)
}

// Encrypt seals plaintext into nonce || ciphertext-with-tag. The nonce is a
// fresh 12-byte random value per call, so two encryptions of the same input
// yield different ciphertexts (no deterministic leakage).
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, ErrCipherMissing
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("read nonce: %w", err)
	}
	sealed := c.aead.Seal(nil, nonce, plaintext, nil)
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

// Decrypt reverses Encrypt. Returns ErrInvalidCiphertext on auth failure so
// the caller can distinguish "wrong key / tampered" from infrastructure
// errors.
func (c *Cipher) Decrypt(blob []byte) ([]byte, error) {
	if c == nil || c.aead == nil {
		return nil, ErrCipherMissing
	}
	nonceSize := c.aead.NonceSize()
	if len(blob) < nonceSize+c.aead.Overhead() {
		return nil, ErrInvalidCiphertext
	}
	nonce, sealed := blob[:nonceSize], blob[nonceSize:]
	plaintext, err := c.aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, ErrInvalidCiphertext
	}
	return plaintext, nil
}

// MaskValue produces the non-sensitive preview stored in value_hint and
// returned by list/get endpoints. The plaintext never leaves the service
// layer — only this masked form does. The rule of thumb:
//
//	len <= 8  → "***"
//	len <= 16 → first 2 + *** + last 2
//	default   → first 4 + ***...*** + last 4
//
// We intentionally hide the *exact* length so a list endpoint cannot be
// used to fingerprint specific secrets.
func MaskValue(plaintext string) string {
	n := len(plaintext)
	switch {
	case n == 0:
		return ""
	case n <= 8:
		return "***"
	case n <= 16:
		return plaintext[:2] + "***" + plaintext[n-2:]
	default:
		return plaintext[:4] + "***...***" + plaintext[n-4:]
	}
}
