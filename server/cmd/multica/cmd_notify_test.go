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

func TestNotifyCommandRegistered(t *testing.T) {
	if _, _, err := notifyCmd.Find([]string{"bind-wechat"}); err != nil {
		t.Fatalf("expected notify bind-wechat command to exist: %v", err)
	}
}
