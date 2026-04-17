// Package secrets provides AES-256-GCM encryption for small values (PATs, OAuth tokens).
// Ciphertext format: [12-byte nonce][ciphertext][16-byte GCM tag] (the Seal output already
// appends the tag, so the on-disk format is simply [nonce][Seal(output)]).
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
)

const keySize = 32 // AES-256

// Cipher encrypts and decrypts small byte slices with AES-GCM.
// Construct via NewCipher (direct key) or Load (from MULTICA_SECRETS_KEY env var).
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher constructs a Cipher from a 32-byte key. Returns an error for any other size.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("secrets: key must be %d bytes, got %d", keySize, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: aes.NewCipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: cipher.NewGCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt returns [nonce || ciphertext || tag].
func (c *Cipher) Encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("secrets: read nonce: %w", err)
	}
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt expects the output format from Encrypt.
func (c *Cipher) Decrypt(ciphertext []byte) ([]byte, error) {
	ns := c.aead.NonceSize()
	if len(ciphertext) < ns+c.aead.Overhead() {
		return nil, errors.New("secrets: ciphertext too short")
	}
	nonce, body := ciphertext[:ns], ciphertext[ns:]
	return c.aead.Open(nil, nonce, body, nil)
}

// Load reads MULTICA_SECRETS_KEY (base64-encoded 32 bytes) and returns a Cipher.
func Load() (*Cipher, error) {
	raw := os.Getenv("MULTICA_SECRETS_KEY")
	if raw == "" {
		return nil, errors.New("secrets: MULTICA_SECRETS_KEY is not set")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("secrets: MULTICA_SECRETS_KEY must be base64: %w", err)
	}
	return NewCipher(key)
}
