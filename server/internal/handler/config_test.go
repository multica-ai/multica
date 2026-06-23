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
	t.Setenv("SERVER_URL", "https://api.example.com")
	t.Setenv("POSTHOG_API_KEY", "phc_test")
	t.Setenv("POSTHOG_HOST", "https://eu.i.posthog.com")
	t.Setenv("APP_ENV", "development")

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
	if cfg.ServerURL != "https://api.example.com" {
		t.Fatalf("server_url: want https://api.example.com, got %q", cfg.ServerURL)
	}
	if cfg.PosthogKey != "phc_test" {
		t.Fatalf("posthog_key: want phc_test, got %q", cfg.PosthogKey)
	}
	if cfg.PosthogHost != "https://eu.i.posthog.com" {
		t.Fatalf("posthog_host: want https://eu.i.posthog.com, got %q", cfg.PosthogHost)
	}
	if cfg.AnalyticsEnvironment != "dev" {
		t.Fatalf("analytics_environment: want dev, got %q", cfg.AnalyticsEnvironment)
	}
	if cfg.AppEnv != "development" {
		t.Fatalf("app_env: want development, got %q", cfg.AppEnv)
	}
}

func TestGetConfigCliServerURL(t *testing.T) {
	origStorage := testHandler.Storage
	testHandler.Storage = &mockStorage{}
	defer func() { testHandler.Storage = origStorage }()

	t.Run("explicit CLI_SERVER_URL", func(t *testing.T) {
		t.Setenv("SERVER_URL", "https://api.example.com")
		t.Setenv("CLI_SERVER_URL", "http://localhost:9999")
		t.Setenv("ANALYTICS_DISABLED", "true")

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

		if cfg.ServerURL != "https://api.example.com" {
			t.Fatalf("server_url: want https://api.example.com, got %q", cfg.ServerURL)
		}
		if cfg.CliServerURL != "http://localhost:9999" {
			t.Fatalf("cli_server_url: want http://localhost:9999, got %q", cfg.CliServerURL)
		}
	})

	t.Run("fallback to SERVER_URL", func(t *testing.T) {
		t.Setenv("SERVER_URL", "https://api.example.com")
		t.Setenv("CLI_SERVER_URL", "")
		t.Setenv("ANALYTICS_DISABLED", "true")

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

		if cfg.ServerURL != "https://api.example.com" {
			t.Fatalf("server_url: want https://api.example.com, got %q", cfg.ServerURL)
		}
		if cfg.CliServerURL != "https://api.example.com" {
			t.Fatalf("cli_server_url: want https://api.example.com, got %q", cfg.CliServerURL)
		}
	})
}
