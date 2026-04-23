package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	notifyutil "github.com/multica-ai/multica/server/internal/notify"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestStartMyDingTalkBinding(t *testing.T) {
	t.Setenv("DINGTALK_CLIENT_ID", "ding-client-id")
	t.Setenv("DINGTALK_CLIENT_SECRET", "ding-client-secret")
	t.Setenv("MULTICA_APP_URL", "https://app.multica.test")

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/me/notification-bindings/dingtalk/start", map[string]any{
		"next_path": "/handler-tests/settings",
	})

	testHandler.StartMyDingTalkBinding(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp StartDingTalkBindingResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	parsed, err := url.Parse(resp.AuthURL)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	query := parsed.Query()
	if query.Get("client_id") != "ding-client-id" {
		t.Fatalf("expected client_id %q, got %q", "ding-client-id", query.Get("client_id"))
	}
	if query.Get("redirect_uri") != "https://app.multica.test/auth/callback" {
		t.Fatalf("expected redirect_uri to point at auth callback, got %q", query.Get("redirect_uri"))
	}

	state, err := notifyutil.ParseDingTalkState(query.Get("state"))
	if err != nil {
		t.Fatalf("parse DingTalk state: %v", err)
	}
	if state.UserID != testUserID {
		t.Fatalf("expected state user_id %q, got %q", testUserID, state.UserID)
	}
	if state.NextPath != "/handler-tests/settings" {
		t.Fatalf("expected state next_path %q, got %q", "/handler-tests/settings", state.NextPath)
	}
	if state.IssuedAt == 0 {
		t.Fatal("expected non-zero issued_at in DingTalk state")
	}
}

func TestCompleteMyDingTalkBinding(t *testing.T) {
	cleanupNotificationSettings(t)
	t.Cleanup(func() { cleanupNotificationSettings(t) })

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST /token, got %s", r.Method)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"accessToken": "ding-access-token",
				"refreshToken": "ding-refresh-token",
				"expireIn": 3600,
				"corpId": "ding-corp-id",
				"openId": "token-open-id"
			}`))
		case "/userinfo":
			if got := r.Header.Get("Authorization"); got != "Bearer ding-access-token" {
				t.Fatalf("expected bearer token, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"unionId": "union-id-123",
				"openId": "profile-open-id",
				"name": "Ding User",
				"avatarUrl": "https://avatar.example/ding.png",
				"mobile": "13800000000"
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(tokenServer.Close)

	t.Setenv("DINGTALK_CLIENT_ID", "ding-client-id")
	t.Setenv("DINGTALK_CLIENT_SECRET", "ding-client-secret")
	t.Setenv("DINGTALK_TOKEN_URL", tokenServer.URL+"/token")
	t.Setenv("DINGTALK_USERINFO_URL", tokenServer.URL+"/userinfo")

	state, err := notifyutil.BuildDingTalkState(notifyutil.DingTalkBindingState{
		UserID:   testUserID,
		NextPath: "/handler-tests/settings",
		IssuedAt: time.Now().UTC().Unix(),
	})
	if err != nil {
		t.Fatalf("BuildDingTalkState: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/me/notification-bindings/dingtalk/callback", map[string]any{
		"code":  "test-code",
		"state": state,
	})

	testHandler.CompleteMyDingTalkBinding(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp CompleteDingTalkBindingResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Binding.Provider != "dingtalk" {
		t.Fatalf("expected provider 'dingtalk', got %q", resp.Binding.Provider)
	}
	if resp.Binding.ExternalUserID != "union-id-123" {
		t.Fatalf("expected external_user_id %q, got %q", "union-id-123", resp.Binding.ExternalUserID)
	}
	if resp.Binding.DisplayName == nil || *resp.Binding.DisplayName != "Ding User" {
		t.Fatalf("expected display_name %q, got %#v", "Ding User", resp.Binding.DisplayName)
	}
	if resp.NextPath == nil || *resp.NextPath != "/handler-tests/settings" {
		t.Fatalf("expected next_path %q, got %#v", "/handler-tests/settings", resp.NextPath)
	}

	bindings, err := db.New(testPool).ListExternalAccountBindingsByUser(req.Context(), parseUUID(testUserID))
	if err != nil {
		t.Fatalf("ListExternalAccountBindingsByUser: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(bindings))
	}

	accessToken, err := notifyutil.DecryptToken(bindings[0].AccessTokenEncrypted.String)
	if err != nil {
		t.Fatalf("DecryptToken(access): %v", err)
	}
	if accessToken != "ding-access-token" {
		t.Fatalf("expected access token %q, got %q", "ding-access-token", accessToken)
	}

	refreshToken, err := notifyutil.DecryptToken(bindings[0].RefreshTokenEncrypted.String)
	if err != nil {
		t.Fatalf("DecryptToken(refresh): %v", err)
	}
	if refreshToken != "ding-refresh-token" {
		t.Fatalf("expected refresh token %q, got %q", "ding-refresh-token", refreshToken)
	}

	if !bindings[0].TokenExpiresAt.Valid {
		t.Fatal("expected token_expires_at to be populated")
	}
	if bindings[0].Status != "active" {
		t.Fatalf("expected binding status %q, got %q", "active", bindings[0].Status)
	}
	var metadata map[string]any
	if err := json.Unmarshal(bindings[0].Metadata, &metadata); err != nil {
		t.Fatalf("unmarshal binding metadata: %v", err)
	}
	if metadata["corp_id"] != "ding-corp-id" {
		t.Fatalf("expected metadata corp_id %q, got %#v", "ding-corp-id", metadata["corp_id"])
	}
}
