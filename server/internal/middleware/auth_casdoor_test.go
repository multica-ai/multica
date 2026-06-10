package middleware

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/multica-ai/multica/server/internal/auth"
)

// casdoorCookieName is the Casdoor session cookie read by the middleware.
// Duplicated here only for test clarity; the production constant lives in
// auth_casdoor.go.
const casdoorCookieName = "zgsmAdminToken"

// testJWKS mirrors auth.jwksResponse / auth.jwkKey — those types are
// unexported, so the test rebuilds the minimal JWKS JSON it needs.
type testJWKS struct {
	Keys []testJWK `json:"keys"`
}

type testJWK struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// setupTestJWKS spins up an httptest server that serves a single-key JWKS
// document for the given RSA public key and kid, and returns a JWKSProvider
// that has already been preloaded against it.
func setupTestJWKS(t *testing.T, pub *rsa.PublicKey, kid string) *auth.JWKSProvider {
	t.Helper()

	nBytes := pub.N.Bytes()
	eBytes := big.NewInt(int64(pub.E)).Bytes()
	body, err := json.Marshal(testJWKS{
		Keys: []testJWK{
			{
				Kty: "RSA",
				Use: "sig",
				Kid: kid,
				Alg: "RS256",
				N:   base64.RawURLEncoding.EncodeToString(nBytes),
				E:   base64.RawURLEncoding.EncodeToString(eBytes),
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal JWKS: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	p := auth.NewJWKSProvider(srv.URL)
	p.Preload()
	return p
}

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

// stubResolver returns a SubjectResolver that maps any subject to the given
// Multica user UUID. It fails the test when the resolver is called with an
// unexpected subject.
func stubResolver(t *testing.T, wantSubject, multicaUUID string) SubjectResolver {
	t.Helper()
	return func(_ context.Context, subjectID, name, email string) (string, error) {
		if subjectID != wantSubject {
			t.Fatalf("resolver called with subject %q, want %q", subjectID, wantSubject)
		}
		return multicaUUID, nil
	}
}

func TestCasdoorAuth_ValidCookie(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	const (
		kid          = "casdoor-test-kid"
		subjectID    = "casdoor-user-42"
		multicaUUID  = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	)
	jwks := setupTestJWKS(t, &key.PublicKey, kid)
	resolver := stubResolver(t, subjectID, multicaUUID)

	mw := CasdoorAuth(jwks, resolver)

	var gotUserID, gotSubject string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = r.Header.Get("X-User-ID")
		gotSubject = r.Header.Get("X-Subject-ID")
		w.WriteHeader(http.StatusOK)
	}))

	tokenStr := signRS256(t, key, kid, jwt.MapClaims{
		"sub":   subjectID,
		"name":  "Test User",
		"email": "test@example.com",
		"exp":   time.Now().Add(1 * time.Hour).Unix(),
	})

	req := httptest.NewRequest("GET", "/api/issues", nil)
	req.AddCookie(&http.Cookie{Name: casdoorCookieName, Value: tokenStr})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body=%s", w.Code, w.Body.String())
	}
	if gotUserID != multicaUUID {
		t.Errorf("X-User-ID = %q, want %q", gotUserID, multicaUUID)
	}
	if gotSubject != subjectID {
		t.Errorf("X-Subject-ID = %q, want %q", gotSubject, subjectID)
	}
}

func TestCasdoorAuth_NoCookie_Returns401(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	jwks := setupTestJWKS(t, &key.PublicKey, "kid")
	resolver := func(_ context.Context, _, _, _ string) (string, error) {
		t.Fatal("resolver should not be called when no token is present")
		return "", nil
	}

	mw := CasdoorAuth(jwks, resolver)
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called")
	}))

	req := httptest.NewRequest("GET", "/api/issues", nil)
	// No cookie, no Authorization header.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	body := w.Body.String()
	if body == "" {
		t.Fatal("expected non-empty error body")
	}
	// Body should be valid JSON with an "error" key.
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("response body is not JSON: %v; raw=%q", err, body)
	}
	if _, ok := resp["error"]; !ok {
		t.Errorf("expected 'error' key in response, got %v", resp)
	}
}

func TestCasdoorAuth_PATTokenPassesThrough(t *testing.T) {
	// JWKS and resolver are irrelevant — a PAT-prefixed Bearer token must
	// short-circuit before either is consulted.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	jwks := setupTestJWKS(t, &key.PublicKey, "kid")
	resolver := func(_ context.Context, _, _, _ string) (string, error) {
		t.Fatal("resolver should not be called for PAT tokens")
		return "", nil
	}

	mw := CasdoorAuth(jwks, resolver)

	called := false
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// The middleware must NOT have set X-User-ID or X-Subject-ID —
		// those belong to the downstream PAT middleware.
		if uid := r.Header.Get("X-User-ID"); uid != "" {
			t.Errorf("X-User-ID should not be set by CasdoorAuth for PAT, got %q", uid)
		}
		if sid := r.Header.Get("X-Subject-ID"); sid != "" {
			t.Errorf("X-Subject-ID should not be set by CasdoorAuth for PAT, got %q", sid)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/issues", nil)
	req.Header.Set("Authorization", "Bearer mul_some_personal_access_token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 pass-through for PAT, got %d", w.Code)
	}
	if !called {
		t.Fatal("next handler was not called for PAT pass-through")
	}
}
