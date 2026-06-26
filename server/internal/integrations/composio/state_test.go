package composio

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var testSecret = []byte("test-state-secret-0123456789")

func TestSignVerifyState_RoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	tok, err := signState(testSecret, stateClaims{
		UserID:       "11111111-1111-1111-1111-111111111111",
		ToolkitSlug:  "notion",
		AuthConfigID: "ac_notion",
		Exp:          now.Add(5 * time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	got, err := verifyState(testSecret, tok, now)
	if err != nil {
		t.Fatalf("verifyState: %v", err)
	}
	if got.UserID != "11111111-1111-1111-1111-111111111111" {
		t.Errorf("user id = %q", got.UserID)
	}
	if got.ToolkitSlug != "notion" {
		t.Errorf("toolkit slug = %q", got.ToolkitSlug)
	}
	if got.AuthConfigID != "ac_notion" {
		t.Errorf("auth config id = %q", got.AuthConfigID)
	}
}

func TestVerifyState_Expired(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	tok, err := signState(testSecret, stateClaims{
		UserID:      "u",
		ToolkitSlug: "notion",
		Exp:         now.Add(-time.Second).Unix(),
	})
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	if _, err := verifyState(testSecret, tok, now); !errors.Is(err, ErrStateExpired) {
		t.Fatalf("expected ErrStateExpired, got %v", err)
	}
}

func TestVerifyState_Tampered(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	tok, err := signState(testSecret, stateClaims{UserID: "u", ToolkitSlug: "notion", Exp: now.Add(time.Minute).Unix()})
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	// Flip a byte in the payload segment.
	payload, sig, _ := strings.Cut(tok, ".")
	tampered := payload[:len(payload)-1] + flipLastChar(payload) + "." + sig
	if _, err := verifyState(testSecret, tampered, now); !errors.Is(err, ErrStateSignature) && !errors.Is(err, ErrStateMalformed) {
		t.Fatalf("expected signature/malformed error, got %v", err)
	}
}

func TestVerifyState_WrongSecret(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	tok, _ := signState(testSecret, stateClaims{UserID: "u", ToolkitSlug: "notion", Exp: now.Add(time.Minute).Unix()})
	if _, err := verifyState([]byte("a-different-secret"), tok, now); !errors.Is(err, ErrStateSignature) {
		t.Fatalf("expected ErrStateSignature, got %v", err)
	}
}

func TestVerifyState_Malformed(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_700_000_000, 0)
	for _, tok := range []string{"", "nodot", ".", "a.", ".b"} {
		if _, err := verifyState(testSecret, tok, now); !errors.Is(err, ErrStateMalformed) {
			t.Errorf("token %q: expected ErrStateMalformed, got %v", tok, err)
		}
	}
}

// flipLastChar returns a single replacement char different from the payload's
// last character so the tampered payload is guaranteed to differ.
func flipLastChar(payload string) string {
	last := payload[len(payload)-1]
	if last == 'A' {
		return "B"
	}
	return "A"
}
