package handler

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
)

// setupFakeGoogle stands up a fake OIDC issuer with discovery + JWKs and a
// fake token endpoint. Tests sign synthetic ID tokens with the issuer's
// private key. The handler's googleTokenURL is pointed at the fake token
// endpoint so the handler never reaches the real Google.
type fakeGoogle struct {
	issuer   string
	priv     *rsa.PrivateKey
	tokenURL string
	stop     func()
}

func setupFakeGoogle(t *testing.T) *fakeGoogle {
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
			"token_endpoint":                        s.URL + "/token-discovery",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{
			Keys: []jose.JSONWebKey{{Key: &priv.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}},
		})
	})
	s = httptest.NewServer(mux)

	return &fakeGoogle{issuer: s.URL, priv: priv, stop: s.Close}
}

func (f *fakeGoogle) startTokenEndpoint(t *testing.T, idToken string) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "x", "id_token": idToken, "token_type": "Bearer",
		})
	}))
	prevStop := f.stop
	f.tokenURL = ts.URL
	f.stop = func() { ts.Close(); prevStop() }
}

func (f *fakeGoogle) signIDToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "k1"
	s, err := tok.SignedString(f.priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// runGoogleLogin builds a POST request and runs h.GoogleLogin.
func runGoogleLogin(t *testing.T, h *Handler) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{
		"code": "abc", "redirect_uri": "http://localhost/cb",
	})
	req := httptest.NewRequest("POST", "/auth/google", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	h.GoogleLogin(rec, req)
	return rec
}

func TestGoogleLogin_NotConfiguredWhenVerifierNil(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")
	h := newTestHandler(Config{AllowSignup: true})
	h.GoogleVerifier = nil

	rec := runGoogleLogin(t, h)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGoogleLogin_RejectsWrongAudience(t *testing.T) {
	g := setupFakeGoogle(t)
	v, err := auth.NewGoogleVerifierForIssuer(context.Background(), g.issuer, "client-1")
	if err != nil {
		t.Fatal(err)
	}
	rawID := g.signIDToken(t, jwt.MapClaims{
		"iss": g.issuer, "aud": "attacker", "sub": "x",
		"email": "a@b.com", "email_verified": true,
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	g.startTokenEndpoint(t, rawID)
	defer g.stop()

	t.Setenv("GOOGLE_CLIENT_ID", "client-1")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	h := newTestHandler(Config{AllowSignup: true})
	h.GoogleVerifier = v
	h.googleTokenURL = g.tokenURL
	h.Analytics = analytics.NoopClient{}

	rec := runGoogleLogin(t, h)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid Google identity") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestGoogleLogin_RejectsExpiredToken(t *testing.T) {
	g := setupFakeGoogle(t)
	v, err := auth.NewGoogleVerifierForIssuer(context.Background(), g.issuer, "client-1")
	if err != nil {
		t.Fatal(err)
	}
	rawID := g.signIDToken(t, jwt.MapClaims{
		"iss": g.issuer, "aud": "client-1", "sub": "x",
		"email": "a@b.com", "email_verified": true,
		"exp": time.Now().Add(-time.Minute).Unix(),
	})
	g.startTokenEndpoint(t, rawID)
	defer g.stop()

	t.Setenv("GOOGLE_CLIENT_ID", "client-1")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	h := newTestHandler(Config{AllowSignup: true})
	h.GoogleVerifier = v
	h.googleTokenURL = g.tokenURL
	h.Analytics = analytics.NoopClient{}

	rec := runGoogleLogin(t, h)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// The unverified-email check lives inside findOrCreateUserByGoogle and bails
// before any DB call. So no Queries mock is required: we only need a verifier
// that lets the token through (email_verified=false is a valid claim, just
// one we refuse to honor).
func TestGoogleLogin_RejectsUnverifiedEmail(t *testing.T) {
	g := setupFakeGoogle(t)
	v, err := auth.NewGoogleVerifierForIssuer(context.Background(), g.issuer, "client-1")
	if err != nil {
		t.Fatal(err)
	}
	rawID := g.signIDToken(t, jwt.MapClaims{
		"iss": g.issuer, "aud": "client-1", "sub": "x",
		"email": "victim@example.com", "email_verified": false,
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	g.startTokenEndpoint(t, rawID)
	defer g.stop()

	t.Setenv("GOOGLE_CLIENT_ID", "client-1")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	h := newTestHandler(Config{AllowSignup: true})
	h.GoogleVerifier = v
	h.googleTokenURL = g.tokenURL
	h.Analytics = analytics.NoopClient{}
	// Queries unset — the email_verified check fires before any query.

	rec := runGoogleLogin(t, h)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "failed to sign in") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

// Smoke test for the happy path: token verifies, identity is verified,
// findOrCreateUserByGoogle reaches the DB. Uses the existing simple mockDB
// that returns one error for any Scan — set to ErrNoRows so GetUserByGoogleID
// and GetUserByEmail both miss, then CreateUser's Scan also returns ErrNoRows
// which surfaces as a 500. We assert that the handler reached that point
// (verifier accepted, no auth-layer error). A real DB-backed test will
// exercise the full create path.
func TestGoogleLogin_VerifiedTokenReachesDB(t *testing.T) {
	g := setupFakeGoogle(t)
	v, err := auth.NewGoogleVerifierForIssuer(context.Background(), g.issuer, "client-1")
	if err != nil {
		t.Fatal(err)
	}
	rawID := g.signIDToken(t, jwt.MapClaims{
		"iss": g.issuer, "aud": "client-1", "sub": "google-sub-new",
		"email": "new@example.com", "email_verified": true, "name": "New",
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	g.startTokenEndpoint(t, rawID)
	defer g.stop()

	t.Setenv("GOOGLE_CLIENT_ID", "client-1")
	t.Setenv("GOOGLE_CLIENT_SECRET", "secret")
	h := newTestHandler(Config{AllowSignup: true})
	h.GoogleVerifier = v
	h.googleTokenURL = g.tokenURL
	h.Analytics = analytics.NoopClient{}
	h.Queries = db.New(&mockDB{getUserErr: pgx.ErrNoRows})

	rec := runGoogleLogin(t, h)
	// All three queries (GetUserByGoogleID, GetUserByEmail, CreateUser) return
	// ErrNoRows. The first two miss as expected; CreateUser returning ErrNoRows
	// is treated as a DB error and surfaces as 500. The point of this test is
	// that we got far enough to call CreateUser at all — meaning verification
	// passed. A 401/403/503 here would indicate the new code path broke.
	if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden || rec.Code == http.StatusServiceUnavailable {
		t.Fatalf("expected to reach DB layer, got %d body=%s", rec.Code, rec.Body.String())
	}
}
