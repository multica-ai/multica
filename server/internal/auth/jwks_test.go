package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// generateTestRSA creates a 2048-bit RSA key for JWKS test fixtures.
func generateTestRSA(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key
}

// jwkFromRSA builds a jwksResponse JSON body from an RSA public key and kid.
func jwkFromRSA(t *testing.T, pub *rsa.PublicKey, kid string) []byte {
	t.Helper()
	nBytes := pub.N.Bytes()
	eBytes := big.NewInt(int64(pub.E)).Bytes()
	resp := jwksResponse{
		Keys: []jwkKey{
			{
				Kty: "RSA",
				Use: "sig",
				Kid: kid,
				Alg: "RS256",
				N:   base64.RawURLEncoding.EncodeToString(nBytes),
				E:   base64.RawURLEncoding.EncodeToString(eBytes),
			},
		},
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal JWKS: %v", err)
	}
	return body
}

func TestJWKSProvider_FetchesAndCachesKeys(t *testing.T) {
	key := generateTestRSA(t)
	body := jwkFromRSA(t, &key.PublicKey, "test-kid-1")

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	p := NewJWKSProvider(srv.URL)
	p.Preload()

	// First GetKey should return the cached key without a second HTTP call.
	pub, err := p.GetKey("test-kid-1")
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if pub == nil {
		t.Fatal("expected non-nil public key")
	}
	if pub.N.Cmp(key.PublicKey.N) != 0 || pub.E != key.PublicKey.E {
		t.Fatal("returned key does not match the served key")
	}

	// Second GetKey for the same kid must not trigger another fetch.
	pub2, err := p.GetKey("test-kid-1")
	if err != nil {
		t.Fatalf("GetKey (second call): %v", err)
	}
	if pub2 != pub {
		t.Fatal("expected identical pointer from cache on second call")
	}

	if n := calls.Load(); n != 1 {
		t.Fatalf("expected exactly 1 HTTP call (preload), got %d", n)
	}
}

func TestJWKSProvider_UnknownKidTriggersRefresh(t *testing.T) {
	key1 := generateTestRSA(t)
	key2 := generateTestRSA(t)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.Write(jwkFromRSA(t, &key1.PublicKey, "kid-1"))
		} else {
			// Second call serves both keys.
			resp := jwksResponse{
				Keys: []jwkKey{
					mustJWK(t, &key1.PublicKey, "kid-1"),
					mustJWK(t, &key2.PublicKey, "kid-2"),
				},
			}
			body, _ := json.Marshal(resp)
			w.Write(body)
		}
	}))
	defer srv.Close()

	p := NewJWKSProvider(srv.URL)
	p.Preload()

	// kid-1 is cached.
	if _, err := p.GetKey("kid-1"); err != nil {
		t.Fatalf("GetKey kid-1: %v", err)
	}

	// Allow immediate refresh for the unknown-kid lookup.
	p.minRefresh = 0

	// kid-2 is unknown, triggers refresh.
	pub2, err := p.GetKey("kid-2")
	if err != nil {
		t.Fatalf("GetKey kid-2: %v", err)
	}
	if pub2 == nil {
		t.Fatal("expected non-nil key for kid-2 after refresh")
	}
	if pub2.N.Cmp(key2.PublicKey.N) != 0 {
		t.Fatal("kid-2 key does not match")
	}

	if n := calls.Load(); n != 2 {
		t.Fatalf("expected 2 HTTP calls (preload + refresh), got %d", n)
	}
}

func mustJWK(t *testing.T, pub *rsa.PublicKey, kid string) jwkKey {
	t.Helper()
	nBytes := pub.N.Bytes()
	eBytes := big.NewInt(int64(pub.E)).Bytes()
	return jwkKey{
		Kty: "RSA",
		Use: "sig",
		Kid: kid,
		Alg: "RS256",
		N:   base64.RawURLEncoding.EncodeToString(nBytes),
		E:   base64.RawURLEncoding.EncodeToString(eBytes),
	}
}

func TestJWKSProvider_MinRefreshInterval(t *testing.T) {
	key1 := generateTestRSA(t)
	key2 := generateTestRSA(t)

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			// First call: serve only rate-kid.
			w.Write(jwkFromRSA(t, &key1.PublicKey, "rate-kid"))
		} else {
			// Subsequent calls: serve both keys.
			resp := jwksResponse{
				Keys: []jwkKey{
					mustJWK(t, &key1.PublicKey, "rate-kid"),
					mustJWK(t, &key2.PublicKey, "unknown-kid"),
				},
			}
			body, _ := json.Marshal(resp)
			w.Write(body)
		}
	}))
	defer srv.Close()

	p := NewJWKSProvider(srv.URL)
	// Use a long minRefresh so repeated calls are rate-limited.
	p.minRefresh = 1 * time.Hour

	p.Preload()
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected 1 call after preload, got %d", n)
	}

	// Ask for an unknown kid; refresh should be skipped due to rate limit,
	// and GetKey should return an error (key still not in cache).
	_, err := p.GetKey("unknown-kid")
	if err == nil {
		t.Fatal("expected error for unknown kid when refresh is rate-limited")
	}

	// No additional HTTP call should have been made.
	if n := calls.Load(); n != 1 {
		t.Fatalf("expected still 1 HTTP call (rate-limited), got %d", n)
	}

	// Now shrink minRefresh so the next call is allowed.
	p.minRefresh = 0
	pub, err := p.GetKey("unknown-kid")
	if err != nil {
		t.Fatalf("GetKey after minRefresh=0: %v", err)
	}
	if pub == nil {
		t.Fatal("expected non-nil key after rate limit reset")
	}
	if pub.N.Cmp(key2.PublicKey.N) != 0 {
		t.Fatal("unknown-kid key does not match key2")
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("expected 2 HTTP calls after rate limit reset, got %d", n)
	}
}
