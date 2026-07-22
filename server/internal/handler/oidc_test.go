package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
)

func TestStartOIDCLoginUsesDiscoveryStateNonceAndPKCE(t *testing.T) {
	var issuer string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/authorize",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/jwks",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	defer provider.Close()
	issuer = provider.URL

	t.Setenv("OIDC_ISSUER_URL", issuer)
	t.Setenv("OIDC_CLIENT_ID", "test-client")
	t.Setenv("OIDC_CLIENT_SECRET", "test-secret")
	t.Setenv("OIDC_REDIRECT_URI", "http://localhost:3000/auth/callback")

	req := httptest.NewRequest(http.MethodPost, "/auth/oidc/start", strings.NewReader(`{"app_state":"next:/invite/123"}`))
	w := httptest.NewRecorder()
	testHandler.StartOIDCLogin(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartOIDCLogin: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response oidcStartResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	authorizationURL, err := url.Parse(response.AuthorizationURL)
	if err != nil {
		t.Fatalf("parse authorization URL: %v", err)
	}
	query := authorizationURL.Query()
	if authorizationURL.String() == "" || query.Get("client_id") != "test-client" || query.Get("response_type") != "code" {
		t.Fatalf("unexpected authorization URL: %s", response.AuthorizationURL)
	}
	if !strings.HasPrefix(query.Get("state"), "oidc.") || query.Get("nonce") == "" {
		t.Fatalf("authorization URL is missing OIDC state or nonce: %s", response.AuthorizationURL)
	}
	if query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization URL is missing PKCE S256 challenge: %s", response.AuthorizationURL)
	}

	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != auth.OIDCFlowCookieName {
		t.Fatalf("OIDC flow cookie missing: %v", cookies)
	}
	flowRequest := httptest.NewRequest(http.MethodPost, "/auth/oidc", nil)
	flowRequest.AddCookie(cookies[0])
	flow, err := auth.ReadOIDCFlowCookie(flowRequest)
	if err != nil {
		t.Fatalf("read OIDC flow cookie: %v", err)
	}
	if flow.State != query.Get("state") || flow.AppState != "next:/invite/123" {
		t.Fatalf("flow does not match authorization URL: %s", fmt.Sprint(flow))
	}
}

func TestOIDCConfigRequiresCompleteConfiguration(t *testing.T) {
	t.Setenv("OIDC_ISSUER_URL", "https://auth.example.com")
	t.Setenv("OIDC_CLIENT_ID", "client")
	t.Setenv("OIDC_CLIENT_SECRET", "")
	t.Setenv("OIDC_REDIRECT_URI", "https://app.example.com/auth/callback")
	if _, err := loadOIDCRuntimeConfig(); err == nil {
		t.Fatal("loadOIDCRuntimeConfig accepted a missing client secret")
	}
}

func TestOIDCLoginRejectsStateMismatchBeforeTokenExchange(t *testing.T) {
	t.Setenv("OIDC_ISSUER_URL", "https://auth.example.com")
	t.Setenv("OIDC_CLIENT_ID", "client")
	t.Setenv("OIDC_CLIENT_SECRET", "secret")
	t.Setenv("OIDC_REDIRECT_URI", "https://app.example.com/auth/callback")
	flow, err := auth.NewOIDCFlow("", "pkce-verifier")
	if err != nil {
		t.Fatalf("NewOIDCFlow: %v", err)
	}
	cookieRecorder := httptest.NewRecorder()
	if err := auth.SetOIDCFlowCookie(cookieRecorder, flow); err != nil {
		t.Fatalf("SetOIDCFlowCookie: %v", err)
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/auth/oidc",
		strings.NewReader(`{"code":"code","state":"oidc.attacker-state"}`),
	)
	req.AddCookie(cookieRecorder.Result().Cookies()[0])
	w := httptest.NewRecorder()
	testHandler.OIDCLogin(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("OIDCLogin: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestValidOIDCURL(t *testing.T) {
	tests := map[string]bool{
		"https://auth.example.com/application/o/multica": true,
		"http://localhost:9000/application/o/multica":    true,
		"http://127.0.0.1:9000":                          true,
		"http://[::1]:9000":                              true,
		"http://auth.example.com":                        false,
		"file://localhost/tmp/provider":                  false,
	}
	for raw, want := range tests {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if got := validOIDCURL(parsed); got != want {
			t.Errorf("validOIDCURL(%q): got %v, want %v", raw, got, want)
		}
	}
}
