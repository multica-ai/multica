package transport

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

// dhKeyPair is an ephemeral X25519 key pair generated per connection.
type dhKeyPair struct {
	priv      *ecdh.PrivateKey
	publicB64 string
}

// generateDHKeyPair creates a fresh X25519 key pair. crypto/ecdh applies the
// RFC 7748 clamping internally, matching curve25519-js's generateKeyPair.
func generateDHKeyPair() (*dhKeyPair, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &dhKeyPair{
		priv:      priv,
		publicB64: base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes()),
	}, nil
}

// x25519PrivFromBytes reconstructs a private key from 32 raw bytes (used in
// tests with fixed seeds).
func x25519PrivFromBytes(b []byte) (*ecdh.PrivateKey, error) {
	return ecdh.X25519().NewPrivateKey(b)
}

// x25519PubFromBytes reconstructs a public key from 32 raw bytes.
func x25519PubFromBytes(b []byte) (*ecdh.PublicKey, error) {
	return ecdh.X25519().NewPublicKey(b)
}

// deriveAESKeyIV computes the AES-128-CBC key and IV from the DH handshake, as
// CONNACK delivers them. This MUST be byte-for-byte identical to the TS client:
//
//	secret  = ECDH(dhPriv, serverPubKey)          // 32 raw bytes
//	aesKey  = MD5(base64(secret))[:16]            // first 16 HEX chars (ASCII)
//	aesIV   = salt[:16]                            // first 16 bytes of salt
//
// A wrong key or IV does not error — it silently fails to decrypt EVERY message
// while the connection looks healthy (heartbeat fine). The caller validates the
// salt length (>=16 bytes) before calling and fails the handshake otherwise.
func deriveAESKeyIV(priv *ecdh.PrivateKey, serverKeyB64, salt string) (key string, iv string, err error) {
	if len([]byte(salt)) < 16 {
		return "", "", fmt.Errorf("im: CONNACK salt too short (got %d bytes, need >=16)", len([]byte(salt)))
	}
	serverPubBytes, err := base64.StdEncoding.DecodeString(serverKeyB64)
	if err != nil {
		return "", "", fmt.Errorf("im: invalid server key base64: %w", err)
	}
	serverPub, err := ecdh.X25519().NewPublicKey(serverPubBytes)
	if err != nil {
		return "", "", fmt.Errorf("im: invalid server public key: %w", err)
	}
	secret, err := priv.ECDH(serverPub)
	if err != nil {
		return "", "", fmt.Errorf("im: ECDH failed: %w", err)
	}

	secretB64 := base64.StdEncoding.EncodeToString(secret)
	sum := md5.Sum([]byte(secretB64))
	fullHex := hex.EncodeToString(sum[:]) // 32 lowercase hex chars
	key = fullHex[:16]                    // 16 chars = 16 ASCII bytes
	iv = string([]byte(salt)[:16])        // first 16 bytes of salt
	return key, iv, nil
}

var errBadCipher = errors.New("im: ciphertext not a multiple of block size")

// newAESBlock builds a reusable AES cipher block from the derived key. The key
// is fixed for the lifetime of a connection, so the block (which performs the
// expensive key expansion) is created once in onConnack and reused for every
// inbound message — cipher.NewCBCDecrypter on top of it is cheap.
func newAESBlock(key string) (cipher.Block, error) {
	return aes.NewCipher([]byte(key))
}

// aesDecrypt decrypts a RECV payload. The payload bytes are the base64-encoded
// ciphertext (the wire carries base64 text); we decode then AES-CBC decrypt and
// strip PKCS#7 padding. block is the per-connection cipher built by newAESBlock.
func aesDecrypt(payload []byte, block cipher.Block, iv string) ([]byte, error) {
	ct, err := base64.StdEncoding.DecodeString(string(payload))
	if err != nil {
		return nil, fmt.Errorf("im: payload base64 decode: %w", err)
	}
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, errBadCipher
	}
	mode := cipher.NewCBCDecrypter(block, []byte(iv))
	out := make([]byte, len(ct))
	mode.CryptBlocks(out, ct)
	return pkcs7Unpad(out)
}

// pkcs7Unpad removes PKCS#7 padding, validating the padding bytes.
func pkcs7Unpad(b []byte) ([]byte, error) {
	n := len(b)
	if n == 0 {
		return nil, errBadCipher
	}
	pad := int(b[n-1])
	if pad == 0 || pad > aes.BlockSize || pad > n {
		return nil, fmt.Errorf("im: invalid PKCS#7 padding length %d", pad)
	}
	for _, c := range b[n-pad:] {
		if int(c) != pad {
			return nil, errors.New("im: invalid PKCS#7 padding bytes")
		}
	}
	return b[:n-pad], nil
}
