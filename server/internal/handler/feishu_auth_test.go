package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestExchangeFeishuCodeUsesAppCredentials(t *testing.T) {
	oldClient := http.DefaultClient
	t.Cleanup(func() {
		http.DefaultClient = oldClient
	})

	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://open.feishu.cn/open-apis/authen/v1/access_token" {
			t.Fatalf("unexpected URL: %s", req.URL.String())
		}
		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("unexpected content type: %s", got)
		}

		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}

		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}

		if payload["app_id"] != "app-id" {
			t.Fatalf("expected app_id, got %q", payload["app_id"])
		}
		if payload["app_secret"] != "app-secret" {
			t.Fatalf("expected app_secret, got %q", payload["app_secret"])
		}
		if _, ok := payload["client_id"]; ok {
			t.Fatalf("did not expect client_id in payload: %s", string(body))
		}
		if _, ok := payload["client_secret"]; ok {
			t.Fatalf("did not expect client_secret in payload: %s", string(body))
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"success","data":{"access_token":"token-123"}}`)),
		}, nil
	})}

	req := (&http.Request{Method: http.MethodPost, Header: make(http.Header)}).WithContext(context.Background())
	token, err := exchangeFeishuCode(req, "auth-code", "http://127.0.0.1:13030/auth/callback", "app-id", "app-secret")
	if err != nil {
		t.Fatalf("exchangeFeishuCode returned error: %v", err)
	}
	if token != "token-123" {
		t.Fatalf("unexpected token: %q", token)
	}
}

func TestResolveFeishuUserWithoutEmailCreatesPlaceholderUser(t *testing.T) {
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS external_identity (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
			provider TEXT NOT NULL,
			provider_user_id TEXT NOT NULL,
			union_id TEXT,
			tenant_key TEXT,
			email TEXT,
			name TEXT,
			avatar_url TEXT,
			raw_profile JSONB NOT NULL DEFAULT '{}',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE (provider, provider_user_id)
		)
	`); err != nil {
		t.Fatalf("ensure external_identity table: %v", err)
	}

	openID := "ou_test_no_email_user"
	expectedEmail := feishuPlaceholderEmail(openID)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, expectedEmail)
	})

	user, err := testHandler.resolveFeishuUser((&http.Request{Method: http.MethodPost, Header: make(http.Header)}).WithContext(ctx), feishuUserInfoResponse{
		Data: struct {
			OpenID    string `json:"open_id"`
			UnionID   string `json:"union_id"`
			TenantKey string `json:"tenant_key"`
			Email     string `json:"email"`
			Name      string `json:"name"`
			AvatarURL string `json:"avatar_url"`
		}{
			OpenID:    openID,
			UnionID:   "on_test_union",
			TenantKey: "tenant-test",
			Name:      "Feishu No Email",
			AvatarURL: "https://example.com/avatar.png",
		},
	}, []byte(`{"open_id":"ou_test_no_email_user"}`))
	if err != nil {
		t.Fatalf("resolveFeishuUser returned error: %v", err)
	}
	if user.Email != expectedEmail {
		t.Fatalf("expected placeholder email %q, got %q", expectedEmail, user.Email)
	}
	if user.Name != "Feishu No Email" {
		t.Fatalf("expected user name to come from Feishu profile, got %q", user.Name)
	}

	identity, err := testHandler.Queries.GetExternalIdentityByProvider(ctx, db.GetExternalIdentityByProviderParams{
		Provider:       feishuProvider,
		ProviderUserID: openID,
	})
	if err != nil {
		t.Fatalf("GetExternalIdentityByProvider returned error: %v", err)
	}
	if !identity.Email.Valid || identity.Email.String != expectedEmail {
		t.Fatalf("expected external identity email %q, got %+v", expectedEmail, identity.Email)
	}
}
