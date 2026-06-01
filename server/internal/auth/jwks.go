package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// jwksMaxBody is the maximum bytes read from a JWKS HTTP response.
// Casdoor's JWKS is small; 1MB is a generous upper bound that protects
// against a misbehaving endpoint filling memory.
const jwksMaxBody = 1 << 20 // 1 MB

// JWKSProvider fetches and caches JSON Web Key Sets from Casdoor's OIDC endpoint.
// It supports automatic refresh and on-demand re-fetch when an unknown key ID is encountered.
type JWKSProvider struct {
	jwksURL    string
	mu         sync.RWMutex
	keys       map[string]*rsa.PublicKey // kid -> public key
	lastFetch  time.Time
	minRefresh time.Duration // minimum interval between remote fetches
	httpClient *http.Client
}

type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// NewJWKSProvider creates a provider that fetches keys from casdoorEndpoint + "/.well-known/jwks".
// casdoorEndpoint should be the base URL, e.g. "http://localhost:8000".
func NewJWKSProvider(casdoorEndpoint string) *JWKSProvider {
	return &JWKSProvider{
		jwksURL:    casdoorEndpoint + "/.well-known/jwks",
		keys:       make(map[string]*rsa.PublicKey),
		minRefresh: 5 * time.Minute,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// GetKey returns the RSA public key for the given key ID.
// If the key is not cached, it attempts a remote fetch (rate-limited).
// If kid is empty, returns the first available key (Casdoor typically uses a single key).
func (p *JWKSProvider) GetKey(kid string) (*rsa.PublicKey, error) {
	// Try cached key first.
	p.mu.RLock()
	if kid == "" {
		for _, key := range p.keys {
			p.mu.RUnlock()
			return key, nil
		}
		p.mu.RUnlock()
	} else {
		key, ok := p.keys[kid]
		p.mu.RUnlock()
		if ok {
			return key, nil
		}
	}

	// Cache miss — try to refresh from remote.
	if err := p.refresh(); err != nil {
		return nil, fmt.Errorf("JWKS fetch failed: %w", err)
	}

	// Retry from cache.
	p.mu.RLock()
	defer p.mu.RUnlock()
	if kid == "" {
		for _, key := range p.keys {
			return key, nil
		}
		return nil, fmt.Errorf("no keys available in JWKS")
	}
	key, ok := p.keys[kid]
	if !ok {
		return nil, fmt.Errorf("key %q not found in JWKS", kid)
	}
	return key, nil
}

// Preload fetches keys eagerly at startup. Non-fatal: logs a warning on failure.
func (p *JWKSProvider) Preload() {
	if err := p.refresh(); err != nil {
		slog.Warn("[JWKS] initial key fetch failed (JWT verification will fall back to Casdoor API)", "err", err)
	} else {
		p.mu.RLock()
		count := len(p.keys)
		p.mu.RUnlock()
		slog.Info("[JWKS] loaded signing key(s)", "count", count, "url", p.jwksURL)
	}
}

// refresh fetches the JWKS from the remote endpoint, rate-limited by minRefresh.
func (p *JWKSProvider) refresh() error {
	p.mu.Lock()
	if time.Since(p.lastFetch) < p.minRefresh {
		p.mu.Unlock()
		return nil // too soon, skip
	}
	p.lastFetch = time.Now()
	p.mu.Unlock()

	resp, err := p.httpClient.Get(p.jwksURL)
	if err != nil {
		return fmt.Errorf("GET %s: %w", p.jwksURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, jwksMaxBody))
		return fmt.Errorf("GET %s returned %d: %s", p.jwksURL, resp.StatusCode, string(body))
	}

	// Limit body size to protect against misbehaving endpoints.
	limitedBody := io.LimitReader(resp.Body, jwksMaxBody)

	var jwks jwksResponse
	if err := json.NewDecoder(limitedBody).Decode(&jwks); err != nil {
		return fmt.Errorf("decode JWKS: %w", err)
	}

	newKeys := make(map[string]*rsa.PublicKey)
	for _, k := range jwks.Keys {
		// Skip non-RSA and non-RS256 keys.
		if k.Kty != "RSA" {
			continue
		}
		if k.Alg != "" && k.Alg != "RS256" {
			continue
		}
		pub, err := parseRSAPublicKey(k)
		if err != nil {
			slog.Warn("[JWKS] skipping unparseable key", "kid", k.Kid, "err", err)
			continue
		}
		kid := k.Kid
		if kid == "" {
			kid = "_default"
		}
		newKeys[kid] = pub
	}

	if len(newKeys) == 0 {
		return fmt.Errorf("no valid RSA keys in JWKS response")
	}

	p.mu.Lock()
	p.keys = newKeys
	p.mu.Unlock()
	return nil
}

// parseRSAPublicKey constructs an *rsa.PublicKey from JWK parameters (n, e).
func parseRSAPublicKey(k jwkKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}
