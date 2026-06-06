package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunNotifyBindWechat(t *testing.T) {
	var gotAuth string
	var gotRequest notifyBindWechatRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me/notification-bindings/openclaw-weixin" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":               "binding-1",
			"provider":         "openclaw_weixin",
			"external_user_id": gotRequest.WechatID,
			"display_name":     gotRequest.WechatID,
			"status":           "active",
			"metadata": map[string]any{
				"channel": gotRequest.Channel,
			},
			"created_at": "2026-05-20T00:00:00Z",
			"updated_at": "2026-05-20T00:00:00Z",
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")

	cmd := testCmd()
	cmd.Flags().String("wechat-id", "", "")
	cmd.Flags().String("channel", "openclaw-weixin", "")
	cmd.Flags().String("output", "json", "")
	if err := cmd.Flags().Set("wechat-id", " user@im.wechat "); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runNotifyBindWechat(cmd, nil); err != nil {
		t.Fatalf("runNotifyBindWechat() error = %v", err)
	}

	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	if gotRequest.WechatID != "user@im.wechat" {
		t.Fatalf("wechat_id = %q, want trimmed value", gotRequest.WechatID)
	}
	if gotRequest.Channel != "openclaw-weixin" {
		t.Fatalf("channel = %q, want openclaw-weixin", gotRequest.Channel)
	}

	var result notifyBindWechatResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out.String())
	}
	if !result.Success || result.WechatID != "user@im.wechat" || result.Binding == nil {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestRunNotifyBindWechatRequiresWechatID(t *testing.T) {
	cmd := testCmd()
	cmd.Flags().String("wechat-id", "", "")
	cmd.Flags().String("channel", "openclaw-weixin", "")
	cmd.Flags().String("output", "table", "")

	err := runNotifyBindWechat(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--wechat-id is required") {
		t.Fatalf("error = %v, want --wechat-id validation", err)
	}
}

func TestRunNotifyDebugDeliveries(t *testing.T) {
	var gotAuth string
	var gotPath string
	var gotQuery = map[string]string{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		for key, values := range r.URL.Query() {
			if len(values) > 0 {
				gotQuery[key] = values[0]
			}
		}
		if r.URL.Path != "/api/workspaces/workspace-1/notification-debug/deliveries" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total": 1,
			"rows": []map[string]any{
				{
					"notification_event": map[string]any{
						"id":                "event-1",
						"recipient_user_id": "recipient-1",
						"type":              "new_comment",
						"comment_id":        "comment-1",
						"created_at":        "2026-06-06T00:00:00Z",
					},
					"delivery": map[string]any{
						"id":               "delivery-1",
						"channel":          "openclaw_weixin",
						"status":           "pending",
						"attempt_count":    1,
						"payload_snapshot": map[string]any{"wechat_id": "wechat-user", "channel": "openclaw-weixin"},
						"created_at":       "2026-06-06T00:00:00Z",
						"updated_at":       "2026-06-06T00:00:00Z",
					},
				},
			},
		})
	}))
	defer srv.Close()

	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-1")

	cmd := testCmd()
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("issue-id", "", "")
	cmd.Flags().String("recipient-id", "", "")
	cmd.Flags().String("comment-id", "", "")
	cmd.Flags().String("event-type", "", "")
	cmd.Flags().String("channel", "", "")
	cmd.Flags().Int("limit", 100, "")
	cmd.Flags().String("output", "json", "")
	if err := cmd.Flags().Set("issue-id", "issue-1"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("channel", "openclaw_weixin"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("limit", "25"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("output", "json"); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := runNotifyDebugDeliveries(cmd, nil); err != nil {
		t.Fatalf("runNotifyDebugDeliveries() error = %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	if gotPath != "/api/workspaces/workspace-1/notification-debug/deliveries" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotQuery["issue_id"] != "issue-1" || gotQuery["channel"] != "openclaw_weixin" || gotQuery["limit"] != "25" {
		t.Fatalf("unexpected query params: %#v", gotQuery)
	}

	var result notifyDebugDeliveriesResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out.String())
	}
	if result.Total != 1 || len(result.Rows) != 1 || result.Rows[0].Delivery == nil {
		t.Fatalf("unexpected result: %#v", result)
	}
	var payload map[string]string
	if err := json.Unmarshal(result.Rows[0].Delivery.PayloadSnapshot, &payload); err != nil {
		t.Fatalf("decode payload_snapshot: %v", err)
	}
	if payload["wechat_id"] != "wechat-user" || payload["channel"] != "openclaw-weixin" {
		t.Fatalf("unexpected payload_snapshot: %#v", payload)
	}
}

func TestNotifyCommandRegistered(t *testing.T) {
	if _, _, err := notifyCmd.Find([]string{"bind-wechat"}); err != nil {
		t.Fatalf("expected notify bind-wechat command to exist: %v", err)
	}
	if _, _, err := notifyCmd.Find([]string{"debug-deliveries"}); err != nil {
		t.Fatalf("expected notify debug-deliveries command to exist: %v", err)
	}
}
