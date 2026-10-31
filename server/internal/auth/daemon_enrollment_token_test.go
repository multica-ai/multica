package auth

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateDaemonEnrollmentToken(t *testing.T) {
	first, err := GenerateDaemonEnrollmentToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateDaemonEnrollmentToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("independently minted credentials must differ")
	}
	if !strings.HasPrefix(first, DaemonEnrollmentTokenPrefix) {
		t.Fatal("missing service credential prefix")
	}
	secret, err := hex.DecodeString(strings.TrimPrefix(first, DaemonEnrollmentTokenPrefix))
	if err != nil || len(secret) != 32 {
		t.Fatal("credential must contain 32 random bytes")
	}
	if HashToken(first) == first || HashToken(first) == HashToken(second) {
		t.Fatal("token hash must identify the secret without retaining plaintext")
	}
}
