package wecom

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
)

// Fixed AES-256-CBC + PKCS#7 vectors (spec §5.6, §9 crypto_test). key is a
// 32-byte key with distinct byte values; iv is key[:16] per the WeCom media
// scheme. Ciphertext was produced once with crypto/cipher's own
// CBCEncrypter against the fixed key/plaintext below and is embedded here as
// a constant so this test exercises decryptAESCBCPKCS7's CBC decrypt +
// PKCS#7 unpad logic against an independently-fixed input, not a value
// computed by the function under test.
const (
	fixedKeyHex    = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	fixedKeyBase64 = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA="

	fixedPlaintext     = "wecom media crypto fixed vector for task 12 testing 1234567890"
	fixedCiphertextHex = "aa960e9ae5166625550c007fbf15c7a5b3a35f1ac57efad420f990e3457b0b8" +
		"ce91165a60496b9bb402f98aec8dccc4716c00023ad228ee8386361a4709a4d4a"

	shortPlaintext     = "hi"
	shortCiphertextHex = "48979184ad490adeea761e2342f60ddb"
)

func mustHexDecode(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex decode %q: %v", s, err)
	}
	return b
}

func fixedKeyBytes(t *testing.T) []byte {
	return mustHexDecode(t, fixedKeyHex)
}

// TestDecodeAESKey_Base64AndHex confirms both wire encodings decode to the
// identical 32-byte key (spec §10 item 1: encoding is unconfirmed, so
// crypto.go and its tests must handle either). base64 is tried first
// (documented preferred order in decodeAESKey).
func TestDecodeAESKey_Base64AndHex(t *testing.T) {
	want := fixedKeyBytes(t)

	base64Decoded, err := decodeAESKey(fixedKeyBase64)
	if err != nil {
		t.Fatalf("decodeAESKey(base64): %v", err)
	}
	if !bytes.Equal(base64Decoded, want) {
		t.Fatalf("decodeAESKey(base64) = %x, want %x", base64Decoded, want)
	}

	hexDecoded, err := decodeAESKey(fixedKeyHex)
	if err != nil {
		t.Fatalf("decodeAESKey(hex): %v", err)
	}
	if !bytes.Equal(hexDecoded, want) {
		t.Fatalf("decodeAESKey(hex) = %x, want %x", hexDecoded, want)
	}
}

func TestDecodeAESKey_PrefersBase64WhenBothWouldParse(t *testing.T) {
	// fixedKeyHex is 64 ASCII chars; standard base64 can decode 64 chars of
	// its alphabet too, but not to exactly 32 bytes here, so the hex
	// fallback must still fire. This asserts decodeAESKey never returns a
	// wrong-length key from the base64 attempt instead of falling through.
	got, err := decodeAESKey(fixedKeyHex)
	if err != nil {
		t.Fatalf("decodeAESKey: %v", err)
	}
	if len(got) != aesKeySize {
		t.Fatalf("decoded key length = %d, want %d", len(got), aesKeySize)
	}
}

func TestDecodeAESKey_InvalidInput(t *testing.T) {
	for _, in := range []string{"", "not-base64-or-hex!!", "0102", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := decodeAESKey(in); !errors.Is(err, ErrInvalidAESKey) {
			t.Fatalf("decodeAESKey(%q) err = %v, want ErrInvalidAESKey", in, err)
		}
	}
}

func TestDecryptAESCBCPKCS7_FixedVectors(t *testing.T) {
	key := fixedKeyBytes(t)

	cases := []struct {
		name       string
		ciphertext string
		want       string
	}{
		{"multi-block", fixedCiphertextHex, fixedPlaintext},
		{"single-block-short", shortCiphertextHex, shortPlaintext},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ciphertext := mustHexDecode(t, tc.ciphertext)
			got, err := decryptAESCBCPKCS7(key, ciphertext)
			if err != nil {
				t.Fatalf("decryptAESCBCPKCS7: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("plaintext = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecryptAESCBCPKCS7_WrongKeySize(t *testing.T) {
	if _, err := decryptAESCBCPKCS7(make([]byte, 16), mustHexDecode(t, shortCiphertextHex)); !errors.Is(err, ErrInvalidAESKey) {
		t.Fatalf("err = %v, want ErrInvalidAESKey", err)
	}
}

func TestDecryptAESCBCPKCS7_NotBlockAligned(t *testing.T) {
	key := fixedKeyBytes(t)
	ciphertext := mustHexDecode(t, shortCiphertextHex)
	if _, err := decryptAESCBCPKCS7(key, ciphertext[:len(ciphertext)-1]); !errors.Is(err, ErrCiphertextNotBlockAligned) {
		t.Fatalf("err = %v, want ErrCiphertextNotBlockAligned", err)
	}
	if _, err := decryptAESCBCPKCS7(key, nil); !errors.Is(err, ErrCiphertextNotBlockAligned) {
		t.Fatalf("empty ciphertext err = %v, want ErrCiphertextNotBlockAligned", err)
	}
}

func TestDecryptAESCBCPKCS7_TamperedPadding(t *testing.T) {
	key := fixedKeyBytes(t)
	ciphertext := mustHexDecode(t, shortCiphertextHex)
	// Flip the last ciphertext byte: CBC decryption only affects the
	// corresponding plaintext block's pad bytes, which lets us exercise the
	// padding-validation branch deterministically.
	ciphertext[len(ciphertext)-1] ^= 0xFF
	if _, err := decryptAESCBCPKCS7(key, ciphertext); !errors.Is(err, ErrInvalidPKCS7Padding) {
		t.Fatalf("err = %v, want ErrInvalidPKCS7Padding", err)
	}
}

// TestDecryptFromReader_MatchesBufferedFixedVectors confirms the streaming
// path (media.go's temp-file-to-temp-file decrypt) produces byte-identical
// output to the buffered decryptAESCBCPKCS7 for the same fixed vectors.
func TestDecryptFromReader_MatchesBufferedFixedVectors(t *testing.T) {
	key := fixedKeyBytes(t)
	for _, hexCiphertext := range []string{fixedCiphertextHex, shortCiphertextHex} {
		ciphertext := mustHexDecode(t, hexCiphertext)
		want, err := decryptAESCBCPKCS7(key, ciphertext)
		if err != nil {
			t.Fatalf("decryptAESCBCPKCS7: %v", err)
		}
		var buf bytes.Buffer
		n, err := decryptFromReader(key, bytes.NewReader(ciphertext), &buf)
		if err != nil {
			t.Fatalf("decryptFromReader: %v", err)
		}
		if n != int64(buf.Len()) {
			t.Fatalf("reported %d bytes written, buffer has %d", n, buf.Len())
		}
		if !bytes.Equal(buf.Bytes(), want) {
			t.Fatalf("streamed = %q, want %q", buf.Bytes(), want)
		}
	}
}

// TestDecryptFromReader_RandomRoundTrip encrypts random multi-block
// plaintexts with crypto/cipher directly (an independent reference
// implementation of AES-256-CBC) and asserts the streaming decryptor
// recovers them byte-for-byte across many block-count / final-partial-block
// combinations, including one crossing the block boundary the streaming
// decryptor uses to hold back the final block.
func TestDecryptFromReader_RandomRoundTrip(t *testing.T) {
	key := fixedKeyBytes(t)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	iv := key[:aesBlockSize]

	for _, plainLen := range []int{0, 1, 15, 16, 17, 31, 32, 33, 4096, 4096 + 7} {
		plain := make([]byte, plainLen)
		if _, err := rand.Read(plain); err != nil {
			t.Fatalf("rand.Read: %v", err)
		}
		padLen := aesBlockSize - (plainLen % aesBlockSize)
		padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(padLen)}, padLen)...)

		ciphertext := make([]byte, len(padded))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)

		var buf bytes.Buffer
		n, err := decryptFromReader(key, bytes.NewReader(ciphertext), &buf)
		if err != nil {
			t.Fatalf("plainLen=%d: decryptFromReader: %v", plainLen, err)
		}
		if n != int64(plainLen) {
			t.Fatalf("plainLen=%d: reported %d bytes, want %d", plainLen, n, plainLen)
		}
		if !bytes.Equal(buf.Bytes(), plain) {
			t.Fatalf("plainLen=%d: round trip mismatch", plainLen)
		}
	}
}

func TestDecryptFromReader_NotBlockAligned(t *testing.T) {
	key := fixedKeyBytes(t)
	ciphertext := mustHexDecode(t, shortCiphertextHex)
	var buf bytes.Buffer
	_, err := decryptFromReader(key, bytes.NewReader(ciphertext[:len(ciphertext)-1]), &buf)
	if !errors.Is(err, ErrCiphertextNotBlockAligned) {
		t.Fatalf("err = %v, want ErrCiphertextNotBlockAligned", err)
	}
}

func TestDecryptFromReader_PropagatesReadError(t *testing.T) {
	key := fixedKeyBytes(t)
	boom := errors.New("boom")
	var buf bytes.Buffer
	_, err := decryptFromReader(key, io.MultiReader(strings.NewReader("0123456789012345"), errReader{boom}), &buf)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }
