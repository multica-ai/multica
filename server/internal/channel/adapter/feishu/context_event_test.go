package feishu_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/channel/adapter/feishu"
)

// tc-10-1: thread message sets ThreadID to root_id.
func TestAdapter_NormalisesThreadMessage(t *testing.T) {
	t.Parallel()

	const botID = "ou_bot_xxx"
	fake := newFakeFeishuClient(botID)
	adapter := feishu.NewAdapter(fake, feishu.Config{AppID: "cli_test"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = adapter.Disconnect(context.Background())
	})

	rawJSON := []byte(`{
        "schema": "2.0",
        "header": {"event_id": "evt_thread_001", "event_type": "im.message.receive_v1"},
        "event": {
            "sender": {"sender_id": {"open_id": "ou_user_001"}, "sender_type": "user"},
            "message": {
                "message_id": "om_child_001",
                "chat_id": "oc_001",
                "chat_type": "group",
                "message_type": "text",
                "content": "{\"text\":\"thread 内消息\"}",
                "mentions": [],
                "root_id": "om_root_001",
                "parent_id": "om_root_001"
            }
        }
    }`)

	fake.push(t, feishu.RawEvent{
		EventID:   "evt_thread_001",
		EventType: "im.message.receive_v1",
		Payload:   rawJSON,
	})

	select {
	case ev, ok := <-adapter.Events():
		if !ok {
			t.Fatal("Events() channel closed before delivering inbound event")
		}
		if ev.ThreadID != "om_root_001" {
			t.Fatalf("ThreadID = %q, want om_root_001", ev.ThreadID)
		}
		// root message: parent_id == message_id == root_id → not a reply
		if ev.ReplyToMessageID != "" {
			t.Fatalf("ReplyToMessageID = %q, want empty for root message", ev.ReplyToMessageID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive inbound event within 2s")
	}
}

// tc-9-1: reply-to message sets ReplyToMessageID to parent_id.
func TestAdapter_NormalisesReplyToMessage(t *testing.T) {
	t.Parallel()

	const botID = "ou_bot_xxx"
	fake := newFakeFeishuClient(botID)
	adapter := feishu.NewAdapter(fake, feishu.Config{AppID: "cli_test"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = adapter.Disconnect(context.Background())
	})

	rawJSON := []byte(`{
        "schema": "2.0",
        "header": {"event_id": "evt_reply_001", "event_type": "im.message.receive_v1"},
        "event": {
            "sender": {"sender_id": {"open_id": "ou_user_001"}, "sender_type": "user"},
            "message": {
                "message_id": "om_child_002",
                "chat_id": "oc_001",
                "chat_type": "group",
                "message_type": "text",
                "content": "{\"text\":\"回复消息\"}",
                "mentions": [],
                "root_id": "om_root_001",
                "parent_id": "om_parent_001"
            }
        }
    }`)

	fake.push(t, feishu.RawEvent{
		EventID:   "evt_reply_001",
		EventType: "im.message.receive_v1",
		Payload:   rawJSON,
	})

	select {
	case ev, ok := <-adapter.Events():
		if !ok {
			t.Fatal("Events() channel closed before delivering inbound event")
		}
		if ev.ThreadID != "om_root_001" {
			t.Fatalf("ThreadID = %q, want om_root_001", ev.ThreadID)
		}
		if ev.ReplyToMessageID != "om_parent_001" {
			t.Fatalf("ReplyToMessageID = %q, want om_parent_001", ev.ReplyToMessageID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive inbound event within 2s")
	}
}

// tc-9-3: plain message has empty ThreadID and ReplyToMessageID.
func TestAdapter_NormalisesPlainMessage_NoContextFields(t *testing.T) {
	t.Parallel()

	const botID = "ou_bot_xxx"
	fake := newFakeFeishuClient(botID)
	adapter := feishu.NewAdapter(fake, feishu.Config{AppID: "cli_test"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = adapter.Disconnect(context.Background())
	})

	rawJSON := []byte(`{
        "schema": "2.0",
        "header": {"event_id": "evt_plain_001", "event_type": "im.message.receive_v1"},
        "event": {
            "sender": {"sender_id": {"open_id": "ou_user_001"}, "sender_type": "user"},
            "message": {
                "message_id": "om_plain_001",
                "chat_id": "oc_001",
                "chat_type": "group",
                "message_type": "text",
                "content": "{\"text\":\"普通消息\"}",
                "mentions": []
            }
        }
    }`)

	fake.push(t, feishu.RawEvent{
		EventID:   "evt_plain_001",
		EventType: "im.message.receive_v1",
		Payload:   rawJSON,
	})

	select {
	case ev, ok := <-adapter.Events():
		if !ok {
			t.Fatal("Events() channel closed before delivering inbound event")
		}
		if ev.ThreadID != "" {
			t.Fatalf("ThreadID = %q, want empty", ev.ThreadID)
		}
		if ev.ReplyToMessageID != "" {
			t.Fatalf("ReplyToMessageID = %q, want empty", ev.ReplyToMessageID)
		}
		if ev.QuotedMessageID != "" {
			t.Fatalf("QuotedMessageID = %q, want empty", ev.QuotedMessageID)
		}
		if ev.QuotedText != "" {
			t.Fatalf("QuotedText = %q, want empty", ev.QuotedText)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive inbound event within 2s")
	}
}

// tc-8-1: quote block sets QuotedMessageID and QuotedText.
func TestAdapter_NormalisesQuoteMessage(t *testing.T) {
	t.Parallel()

	const botID = "ou_bot_xxx"
	fake := newFakeFeishuClient(botID)
	adapter := feishu.NewAdapter(fake, feishu.Config{AppID: "cli_test"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = adapter.Disconnect(context.Background())
	})

	// Feishu quote is often inside a post message type with structured content.
	// We test the adapter's ability to extract quote info from content JSON.
	rawJSON := []byte(`{
        "schema": "2.0",
        "header": {"event_id": "evt_quote_001", "event_type": "im.message.receive_v1"},
        "event": {
            "sender": {"sender_id": {"open_id": "ou_user_001"}, "sender_type": "user"},
            "message": {
                "message_id": "om_quote_001",
                "chat_id": "oc_001",
                "chat_type": "group",
                "message_type": "text",
                "content": "{\"text\":\"详情看看\",\"quote\":{\"message_id\":\"om_quoted_001\",\"text\":\"STA-68 的状态是 open\"}}",
                "mentions": []
            }
        }
    }`)

	fake.push(t, feishu.RawEvent{
		EventID:   "evt_quote_001",
		EventType: "im.message.receive_v1",
		Payload:   rawJSON,
	})

	select {
	case ev, ok := <-adapter.Events():
		if !ok {
			t.Fatal("Events() channel closed before delivering inbound event")
		}
		if ev.QuotedMessageID != "om_quoted_001" {
			t.Fatalf("QuotedMessageID = %q, want om_quoted_001", ev.QuotedMessageID)
		}
		if ev.QuotedText != "STA-68 的状态是 open" {
			t.Fatalf("QuotedText = %q, want 'STA-68 的状态是 open'", ev.QuotedText)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive inbound event within 2s")
	}
}

// tc-8-3: quote text truncated to 200 runes.
func TestAdapter_NormalisesQuoteMessage_Truncation(t *testing.T) {
	t.Parallel()

	const botID = "ou_bot_xxx"
	fake := newFakeFeishuClient(botID)
	adapter := feishu.NewAdapter(fake, feishu.Config{AppID: "cli_test"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = adapter.Disconnect(context.Background())
	})

	longText := strings.Repeat("a", 250)
	rawJSON := []byte(`{
        "schema": "2.0",
        "header": {"event_id": "evt_quote_002", "event_type": "im.message.receive_v1"},
        "event": {
            "sender": {"sender_id": {"open_id": "ou_user_001"}, "sender_type": "user"},
            "message": {
                "message_id": "om_quote_002",
                "chat_id": "oc_001",
                "chat_type": "group",
                "message_type": "text",
                "content": "{\"text\":\"看看这个\",\"quote\":{\"message_id\":\"om_quoted_002\",\"text\":\"` + longText + `\"}}",
                "mentions": []
            }
        }
    }`)

	fake.push(t, feishu.RawEvent{
		EventID:   "evt_quote_002",
		EventType: "im.message.receive_v1",
		Payload:   rawJSON,
	})

	select {
	case ev, ok := <-adapter.Events():
		if !ok {
			t.Fatal("Events() channel closed before delivering inbound event")
		}
		if ev.QuotedMessageID != "om_quoted_002" {
			t.Fatalf("QuotedMessageID = %q, want om_quoted_002", ev.QuotedMessageID)
		}
		runes := []rune(ev.QuotedText)
		if len(runes) > 201 {
			t.Fatalf("QuotedText rune count = %d, want <= 201", len(runes))
		}
		if !strings.HasSuffix(ev.QuotedText, "…") {
			t.Fatal("QuotedText should end with ellipsis when truncated")
		}
		if !strings.HasPrefix(ev.QuotedText, strings.Repeat("a", 50)) {
			t.Fatal("QuotedText should preserve prefix")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive inbound event within 2s")
	}
}

// tc-8-4: missing quote fields → zero values, no panic.
func TestAdapter_NormalisesQuoteMessage_MissingFields(t *testing.T) {
	t.Parallel()

	const botID = "ou_bot_xxx"
	fake := newFakeFeishuClient(botID)
	adapter := feishu.NewAdapter(fake, feishu.Config{AppID: "cli_test"})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := adapter.Connect(ctx); err != nil {
		t.Fatalf("Connect returned error: %v", err)
	}
	t.Cleanup(func() {
		_ = adapter.Disconnect(context.Background())
	})

	rawJSON := []byte(`{
        "schema": "2.0",
        "header": {"event_id": "evt_quote_003", "event_type": "im.message.receive_v1"},
        "event": {
            "sender": {"sender_id": {"open_id": "ou_user_001"}, "sender_type": "user"},
            "message": {
                "message_id": "om_quote_003",
                "chat_id": "oc_001",
                "chat_type": "group",
                "message_type": "text",
                "content": "{\"text\":\"普通文本\"}",
                "mentions": []
            }
        }
    }`)

	fake.push(t, feishu.RawEvent{
		EventID:   "evt_quote_003",
		EventType: "im.message.receive_v1",
		Payload:   rawJSON,
	})

	select {
	case ev, ok := <-adapter.Events():
		if !ok {
			t.Fatal("Events() channel closed before delivering inbound event")
		}
		if ev.QuotedMessageID != "" {
			t.Fatalf("QuotedMessageID = %q, want empty", ev.QuotedMessageID)
		}
		if ev.QuotedText != "" {
			t.Fatalf("QuotedText = %q, want empty", ev.QuotedText)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive inbound event within 2s")
	}
}
