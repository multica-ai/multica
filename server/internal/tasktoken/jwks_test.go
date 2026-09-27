package tasktoken

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testRSAKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal rsa key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func TestJWKSIsEmptyWhenUnconfigured(t *testing.T) {
	var iss *Issuer
	set, err := iss.JWKS()
	if err != nil {
		t.Fatalf("JWKS() error = %v, want nil", err)
	}
	if len(set.Keys) != 0 {
		t.Fatalf("JWKS() returned %d keys, want 0 for a nil issuer", len(set.Keys))
	}
}

func TestJWKSPublishesOneEntryPerKeyID(t *testing.T) {
	keyPEM, _ := testKeyPEM(t)
	catalog := `[
	  {"id":"erp","label":"ERP","env":"TOKEN_ERP","key_id":"erp-2026","claims":{"sub":"{{identity.id}}"}},
	  {"id":"wiki","label":"Wiki","env":"TOKEN_WIKI","key_id":"wiki-2026","claims":{"sub":"{{identity.id}}"}}
	]`
	iss, err := NewIssuer(catalog, keyPEM, "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}

	set, err := iss.JWKS()
	if err != nil {
		t.Fatalf("JWKS() error = %v", err)
	}
	if len(set.Keys) != 2 {
		t.Fatalf("JWKS() returned %d keys, want 2", len(set.Keys))
	}

	byKID := map[string]JWK{}
	for _, k := range set.Keys {
		byKID[k.Kid] = k
	}
	for _, want := range []string{"erp-2026", "wiki-2026"} {
		k, ok := byKID[want]
		if !ok {
			t.Fatalf("JWKS() has no entry for kid %q", want)
		}
		if k.Kty != "EC" {
			t.Errorf("kid %q: Kty = %q, want EC", want, k.Kty)
		}
		if k.Crv != "P-256" {
			t.Errorf("kid %q: Crv = %q, want P-256", want, k.Crv)
		}
		if k.Use != "sig" {
			t.Errorf("kid %q: Use = %q, want sig", want, k.Use)
		}
		if k.Alg != "ES256" {
			t.Errorf("kid %q: Alg = %q, want ES256", want, k.Alg)
		}
	}
}

// The set is served to anyone who asks, so the serialised form is the thing
// that matters: no private component of the signing key may appear in it.
func TestJWKSJSONCarriesNoPrivateKeyMaterial(t *testing.T) {
	keyPEM, _ := testKeyPEM(t)
	catalog := `[{"id":"erp","label":"ERP","env":"TOKEN_ERP","key_id":"k","claims":{"sub":"{{identity.id}}"}}]`
	iss, err := NewIssuer(catalog, keyPEM, "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}

	set, err := iss.JWKS()
	if err != nil {
		t.Fatalf("JWKS() error = %v", err)
	}
	encoded, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal set: %v", err)
	}

	var decoded struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal set: %v", err)
	}
	// "d" is the private exponent for both RSA and EC keys in RFC 7518.
	for _, private := range []string{"d", "p", "q", "dp", "dq", "qi"} {
		for _, k := range decoded.Keys {
			if _, ok := k[private]; ok {
				t.Errorf("published JWK carries private component %q", private)
			}
		}
	}
}

func TestJWKSDeduplicatesTemplatesSharingAKeyID(t *testing.T) {
	keyPEM, _ := testKeyPEM(t)
	catalog := `[
	  {"id":"erp","label":"ERP","env":"TOKEN_ERP","key_id":"shared","claims":{"sub":"{{identity.id}}"}},
	  {"id":"wiki","label":"Wiki","env":"TOKEN_WIKI","key_id":"shared","claims":{"sub":"{{identity.id}}"}}
	]`
	iss, err := NewIssuer(catalog, keyPEM, "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}

	set, err := iss.JWKS()
	if err != nil {
		t.Fatalf("JWKS() error = %v", err)
	}
	if len(set.Keys) != 1 {
		t.Fatalf("JWKS() returned %d keys, want 1 — two templates share one kid", len(set.Keys))
	}
}

func TestJWKSPublishesAnUnlabelledEntryForTemplatesWithoutAKeyID(t *testing.T) {
	keyPEM, _ := testKeyPEM(t)
	catalog := `[
	  {"id":"erp","label":"ERP","env":"TOKEN_ERP","key_id":"erp-2026","claims":{"sub":"{{identity.id}}"}},
	  {"id":"wiki","label":"Wiki","env":"TOKEN_WIKI","claims":{"sub":"{{identity.id}}"}}
	]`
	iss, err := NewIssuer(catalog, keyPEM, "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}

	set, err := iss.JWKS()
	if err != nil {
		t.Fatalf("JWKS() error = %v", err)
	}
	if len(set.Keys) != 2 {
		t.Fatalf("JWKS() returned %d keys, want 2", len(set.Keys))
	}

	var found bool
	for _, k := range set.Keys {
		if k.Kid == "" {
			found = true
		}
	}
	if !found {
		t.Fatal("JWKS() has no kid-less entry; a token signed from a template without key_id carries no kid and would not match anything")
	}
}

// A verifier that decodes x/y into fixed-width buffers rejects a coordinate
// whose leading zero byte was trimmed. big.Int.Bytes() trims, so the encoder
// has to left-pad to the curve size.
func TestJWKSECCoordinatesArePaddedToTheCurveSize(t *testing.T) {
	keyPEM, pub := testKeyPEM(t)
	catalog := `[{"id":"erp","label":"ERP","env":"TOKEN_ERP","key_id":"k","claims":{"sub":"{{identity.id}}"}}]`
	iss, err := NewIssuer(catalog, keyPEM, "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}

	set, err := iss.JWKS()
	if err != nil {
		t.Fatalf("JWKS() error = %v", err)
	}
	if len(set.Keys) != 1 {
		t.Fatalf("JWKS() returned %d keys, want 1", len(set.Keys))
	}

	size := (pub.Curve.Params().BitSize + 7) / 8
	for name, encoded := range map[string]string{"x": set.Keys[0].X, "y": set.Keys[0].Y} {
		raw, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			t.Fatalf("%s is not base64url: %v", name, err)
		}
		if len(raw) != size {
			t.Errorf("%s decoded to %d bytes, want %d (the P-256 coordinate size)", name, len(raw), size)
		}
	}

	if got := new(big.Int).SetBytes(mustDecode(t, set.Keys[0].X)); got.Cmp(pub.X) != 0 {
		t.Errorf("x decodes to %v, want %v", got, pub.X)
	}
	if got := new(big.Int).SetBytes(mustDecode(t, set.Keys[0].Y)); got.Cmp(pub.Y) != 0 {
		t.Errorf("y decodes to %v, want %v", got, pub.Y)
	}
}

func TestJWKSPublishesRSAKeys(t *testing.T) {
	keyPEM := testRSAKeyPEM(t)
	catalog := `[{"id":"erp","label":"ERP","env":"TOKEN_ERP","algorithm":"RS256","key_id":"k","claims":{"sub":"{{identity.id}}"}}]`
	iss, err := NewIssuer(catalog, keyPEM, "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}

	set, err := iss.JWKS()
	if err != nil {
		t.Fatalf("JWKS() error = %v", err)
	}
	if len(set.Keys) != 1 {
		t.Fatalf("JWKS() returned %d keys, want 1", len(set.Keys))
	}
	k := set.Keys[0]
	if k.Kty != "RSA" {
		t.Errorf("Kty = %q, want RSA", k.Kty)
	}
	if k.N == "" || k.E == "" {
		t.Errorf("RSA entry is missing modulus or exponent: n=%q e=%q", k.N, k.E)
	}
	if k.Crv != "" || k.X != "" || k.Y != "" {
		t.Errorf("RSA entry carries EC fields: crv=%q x=%q y=%q", k.Crv, k.X, k.Y)
	}
}

// The point of the whole endpoint: a verifier that only ever sees the JWK Set
// can validate a token this issuer actually signed.
func TestTokenSignedByIssuerVerifiesAgainstPublishedJWKS(t *testing.T) {
	keyPEM, _ := testKeyPEM(t)
	catalog := `[{"id":"erp","label":"ERP","env":"TOKEN_ERP","key_id":"erp-2026","claims":{"sub":"{{identity.email_local}}"}}]`
	iss, err := NewIssuer(catalog, keyPEM, "")
	if err != nil {
		t.Fatalf("NewIssuer() error = %v", err)
	}

	env, _ := iss.Issue([]string{"erp"}, Context{Identity: Identity{Email: "alice@example.com"}}, time.Now())
	raw := env["TOKEN_ERP"]
	if raw == "" {
		t.Fatal("Issue() produced no token for TOKEN_ERP")
	}

	set, err := iss.JWKS()
	if err != nil {
		t.Fatalf("JWKS() error = %v", err)
	}

	parsed, err := jwt.Parse(raw, func(tok *jwt.Token) (any, error) {
		kid, _ := tok.Header["kid"].(string)
		return publicKeyFromJWKSet(t, set, kid), nil
	})
	if err != nil {
		t.Fatalf("verifying the issued token against the published JWKS failed: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("token parsed but is not valid")
	}
	claims := parsed.Claims.(jwt.MapClaims)
	if claims["sub"] != "alice" {
		t.Errorf("sub = %v, want alice", claims["sub"])
	}
}

func mustDecode(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return raw
}

// publicKeyFromJWKSet rebuilds a verifying key the way an external system
// would: look up the kid, read the public components, and never touch the
// issuer's own key material.
func publicKeyFromJWKSet(t *testing.T, set JWKSet, kid string) any {
	t.Helper()
	for _, k := range set.Keys {
		if k.Kid != kid {
			continue
		}
		if k.Kty != "EC" {
			t.Fatalf("unexpected kty %q", k.Kty)
		}
		return &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(mustDecode(t, k.X)),
			Y:     new(big.Int).SetBytes(mustDecode(t, k.Y)),
		}
	}
	t.Fatalf("no key in the set matches kid %q", kid)
	return nil
}
