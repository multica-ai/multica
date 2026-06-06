package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestResolveOpenclawNotificationCWDUsesWorkspaceEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENCLAW_WORKSPACE", dir)

	got, err := resolveOpenclawNotificationCWD()
	if err != nil {
		t.Fatalf("resolveOpenclawNotificationCWD: %v", err)
	}
	if got != dir {
		t.Fatalf("cwd = %q, want env dir %q", got, dir)
	}
}

func TestResolveOpenclawNotificationCWDCreatesHomeWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("OPENCLAW_WORKSPACE", filepath.Join(home, "missing"))
	t.Setenv("HOME", home)

	got, err := resolveOpenclawNotificationCWD()
	if err != nil {
		t.Fatalf("resolveOpenclawNotificationCWD: %v", err)
	}
	want := filepath.Join(home, ".openclaw", "workspace")
	if got != want {
		t.Fatalf("cwd = %q, want %q", got, want)
	}
	if info, err := os.Stat(got); err != nil || !info.IsDir() {
		t.Fatalf("expected cwd to exist as directory, info=%v err=%v", info, err)
	}
}

func TestHandleNotificationDeliverSendsSuccessResultWithStableCWD(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalRunner := runOpenclawMessageCommand
	t.Cleanup(func() { runOpenclawMessageCommand = originalRunner })
	var gotCWD string
	runOpenclawMessageCommand = func(_ context.Context, payload protocol.NotificationDeliverPayload, cwd string) ([]byte, error) {
		gotCWD = cwd
		if payload.DeliveryID != "delivery-1" {
			t.Fatalf("delivery_id = %q, want delivery-1", payload.DeliveryID)
		}
		return []byte("sent"), nil
	}

	d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	writes := make(chan []byte, 1)
	d.handleNotificationDeliver(marshalRaw(protocol.NotificationDeliverPayload{
		DeliveryID:      "delivery-1",
		Channel:         "openclaw_weixin",
		Type:            "openclaw_weixin",
		WechatID:        "wechat-user",
		OpenClawChannel: "openclaw-weixin",
		Content:         "hello",
	}), writes)

	if gotCWD == "" {
		t.Fatal("expected command runner to receive stable cwd")
	}
	var msg protocol.Message
	if err := json.Unmarshal(<-writes, &msg); err != nil {
		t.Fatalf("unmarshal result frame: %v", err)
	}
	if msg.Type != protocol.EventNotificationDeliveryResult {
		t.Fatalf("frame type = %q, want %q", msg.Type, protocol.EventNotificationDeliveryResult)
	}
	var result protocol.NotificationDeliveryResultPayload
	if err := json.Unmarshal(msg.Payload, &result); err != nil {
		t.Fatalf("unmarshal result payload: %v", err)
	}
	if !result.Success || result.DeliveryID != "delivery-1" || result.Channel != "openclaw_weixin" || result.Output != "sent" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestHandleNotificationDeliverSendsFailureResult(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	originalRunner := runOpenclawMessageCommand
	t.Cleanup(func() { runOpenclawMessageCommand = originalRunner })
	runOpenclawMessageCommand = func(context.Context, protocol.NotificationDeliverPayload, string) ([]byte, error) {
		return []byte("uv_cwd"), errors.New("exit status 1")
	}

	d := &Daemon{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	writes := make(chan []byte, 1)
	d.handleNotificationDeliver(marshalRaw(protocol.NotificationDeliverPayload{
		DeliveryID: "delivery-1",
		Channel:    "openclaw_weixin",
		Type:       "openclaw_weixin",
		WechatID:   "wechat-user",
		Content:    "hello",
	}), writes)

	var msg protocol.Message
	if err := json.Unmarshal(<-writes, &msg); err != nil {
		t.Fatalf("unmarshal result frame: %v", err)
	}
	var result protocol.NotificationDeliveryResultPayload
	if err := json.Unmarshal(msg.Payload, &result); err != nil {
		t.Fatalf("unmarshal result payload: %v", err)
	}
	if result.Success || result.Error != "exit status 1" || result.Output != "uv_cwd" {
		t.Fatalf("unexpected failure result: %#v", result)
	}
}
