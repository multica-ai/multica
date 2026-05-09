package secrets

import (
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	plaintext := "ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	ciphertext, err := Encrypt([]byte(plaintext), key)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(ciphertext, []byte(plaintext)) {
		t.Fatalf("plaintext leaked into ciphertext: %x", ciphertext)
	}
	out, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if out != plaintext {
		t.Fatalf("round trip mismatch: got %q want %q", out, plaintext)
	}
}

func TestEncrypt_RandomNonce(t *testing.T) {
	// Two encryptions of the same plaintext + same key must produce
	// distinct ciphertexts — that's the GCM nonce randomness guarantee
	// and the difference between "secure" and "deterministically
	// catastrophic".
	key := make([]byte, 32)
	plaintext := []byte("hello")
	a, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	b, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatalf("two encryptions produced identical output — nonce reuse")
	}
}

func TestDecrypt_TamperedCiphertextFails(t *testing.T) {
	key := make([]byte, 32)
	ct, err := Encrypt([]byte("payload"), key)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	// Flip a bit in the sealed body — GCM auth tag must reject.
	ct[len(ct)-1] ^= 0x01
	if _, err := Decrypt(ct, key); err == nil {
		t.Fatalf("expected error on tampered ciphertext")
	}
}

func TestDecrypt_TooShort(t *testing.T) {
	key := make([]byte, 32)
	if _, err := Decrypt([]byte{0x01, 0x02}, key); err != ErrCiphertextTooShort {
		t.Fatalf("expected ErrCiphertextTooShort, got %v", err)
	}
}

func TestEncrypt_RejectsBadKeyLen(t *testing.T) {
	if _, err := Encrypt([]byte("hi"), make([]byte, 16)); err == nil {
		t.Fatalf("expected error for 16-byte key")
	}
}

func TestLoadKey_ReadsHexEnv(t *testing.T) {
	ResetForTest()
	defer ResetForTest()
	want := bytes.Repeat([]byte{0xab}, 32)
	t.Setenv(EncryptionKeyEnv, hex.EncodeToString(want))
	got, err := LoadKey()
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("LoadKey mismatch")
	}
}

func TestLoadKey_RejectsBadHex(t *testing.T) {
	ResetForTest()
	defer ResetForTest()
	t.Setenv(EncryptionKeyEnv, "not-hex")
	if _, err := LoadKey(); err == nil {
		t.Fatalf("expected error from invalid hex")
	}
}

func TestLoadKey_RejectsWrongLen(t *testing.T) {
	ResetForTest()
	defer ResetForTest()
	t.Setenv(EncryptionKeyEnv, hex.EncodeToString([]byte{0x01, 0x02}))
	if _, err := LoadKey(); err == nil {
		t.Fatalf("expected error from short key")
	}
}

func TestLoadKey_DerivesDevKeyWhenMissing(t *testing.T) {
	ResetForTest()
	defer ResetForTest()
	os.Unsetenv(EncryptionKeyEnv)
	t.Setenv("JWT_SECRET", "test-jwt-secret")
	key, err := LoadKey()
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("dev key wrong length: %d", len(key))
	}
}

func TestGenerateRandomURLSafe(t *testing.T) {
	a, err := GenerateRandomURLSafe()
	if err != nil {
		t.Fatalf("a: %v", err)
	}
	b, err := GenerateRandomURLSafe()
	if err != nil {
		t.Fatalf("b: %v", err)
	}
	if a == b {
		t.Fatalf("two random secrets matched — not random")
	}
	if len(a) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(a))
	}
	if strings.ToLower(a) != a {
		t.Fatalf("expected lowercase hex, got %q", a)
	}
	if _, err := hex.DecodeString(a); err != nil {
		t.Fatalf("not hex: %v", err)
	}
}

func TestEncryptString_DecryptString(t *testing.T) {
	ResetForTest()
	defer ResetForTest()
	t.Setenv(EncryptionKeyEnv, hex.EncodeToString(bytes.Repeat([]byte{0x42}, 32)))
	ct, err := EncryptString("ghp_secret")
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}
	out, err := DecryptString(ct)
	if err != nil {
		t.Fatalf("DecryptString: %v", err)
	}
	if out != "ghp_secret" {
		t.Fatalf("round-trip mismatch")
	}
}
