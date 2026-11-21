// Package secretbox provides authenticated symmetric encryption for
// secrets stored at rest — primarily Lark `app_secret` and any future
// per-tenant secret column that must not appear in plaintext in a DB
// dump (MUL-2671 §4.4).
//
// Construction: AES-256-GCM with a per-message 12-byte random nonce
// prepended to the ciphertext. GCM provides both confidentiality and
// integrity, so a tampered row decrypts to an error instead of
// silently garbled plaintext.
//
// Key: 32 bytes. Loaded from an env var as base64 (LoadKey). Rotation
// is not supported in this iteration — once we have multiple keys in
// production we add a single-byte prefix to ciphertext for key id;
// today every ciphertext is keyed by the one current master key.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// KeySize is the required master-key length in bytes (AES-256).
const KeySize = 32

// ErrInvalidKey is returned by New when the key length is not KeySize.
var ErrInvalidKey = errors.New("secretbox: key must be 32 bytes")

// ErrKeyNotSet is returned by LoadKey when the requested environment variable
// is empty or not set. Callers can use errors.Is(err, ErrKeyNotSet) to
// distinguish an intentionally unconfigured integration from an invalid key.
var ErrKeyNotSet = errors.New("secretbox: key is not set")

// ErrCiphertextTooShort is returned when the input to Open is smaller
// than the nonce + GCM tag overhead.
var ErrCiphertextTooShort = errors.New("secretbox: ciphertext too short")

// Box encrypts and decrypts byte slices using a fixed master key.
// Box instances are safe for concurrent use after construction —
// cipher.AEAD itself is goroutine-safe.
type Box struct {
	aead cipher.AEAD
}

// New constructs a Box bound to the given 32-byte master key. Callers
// should hold the returned *Box for the process lifetime; constructing
// it per request needlessly re-derives the AES round keys.
func New(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretbox: aes.NewCipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: cipher.NewGCM: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plaintext and returns nonce || ciphertext || tag. The
// nonce is randomly generated per call; callers must NOT cache or
// reuse the output as if it were deterministic (e.g. don't index a
// secret by its ciphertext — two Seal calls on the same plaintext
// produce different bytes).
func (b *Box) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secretbox: read nonce: %w", err)
	}
	// b.aead.Seal appends ciphertext+tag to its first argument; we
	// pass `nonce` so the caller receives a single contiguous slice
	// laid out as nonce||ciphertext||tag, which Open then splits.
	return b.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open reverses Seal. Returns ErrCiphertextTooShort or an
// authentication error (from GCM) if the input is malformed or
// tampered.
func (b *Box) Open(sealed []byte) ([]byte, error) {
	ns := b.aead.NonceSize()
	if len(sealed) < ns+b.aead.Overhead() {
		return nil, ErrCiphertextTooShort
	}
	nonce, ciphertext := sealed[:ns], sealed[ns:]
	return b.aead.Open(nil, nonce, ciphertext, nil)
}

// LoadKey reads a 32-byte key from the given env var. It accepts base64 (standard
// or URL-safe, padded or unpadded), 64-character hex strings, or raw 32-byte
// strings, with leading/trailing whitespace and optional wrapping quotes
// stripped. Empty env values return an error wrapping ErrKeyNotSet.
func LoadKey(envVar string) ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(envVar))
	if (strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"")) ||
		(strings.HasPrefix(raw, "'") && strings.HasSuffix(raw, "'")) {
		raw = strings.TrimSpace(raw[1 : len(raw)-1])
	}
	if raw == "" {
		return nil, fmt.Errorf("secretbox: %s is not set: %w", envVar, ErrKeyNotSet)
	}

	// 1. Base64 encodings (standard and URL-safe, padded and unpadded)
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.RawURLEncoding,
	}
	var lastLen int
	for _, enc := range encodings {
		if key, err := enc.DecodeString(raw); err == nil {
			if len(key) == KeySize {
				return key, nil
			}
			lastLen = len(key)
		}
	}

	// 2. Hex encoding (e.g. from `openssl rand -hex 32`)
	if len(raw) == KeySize*2 {
		if key, err := hex.DecodeString(raw); err == nil && len(key) == KeySize {
			return key, nil
		}
	}

	// 3. Raw 32 ASCII/UTF-8 bytes
	if len(raw) == KeySize {
		return []byte(raw), nil
	}

	if lastLen > 0 {
		return nil, fmt.Errorf("secretbox: %s decodes to %d bytes, expected %d", envVar, lastLen, KeySize)
	}
	return nil, fmt.Errorf("secretbox: %s is not a valid 32-byte key (expected base64, hex, or 32 raw bytes)", envVar)
}
