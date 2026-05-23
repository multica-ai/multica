package githubapp

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TokenFromEnv signs a GitHub App JWT from GITHUB_APP_ID and
// GITHUB_APP_PRIVATE_KEY. The private key may be raw PEM, escaped-newline PEM,
// or base64-encoded PEM to fit common secret stores.
func TokenFromEnv(now time.Time) (token string, configured bool, err error) {
	appID := strings.TrimSpace(os.Getenv("GITHUB_APP_ID"))
	privateKeyPEM := strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY"))
	if appID == "" || privateKeyPEM == "" {
		return "", false, nil
	}
	key, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return "", true, err
	}
	issuedAt := now.Add(-1 * time.Minute)
	tokenObj := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer:    appID,
		IssuedAt:  jwt.NewNumericDate(issuedAt),
		ExpiresAt: jwt.NewNumericDate(issuedAt.Add(10 * time.Minute)),
	})
	signed, err := tokenObj.SignedString(key)
	if err != nil {
		return "", true, err
	}
	return signed, true, nil
}

func parsePrivateKey(raw string) (*rsa.PrivateKey, error) {
	normalized := strings.TrimSpace(strings.ReplaceAll(raw, `\n`, "\n"))
	if !strings.Contains(normalized, "-----BEGIN") {
		if decoded, err := base64.StdEncoding.DecodeString(normalized); err == nil {
			normalized = strings.TrimSpace(string(decoded))
		}
	}
	block, _ := pem.Decode([]byte(normalized))
	if block == nil {
		return nil, errors.New("github app private key is not PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse github app private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("github app private key is not RSA")
	}
	return key, nil
}
