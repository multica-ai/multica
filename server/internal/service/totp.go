// Package service — TOTPService is the application-layer wrapper around
// pquerna/otp + the secretbox at-rest encryption used to persist
// per-user TOTP secrets.
package service

import (
	"errors"
	"fmt"

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
		Period:      30,
		SecretSize:  20, // bytes; RFC 6238 default
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1, // universal authenticator-app support
	})
	if err != nil {
		return "", "", fmt.Errorf("totp: generate: %w", err)
	}
	return key.Secret(), key.URL(), nil
}

// ValidateCode checks a 6-digit code against the given base32 secret with
// the default ±1 epoch (30s) skew window.
func (s *TOTPService) ValidateCode(secret, code string) bool {
	return totp.Validate(code, secret)
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

