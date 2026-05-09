package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOAuthRegistryFromEnvExposesConfiguredProviders(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "google-client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "google-secret")
	t.Setenv("LARK_OAUTH_CLIENT_ID", "cli_lark")
	t.Setenv("LARK_OAUTH_CLIENT_SECRET", "lark-secret")
	t.Setenv("LARK_OAUTH_BASE_URL", "https://open.larksuite.com")
	t.Setenv("LARK_OAUTH_LABEL", "Company Lark")
	t.Setenv("LARK_OAUTH_SCOPE", "contact:user.email:readonly")

	registry := NewOAuthProviderRegistryFromEnv(http.DefaultClient)
	configs := registry.PublicConfigs()

	if len(configs) != 2 {
		t.Fatalf("provider count: want 2, got %d: %#v", len(configs), configs)
	}

	google := configs[0]
	if google.ID != "google" || google.Label != "Google" || google.ClientID != "google-client" {
		t.Fatalf("google public config mismatch: %#v", google)
	}
	if google.AuthorizationURL != "https://accounts.google.com/o/oauth2/v2/auth" {
		t.Fatalf("google authorization URL: %q", google.AuthorizationURL)
	}
	if google.ExtraAuthParams["access_type"] != "offline" || google.ExtraAuthParams["prompt"] != "select_account" {
		t.Fatalf("google extra params missing: %#v", google.ExtraAuthParams)
	}

	lark := configs[1]
	if lark.ID != "feishu_lark" || lark.Label != "Company Lark" || lark.ClientID != "cli_lark" {
		t.Fatalf("lark public config mismatch: %#v", lark)
	}
	if lark.AuthorizationURL != "https://accounts.larksuite.com/open-apis/authen/v1/authorize" {
		t.Fatalf("lark authorization URL: %q", lark.AuthorizationURL)
	}
	if !lark.PKCE {
		t.Fatalf("lark public config should require PKCE")
	}
	if lark.Scope != "contact:user.email:readonly" {
		t.Fatalf("lark scope: %q", lark.Scope)
	}

	raw, err := json.Marshal(configs)
	if err != nil {
		t.Fatalf("marshal configs: %v", err)
	}
	if string(raw) == "" || containsSensitiveOAuthValue(string(raw), "google-secret", "lark-secret") {
		t.Fatalf("public config leaked a client secret: %s", raw)
	}
}

func TestLarkOAuthProviderExchangeUsesConfiguredEndpoints(t *testing.T) {
	var sawTokenRequest bool
	var sawUserInfoRequest bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/authen/v2/oauth/token":
			sawTokenRequest = true
			if r.Method != http.MethodPost {
				t.Fatalf("token method: want POST, got %s", r.Method)
			}
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode token request: %v", err)
			}
			want := map[string]string{
				"grant_type":    "authorization_code",
				"client_id":     "cli_lark",
				"client_secret": "lark-secret",
				"code":          "auth-code",
				"redirect_uri":  "http://localhost:3000/auth/callback",
				"code_verifier": "pkce-verifier",
			}
			for key, value := range want {
				if body[key] != value {
					t.Fatalf("token request %s: want %q, got %q in %#v", key, value, body[key], body)
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"user-token","token_type":"Bearer","expires_in":7200}`))
		case "/open-apis/authen/v1/user_info":
			sawUserInfoRequest = true
			if got := r.Header.Get("Authorization"); got != "Bearer user-token" {
				t.Fatalf("userinfo authorization header: %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"code": 0,
				"msg": "success",
				"data": {
					"name": "Ada Lovelace",
					"avatar_url": "https://avatar.example/ada.png",
					"email": "ada.personal@example.com",
					"enterprise_email": "ada@company.example"
				}
			}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	t.Setenv("LARK_OAUTH_CLIENT_ID", "cli_lark")
	t.Setenv("LARK_OAUTH_CLIENT_SECRET", "lark-secret")
	t.Setenv("LARK_OAUTH_BASE_URL", server.URL)

	registry := NewOAuthProviderRegistryFromEnv(server.Client())
	provider, ok := registry.Get("feishu_lark")
	if !ok {
		t.Fatalf("feishu_lark provider should be configured")
	}

	identity, err := provider.Exchange(context.Background(), OAuthExchangeRequest{
		Code:         "auth-code",
		RedirectURI:  "http://localhost:3000/auth/callback",
		CodeVerifier: "pkce-verifier",
	})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if !sawTokenRequest || !sawUserInfoRequest {
		t.Fatalf("exchange did not call all expected endpoints: token=%v userinfo=%v", sawTokenRequest, sawUserInfoRequest)
	}
	if identity.Email != "ada@company.example" {
		t.Fatalf("identity email: want enterprise email, got %q", identity.Email)
	}
	if identity.Name != "Ada Lovelace" {
		t.Fatalf("identity name: %q", identity.Name)
	}
	if identity.AvatarURL != "https://avatar.example/ada.png" {
		t.Fatalf("identity avatar: %q", identity.AvatarURL)
	}
}

func containsSensitiveOAuthValue(haystack string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}
