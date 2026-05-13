package lark

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/notifications"
)

func TestNotificationChannelSendsInteractiveCard(t *testing.T) {
	var sawTokenRequest bool
	var sawMessageRequest bool
	var sentCard map[string]any

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/token":
			sawTokenRequest = true
			if r.Method != http.MethodPost {
				t.Fatalf("token method = %s, want POST", r.Method)
			}
			return jsonResponse(map[string]any{
				"code":                0,
				"tenant_access_token": "tenant-token",
				"expire":              7200,
			}), nil
		case "/messages":
			sawMessageRequest = true
			if got := r.URL.Query().Get("receive_id_type"); got != "open_id" {
				t.Fatalf("receive_id_type = %q, want open_id", got)
			}
			if got := r.Header.Get("Authorization"); got != "Bearer tenant-token" {
				t.Fatalf("Authorization = %q", got)
			}
			var payload struct {
				ReceiveID string `json:"receive_id"`
				MsgType   string `json:"msg_type"`
				Content   string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if payload.ReceiveID != "ou_user" {
				t.Fatalf("receive_id = %q", payload.ReceiveID)
			}
			if payload.MsgType != "interactive" {
				t.Fatalf("msg_type = %q", payload.MsgType)
			}
			if err := json.Unmarshal([]byte(payload.Content), &sentCard); err != nil {
				t.Fatal(err)
			}
			return jsonResponse(map[string]any{"code": 0}), nil
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
			return nil, nil
		}
	})}

	channel := NewNotificationChannel(NotificationConfig{
		Enabled:              true,
		AppID:                "app",
		AppSecret:            "secret",
		TenantAccessTokenURL: "https://lark.test/token",
		MessageURL:           "https://lark.test/messages",
	}, client)
	err := channel.Send(t.Context(), notifications.NotificationMessage{
		RecipientExternal: "ou_user",
		Type:              "issue_assigned",
		IssueIdentifier:   "MUL-123",
		IssueStatus:       "todo",
		Title:             "Build Lark inbox delivery",
		URL:               "https://multica.test/acme/issues/issue-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawTokenRequest || !sawMessageRequest {
		t.Fatalf("expected token and message requests, got token=%v message=%v", sawTokenRequest, sawMessageRequest)
	}
	header, _ := sentCard["header"].(map[string]any)
	title, _ := header["title"].(map[string]any)
	if header["template"] != "blue" {
		t.Fatalf("card template = %#v", header["template"])
	}
	if title["content"] != "你被指派了 MUL-123" {
		t.Fatalf("card title = %#v", title["content"])
	}
	elements, _ := sentCard["elements"].([]any)
	if len(elements) != 2 {
		t.Fatalf("elements length = %d, want 2", len(elements))
	}
	body, _ := elements[0].(map[string]any)
	text, _ := body["text"].(map[string]any)
	content, _ := text["content"].(string)
	if !strings.Contains(content, "**任务：** MUL-123") {
		t.Fatalf("card body missing issue: %q", content)
	}
	if !strings.Contains(content, "**状态：** 待办") {
		t.Fatalf("card body missing status: %q", content)
	}
	action, _ := elements[1].(map[string]any)
	actions, _ := action["actions"].([]any)
	button, _ := actions[0].(map[string]any)
	buttonText, _ := button["text"].(map[string]any)
	if buttonText["content"] != "打开 Multica" {
		t.Fatalf("button text = %#v", buttonText["content"])
	}
	if button["url"] != "https://multica.test/acme/issues/issue-1" {
		t.Fatalf("button url = %#v", button["url"])
	}
}

func TestNotificationCardTemplateByStatus(t *testing.T) {
	tests := []struct {
		name   string
		msg    notifications.NotificationMessage
		expect string
	}{
		{
			name:   "todo is blue",
			msg:    notifications.NotificationMessage{IssueStatus: "todo"},
			expect: "blue",
		},
		{
			name:   "in progress is yellow",
			msg:    notifications.NotificationMessage{IssueStatus: "in_progress"},
			expect: "yellow",
		},
		{
			name:   "blocked is red",
			msg:    notifications.NotificationMessage{IssueStatus: "blocked"},
			expect: "red",
		},
		{
			name:   "done is green",
			msg:    notifications.NotificationMessage{IssueStatus: "done"},
			expect: "green",
		},
		{
			name:   "task failed is red",
			msg:    notifications.NotificationMessage{Type: "task_failed", IssueStatus: "in_progress"},
			expect: "red",
		},
		{
			name:   "agent completed is green",
			msg:    notifications.NotificationMessage{Type: "agent_completed", IssueStatus: "in_progress"},
			expect: "green",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			card := buildNotificationCard(tt.msg)
			header, _ := card["header"].(map[string]any)
			if header["template"] != tt.expect {
				t.Fatalf("template = %#v, want %q", header["template"], tt.expect)
			}
		})
	}
}
