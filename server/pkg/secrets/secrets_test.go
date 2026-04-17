package secrets

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func TestCipher_EncryptDecryptRoundTrip(t *testing.T) {
	c, err := NewCipher(testKey(t))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	plaintext := []byte("glpat-xxxxxxxxxxxxxxxxxxxx")
	ciphertext, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext equals plaintext")
	}
	got, err := c.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("round-trip mismatch: got %q want %q", got, plaintext)
	}
}

func TestCipher_EncryptProducesFreshNonce(t *testing.T) {
	c, err := NewCipher(testKey(t))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	plaintext := []byte("same plaintext")
	a, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	b, err := c.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("expected different ciphertexts due to fresh nonce")
	}
}

func TestCipher_DecryptTamperedFails(t *testing.T) {
	c, err := NewCipher(testKey(t))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	ciphertext, err := c.Encrypt([]byte("plaintext"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	// Flip one byte in the tag region.
	ciphertext[len(ciphertext)-1] ^= 0xff
	if _, err := c.Decrypt(ciphertext); err == nil {
		t.Fatal("expected decrypt to fail on tampered ciphertext")
	}
}

func TestCipher_RejectsWrongKeySize(t *testing.T) {
	if _, err := NewCipher(make([]byte, 16)); err == nil {
		t.Fatal("expected NewCipher to reject 16-byte key")
	}
	if _, err := NewCipher(make([]byte, 64)); err == nil {
		t.Fatal("expected NewCipher to reject 64-byte key")
	}
}

func TestLoadFromEnv_Success(t *testing.T) {
	key := testKey(t)
	t.Setenv("MULTICA_SECRETS_KEY", base64.StdEncoding.EncodeToString(key))
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	ciphertext, err := c.Encrypt([]byte("x"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := c.Decrypt(ciphertext); err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
}

func TestLoadFromEnv_Missing(t *testing.T) {
	t.Setenv("MULTICA_SECRETS_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected Load to fail when env var missing")
	}
}

func TestLoadFromEnv_WrongSize(t *testing.T) {
	t.Setenv("MULTICA_SECRETS_KEY", base64.StdEncoding.EncodeToString([]byte("too-short")))
	if _, err := Load(); err == nil {
		t.Fatal("expected Load to fail on wrong-size key")
	}
}
