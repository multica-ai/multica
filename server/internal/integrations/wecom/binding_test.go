package wecom

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestHashBindingTokenDeterministic(t *testing.T) {
	raw := "test-token-value"
	sum := sha256.Sum256([]byte(raw))
	want := hex.EncodeToString(sum[:])
	if got := hashBindingToken(raw); got != want {
		t.Fatalf("hashBindingToken = %q, want %q", got, want)
	}
}

func TestRandomBindingTokenLength(t *testing.T) {
	raw, err := randomBindingToken(32)
	if err != nil {
		t.Fatalf("randomBindingToken: %v", err)
	}
	if len(raw) < 20 {
		t.Fatalf("token too short: %q", raw)
	}
}
