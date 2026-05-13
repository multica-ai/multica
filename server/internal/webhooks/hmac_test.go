package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// computeSig is the production scheme inverted for test convenience —
// makes the test fixtures readable rather than embedding precomputed
// hex strings the maintainer can't verify by eye.
func computeSig(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyHMACSHA256_TruthTable(t *testing.T) {
	const (
		currentSecret  = "current-secret-please-rotate"
		previousSecret = "previous-secret-being-retired"
		body           = `{"hello":"world","payload_version":3}`
	)

	tests := []struct {
		name   string
		header string
		want   error
	}{
		{
			name:   "valid signature with current secret",
			header: computeSig(currentSecret, body),
			want:   nil,
		},
		{
			name:   "valid signature with previous secret (rotation window)",
			header: computeSig(previousSecret, body),
			want:   nil,
		},
		{
			name:   "missing header",
			header: "",
			want:   ErrSignatureMissing,
		},
		{
			name:   "malformed: no sha256= prefix",
			header: hex.EncodeToString(make([]byte, sha256.Size)),
			want:   ErrSignatureMalformed,
		},
		{
			name:   "malformed: wrong algorithm tag",
			header: "sha512=" + hex.EncodeToString(make([]byte, sha256.Size)),
			want:   ErrSignatureMalformed,
		},
		{
			name:   "malformed: too short hex",
			header: "sha256=deadbeef",
			want:   ErrSignatureMalformed,
		},
		{
			name:   "malformed: non-hex characters",
			header: "sha256=" + strings.Repeat("z", sha256.Size*2),
			want:   ErrSignatureMalformed,
		},
		{
			name:   "invalid: well-formed but wrong secret",
			header: computeSig("not-a-known-secret", body),
			want:   ErrSignatureInvalid,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyHMACSHA256(tc.header, []byte(body), []byte(currentSecret), []byte(previousSecret))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestVerifyHMACSHA256_NoPreviousSecretConfigured(t *testing.T) {
	// Steady-state: only `current` is set, `previous` is empty. A
	// signature computed with an arbitrary other key must still be
	// rejected; this guards against the bug where missing-previous
	// would short-circuit through an `if len(previous) == 0 {return
	// nil}` branch.
	body := "anything"
	current := "the-only-secret"

	good := computeSig(current, body)
	if err := VerifyHMACSHA256(good, []byte(body), []byte(current), nil); err != nil {
		t.Fatalf("valid signature with no rotation should pass, got %v", err)
	}

	bad := computeSig("attacker-key", body)
	if err := VerifyHMACSHA256(bad, []byte(body), []byte(current), nil); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("invalid signature with no rotation must fail, got %v", err)
	}
}

func TestVerifyHMACSHA256_BodySensitive(t *testing.T) {
	// Sanity check: same signature against a different body must
	// fail. Without this, a subtle bug that hashed the wrong buffer
	// (e.g. the query string instead of the body) would pass the
	// other tests.
	const secret = "s"
	bodyA := "the-real-payload"
	bodyB := "the-payload-an-attacker-replaced"

	sigA := computeSig(secret, bodyA)
	if err := VerifyHMACSHA256(sigA, []byte(bodyB), []byte(secret), nil); !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("body-A sig against body-B must be invalid, got %v", err)
	}
}
