package main

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func buildInboxForwardingRouter(t *testing.T) *handler.Handler {
	t.Helper()
	_, h := NewRouterWithOptions(
		testPool,
		realtime.NewHub(),
		events.New(),
		analytics.NoopClient{},
		nil,
		RouterOptions{},
	)
	if h.InboxChannelDispatcher != nil {
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := h.InboxChannelDispatcher.Close(ctx); err != nil {
				t.Errorf("close Inbox Channel dispatcher: %v", err)
			}
		})
	}
	return h
}

func testLarkSecretKey() string {
	return base64.StdEncoding.EncodeToString(make([]byte, 32))
}

func TestRouterLeavesInboxChannelForwardingDisabledWhenEnvEmpty(t *testing.T) {
	t.Setenv("MULTICA_INBOX_FORWARD_CHANNELS", "")
	t.Setenv("MULTICA_LARK_SECRET_KEY", testLarkSecretKey())
	h := buildInboxForwardingRouter(t)

	if h.InboxChannelDispatcher != nil {
		t.Fatal("Inbox Channel dispatcher started with an empty allowlist")
	}
}

func TestRouterRegistersFeishuInboxSenderWhenConfiguredAndLarkReady(t *testing.T) {
	t.Setenv("MULTICA_INBOX_FORWARD_CHANNELS", "feishu")
	t.Setenv("MULTICA_LARK_SECRET_KEY", testLarkSecretKey())
	h := buildInboxForwardingRouter(t)

	if h.InboxChannelDispatcher == nil {
		t.Fatal("Inbox Channel dispatcher was not started")
	}
	if h.InboxChannelSenders == nil {
		t.Fatal("Inbox Channel sender registry is nil")
	}
	if _, ok := h.InboxChannelSenders.Lookup(channel.TypeFeishu); !ok {
		t.Fatal("Feishu Inbox sender was not registered")
	}
}

func TestRouterSkipsConfiguredFeishuSenderWhenLarkUnavailable(t *testing.T) {
	t.Setenv("MULTICA_INBOX_FORWARD_CHANNELS", "feishu")
	t.Setenv("MULTICA_LARK_SECRET_KEY", "")
	h := buildInboxForwardingRouter(t)

	if h.InboxChannelDispatcher == nil {
		t.Fatal("configured dispatcher should keep Inbox-only behavior without a sender")
	}
	if _, ok := h.InboxChannelSenders.Lookup(channel.TypeFeishu); ok {
		t.Fatal("Feishu Inbox sender registered without Lark initialization")
	}
}
