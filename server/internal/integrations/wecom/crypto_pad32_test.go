package wecom

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"testing"
)

// TestDecryptFromReader_WeComPadsTo32 pins the padding block WeCom actually
// uses. Spec §3 states the media plaintext is "PKCS#7 填充至 32 字节倍数" — the
// same 32-byte block as WeCom's callback-mode WXBizMsgCrypt libraries, not the
// 16-byte AES block. §5.6's "100 MiB + 32 bytes" Content-Length allowance is
// the same 32 showing up as the worst-case pad overhead.
//
// Padding to a multiple of 32 produces pad lengths 1..32. Everything in 17..32
// spans TWO AES blocks, so a decryptor that both caps the pad at 16 and holds
// back only one block rejects roughly half of all real attachments.
func TestDecryptFromReader_WeComPadsTo32(t *testing.T) {
	const wecomPadBlock = 32

	key := fixedKeyBytes(t)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	iv := key[:aesBlockSize]

	// plainLen % 32 in 0..15 yields pad 17..32 (the failing half); 16..31
	// yields pad 1..16 (what the 16-byte assumption happens to accept).
	for _, plainLen := range []int{0, 1, 15, 16, 17, 31, 32, 33, 47, 48, 4096, 4096 + 7} {
		plain := make([]byte, plainLen)
		if _, err := rand.Read(plain); err != nil {
			t.Fatalf("rand.Read: %v", err)
		}
		padLen := wecomPadBlock - (plainLen % wecomPadBlock)
		padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(padLen)}, padLen)...)

		ciphertext := make([]byte, len(padded))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)

		var buf bytes.Buffer
		n, err := decryptFromReader(key, bytes.NewReader(ciphertext), &buf)
		if err != nil {
			t.Errorf("plainLen=%d (pad=%d): decryptFromReader: %v", plainLen, padLen, err)
			continue
		}
		if n != int64(plainLen) {
			t.Errorf("plainLen=%d (pad=%d): reported %d bytes", plainLen, padLen, n)
			continue
		}
		if !bytes.Equal(buf.Bytes(), plain) {
			t.Errorf("plainLen=%d (pad=%d): round trip mismatch (got %d bytes)",
				plainLen, padLen, buf.Len())
		}
	}
}
