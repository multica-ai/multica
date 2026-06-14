package service

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func makeTOTPKeyEnv(t *testing.T) string {
	t.Helper()
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw[:])
}

func TestNewTOTPService_RequiresKey(t *testing.T) {
	t.Setenv("MULTICA_USER_TOTP_KEY", "")
	if _, err := NewTOTPService(); err == nil {
		t.Fatal("expected error when key is unset")
	}
}

func TestTOTPService_GenerateAndValidate(t *testing.T) {
	t.Setenv("MULTICA_USER_TOTP_KEY", makeTOTPKeyEnv(t))
	svc, err := NewTOTPService()
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	secret, otpauthURL, err := svc.GenerateSecret("alice@example.com")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.HasPrefix(otpauthURL, "otpauth://totp/Multica:alice@example.com") {
		t.Errorf("otpauthURL = %q, want prefix otpauth://totp/Multica:alice@example.com", otpauthURL)
	}
	if len(secret) < 16 {
		t.Errorf("secret too short: %d chars", len(secret))
	}

	// generate a valid code for "now"
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	if !svc.ValidateCode(secret, code) {
		t.Error("freshly-generated code did not validate")
	}
	if svc.ValidateCode(secret, "000000") {
		t.Error("bogus 000000 accidentally validated")
	}
}

func TestTOTPService_SealOpen_Roundtrip(t *testing.T) {
	t.Setenv("MULTICA_USER_TOTP_KEY", makeTOTPKeyEnv(t))
	svc, _ := NewTOTPService()

	secret := "ABCDEF234567ABCDEF234567ABCDEF23"
	sealed, err := svc.SealSecret(secret)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if len(sealed) <= len(secret) {
		t.Errorf("sealed should be longer than plaintext; got %d bytes for %d-char plaintext", len(sealed), len(secret))
	}

	plain, err := svc.OpenSecret(sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if plain != secret {
		t.Errorf("roundtrip mismatch: got %q, want %q", plain, secret)
	}
}
