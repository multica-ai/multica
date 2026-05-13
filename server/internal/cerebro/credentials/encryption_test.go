package credentials

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func newTestCipher(t *testing.T) *Cipher {
	t.Helper()
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatalf("read random key: %v", err)
	}
	c, err := NewCipher(key)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

// Round-trips the plaintext through Encrypt/Decrypt. The blob shape — nonce
// || ciphertext-with-tag — is internal, but the property the rest of the
// service relies on is "what you encrypt is exactly what you decrypt", so
// that's what we test.
func TestCipher_EncryptDecryptRoundTrip(t *testing.T) {
	c := newTestCipher(t)
	plaintexts := []string{
		"sk-test-1234567890abcdef",
		"",
		"a",
		strings.Repeat("x", 4096),
		"with unicode æøå and 🔑",
	}
	for _, pt := range plaintexts {
		blob, err := c.Encrypt([]byte(pt))
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", pt, err)
		}
		// Critical: the ciphertext must not contain the plaintext.
		if pt != "" && bytes.Contains(blob, []byte(pt)) {
			t.Fatalf("Encrypt(%q) leaked plaintext into ciphertext", pt)
		}
		got, err := c.Decrypt(blob)
		if err != nil {
			t.Fatalf("Decrypt: %v", err)
		}
		if string(got) != pt {
			t.Fatalf("round-trip mismatch: got %q want %q", got, pt)
		}
	}
}

// Two encryptions of the same input must produce different ciphertexts —
// otherwise an attacker with read access to the DB could correlate
// duplicate credentials. AES-256-GCM guarantees this when the nonce is
// random; we still pin it as a test so a future refactor cannot regress.
func TestCipher_NoncesAreUnique(t *testing.T) {
	c := newTestCipher(t)
	pt := []byte("identical-plaintext")
	first, err := c.Encrypt(pt)
	if err != nil {
		t.Fatalf("Encrypt #1: %v", err)
	}
	second, err := c.Encrypt(pt)
	if err != nil {
		t.Fatalf("Encrypt #2: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("expected distinct ciphertexts for identical plaintexts; nonces are not unique")
	}
}

// A flipped bit anywhere in the blob must fail authentication. This is
// what makes "encrypted at rest" actually safe — a tampered row decrypts
// to garbage in CBC modes, but GCM detects it.
func TestCipher_TamperingFails(t *testing.T) {
	c := newTestCipher(t)
	blob, err := c.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	for i := range blob {
		// Flip one bit at position i.
		corrupted := append([]byte{}, blob...)
		corrupted[i] ^= 0x01
		if _, err := c.Decrypt(corrupted); !errors.Is(err, ErrInvalidCiphertext) {
			t.Fatalf("Decrypt(tampered@%d): expected ErrInvalidCiphertext, got %v", i, err)
		}
	}
}

// A ciphertext produced under key K1 must not decrypt under key K2 — key
// rotation must invalidate old blobs cleanly.
func TestCipher_WrongKeyFails(t *testing.T) {
	c1 := newTestCipher(t)
	c2 := newTestCipher(t)
	blob, err := c1.Encrypt([]byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := c2.Decrypt(blob); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("Decrypt with wrong key: expected ErrInvalidCiphertext, got %v", err)
	}
}

// A too-short blob must not panic — it must surface ErrInvalidCiphertext.
// Some callers pass DB rows that may be NULL or zero-length during a
// failed migration; we should fail gracefully.
func TestCipher_TooShortFails(t *testing.T) {
	c := newTestCipher(t)
	for _, blob := range [][]byte{nil, {}, {0x00}, bytes.Repeat([]byte{0x00}, 12)} {
		if _, err := c.Decrypt(blob); !errors.Is(err, ErrInvalidCiphertext) {
			t.Fatalf("Decrypt(short): expected ErrInvalidCiphertext, got %v", err)
		}
	}
}

func TestCipher_NilCipher(t *testing.T) {
	var c *Cipher
	if _, err := c.Encrypt([]byte("x")); !errors.Is(err, ErrCipherMissing) {
		t.Fatalf("nil cipher Encrypt: expected ErrCipherMissing, got %v", err)
	}
	if _, err := c.Decrypt([]byte("x")); !errors.Is(err, ErrCipherMissing) {
		t.Fatalf("nil cipher Decrypt: expected ErrCipherMissing, got %v", err)
	}
}

func TestNewCipher_RejectsWrongKeyLength(t *testing.T) {
	for _, n := range []int{0, 1, 15, 16, 24, 31, 33, 64} {
		key := bytes.Repeat([]byte{0xab}, n)
		if _, err := NewCipher(key); err == nil {
			t.Fatalf("NewCipher(len=%d): expected error, got nil", n)
		}
	}
}

func TestNewCipherFromEnv_UnsetReturnsNil(t *testing.T) {
	t.Setenv(KeyEnvVar, "")
	c, err := NewCipherFromEnv()
	if err != nil {
		t.Fatalf("unset key: unexpected error %v", err)
	}
	if c != nil {
		t.Fatal("unset key: expected nil cipher")
	}
}

func TestNewCipherFromEnv_ParsesBase64(t *testing.T) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		t.Fatalf("read random key: %v", err)
	}
	t.Setenv(KeyEnvVar, base64.StdEncoding.EncodeToString(key))
	c, err := NewCipherFromEnv()
	if err != nil {
		t.Fatalf("NewCipherFromEnv: %v", err)
	}
	if c == nil {
		t.Fatal("NewCipherFromEnv: expected cipher, got nil")
	}
	// Round-trip via a plaintext to confirm the key was actually used.
	blob, err := c.Encrypt([]byte("hello"))
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	got, err := c.Decrypt(blob)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("Decrypt: got %q want %q", got, "hello")
	}
}

func TestNewCipherFromEnv_RejectsBadBase64(t *testing.T) {
	t.Setenv(KeyEnvVar, "not base64 !!!")
	if _, err := NewCipherFromEnv(); err == nil {
		t.Fatal("expected error for malformed base64 key")
	}
}

func TestMustNewCipherFromEnv_PanicsOnError(t *testing.T) {
	t.Setenv(KeyEnvVar, "not base64 !!!")
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on malformed key")
		}
	}()
	_ = MustNewCipherFromEnv()
}

func TestMaskValue(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"abc", "***"},
		{"abcdefgh", "***"},               // exactly 8 → fully masked
		{"abcdefghi", "ab***hi"},          // 9 → first/last 2
		{"abcdefghijklmnop", "ab***op"},   // 16 → first/last 2
		{"abcdefghijklmnopq", "abcd***...***nopq"},
	}
	for _, tc := range cases {
		got := MaskValue(tc.in)
		if got != tc.want {
			t.Errorf("MaskValue(%q) = %q, want %q", tc.in, got, tc.want)
		}
		// Critical invariant for any non-trivial value: the middle of the
		// secret must not appear in the masked output.
		if len(tc.in) > 8 {
			mid := tc.in[len(tc.in)/2-1 : len(tc.in)/2+1]
			if strings.Contains(got, mid) {
				t.Errorf("MaskValue(%q) leaked middle %q in %q", tc.in, mid, got)
			}
		}
	}
}

// Os.Setenv check — purely defensive: the test runner controls env, so
// this is a tripwire if t.Setenv ever stops working.
func TestEnvTripwire(t *testing.T) {
	t.Setenv(KeyEnvVar, "tripwire")
	if os.Getenv(KeyEnvVar) != "tripwire" {
		t.Fatal("t.Setenv is not propagating")
	}
}
