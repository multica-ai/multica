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
// keeps the replaced key as a previous key (LoadPreviousKey): Seal uses
// only the current key, Open tries the current key first and then each
// previous key. Ciphertext carries no key id; GCM authentication makes a
// wrong key fail to open instead of yielding garbage, so trial decryption
// identifies the key.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
)

// KeySize is the required master-key length in bytes (AES-256).
const KeySize = 32

// ErrInvalidKey is returned by New when the key length is not KeySize.
var ErrInvalidKey = errors.New("secretbox: key must be 32 bytes")

// ErrCiphertextTooShort is returned when the input to Open is smaller
// than the nonce + GCM tag overhead.
var ErrCiphertextTooShort = errors.New("secretbox: ciphertext too short")

// Box encrypts with a current master key and decrypts with the current
// key or any previous key. Box instances are safe for concurrent use
// after construction — cipher.AEAD itself is goroutine-safe.
type Box struct {
	aead     cipher.AEAD
	previous []cipher.AEAD
}

// New constructs a Box that seals with key and also opens ciphertext
// sealed under any of previous. Callers should hold the returned *Box
// for the process lifetime; constructing it per request needlessly
// re-derives the AES round keys.
func New(key []byte, previous ...[]byte) (*Box, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	b := &Box{aead: aead}
	for _, k := range previous {
		prev, err := newAEAD(k)
		if err != nil {
			return nil, err
		}
		b.previous = append(b.previous, prev)
	}
	return b, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
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
	return aead, nil
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

// Open reverses Seal, trying the current key and then each previous
// key. Returns ErrCiphertextTooShort, or the current key's
// authentication error (from GCM) if no key opens the input.
func (b *Box) Open(sealed []byte) ([]byte, error) {
	plaintext, _, err := b.open(sealed)
	return plaintext, err
}

// SealedWithCurrent reports whether sealed opens under the current key.
// A re-encryption pass uses it to find rows still sealed under a
// previous key.
func (b *Box) SealedWithCurrent(sealed []byte) bool {
	_, current, err := b.open(sealed)
	return err == nil && current
}

func (b *Box) open(sealed []byte) (plaintext []byte, current bool, err error) {
	ns := b.aead.NonceSize()
	if len(sealed) < ns+b.aead.Overhead() {
		return nil, false, ErrCiphertextTooShort
	}
	nonce, ciphertext := sealed[:ns], sealed[ns:]
	plaintext, err = b.aead.Open(nil, nonce, ciphertext, nil)
	if err == nil {
		return plaintext, true, nil
	}
	for _, prev := range b.previous {
		if p, perr := prev.Open(nil, nonce, ciphertext, nil); perr == nil {
			return p, false, nil
		}
	}
	return nil, false, err
}

// LoadKey reads a base64-encoded 32-byte key from the given env var.
// Returns ErrInvalidKey if the decoded length is not KeySize. Empty
// env values are treated as "not configured" and surface as a clear
// error rather than silently using a zero key.
func LoadKey(envVar string) ([]byte, error) {
	raw := os.Getenv(envVar)
	if raw == "" {
		return nil, fmt.Errorf("secretbox: %s is not set", envVar)
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("secretbox: %s is not valid base64: %w", envVar, err)
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("secretbox: %s decodes to %d bytes, expected %d", envVar, len(key), KeySize)
	}
	return key, nil
}

// PreviousKeySuffix names the env var holding the key replaced by the
// current one, e.g. MULTICA_LARK_SECRET_KEY_PREVIOUS.
const PreviousKeySuffix = "_PREVIOUS"

// LoadPreviousKey reads envVar+PreviousKeySuffix. Unset returns nil with
// no error; a set but invalid value is an error, so a typo cannot leave
// old ciphertext silently unreadable.
func LoadPreviousKey(envVar string) ([]byte, error) {
	name := envVar + PreviousKeySuffix
	if os.Getenv(name) == "" {
		return nil, nil
	}
	return LoadKey(name)
}
