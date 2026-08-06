package wecom

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// aesKeySize is the WeCom media key length: AES-256 uses a 32-byte key
// (spec §5.6). aesBlockSize is the AES/CBC block size, and also the length
// of the IV WeCom derives as key[:16].
const (
	aesKeySize   = 32
	aesBlockSize = aes.BlockSize
)

// Sentinel errors surfaced by aeskey decoding and CBC/PKCS#7 decryption.
// Callers must not fold the underlying platform response (which may still be
// buffered upstream) into these — see media.go's logging rule: aeskey,
// download URL, temp paths, and plaintext never reach a log line.
var (
	// ErrInvalidAESKey is returned when neither base64 nor hex decoding of
	// the wire aeskey string yields exactly aesKeySize bytes.
	ErrInvalidAESKey = errors.New("wecom: aeskey does not decode to 32 bytes")
	// ErrCiphertextNotBlockAligned is returned when ciphertext length is not
	// a positive multiple of the AES block size.
	ErrCiphertextNotBlockAligned = errors.New("wecom: ciphertext is not a multiple of the AES block size")
	// ErrInvalidPKCS7Padding is returned when the final decrypted block does
	// not carry valid PKCS#7 padding.
	ErrInvalidPKCS7Padding = errors.New("wecom: invalid PKCS#7 padding")
)

// decodeAESKey decodes a WeCom media aeskey wire string into raw key bytes.
//
// WeCom's public docs do not state whether aeskey is base64 or hex encoded
// (spec §10 item 1); this is a deployment smoke item, not a design choice we
// can make from documentation alone. The order tried here — standard base64
// first, then lowercase/uppercase hex — is the preferred decode order for
// that smoke test: a 32-byte key hex-encodes to exactly 64 ASCII characters,
// which is also a length base64 can decode (48 raw bytes), so trying base64
// first and requiring an exact 32-byte result cannot silently accept a
// mis-decoded hex key. Once the smoke test confirms one encoding, callers
// may shortcut directly to it; this function keeps both paths so a
// misconfigured or reverted deployment fails closed (ErrInvalidAESKey)
// rather than decrypting garbage.
func decodeAESKey(aeskey string) ([]byte, error) {
	if key, err := base64.StdEncoding.DecodeString(aeskey); err == nil && len(key) == aesKeySize {
		return key, nil
	}
	if key, err := hex.DecodeString(aeskey); err == nil && len(key) == aesKeySize {
		return key, nil
	}
	return nil, ErrInvalidAESKey
}

// decryptAESCBCPKCS7 decrypts a fully-buffered ciphertext with AES-256-CBC
// using iv = key[:16] (spec §5.6) and strips PKCS#7 padding. Used by tests
// against fixed vectors; media.go's streaming path uses
// newStreamingCBCDecryptor for large payloads instead of buffering.
func decryptAESCBCPKCS7(key, ciphertext []byte) ([]byte, error) {
	if len(key) != aesKeySize {
		return nil, ErrInvalidAESKey
	}
	if len(ciphertext) == 0 || len(ciphertext)%aesBlockSize != 0 {
		return nil, ErrCiphertextNotBlockAligned
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("wecom: new aes cipher: %w", err)
	}
	mode := cipher.NewCBCDecrypter(block, key[:aesBlockSize])
	plain := make([]byte, len(ciphertext))
	mode.CryptBlocks(plain, ciphertext)
	return removePKCS7Padding(plain)
}

// removePKCS7Padding validates and strips PKCS#7 padding from a decrypted
// buffer whose length is already a positive multiple of aesBlockSize.
func removePKCS7Padding(data []byte) ([]byte, error) {
	n := len(data)
	if n == 0 || n%aesBlockSize != 0 {
		return nil, ErrInvalidPKCS7Padding
	}
	pad := int(data[n-1])
	if pad == 0 || pad > aesBlockSize || pad > n {
		return nil, ErrInvalidPKCS7Padding
	}
	if !bytes.Equal(data[n-pad:], bytes.Repeat([]byte{byte(pad)}, pad)) {
		return nil, ErrInvalidPKCS7Padding
	}
	return data[:n-pad], nil
}

// streamingCBCDecryptor decrypts an AES-256-CBC + PKCS#7 stream one block at
// a time without ever buffering the full ciphertext or plaintext in memory
// (spec §5.6 item 3: ciphertext lives in a 0600 temp file; this type is what
// streams it to the second temp file). It holds back the most recently
// decrypted block so the true final block — the one carrying the PKCS#7
// pad — is only written, unpadded, once Close confirms no further ciphertext
// follows.
type streamingCBCDecryptor struct {
	mode    cipher.BlockMode
	dst     io.Writer
	written int64

	pending     [aesBlockSize]byte
	havePending bool
}

// newStreamingCBCDecryptor constructs a decryptor writing plaintext to dst
// as ciphertext blocks are fed via Write. iv is key[:16] per spec §5.6.
func newStreamingCBCDecryptor(key []byte, dst io.Writer) (*streamingCBCDecryptor, error) {
	if len(key) != aesKeySize {
		return nil, ErrInvalidAESKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("wecom: new aes cipher: %w", err)
	}
	return &streamingCBCDecryptor{
		mode: cipher.NewCBCDecrypter(block, key[:aesBlockSize]),
		dst:  dst,
	}, nil
}

// decryptFromReader copies ciphertext from src (read in full aesBlockSize
// chunks) into a streamingCBCDecryptor and finalizes it, returning the
// number of plaintext bytes written to dst.
func decryptFromReader(key []byte, src io.Reader, dst io.Writer) (int64, error) {
	dec, err := newStreamingCBCDecryptor(key, dst)
	if err != nil {
		return 0, err
	}
	buf := make([]byte, aesBlockSize)
	for {
		n, rerr := io.ReadFull(src, buf)
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			if rerr == io.ErrUnexpectedEOF {
				return dec.written, ErrCiphertextNotBlockAligned
			}
			return dec.written, rerr
		}
		if err := dec.writeBlock(buf[:n]); err != nil {
			return dec.written, err
		}
	}
	if err := dec.finish(); err != nil {
		return dec.written, err
	}
	return dec.written, nil
}

func (d *streamingCBCDecryptor) writeBlock(ciphertext []byte) error {
	if len(ciphertext) != aesBlockSize {
		return ErrCiphertextNotBlockAligned
	}
	var plain [aesBlockSize]byte
	d.mode.CryptBlocks(plain[:], ciphertext)
	if d.havePending {
		if _, err := d.dst.Write(d.pending[:]); err != nil {
			return err
		}
		d.written += aesBlockSize
	}
	d.pending = plain
	d.havePending = true
	return nil
}

// finish strips PKCS#7 padding from the held-back final block and writes
// the remainder. It is an error to call finish before any block was fed —
// that means the ciphertext was empty, which is never valid PKCS#7 input.
func (d *streamingCBCDecryptor) finish() error {
	if !d.havePending {
		return ErrCiphertextNotBlockAligned
	}
	unpadded, err := removePKCS7Padding(d.pending[:])
	if err != nil {
		return err
	}
	if len(unpadded) > 0 {
		if _, err := d.dst.Write(unpadded); err != nil {
			return err
		}
	}
	d.written += int64(len(unpadded))
	return nil
}
