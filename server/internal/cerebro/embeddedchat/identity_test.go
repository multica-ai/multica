package embeddedchat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyUsesAuthoritativeSupabaseUserEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("apikey") != "finance-publishable-key" {
			http.Error(w, "missing api key", http.StatusUnauthorized)
			return
		}
		switch r.Header.Get("Authorization") {
		case "Bearer valid-google-token":
			_, _ = w.Write([]byte(`{"id":"supabase-user-1","email":"Jesper@Firtal.com","app_metadata":{"provider":"google"}}`))
		case "Bearer valid-github-token":
			_, _ = w.Write([]byte(`{"id":"supabase-user-2","email":"jesper@firtal.com","app_metadata":{"provider":"github"}}`))
		default:
			http.Error(w, "invalid token", http.StatusUnauthorized)
		}
	}))
	defer server.Close()

	verifier := NewVerifier(map[string]Provider{"finance": {
		UserInfoURL: server.URL, APIKey: "finance-publishable-key", Provider: "google",
	}})
	identity, err := verifier.Verify(context.Background(), "finance", "valid-google-token")
	if err != nil {
		t.Fatalf("verify Google identity: %v", err)
	}
	if identity.Email != "jesper@firtal.com" {
		t.Fatalf("email = %q", identity.Email)
	}
	if _, err := verifier.Verify(context.Background(), "finance", "valid-github-token"); err == nil {
		t.Fatal("non-Google identity must be rejected")
	}
	if _, err := verifier.Verify(context.Background(), "finance", "expired-token"); err == nil {
		t.Fatal("token rejected by Supabase must be rejected")
	}
	if _, err := verifier.Verify(context.Background(), "unknown", "valid-google-token"); err == nil {
		t.Fatal("unknown source must be rejected")
	}
}

func TestParseProvidersResolvesAPIKeyFromEnvironment(t *testing.T) {
	t.Setenv("FINANCE_SUPABASE_PUBLISHABLE_KEY", "publishable-key")
	providers, err := ParseProviders(`{"finance":{"user_info_url":"https://finance.supabase.co/auth/v1/user","api_key_env":"FINANCE_SUPABASE_PUBLISHABLE_KEY","required_provider":"google"}}`)
	if err != nil {
		t.Fatalf("parse provider: %v", err)
	}
	if providers["finance"].APIKey != "publishable-key" {
		t.Fatal("provider API key was not resolved")
	}
	if _, err := ParseProviders(`{"finance":{"user_info_url":"https://finance.supabase.co/auth/v1/user","api_key_env":"MISSING_KEY","required_provider":"google"}}`); err == nil {
		t.Fatal("missing provider API key must fail closed")
	}
}
