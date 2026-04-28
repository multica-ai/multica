package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	notifyutil "github.com/multica-ai/multica/server/internal/notify"
)

func TestStartMyGoogleBindingUsesRequestedCallbackRedirect(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "google-client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "google-client-secret")
	t.Setenv("MULTICA_APP_URL", "https://app.multica.test")

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/me/notification-bindings/google/start", map[string]any{
		"next_path":    "/handler-tests/settings",
		"redirect_uri": "http://localhost:3000/auth/callback",
	})

	testHandler.StartMyGoogleBinding(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp StartGoogleBindingResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	parsed, err := url.Parse(resp.AuthURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	query := parsed.Query()
	if query.Get("client_id") != "google-client-id" {
		t.Fatalf("expected client_id %q, got %q", "google-client-id", query.Get("client_id"))
	}
	if query.Get("redirect_uri") != "http://localhost:3000/auth/callback" {
		t.Fatalf("expected redirect_uri to use local auth callback, got %q", query.Get("redirect_uri"))
	}

	state, err := notifyutil.ParseGoogleBindingState(query.Get("state"))
	if err != nil {
		t.Fatalf("parse Google state: %v", err)
	}
	if state.UserID != testUserID {
		t.Fatalf("expected state user_id %q, got %q", testUserID, state.UserID)
	}
	if state.NextPath != "/handler-tests/settings" {
		t.Fatalf("expected state next_path %q, got %q", "/handler-tests/settings", state.NextPath)
	}
	if state.RedirectURI != "http://localhost:3000/auth/callback" {
		t.Fatalf("expected state redirect_uri %q, got %q", "http://localhost:3000/auth/callback", state.RedirectURI)
	}
	if state.IssuedAt == 0 {
		t.Fatal("expected non-zero issued_at in Google state")
	}
}
