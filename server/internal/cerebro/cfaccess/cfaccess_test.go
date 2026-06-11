package cfaccess

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testTeam spins up a fake Cloudflare Access certs endpoint backed by a freshly
// generated RSA key, and returns a verifier pointed at it plus a signer.
type testTeam struct {
	key    *rsa.PrivateKey
	kid    string
	server *httptest.Server
}

func newTestTeam(t *testing.T) *testTeam {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tt := &testTeam{key: key, kid: "test-kid-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/cdn-cgi/access/certs", func(w http.ResponseWriter, r *http.Request) {
		n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
		e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[{"kty":"RSA","kid":"` + tt.kid + `","n":"` + n + `","e":"` + e + `"}]}`))
	})
	tt.server = httptest.NewServer(mux)
	t.Cleanup(tt.server.Close)
	return tt
}

// verifier returns a Verifier whose issuer + certURL point at the test server.
func (tt *testTeam) verifier(aud string, lookup EmailLookup) *Verifier {
	v := NewVerifier("firtal", []string{aud}, lookup)
	v.issuer = tt.server.URL
	v.certURL = tt.server.URL + "/cdn-cgi/access/certs"
	return v
}

func (tt *testTeam) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = tt.kid
	s, err := tok.SignedString(tt.key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func okLookup(id string) EmailLookup {
	return func(_ context.Context, _ string) (string, error) { return id, nil }
}

func failLookup() EmailLookup {
	return func(_ context.Context, _ string) (string, error) { return "", errors.New("no rows") }
}

func TestAuthenticate_ValidHumanStamp(t *testing.T) {
	tt := newTestTeam(t)
	v := tt.verifier("aud-123", okLookup("user-uuid-1"))
	token := tt.sign(t, jwt.MapClaims{
		"iss":   tt.server.URL,
		"aud":   []string{"aud-123"},
		"email": "Jeh@Firtal.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	id, err := v.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	if id.UserID != "user-uuid-1" {
		t.Errorf("UserID = %q, want user-uuid-1", id.UserID)
	}
	if id.Email != "jeh@firtal.com" {
		t.Errorf("Email = %q, want lowercased jeh@firtal.com", id.Email)
	}
}

func TestAuthenticate_AudAsString(t *testing.T) {
	tt := newTestTeam(t)
	v := tt.verifier("aud-123", okLookup("u1"))
	token := tt.sign(t, jwt.MapClaims{
		"iss":   tt.server.URL,
		"aud":   "aud-123", // single string, not array
		"email": "a@b.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	if _, err := v.Authenticate(context.Background(), token); err != nil {
		t.Fatalf("string aud should verify: %v", err)
	}
}

func TestAuthenticate_WrongAudRejected(t *testing.T) {
	tt := newTestTeam(t)
	v := tt.verifier("aud-123", okLookup("u1"))
	token := tt.sign(t, jwt.MapClaims{
		"iss":   tt.server.URL,
		"aud":   []string{"some-other-aud"},
		"email": "a@b.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	_, err := v.Authenticate(context.Background(), token)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong aud should be ErrInvalid, got %v", err)
	}
}

func TestAuthenticate_WrongIssuerRejected(t *testing.T) {
	tt := newTestTeam(t)
	v := tt.verifier("aud-123", okLookup("u1"))
	token := tt.sign(t, jwt.MapClaims{
		"iss":   "https://evil.cloudflareaccess.com",
		"aud":   []string{"aud-123"},
		"email": "a@b.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	if _, err := v.Authenticate(context.Background(), token); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong issuer should be ErrInvalid, got %v", err)
	}
}

func TestAuthenticate_ExpiredRejected(t *testing.T) {
	tt := newTestTeam(t)
	v := tt.verifier("aud-123", okLookup("u1"))
	token := tt.sign(t, jwt.MapClaims{
		"iss":   tt.server.URL,
		"aud":   []string{"aud-123"},
		"email": "a@b.com",
		"exp":   time.Now().Add(-time.Minute).Unix(),
	})
	if _, err := v.Authenticate(context.Background(), token); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expired should be ErrInvalid, got %v", err)
	}
}

func TestAuthenticate_WrongSignerRejected(t *testing.T) {
	tt := newTestTeam(t)
	v := tt.verifier("aud-123", okLookup("u1"))
	// Sign with a different key than the one the JWKS publishes.
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   tt.server.URL,
		"aud":   []string{"aud-123"},
		"email": "a@b.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = tt.kid
	signed, _ := tok.SignedString(otherKey)
	if _, err := v.Authenticate(context.Background(), signed); !errors.Is(err, ErrInvalid) {
		t.Fatalf("forged signature should be ErrInvalid, got %v", err)
	}
}

func TestAuthenticate_NoneAlgRejected(t *testing.T) {
	tt := newTestTeam(t)
	v := tt.verifier("aud-123", okLookup("u1"))
	tok := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"iss":   tt.server.URL,
		"aud":   []string{"aud-123"},
		"email": "a@b.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = tt.kid
	signed, _ := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, err := v.Authenticate(context.Background(), signed); !errors.Is(err, ErrInvalid) {
		t.Fatalf("alg=none should be ErrInvalid, got %v", err)
	}
}

func TestAuthenticate_UnknownUser(t *testing.T) {
	tt := newTestTeam(t)
	v := tt.verifier("aud-123", failLookup())
	token := tt.sign(t, jwt.MapClaims{
		"iss":   tt.server.URL,
		"aud":   []string{"aud-123"},
		"email": "stranger@firtal.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	id, err := v.Authenticate(context.Background(), token)
	if !errors.Is(err, ErrNoUser) {
		t.Fatalf("unknown user should be ErrNoUser, got %v", err)
	}
	if id.UserID != "" {
		t.Errorf("UserID should be empty on ErrNoUser, got %q", id.UserID)
	}
}

func TestAuthenticate_ServiceTokenHasNoUser(t *testing.T) {
	tt := newTestTeam(t)
	v := tt.verifier("aud-123", okLookup("should-not-be-used"))
	// Service-token stamp: common_name, no email.
	token := tt.sign(t, jwt.MapClaims{
		"iss":         tt.server.URL,
		"aud":         []string{"aud-123"},
		"common_name": "machine-laptop-01.access",
		"exp":         time.Now().Add(time.Hour).Unix(),
	})
	id, err := v.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatalf("service token should verify: %v", err)
	}
	if id.UserID != "" {
		t.Errorf("service token must not map to a user, got %q", id.UserID)
	}
	if id.CommonName != "machine-laptop-01.access" {
		t.Errorf("CommonName = %q", id.CommonName)
	}
}

func TestAuthenticatedUser_FallThroughCases(t *testing.T) {
	tt := newTestTeam(t)
	v := tt.verifier("aud-123", okLookup("u1"))

	// nil verifier → not ok, never panics.
	if _, ok := AuthenticatedUser(context.Background(), nil, httptest.NewRequest("GET", "/", nil)); ok {
		t.Error("nil verifier must return ok=false")
	}

	// no header → not ok.
	if _, ok := AuthenticatedUser(context.Background(), v, httptest.NewRequest("GET", "/", nil)); ok {
		t.Error("missing assertion must return ok=false")
	}

	// valid header → ok with mapped id.
	token := tt.sign(t, jwt.MapClaims{
		"iss":   tt.server.URL,
		"aud":   []string{"aud-123"},
		"email": "a@b.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set(HeaderJWTAssertion, token)
	uid, ok := AuthenticatedUser(context.Background(), v, r)
	if !ok || uid != "u1" {
		t.Errorf("valid stamp: ok=%v uid=%q, want true u1", ok, uid)
	}
}

func TestAuthenticateRequest_NoAssertion(t *testing.T) {
	tt := newTestTeam(t)
	v := tt.verifier("aud-123", okLookup("u1"))
	_, err := v.AuthenticateRequest(context.Background(), httptest.NewRequest("GET", "/", nil))
	if !errors.Is(err, ErrNoAssertion) {
		t.Fatalf("want ErrNoAssertion, got %v", err)
	}
}

func TestNewVerifierFromEnv_InertWhenUnset(t *testing.T) {
	t.Setenv(EnvTeamDomain, "")
	t.Setenv(EnvAUD, "")
	if v := NewVerifierFromEnv(okLookup("u1")); v != nil {
		t.Fatal("verifier must be nil when env unset (feature off)")
	}
	t.Setenv(EnvTeamDomain, "firtal")
	if v := NewVerifierFromEnv(okLookup("u1")); v != nil {
		t.Fatal("verifier must be nil when AUD still unset")
	}
	t.Setenv(EnvAUD, "aud-1,aud-2")
	v := NewVerifierFromEnv(okLookup("u1"))
	if v == nil {
		t.Fatal("verifier must be built when both set")
	}
	if _, ok := v.auds["aud-1"]; !ok {
		t.Error("aud-1 not parsed")
	}
	if _, ok := v.auds["aud-2"]; !ok {
		t.Error("aud-2 not parsed")
	}
}

func TestServiceToken(t *testing.T) {
	if (ServiceToken{}).Configured() {
		t.Error("empty token must not be configured")
	}
	if (ServiceToken{ClientID: "id"}).Configured() {
		t.Error("half token must not be configured")
	}
	tok := ServiceToken{ClientID: "id", ClientSecret: "secret"}
	if !tok.Configured() {
		t.Fatal("full token must be configured")
	}
	h := http.Header{}
	tok.Apply(h)
	if h.Get(HeaderClientID) != "id" || h.Get(HeaderClientSecret) != "secret" {
		t.Errorf("Apply did not set headers: %v", h)
	}
	// Unconfigured Apply is a no-op.
	h2 := http.Header{}
	(ServiceToken{}).Apply(h2)
	if len(h2) != 0 {
		t.Errorf("unconfigured Apply must be a no-op, got %v", h2)
	}
}

func TestServiceTokenFromEnv(t *testing.T) {
	t.Setenv(EnvClientID, "cid")
	t.Setenv(EnvClientSecret, "csecret")
	tok := ServiceTokenFromEnv()
	if !tok.Configured() || tok.ClientID != "cid" || tok.ClientSecret != "csecret" {
		t.Errorf("ServiceTokenFromEnv = %+v", tok)
	}
}

func TestNormalizeTeamHost(t *testing.T) {
	cases := map[string]string{
		"firtal":                               "firtal.cloudflareaccess.com",
		"firtal.cloudflareaccess.com":          "firtal.cloudflareaccess.com",
		"https://firtal.cloudflareaccess.com/": "firtal.cloudflareaccess.com",
	}
	for in, want := range cases {
		if got := normalizeTeamHost(in); got != want {
			t.Errorf("normalizeTeamHost(%q) = %q, want %q", in, got, want)
		}
	}
}
