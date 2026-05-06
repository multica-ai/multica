package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExchangeGoogleCodeUsesConfiguredTokenURL(t *testing.T) {
	var sawRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("code"); got != "code-123" {
			t.Fatalf("expected code %q, got %q", "code-123", got)
		}
		if got := r.Form.Get("client_id"); got != "client-id" {
			t.Fatalf("expected client_id %q, got %q", "client-id", got)
		}
		if got := r.Form.Get("client_secret"); got != "client-secret" {
			t.Fatalf("expected client_secret %q, got %q", "client-secret", got)
		}
		if got := r.Form.Get("redirect_uri"); got != "https://app.test/auth/callback" {
			t.Fatalf("expected redirect_uri %q, got %q", "https://app.test/auth/callback", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"access_token": "access-token-123",
			"id_token":     "id-token-123",
			"token_type":   "Bearer",
		})
	}))
	defer server.Close()
	t.Setenv("GOOGLE_TOKEN_URL", server.URL)

	token, err := exchangeGoogleCode(t.Context(), "code-123", "client-id", "client-secret", "https://app.test/auth/callback")
	if err != nil {
		t.Fatalf("exchange code: %v", err)
	}
	if !sawRequest {
		t.Fatal("expected token server to receive request")
	}
	if token.AccessToken != "access-token-123" {
		t.Fatalf("expected access token, got %q", token.AccessToken)
	}
}

func TestFetchGoogleUserInfoUsesConfiguredUserInfoURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-token-123" {
			t.Fatalf("expected bearer token, got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":      "google-user-id",
			"email":   "user@example.com",
			"name":    "Google User",
			"picture": "https://example.com/avatar.png",
		})
	}))
	defer server.Close()
	t.Setenv("GOOGLE_USERINFO_URL", server.URL)

	user, err := fetchGoogleUserInfo(t.Context(), "access-token-123")
	if err != nil {
		t.Fatalf("fetch userinfo: %v", err)
	}
	if user.ID != "google-user-id" || user.Email != "user@example.com" || !strings.Contains(user.Picture, "avatar") {
		t.Fatalf("unexpected userinfo: %+v", user)
	}
}
