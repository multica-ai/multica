package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// signRS256 creates a signed RS256 JWT with the given kid header and claims.
func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

// newTestJWKSProvider spins up an httptest server serving the given RSA key as
// JWKS and returns a *JWKSProvider pointed at it (already preloaded).
func newTestJWKSProvider(t *testing.T, key *rsa.PublicKey, kid string) *JWKSProvider {
	t.Helper()
	body := jwkFromRSA(t, key, kid)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	t.Cleanup(srv.Close)

	p := NewJWKSProvider(srv.URL)
	p.Preload()
	return p
}

func TestParseCasdoorJWT_ValidToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "casdoor-key-1"
	jwks := newTestJWKSProvider(t, &key.PublicKey, kid)

	claims := jwt.MapClaims{
		"sub":                "user-abc-123",
		"name":               "Ada Lovelace",
		"preferred_username": "ada",
		"email":              "ada@example.com",
		"phone":              "+1234567890",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
	}
	tokenStr := signRS256(t, key, kid, claims)

	info, err := ParseCasdoorJWT(tokenStr, jwks)
	if err != nil {
		t.Fatalf("ParseCasdoorJWT: %v", err)
	}

	if info.SubjectID != "user-abc-123" {
		t.Errorf("SubjectID = %q, want %q", info.SubjectID, "user-abc-123")
	}
	if info.Name != "Ada Lovelace" {
		t.Errorf("Name = %q, want %q", info.Name, "Ada Lovelace")
	}
	if info.PreferredUsername != "ada" {
		t.Errorf("PreferredUsername = %q, want %q", info.PreferredUsername, "ada")
	}
	if info.Email != "ada@example.com" {
		t.Errorf("Email = %q, want %q", info.Email, "ada@example.com")
	}
	if info.Phone != "+1234567890" {
		t.Errorf("Phone = %q, want %q", info.Phone, "+1234567890")
	}
}

func TestParseCasdoorJWT_ExpiredToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "casdoor-key-1"
	jwks := newTestJWKSProvider(t, &key.PublicKey, kid)

	claims := jwt.MapClaims{
		"sub":   "user-abc-123",
		"name":  "Ada Lovelace",
		"email": "ada@example.com",
		"exp":   time.Now().Add(-1 * time.Hour).Unix(), // expired
	}
	tokenStr := signRS256(t, key, kid, claims)

	_, err = ParseCasdoorJWT(tokenStr, jwks)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestParseCasdoorJWT_WrongAlgorithm(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const kid = "casdoor-key-1"
	jwks := newTestJWKSProvider(t, &key.PublicKey, kid)

	// Sign with HS256 using the RSA public key bytes as the HMAC secret — a
	// classic algorithm-confusion attack. The parser must reject it regardless.
	hmacSecret := []byte("some-hmac-secret-that-is-not-rsa")
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   "user-abc-123",
		"name":  "Ada Lovelace",
		"email": "ada@example.com",
		"exp":   time.Now().Add(1 * time.Hour).Unix(),
	})
	token.Header["kid"] = kid
	tokenStr, err := token.SignedString(hmacSecret)
	if err != nil {
		t.Fatalf("sign HS256 token: %v", err)
	}

	_, err = ParseCasdoorJWT(tokenStr, jwks)
	if err == nil {
		t.Fatal("expected error for HS256-signed token, got nil")
	}
	if !strings.Contains(err.Error(), "RS256") && !strings.Contains(err.Error(), "method") {
		t.Errorf("error should mention algorithm mismatch, got: %v", err)
	}
}
