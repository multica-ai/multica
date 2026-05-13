package lark

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestInvitationNotifierResolvesEmailAndSendsCard(t *testing.T) {
	var sawContactRequest bool
	var sawMessageRequest bool

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/token":
			return jsonResponse(map[string]any{
				"code":                0,
				"tenant_access_token": "tenant-token",
				"expire":              7200,
			}), nil
		case "/contact":
			sawContactRequest = true
			if got := r.URL.Query().Get("user_id_type"); got != "open_id" {
				t.Fatalf("user_id_type = %q, want open_id", got)
			}
			var payload struct {
				Emails []string `json:"emails"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.Emails) != 1 || payload.Emails[0] != "dev@example.com" {
				t.Fatalf("emails = %#v", payload.Emails)
			}
			return jsonResponse(map[string]any{
				"code": 0,
				"data": map[string]any{
					"user_list": []map[string]string{
						{"email": "dev@example.com", "user_id": "ou_dev"},
					},
				},
			}), nil
		case "/messages":
			sawMessageRequest = true
			if got := r.URL.Query().Get("receive_id_type"); got != "open_id" {
				t.Fatalf("receive_id_type = %q, want open_id", got)
			}
			var payload struct {
				ReceiveID string `json:"receive_id"`
				MsgType   string `json:"msg_type"`
				Content   string `json:"content"`
				UUID      string `json:"uuid"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.ReceiveID != "ou_dev" {
				t.Fatalf("receive_id = %q", payload.ReceiveID)
			}
			if payload.MsgType != "interactive" {
				t.Fatalf("msg_type = %q", payload.MsgType)
			}
			if payload.UUID != "lark-invite:inv-1" {
				t.Fatalf("uuid = %q", payload.UUID)
			}
			if !strings.Contains(payload.Content, "你被邀请加入 Multica 工作区") {
				t.Fatalf("content missing title: %s", payload.Content)
			}
			if !strings.Contains(payload.Content, "https://multica.test/invite/inv-1") {
				t.Fatalf("content missing invite URL: %s", payload.Content)
			}
			return jsonResponse(map[string]any{
				"code": 0,
				"data": map[string]any{"message_id": "om_msg"},
			}), nil
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
			return nil, nil
		}
	})}

	notifier := NewInvitationNotifier(nil, InvitationConfig{
		Enabled:              true,
		AppID:                "app",
		AppSecret:            "secret",
		TenantAccessTokenURL: "https://lark.test/token",
		ContactBatchGetIDURL: "https://lark.test/contact",
		MessageURL:           "https://lark.test/messages",
		AppURL:               "https://multica.test",
		TenantKey:            "tenant",
	}, client)

	openID, err := notifier.resolveOpenIDByEmail(t.Context(), "dev@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if openID != "ou_dev" {
		t.Fatalf("openID = %q", openID)
	}
	messageID, err := notifier.sendInvitationCard(t.Context(), openID, invitationCard{
		InvitationID:  "inv-1",
		DedupeKey:     "lark-invite:inv-1",
		WorkspaceName: "Engineering",
		InviterName:   "Alice",
		Role:          "member",
		InviteURL:     "https://multica.test/invite/inv-1",
		ExpiresAt:     pgtype.Timestamptz{Time: time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC), Valid: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if messageID != "om_msg" {
		t.Fatalf("messageID = %q", messageID)
	}
	if !sawContactRequest || !sawMessageRequest {
		t.Fatalf("expected contact and message requests, got contact=%v message=%v", sawContactRequest, sawMessageRequest)
	}
}

func TestInvitationNotifierOpenIDResponseFallbacks(t *testing.T) {
	openID := openIDFromBatchGetIDResponse(map[string]any{
		"data": map[string]any{
			"email_users": map[string]any{
				"dev@example.com": []any{
					map[string]any{"user_id": "ou_dev"},
				},
			},
		},
	}, "dev@example.com")
	if openID != "ou_dev" {
		t.Fatalf("openID = %q", openID)
	}
}
