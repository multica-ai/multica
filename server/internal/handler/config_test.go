package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
)

func TestGetConfigIncludesRuntimeAuthConfig(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	origProviders := testHandler.OAuthProviders
	testHandler.OAuthProviders = map[string]auth.OAuthProvider{
		"google": auth.NewHTTPOAuthProvider(auth.GoogleSpec),
	}
	defer func() { testHandler.OAuthProviders = origProviders }()

	t.Setenv("ALLOW_SIGNUP", "false")
	t.Setenv("GOOGLE_CLIENT_ID", "google-client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "google-secret")
	t.Setenv("GOOGLE_REDIRECT_URI", "http://localhost:3000/auth/callback")
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
	google, ok := cfg.OAuthProviders["google"]
	if !ok {
		t.Fatalf("oauth_providers[google]: missing")
	}
	if google.ClientID != "google-client-id" {
		t.Fatalf("oauth_providers[google].client_id: want google-client-id, got %q", google.ClientID)
	}
	if google.AuthorizeURL != "https://accounts.google.com/o/oauth2/v2/auth" {
		t.Fatalf("oauth_providers[google].authorize_url: unexpected %q", google.AuthorizeURL)
	}
	if google.CallbackPath != "/auth/callback" {
		t.Fatalf("oauth_providers[google].callback_path: unexpected %q", google.CallbackPath)
	}
	if google.ExtraAuthParams["access_type"] != "offline" {
		t.Fatalf("oauth_providers[google].extra_auth_params[access_type]: unexpected %q", google.ExtraAuthParams["access_type"])
	}
	if cfg.PosthogKey != "phc_test" {
		t.Fatalf("posthog_key: want phc_test, got %q", cfg.PosthogKey)
	}
	if cfg.PosthogHost != "https://eu.i.posthog.com" {
		t.Fatalf("posthog_host: want https://eu.i.posthog.com, got %q", cfg.PosthogHost)
	}
}

func TestGetConfigDerivesCallbackPathFromRedirectURIEnv(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	origProviders := testHandler.OAuthProviders
	testHandler.OAuthProviders = map[string]auth.OAuthProvider{
		"google": auth.NewHTTPOAuthProvider(auth.GoogleSpec),
	}
	defer func() { testHandler.OAuthProviders = origProviders }()

	t.Setenv("GOOGLE_CLIENT_ID", "x")
	t.Setenv("GOOGLE_CLIENT_SECRET", "y")
	t.Setenv("GOOGLE_REDIRECT_URI", "https://app.example.com/api/auth/callback/google")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	google := cfg.OAuthProviders["google"]
	if google.CallbackPath != "/api/auth/callback/google" {
		t.Fatalf("callback_path: want /api/auth/callback/google, got %q", google.CallbackPath)
	}
}

func TestGetConfigOmitsProviderWhenRedirectURIMissing(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	origProviders := testHandler.OAuthProviders
	testHandler.OAuthProviders = map[string]auth.OAuthProvider{
		"google": auth.NewHTTPOAuthProvider(auth.GoogleSpec),
	}
	defer func() { testHandler.OAuthProviders = origProviders }()

	t.Setenv("GOOGLE_CLIENT_ID", "x")
	t.Setenv("GOOGLE_CLIENT_SECRET", "y")
	t.Setenv("GOOGLE_REDIRECT_URI", "")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	w := httptest.NewRecorder()
	testHandler.GetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var cfg AppConfig
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := cfg.OAuthProviders["google"]; ok {
		t.Fatal("expected google to be omitted when REDIRECT_URI env is unset")
	}
}
