// Package service — TOTPService is the application-layer wrapper around
// pquerna/otp + the secretbox at-rest encryption used to persist
// per-user TOTP secrets.
package service

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

const totpKeyEnv = "MULTICA_USER_TOTP_KEY"

// TOTPService handles TOTP secret generation, validation and at-rest
// encryption. Construction fails if MULTICA_USER_TOTP_KEY is unset — the
// caller is expected to gate construction on env presence and skip the
// service entirely when TOTP support is disabled (see /api/config
// totp_supported).
type TOTPService struct {
	box *secretbox.Box
}

func NewTOTPService() (*TOTPService, error) {
	key, err := secretbox.LoadKey(totpKeyEnv)
	if err != nil {
		return nil, fmt.Errorf("totp: %w", err)
	}
	box, err := secretbox.New(key)
	if err != nil {
		return nil, fmt.Errorf("totp: %w", err)
	}
	return &TOTPService{box: box}, nil
}

// GenerateSecret returns a fresh base32 TOTP secret + its otpauth:// URI
// for embedding into a QR code. The account label is the user's email.
func (s *TOTPService) GenerateSecret(accountLabel string) (secret, otpauthURL string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Multica",
		AccountName: accountLabel,
		Period:      totpPeriod,
		SecretSize:  20, // bytes; RFC 6238 default
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1, // universal authenticator-app support
	})
	if err != nil {
		return "", "", fmt.Errorf("totp: generate: %w", err)
	}
	return key.Secret(), key.URL(), nil
}

// totpPeriod is the RFC 6238 time step in seconds; it must match
// GenerateSecret, which is what authenticator apps are configured with.
const totpPeriod = 30

// ValidateCode checks a 6-digit code against the given base32 secret with
// a ±1 step (30s) skew window.
func (s *TOTPService) ValidateCode(secret, code string) bool {
	_, ok := s.MatchCode(secret, code, time.Now())
	return ok
}

// MatchCode checks a 6-digit code against the secret for the time steps
// around now (±1, the same window as totp.Validate) and returns the step
// the code belongs to. Callers persist the step so the same code cannot be
// accepted twice (RFC 6238 §5.2).
func (s *TOTPService) MatchCode(secret, code string, now time.Time) (step int64, ok bool) {
	current := now.Unix() / totpPeriod
	for _, candidate := range []int64{current - 1, current, current + 1} {
		expected, err := totp.GenerateCodeCustom(secret, time.Unix(candidate*totpPeriod, 0).UTC(), totp.ValidateOpts{
			Period:    totpPeriod,
			Digits:    otp.DigitsSix,
			Algorithm: otp.AlgorithmSHA1,
		})
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return candidate, true
		}
	}
	return 0, false
}

// SealSecret encrypts a base32 TOTP secret for at-rest storage.
func (s *TOTPService) SealSecret(secret string) ([]byte, error) {
	if secret == "" {
		return nil, errors.New("totp: secret must not be empty")
	}
	return s.box.Seal([]byte(secret))
}

// OpenSecret decrypts a sealed secret back to its base32 form.
func (s *TOTPService) OpenSecret(sealed []byte) (string, error) {
	plain, err := s.box.Open(sealed)
	if err != nil {
		return "", fmt.Errorf("totp: open: %w", err)
	}
	return string(plain), nil
}
