package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestWecomInboxWebhookWiredWithoutSmartBot(t *testing.T) {
	t.Setenv("MULTICA_WECOM_SECRET_KEY", "")
	t.Setenv("MULTICA_WECOM_NOTIFICATION_WEBHOOK_URL", "")
	t.Setenv("MULTICA_WECOM_NOTIFICATION_WORKSPACE_ID", "")
	t.Setenv("MULTICA_WECOM_NOTIFICATION_MEMBER_ID", "")
	t.Setenv("MULTICA_APP_URL", "https://multica.example")
	boot := func() int {
		bus := events.New()
		NewRouterWithOptions(nil, realtime.NewHub(), bus, analytics.NoopClient{}, nil, RouterOptions{})
		return bus.SubscriberCount(protocol.EventInboxNew)
	}
	before := boot()
	t.Setenv("MULTICA_WECOM_NOTIFICATION_WEBHOOK_URL", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-secret")
	t.Setenv("MULTICA_WECOM_NOTIFICATION_WORKSPACE_ID", "33333333-3333-4333-8333-333333333333")
	t.Setenv("MULTICA_WECOM_NOTIFICATION_MEMBER_ID", "44444444-4444-4444-8444-444444444444")
	if got := boot(); got != before+1 {
		t.Fatalf("group webhook must subscribe independently of smart-bot key: before=%d after=%d", before, got)
	}
	var logged bytes.Buffer
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })
	t.Setenv("MULTICA_WECOM_NOTIFICATION_MEMBER_ID", "")
	if got := boot(); got != before {
		t.Fatalf("invalid redirect config must not take over delivery: got %d want %d", got, before)
	}
	if !strings.Contains(logged.String(), "configuration rejected") || strings.Contains(logged.String(), "synthetic-secret") {
		t.Fatal("configuration rejection was silent or leaked webhook credentials")
	}
}
