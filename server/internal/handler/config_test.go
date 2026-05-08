package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetConfigIncludesRuntimeAuthConfig(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	t.Setenv("ALLOW_SIGNUP", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "google-client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "google-client-secret")
	t.Setenv("LARK_OAUTH_CLIENT_ID", "")
	t.Setenv("LARK_OAUTH_CLIENT_SECRET", "")
	t.Setenv("LARK_APP_ID", "")
	t.Setenv("LARK_APP_SECRET", "")
	t.Setenv("POSTHOG_API_KEY", "phc_test")
	t.Setenv("POSTHOG_HOST", "https://eu.i.posthog.com")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}

	if cfg.CdnDomain != "cdn.example.com" {
		t.Fatalf("cdn_domain: want cdn.example.com, got %q", cfg.CdnDomain)
	}
	if cfg.AllowSignup {
		t.Fatalf("allow_signup: want false, got true")
	}
	if cfg.GoogleClientID != "google-client-id" {
		t.Fatalf("google_client_id: want google-client-id, got %q", cfg.GoogleClientID)
	}
	if len(cfg.OAuthProviders) != 1 {
		t.Fatalf("oauth_providers: want 1 Google provider, got %#v", cfg.OAuthProviders)
	}
	if cfg.OAuthProviders[0].ID != "google" || cfg.OAuthProviders[0].ClientID != "google-client-id" {
		t.Fatalf("oauth_providers[0]: unexpected provider config %#v", cfg.OAuthProviders[0])
	}
	if cfg.PosthogKey != "phc_test" {
		t.Fatalf("posthog_key: want phc_test, got %q", cfg.PosthogKey)
	}
	if cfg.PosthogHost != "https://eu.i.posthog.com" {
		t.Fatalf("posthog_host: want https://eu.i.posthog.com, got %q", cfg.PosthogHost)
	}
}

func TestGetConfigIncludesFeishuLarkOAuthProvider(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "")
	t.Setenv("GOOGLE_CLIENT_SECRET", "")
	t.Setenv("LARK_OAUTH_CLIENT_ID", "cli_lark")
	t.Setenv("LARK_OAUTH_CLIENT_SECRET", "super-secret")
	t.Setenv("LARK_OAUTH_BASE_URL", "https://open.larksuite.com")
	t.Setenv("LARK_OAUTH_LABEL", "Company Lark")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()

	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetConfig: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw config: %v", err)
	}
	if _, ok := raw["lark_oauth_client_secret"]; ok {
		t.Fatalf("config leaked lark secret: %s", w.Body.String())
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if len(cfg.OAuthProviders) != 1 {
		t.Fatalf("oauth_providers: want 1 Feishu/Lark provider, got %#v", cfg.OAuthProviders)
	}
	provider := cfg.OAuthProviders[0]
	if provider.ID != "feishu_lark" {
		t.Fatalf("provider id: %q", provider.ID)
	}
	if provider.Label != "Company Lark" {
		t.Fatalf("provider label: %q", provider.Label)
	}
	if provider.ClientID != "cli_lark" {
		t.Fatalf("provider client_id: %q", provider.ClientID)
	}
	if provider.AuthorizationURL != "https://accounts.larksuite.com/open-apis/authen/v1/authorize" {
		t.Fatalf("provider authorization_url: %q", provider.AuthorizationURL)
	}
	if !provider.PKCE {
		t.Fatalf("provider should require PKCE")
	}
}
