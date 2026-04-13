package crypto

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func generateTestKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestParseHexKey(t *testing.T) {
	t.Run("valid 32-byte hex", func(t *testing.T) {
		key := make([]byte, 32)
		rand.Read(key)
		hexStr := hex.EncodeToString(key)

		parsed, err := ParseHexKey(hexStr)
		if err != nil {
			t.Fatal(err)
		}
		if len(parsed) != 32 {
			t.Fatalf("expected 32 bytes, got %d", len(parsed))
		}
	})

	t.Run("empty string", func(t *testing.T) {
		_, err := ParseHexKey("")
		if err == nil {
			t.Fatal("expected error for empty key")
		}
	})

	t.Run("wrong length", func(t *testing.T) {
		_, err := ParseHexKey("abcdef")
		if err == nil {
			t.Fatal("expected error for short key")
		}
	})

	t.Run("invalid hex chars", func(t *testing.T) {
		_, err := ParseHexKey(strings.Repeat("zz", 32))
		if err == nil {
			t.Fatal("expected error for invalid hex")
		}
	})

	t.Run("uppercase hex", func(t *testing.T) {
		hexStr := strings.Repeat("AB", 32)
		parsed, err := ParseHexKey(hexStr)
		if err != nil {
			t.Fatal(err)
		}
		if len(parsed) != 32 {
			t.Fatalf("expected 32 bytes, got %d", len(parsed))
		}
	})
}

func TestDeriveKey(t *testing.T) {
	masterKey := generateTestKey(t)

	t.Run("different purposes produce different keys", func(t *testing.T) {
		k1, err := DeriveKey(masterKey, "provider-api-key")
		if err != nil {
			t.Fatal(err)
		}
		k2, err := DeriveKey(masterKey, "git-pat")
		if err != nil {
			t.Fatal(err)
		}
		if string(k1) == string(k2) {
			t.Fatal("expected different derived keys for different purposes")
		}
	})

	t.Run("same purpose is deterministic", func(t *testing.T) {
		k1, _ := DeriveKey(masterKey, "test")
		k2, _ := DeriveKey(masterKey, "test")
		if string(k1) != string(k2) {
			t.Fatal("expected same derived key for same inputs")
		}
	})

	t.Run("wrong master key length", func(t *testing.T) {
		_, err := DeriveKey([]byte("short"), "test")
		if err == nil {
			t.Fatal("expected error for short master key")
		}
	})
}

func TestEncryptDecrypt(t *testing.T) {
	masterKey := generateTestKey(t)
	derivedKey, _ := DeriveKey(masterKey, "test")

	t.Run("roundtrip", func(t *testing.T) {
		plaintext := "sk-e2b-abc123def456"
		encrypted, err := Encrypt(plaintext, derivedKey)
		if err != nil {
			t.Fatal(err)
		}
		decrypted, err := Decrypt(encrypted, derivedKey)
		if err != nil {
			t.Fatal(err)
		}
		if decrypted != plaintext {
			t.Fatalf("expected %q, got %q", plaintext, decrypted)
		}
	})

	t.Run("different ciphertexts for same plaintext", func(t *testing.T) {
		plaintext := "same-secret"
		c1, _ := Encrypt(plaintext, derivedKey)
		c2, _ := Encrypt(plaintext, derivedKey)
		if c1 == c2 {
			t.Fatal("expected different ciphertexts due to random nonce")
		}
		// Both should still decrypt correctly.
		d1, _ := Decrypt(c1, derivedKey)
		d2, _ := Decrypt(c2, derivedKey)
		if d1 != plaintext || d2 != plaintext {
			t.Fatal("both ciphertexts should decrypt to the same plaintext")
		}
	})

	t.Run("empty plaintext", func(t *testing.T) {
		encrypted, err := Encrypt("", derivedKey)
		if err != nil {
			t.Fatal(err)
		}
		decrypted, err := Decrypt(encrypted, derivedKey)
		if err != nil {
			t.Fatal(err)
		}
		if decrypted != "" {
			t.Fatalf("expected empty string, got %q", decrypted)
		}
	})

	t.Run("wrong key fails decryption", func(t *testing.T) {
		encrypted, _ := Encrypt("secret", derivedKey)
		wrongKey, _ := DeriveKey(masterKey, "wrong-purpose")
		_, err := Decrypt(encrypted, wrongKey)
		if err == nil {
			t.Fatal("expected decryption to fail with wrong key")
		}
	})

	t.Run("tampered ciphertext fails", func(t *testing.T) {
		encrypted, _ := Encrypt("secret", derivedKey)
		// Flip a character in the middle of the base64 string
		runes := []rune(encrypted)
		mid := len(runes) / 2
		if runes[mid] == 'A' {
			runes[mid] = 'B'
		} else {
			runes[mid] = 'A'
		}
		_, err := Decrypt(string(runes), derivedKey)
		if err == nil {
			t.Fatal("expected decryption to fail with tampered ciphertext")
		}
	})

	t.Run("invalid base64 fails", func(t *testing.T) {
		_, err := Decrypt("not-valid-base64!!!", derivedKey)
		if err == nil {
			t.Fatal("expected error for invalid base64")
		}
	})

	t.Run("ciphertext too short fails", func(t *testing.T) {
		_, err := Decrypt("AQID", derivedKey) // just 3 bytes base64
		if err == nil {
			t.Fatal("expected error for short ciphertext")
		}
	})
}
