package transport

import (
	"encoding/base64"
	"testing"
)

// Golden vectors generated from the TypeScript reference implementation
// (cc-channel-octo) using curve25519-js + md5-typescript + crypto-js, with
// fixed seeds (client=0x07*32, server=0x09*32). This test is the decisive
// cross-library parity check: Go's crypto/ecdh must derive the identical shared
// secret and AES key/IV, otherwise every message silently fails to decrypt.
const (
	goldenClientPrivB64 = "AAcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHB0c="
	goldenServerPubB64  = "V9tLNZ8jrl4Ubk4lEgVnBHIlBjSMFQwUdT0Mkz0E1CE="
	goldenSecretB64     = "L/4yWyomYRvxkRMl1KYrM3SbjzUNDoi6keMWrKrmZVQ="
	goldenSalt          = "abcdefghijklmnop1234"
	goldenAESKey        = "2bb83270351b18c2"
	goldenAESIV         = "abcdefghijklmnop"

	// AES-CBC ciphertext (base64) of the plaintext below, encrypted with the
	// golden key/IV by crypto-js — exactly what the wire carries.
	goldenCipherB64 = "Oxr5sQRzs+M4vUg6JgOKanUw9CAvq+RDM4wieTP3WU9KFZnxJ4APWMnu5l/nLk+h"
	goldenPlaintext = `{"type":1,"content":"hello from octo"}`
)

func TestDeriveAESKeyIV_GoldenParity(t *testing.T) {
	privBytes, err := base64.StdEncoding.DecodeString(goldenClientPrivB64)
	if err != nil {
		t.Fatalf("decode client priv: %v", err)
	}
	priv, err := x25519PrivFromBytes(privBytes)
	if err != nil {
		t.Fatalf("NewPrivateKey: %v", err)
	}

	key, iv, err := deriveAESKeyIV(priv, goldenServerPubB64, goldenSalt)
	if err != nil {
		t.Fatalf("deriveAESKeyIV: %v", err)
	}
	if key != goldenAESKey {
		t.Errorf("aesKey mismatch:\n got  %q\n want %q", key, goldenAESKey)
	}
	if iv != goldenAESIV {
		t.Errorf("aesIV mismatch:\n got  %q\n want %q", iv, goldenAESIV)
	}
}

// TestECDHSharedSecretParity asserts Go's ECDH yields the same raw shared
// secret as curve25519-js for the golden clamped private key + server pubkey.
func TestECDHSharedSecretParity(t *testing.T) {
	privBytes, _ := base64.StdEncoding.DecodeString(goldenClientPrivB64)
	priv, err := x25519PrivFromBytes(privBytes)
	if err != nil {
		t.Fatalf("NewPrivateKey: %v", err)
	}
	serverPubBytes, _ := base64.StdEncoding.DecodeString(goldenServerPubB64)
	serverPub, err := x25519PubFromBytes(serverPubBytes)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	secret, err := priv.ECDH(serverPub)
	if err != nil {
		t.Fatalf("ECDH: %v", err)
	}
	got := base64.StdEncoding.EncodeToString(secret)
	if got != goldenSecretB64 {
		t.Fatalf("shared secret mismatch:\n got  %q\n want %q", got, goldenSecretB64)
	}
}

func TestAESDecrypt_Golden(t *testing.T) {
	// The wire payload is the bytes of the base64 ciphertext string.
	block, err := newAESBlock(goldenAESKey)
	if err != nil {
		t.Fatalf("newAESBlock: %v", err)
	}
	out, err := aesDecrypt([]byte(goldenCipherB64), block, goldenAESIV)
	if err != nil {
		t.Fatalf("aesDecrypt: %v", err)
	}
	if string(out) != goldenPlaintext {
		t.Errorf("plaintext mismatch:\n got  %q\n want %q", string(out), goldenPlaintext)
	}
}

func TestPKCS7Unpad_Invalid(t *testing.T) {
	cases := [][]byte{
		{},                 // empty
		{1, 2, 3, 0},       // pad byte 0
		{1, 2, 3, 99},      // pad > block size
		{1, 2, 5, 5, 5, 5}, // declared pad 5 but bytes don't all equal 5
	}
	for i, c := range cases {
		if _, err := pkcs7Unpad(c); err == nil {
			t.Errorf("case %d: expected error, got nil", i)
		}
	}
}
