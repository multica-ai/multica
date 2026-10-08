package wecom

// media_crypt.go — what a callback's aeskey unlocks.
//
// A smart-bot message that carries a photo, a file or a video gives the bot
// two strings: a Tencent COS URL good for five minutes, and the key the bytes
// behind it are encrypted with. Long-connection mode mints a fresh key per
// URL, which is the difference from callback mode's one deployment-wide
// EncodingAESKey — there is nothing to configure and nothing to rotate, but
// also nothing to fall back on if the key on the frame is unusable.
//
// The algorithm is AES-256-CBC with the IV taken from the front of the key
// itself, and the plaintext PKCS#7-padded to a multiple of 32 bytes
// (developer.work.weixin.qq.com/document/path/101463).

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// mediaAESKeyBytes is the decoded key length AES-256 requires.
const mediaAESKeyBytes = 32

// mediaPadBlock is the block the plaintext is padded up to. It is NOT the AES
// block size, and that gap is the trap: AES works in 16-byte blocks, so a
// PKCS#7 unpadder written against the cipher rejects any pad longer than 16 —
// and a file whose length is already a multiple of 32 is padded with a whole
// 32-byte block. Ordinary payloads land there often enough that such an
// unpadder looks fine in a smoke test and fails in production.
const mediaPadBlock = 32

// decryptMedia turns one downloaded body back into the file the user sent.
// Every failure is an error rather than a best guess: a wrong answer here is
// an attachment that opens as garbage, which is worse than an attachment that
// is honestly missing.
func decryptMedia(aesKey string, ciphertext []byte) ([]byte, error) {
	key, err := decodeMediaAESKey(aesKey)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) == 0 {
		return nil, errors.New("wecom: media ciphertext is empty")
	}
	if len(ciphertext)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("wecom: media ciphertext is %d bytes, not a multiple of the %d-byte AES block", len(ciphertext), aes.BlockSize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("wecom: media cipher: %w", err)
	}
	plain := make([]byte, len(ciphertext))
	// The IV is the front of the key. Reusing key material as an IV is
	// WeCom's choice, not ours; we only have to match it.
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(plain, ciphertext)
	return unpadMedia(plain)
}

// decodeMediaAESKey decodes the base64 the frame carries. Both the padded
// 44-character form and the unpadded 43-character form appear in WeCom's own
// surfaces, so both are accepted; anything that does not come out at exactly
// 32 bytes is refused rather than stretched or truncated into one.
func decodeMediaAESKey(raw string) ([]byte, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, errors.New("wecom: media aeskey is empty")
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if key, err := enc.DecodeString(s); err == nil && len(key) == mediaAESKeyBytes {
			return key, nil
		}
	}
	return nil, fmt.Errorf("wecom: media aeskey does not decode to %d bytes", mediaAESKeyBytes)
}

// unpadMedia strips the PKCS#7 tail, validating every byte of it. A tail that
// does not check out means the key was wrong or the body was truncated, and
// either way the bytes in front of it cannot be trusted to be the file.
func unpadMedia(plain []byte) ([]byte, error) {
	n := len(plain)
	if n == 0 {
		return nil, errors.New("wecom: media plaintext is empty")
	}
	pad := int(plain[n-1])
	if pad < 1 || pad > mediaPadBlock || pad > n {
		return nil, fmt.Errorf("wecom: media padding length %d is out of range (1..%d)", pad, mediaPadBlock)
	}
	for _, b := range plain[n-pad:] {
		if int(b) != pad {
			return nil, errors.New("wecom: media padding bytes disagree; wrong key or truncated body")
		}
	}
	return plain[:n-pad], nil
}
