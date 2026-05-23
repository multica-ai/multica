package githubapp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenFromEnvSignsGitHubAppJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", strings.ReplaceAll(string(privatePEM), "\n", `\n`))

	tokenString, configured, err := TokenFromEnv(time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatalf("TokenFromEnv: %v", err)
	}
	if !configured {
		t.Fatal("configured = false, want true")
	}

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodRS256 {
			t.Fatalf("signing method = %v, want RS256", token.Method)
		}
		return &key.PublicKey, nil
	}, jwt.WithTimeFunc(func() time.Time { return time.Unix(1_800_000_000, 0) }))
	if err != nil || !token.Valid {
		t.Fatalf("invalid token: valid=%v err=%v", token != nil && token.Valid, err)
	}
	claims := token.Claims.(jwt.MapClaims)
	if claims["iss"] != "12345" {
		t.Fatalf("issuer = %v, want 12345", claims["iss"])
	}
}

func TestTokenFromEnvReturnsUnconfiguredWhenMissingCredentials(t *testing.T) {
	t.Setenv("GITHUB_APP_ID", "")
	t.Setenv("GITHUB_APP_PRIVATE_KEY", "")

	tokenString, configured, err := TokenFromEnv(time.Unix(1_800_000_000, 0))
	if err != nil {
		t.Fatalf("TokenFromEnv: %v", err)
	}
	if configured {
		t.Fatal("configured = true, want false")
	}
	if tokenString != "" {
		t.Fatalf("token = %q, want empty", tokenString)
	}
}
