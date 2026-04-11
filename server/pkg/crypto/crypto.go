// Package crypto provides application-layer encryption for sensitive
// configuration values (API keys, tokens) stored at rest in the database.
//
// Ciphertext format: version(1) || nonce(12) || ciphertext || tag(16)
// Key derivation: HKDF-SHA256 from master key with per-purpose info strings.
package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const (
	// KeyVersion is the current encryption key version.
	// Increment when rotating keys; old versions can still decrypt.
	KeyVersion byte = 1

	// masterKeyLen is the expected length of the hex-decoded master key (32 bytes = AES-256).
	masterKeyLen = 32

	// nonceLen is the AES-GCM nonce size.
	nonceLen = 12
)

// ErrInvalidKey is returned when the encryption key is missing, malformed, or wrong.
var ErrInvalidKey = errors.New("crypto: invalid encryption key")

// ErrDecrypt is returned when decryption fails (wrong key, tampered data, etc.).
var ErrDecrypt = errors.New("crypto: decryption failed")

// DeriveKey produces a 32-byte AES-256 key from a master secret using HKDF-SHA256.
// The purpose string isolates keys so that (e.g.) the provider-key derivation
// cannot decrypt a git-pat ciphertext even if the master secret is the same.
//
// Implements RFC 5869 HKDF (Extract + Expand) using only the standard library.
func DeriveKey(masterKey []byte, purpose string) ([]byte, error) {
	if len(masterKey) != masterKeyLen {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidKey, masterKeyLen, len(masterKey))
	}

	// HKDF-Extract: PRK = HMAC-SHA256(salt="", IKM=masterKey)
	extractor := hmac.New(sha256.New, make([]byte, sha256.Size)) // empty salt
	extractor.Write(masterKey)
	prk := extractor.Sum(nil)

	// HKDF-Expand: OKM = HMAC-SHA256(PRK, info || 0x01)
	// We only need 32 bytes (one block of SHA-256), so a single iteration suffices.
	expander := hmac.New(sha256.New, prk)
	expander.Write([]byte(purpose))
	expander.Write([]byte{0x01})
	derived := expander.Sum(nil)

	return derived[:masterKeyLen], nil
}

// Encrypt encrypts plaintext with AES-256-GCM using the provided derived key.
// Returns base64-encoded ciphertext with a version prefix for future key rotation.
func Encrypt(plaintext string, derivedKey []byte) (string, error) {
	if len(derivedKey) != masterKeyLen {
		return "", fmt.Errorf("%w: derived key must be %d bytes", ErrInvalidKey, masterKeyLen)
	}

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return "", fmt.Errorf("crypto: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: new gcm: %w", err)
	}

	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: random nonce: %w", err)
	}

	sealed := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	// Format: version(1) || nonce(12) || ciphertext+tag
	out := make([]byte, 0, 1+nonceLen+len(sealed))
	out = append(out, KeyVersion)
	out = append(out, nonce...)
	out = append(out, sealed...)

	return base64.StdEncoding.EncodeToString(out), nil
}

// Decrypt decrypts a base64-encoded ciphertext produced by Encrypt.
func Decrypt(encoded string, derivedKey []byte) (string, error) {
	if len(derivedKey) != masterKeyLen {
		return "", fmt.Errorf("%w: derived key must be %d bytes", ErrInvalidKey, masterKeyLen)
	}

	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("%w: invalid base64: %w", ErrDecrypt, err)
	}

	// Minimum: version(1) + nonce(12) + tag(16) = 29 bytes
	if len(raw) < 1+nonceLen+16 {
		return "", fmt.Errorf("%w: ciphertext too short", ErrDecrypt)
	}

	version := raw[0]
	if version != KeyVersion {
		return "", fmt.Errorf("%w: unsupported key version %d", ErrDecrypt, version)
	}

	nonce := raw[1 : 1+nonceLen]
	ciphertext := raw[1+nonceLen:]

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return "", fmt.Errorf("%w: new cipher: %w", ErrDecrypt, err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("%w: new gcm: %w", ErrDecrypt, err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrDecrypt, err)
	}

	return string(plaintext), nil
}

// ParseHexKey decodes a hex-encoded master key and validates its length.
// Use this at server startup to load ENCRYPTION_KEY from the environment.
func ParseHexKey(hexKey string) ([]byte, error) {
	if hexKey == "" {
		return nil, fmt.Errorf("%w: empty", ErrInvalidKey)
	}
	// hex package is in the standard library; avoid importing encoding/hex
	// just reuse what's already imported above... actually we do need it.
	b := make([]byte, len(hexKey)/2)
	n := 0
	for i := 0; i < len(hexKey); i += 2 {
		if i+1 >= len(hexKey) {
			return nil, fmt.Errorf("%w: odd-length hex string", ErrInvalidKey)
		}
		hi := unhex(hexKey[i])
		lo := unhex(hexKey[i+1])
		if hi == 0xFF || lo == 0xFF {
			return nil, fmt.Errorf("%w: invalid hex character", ErrInvalidKey)
		}
		b[n] = hi<<4 | lo
		n++
	}
	b = b[:n]
	if len(b) != masterKeyLen {
		return nil, fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidKey, masterKeyLen, len(b))
	}
	return b, nil
}

type contextKey struct{}

// WithEncryptionKey returns a new context carrying the master encryption key.
func WithEncryptionKey(ctx context.Context, key []byte) context.Context {
	return context.WithValue(ctx, contextKey{}, key)
}

// EncryptionKeyFromContext extracts the master encryption key from the context.
// Returns nil if not set.
func EncryptionKeyFromContext(ctx context.Context) []byte {
	if v, ok := ctx.Value(contextKey{}).([]byte); ok {
		return v
	}
	return nil
}

func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10
	default:
		return 0xFF
	}
}
