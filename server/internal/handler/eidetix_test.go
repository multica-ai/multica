package handler

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// newTestEidetixBox builds a secretbox with a throwaway in-test key. NEVER use
// a real Eidetix token in tests; "fake-token" below is not a secret.
func newTestEidetixBox(t *testing.T) *secretbox.Box {
	t.Helper()
	key := make([]byte, secretbox.KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	box, err := secretbox.New(key)
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	return box
}

func TestEidetixTokenRoundTrip(t *testing.T) {
	box := newTestEidetixBox(t)
	const plain = "fake-token-not-a-secret"

	sealed, err := box.Seal([]byte(plain))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if string(sealed) == plain {
		t.Fatalf("sealed bytes must not equal plaintext")
	}
	opened, err := box.Open(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(opened) != plain {
		t.Errorf("round-trip = %q, want %q", opened, plain)
	}
}
