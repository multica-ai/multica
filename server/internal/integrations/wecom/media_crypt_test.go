package wecom

// media_crypt_test.go — the decryptor is checked against a from-scratch
// encryptor written here, not against itself. Every case below encrypts a
// known plaintext with the algorithm WeCom documents (AES-256-CBC, IV = the
// first 16 bytes of the decoded aeskey, PKCS#7 padded to a multiple of 32)
// and asserts the bytes come back byte-identical.
//
// The 32-byte padding block is the whole reason this file exists. AES blocks
// are 16 bytes, so every unpadder that assumes the pad length can be at most
// the block size rejects a WeCom payload whose plaintext happens to land on a
// 32-byte boundary — the pad is then a full 32 bytes and the "impossible"
// branch fires on a perfectly ordinary file.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"strings"
	"testing"
)

// encryptLikeWecom is an independent implementation of the sender side. It
// deliberately does not call anything in media_crypt.go.
func encryptLikeWecom(t *testing.T, aesKeyB64 string, plaintext []byte) []byte {
	t.Helper()
	key, err := base64.StdEncoding.DecodeString(aesKeyB64)
	if err != nil {
		t.Fatalf("test key is not base64: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("test key is %d bytes, want 32", len(key))
	}
	// PKCS#7 to a multiple of 32. A plaintext already on the boundary gets a
	// whole extra 32-byte block, which is the case that breaks naive code.
	pad := 32 - (len(plaintext) % 32)
	padded := append(append([]byte{}, plaintext...), bytes.Repeat([]byte{byte(pad)}, pad)...)

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(out, padded)
	return out
}

// testAESKey is 32 bytes of key material, base64'd the way a callback carries
// it. Fixed rather than random so a failure is reproducible.
const testAESKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="

func TestDecryptMediaRoundTripsRealCiphertext(t *testing.T) {
	cases := []struct {
		name  string
		plain []byte
	}{
		{"one byte", []byte("x")},
		{"exactly 15 bytes", bytes.Repeat([]byte("a"), 15)},
		{"exactly 16 bytes — one AES block", bytes.Repeat([]byte("b"), 16)},
		{"17 bytes", bytes.Repeat([]byte("c"), 17)},
		{"31 bytes", bytes.Repeat([]byte("d"), 31)},
		{"exactly 32 bytes — a full 32-byte pad block follows", bytes.Repeat([]byte("e"), 32)},
		{"33 bytes", bytes.Repeat([]byte("f"), 33)},
		{"64 bytes", bytes.Repeat([]byte("g"), 64)},
		{"a PNG header", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")},
		{"utf-8 text", []byte("这是一份季度报告，请查收。")},
		{"a megabyte", bytes.Repeat([]byte("multica"), 150000)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext := encryptLikeWecom(t, testAESKey, tc.plain)
			if len(ciphertext)%32 != 0 {
				t.Fatalf("the encryptor produced %d bytes, not a multiple of 32", len(ciphertext))
			}
			got, err := decryptMedia(testAESKey, ciphertext)
			if err != nil {
				t.Fatalf("decryptMedia: %v", err)
			}
			if !bytes.Equal(got, tc.plain) {
				t.Fatalf("round trip changed the bytes: got %d bytes, want %d", len(got), len(tc.plain))
			}
		})
	}
}

// TestDecryptMediaAcceptsAnUnpaddedBase64Key: WeCom's own console prints the
// 43-character form of a 32-byte key (no "=" tail). Refusing it would fail on
// a key that is perfectly valid.
func TestDecryptMediaAcceptsAnUnpaddedBase64Key(t *testing.T) {
	unpadded := strings.TrimRight(testAESKey, "=")
	if unpadded == testAESKey {
		t.Fatal("the fixture key has no padding to trim; pick another")
	}
	ciphertext := encryptLikeWecom(t, testAESKey, []byte("hello"))
	got, err := decryptMedia(unpadded, ciphertext)
	if err != nil {
		t.Fatalf("decryptMedia with a 43-char key: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestDecryptMediaRejectsWhatItCannotTrust(t *testing.T) {
	good := encryptLikeWecom(t, testAESKey, []byte("payload"))

	cases := []struct {
		name       string
		key        string
		ciphertext []byte
	}{
		{"no key at all", "", good},
		{"key that is not base64", "not base64 !!!", good},
		{"key that decodes to 16 bytes", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("k"), 16)), good},
		{"empty ciphertext", testAESKey, nil},
		{"ciphertext off the AES block boundary", testAESKey, good[:len(good)-1]},
		{"ciphertext encrypted under a different key", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("z"), 32)), good},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decryptMedia(tc.key, tc.ciphertext); err == nil {
				t.Fatal("want an error, got none — a silent wrong answer here becomes a corrupt attachment")
			}
		})
	}
}

// TestDecryptMediaRejectsATamperedPadTail: the last block decides how many
// bytes come off the end. A pad that does not check out must be an error, not
// a truncated file.
func TestDecryptMediaRejectsATamperedPadTail(t *testing.T) {
	// Build the padded plaintext by hand with a pad length outside 1..32 and
	// encrypt it, so the ciphertext is well-formed but the tail is not.
	key, _ := base64.StdEncoding.DecodeString(testAESKey)
	block, _ := aes.NewCipher(key)
	for _, tc := range []struct {
		name   string
		padded []byte
	}{
		{"pad byte 0", append(bytes.Repeat([]byte("p"), 31), 0)},
		{"pad byte 33 — past the 32-byte block", append(bytes.Repeat([]byte("p"), 31), 33)},
		{"pad bytes disagree", append(append(bytes.Repeat([]byte("p"), 28), 4, 4, 3), 4)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext := make([]byte, len(tc.padded))
			cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(ciphertext, tc.padded)
			if _, err := decryptMedia(testAESKey, ciphertext); err == nil {
				t.Fatal("want an error for a pad tail that does not check out")
			}
		})
	}
}
