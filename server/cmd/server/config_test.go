package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestPublicConfigDoesNotRequireAuth(t *testing.T) {
	t.Setenv("DINGTALK_CLIENT_ID", "ding-client-id")
	t.Setenv("DINGTALK_OAUTH_SCOPE", "openid")
	t.Setenv("NEXT_PUBLIC_HIDE_EMAIL_LOGIN", "true")

	resp, err := http.Get(testServer.URL + "/api/config")
	if err != nil {
		t.Fatalf("GET /api/config: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body struct {
		DingTalkClientID   string `json:"dingtalk_client_id"`
		DingTalkOAuthScope string `json:"dingtalk_oauth_scope"`
		HideEmailLogin     bool   `json:"hide_email_login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode config response: %v", err)
	}

	if body.DingTalkClientID != "ding-client-id" {
		t.Fatalf("expected dingtalk client id from env, got %q", body.DingTalkClientID)
	}
	if body.DingTalkOAuthScope != "openid" {
		t.Fatalf("expected dingtalk oauth scope from env, got %q", body.DingTalkOAuthScope)
	}
	if !body.HideEmailLogin {
		t.Fatal("expected hide_email_login from env")
	}
}
