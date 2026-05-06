package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

// fakeIssuer spins up an HTTPS server that publishes an OIDC discovery doc
// and a JWKs endpoint backed by an RSA key we generated. Tests sign synthetic
// ID tokens with the matching private key.
func fakeIssuer(t *testing.T) (issuer string, priv *rsa.PrivateKey, stop func()) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	var s *httptest.Server
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                s.URL,
			"jwks_uri":                              s.URL + "/jwks",
			"authorization_endpoint":                s.URL + "/auth",
			"token_endpoint":                        s.URL + "/token",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{
			Keys: []jose.JSONWebKey{{Key: &priv.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}},
		})
	})
	s = httptest.NewServer(mux)
	return s.URL, priv, s.Close
}

func signID(t *testing.T, priv *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestVerify_HappyPath(t *testing.T) {
	iss, priv, stop := fakeIssuer(t)
	defer stop()
	v, err := NewGoogleVerifierForIssuer(context.Background(), iss, "client-1")
	if err != nil {
		t.Fatal(err)
	}
	raw := signID(t, priv, jwt.MapClaims{
		"iss": iss, "aud": "client-1", "sub": "google-sub",
		"email": "a@b.com", "email_verified": true, "name": "Alice",
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	id, err := v.Verify(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if id.Sub != "google-sub" || id.Email != "a@b.com" || !id.EmailVerified || id.Name != "Alice" {
		t.Fatalf("got %+v", id)
	}
}

func TestVerify_RejectsWrongAudience(t *testing.T) {
	iss, priv, stop := fakeIssuer(t)
	defer stop()
	v, _ := NewGoogleVerifierForIssuer(context.Background(), iss, "client-1")
	raw := signID(t, priv, jwt.MapClaims{
		"iss": iss, "aud": "attacker", "sub": "x",
		"email": "a@b", "email_verified": true,
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	if _, err := v.Verify(context.Background(), raw); err == nil {
		t.Fatal("expected aud mismatch")
	}
}

func TestVerify_RejectsExpired(t *testing.T) {
	iss, priv, stop := fakeIssuer(t)
	defer stop()
	v, _ := NewGoogleVerifierForIssuer(context.Background(), iss, "client-1")
	raw := signID(t, priv, jwt.MapClaims{
		"iss": iss, "aud": "client-1", "sub": "x",
		"email": "a@b", "email_verified": true,
		"exp": time.Now().Add(-time.Minute).Unix(),
	})
	if _, err := v.Verify(context.Background(), raw); err == nil {
		t.Fatal("expected expired")
	}
}

func TestVerify_RejectsBadSignature(t *testing.T) {
	iss, _, stop := fakeIssuer(t)
	defer stop()
	v, _ := NewGoogleVerifierForIssuer(context.Background(), iss, "client-1")
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	raw := signID(t, other, jwt.MapClaims{
		"iss": iss, "aud": "client-1", "sub": "x",
		"email": "a@b", "email_verified": true,
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	if _, err := v.Verify(context.Background(), raw); err == nil {
		t.Fatal("expected signature error")
	}
}
