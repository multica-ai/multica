package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestExchangeCodeFetchesUserInfoAndChecksTenant(t *testing.T) {
	var sawRedirectURI string
	var sawBearer string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/token":
			var req oauthPayload
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode token request: %v", err)
			}
			sawRedirectURI = req.RedirectURI
			return jsonResponse(map[string]any{
				"code": 0,
				"data": map[string]any{
					"access_token": "uat_test",
				},
			}), nil
		case "/user_info":
			sawBearer = r.Header.Get("Authorization")
			return jsonResponse(map[string]any{
				"code": 0,
				"data": map[string]any{
					"user_id":    "user_1",
					"open_id":    "ou_1",
					"union_id":   "on_1",
					"tenant_key": "tenant_1",
					"email":      "USER@example.com",
					"name":       "Ada",
					"avatar_url": "https://example.com/avatar.png",
				},
			}), nil
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		return nil, nil
	})

	client := NewOAuthClient(Config{
		Enabled:         true,
		AppID:           "cli_x",
		AppSecret:       "secret",
		TokenURL:        "https://lark.test/token",
		UserInfoURL:     "https://lark.test/user_info",
		TenantAllowlist: []string{"tenant_1"},
	}, &http.Client{Transport: transport})

	profile, err := client.ExchangeCode(context.Background(), "code_x", "https://multica.test/auth/callback?provider=lark")
	if err != nil {
		t.Fatalf("ExchangeCode returned error: %v", err)
	}
	if sawRedirectURI != "https://multica.test/auth/callback?provider=lark" {
		t.Fatalf("redirect_uri not forwarded: %q", sawRedirectURI)
	}
	if sawBearer != "Bearer uat_test" {
		t.Fatalf("userinfo auth header: got %q", sawBearer)
	}
	if profile.OpenID != "ou_1" || profile.ExternalUserID != "user_1" || profile.Name != "Ada" {
		t.Fatalf("profile not mapped correctly: %+v", profile)
	}
	if profile.Email != "user@example.com" {
		t.Fatalf("email should be normalized, got %q", profile.Email)
	}
}

func TestExchangeCodeFetchesUserInfoWhenEmailMissingFromToken(t *testing.T) {
	userInfoCalled := false
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/token":
			return jsonResponse(map[string]any{
				"code": 0,
				"data": map[string]any{
					"access_token": "uat_test",
					"open_id":      "ou_1",
					"tenant_key":   "tenant_1",
					"name":         "Ada",
				},
			}), nil
		case "/user_info":
			userInfoCalled = true
			return jsonResponse(map[string]any{
				"code": 0,
				"data": map[string]any{
					"open_id":    "ou_1",
					"tenant_key": "tenant_1",
					"email":      "ADA@example.com",
				},
			}), nil
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		return nil, nil
	})

	client := NewOAuthClient(Config{
		Enabled:     true,
		AppID:       "cli_x",
		AppSecret:   "secret",
		TokenURL:    "https://lark.test/token",
		UserInfoURL: "https://lark.test/user_info",
	}, &http.Client{Transport: transport})

	profile, err := client.ExchangeCode(context.Background(), "code_x", "")
	if err != nil {
		t.Fatalf("ExchangeCode returned error: %v", err)
	}
	if !userInfoCalled {
		t.Fatal("expected user_info to be fetched when token response has no email")
	}
	if profile.Email != "ada@example.com" {
		t.Fatalf("expected email from user_info, got %q", profile.Email)
	}
}

func TestExchangeCodeRejectsTenantOutsideAllowlist(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return jsonResponse(map[string]any{
			"code": 0,
			"data": map[string]any{
				"access_token": "uat_test",
				"open_id":      "ou_1",
				"tenant_key":   "tenant_blocked",
			},
		}), nil
	})

	client := NewOAuthClient(Config{
		Enabled:         true,
		AppID:           "cli_x",
		AppSecret:       "secret",
		TokenURL:        "https://lark.test/token",
		UserInfoURL:     "https://lark.test/user_info",
		TenantAllowlist: []string{"tenant_allowed"},
	}, &http.Client{Transport: transport})

	_, err := client.ExchangeCode(context.Background(), "code_x", "")
	if err == nil || err.Error() != "Lark tenant is not allowed" {
		t.Fatalf("expected tenant rejection, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(v any) *http.Response {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(v)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(&buf),
	}
}
