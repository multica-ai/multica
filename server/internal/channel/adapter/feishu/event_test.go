package feishu_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/channel/adapter/feishu"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// ---------------------------------------------------------------------------
// TC-adapt-image-1: image message normalisation
// ---------------------------------------------------------------------------

func TestAdapter_NormalisesImageMessage(t *testing.T) {
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
        "header": {
            "event_id": "evt_img_001",
            "event_type": "im.message.receive_v1",
            "create_time": "1700000000"
        },
        "event": {
            "sender": {
                "sender_id": {"open_id": "ou_user_001"},
                "sender_type": "user"
            },
            "message": {
                "message_id": "om_img_001",
                "chat_id": "oc_001",
                "chat_type": "group",
                "message_type": "image",
                "create_time": "1700000000",
                "content": "{\"image_key\":\"img_abc123\"}",
                "mentions": []
            }
        }
    }`)

	fake.push(t, feishu.RawEvent{
		EventID:   "evt_img_001",
		EventType: "im.message.receive_v1",
		Payload:   rawJSON,
	})

	select {
	case ev, ok := <-adapter.Events():
		if !ok {
			t.Fatal("Events() channel closed before delivering inbound event")
		}
		if ev.Type != port.EventTypeMessageReceived {
			t.Errorf("Type = %q, want %q", ev.Type, port.EventTypeMessageReceived)
		}
		if ev.EventID != "evt_img_001" {
			t.Errorf("EventID = %q, want %q", ev.EventID, "evt_img_001")
		}
		if len(ev.Attachments) != 1 {
			t.Fatalf("Attachments = %d, want 1", len(ev.Attachments))
		}
		att := ev.Attachments[0]
		if att.FileKey != "img_abc123" {
			t.Errorf("FileKey = %q, want %q", att.FileKey, "img_abc123")
		}
		if att.FileType != "image" {
			t.Errorf("FileType = %q, want %q", att.FileType, "image")
		}
		if att.FileName != "" {
			t.Errorf("FileName = %q, want empty", att.FileName)
		}
		if ev.MessageID != "om_img_001" {
			t.Errorf("MessageID = %q, want %q", ev.MessageID, "om_img_001")
		}
		if len(ev.RawPayload) == 0 {
			t.Error("RawPayload is empty")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive inbound event within 2s")
	}
}

// ---------------------------------------------------------------------------
// TC-adapt-file-1: file message normalisation
// ---------------------------------------------------------------------------

func TestAdapter_NormalisesFileMessage(t *testing.T) {
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
        "header": {
            "event_id": "evt_file_001",
            "event_type": "im.message.receive_v1",
            "create_time": "1700000000"
        },
        "event": {
            "sender": {
                "sender_id": {"open_id": "ou_user_001"},
                "sender_type": "user"
            },
            "message": {
                "message_id": "om_file_001",
                "chat_id": "oc_001",
                "chat_type": "group",
                "message_type": "file",
                "create_time": "1700000000",
                "content": "{\"file_key\":\"file_xyz789\",\"file_name\":\"design.pdf\"}",
                "mentions": []
            }
        }
    }`)

	fake.push(t, feishu.RawEvent{
		EventID:   "evt_file_001",
		EventType: "im.message.receive_v1",
		Payload:   rawJSON,
	})

	select {
	case ev, ok := <-adapter.Events():
		if !ok {
			t.Fatal("Events() channel closed before delivering inbound event")
		}
		if len(ev.Attachments) != 1 {
			t.Fatalf("Attachments = %d, want 1", len(ev.Attachments))
		}
		att := ev.Attachments[0]
		if att.FileKey != "file_xyz789" {
			t.Errorf("FileKey = %q, want %q", att.FileKey, "file_xyz789")
		}
		if att.FileType != "file" {
			t.Errorf("FileType = %q, want %q", att.FileType, "file")
		}
		if att.FileName != "design.pdf" {
			t.Errorf("FileName = %q, want %q", att.FileName, "design.pdf")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive inbound event within 2s")
	}
}

// ---------------------------------------------------------------------------
// TC-adapt-image-2: image message with text + issue identifier in text
// ---------------------------------------------------------------------------

func TestAdapter_NormalisesImageMessage_WithTextAndIssueKey(t *testing.T) {
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

	// Feishu image messages may include a text body alongside the image.
	rawJSON := []byte(`{
        "schema": "2.0",
        "header": {
            "event_id": "evt_img_002",
            "event_type": "im.message.receive_v1"
        },
        "event": {
            "sender": {
                "sender_id": {"open_id": "ou_user_001"},
                "sender_type": "user"
            },
            "message": {
                "message_id": "om_img_002",
                "chat_id": "oc_001",
                "chat_type": "group",
                "message_type": "image",
                "content": "{\"image_key\":\"img_abc123\",\"text\":\"请看 STA-68 的截图\"}",
                "mentions": []
            }
        }
    }`)

	fake.push(t, feishu.RawEvent{
		EventID:   "evt_img_002",
		EventType: "im.message.receive_v1",
		Payload:   rawJSON,
	})

	select {
	case ev, ok := <-adapter.Events():
		if !ok {
			t.Fatal("Events() channel closed before delivering inbound event")
		}
		if len(ev.Attachments) != 1 {
			t.Fatalf("Attachments = %d, want 1", len(ev.Attachments))
		}
		if ev.Attachments[0].FileKey != "img_abc123" {
			t.Errorf("FileKey = %q", ev.Attachments[0].FileKey)
		}
		// Text should still be extracted for intent parsing.
		if !strings.Contains(ev.Text, "STA-68") {
			t.Errorf("Text = %q, should contain STA-68", ev.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive inbound event within 2s")
	}
}

// ---------------------------------------------------------------------------
// TC-adapt-drop-1: unknown message_type is silently dropped
// ---------------------------------------------------------------------------

func TestAdapter_UnknownMessageType_Dropped(t *testing.T) {
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
        "header": {
            "event_id": "evt_drop_001",
            "event_type": "im.message.receive_v1"
        },
        "event": {
            "sender": {
                "sender_id": {"open_id": "ou_user_001"},
                "sender_type": "user"
            },
            "message": {
                "message_id": "om_drop_001",
                "chat_id": "oc_001",
                "chat_type": "group",
                "message_type": "sticker",
                "content": "{\"sticker_id\":\"stk_123\"}"
            }
        }
    }`)

	fake.push(t, feishu.RawEvent{
		EventID:   "evt_drop_001",
		EventType: "im.message.receive_v1",
		Payload:   rawJSON,
	})

	select {
	case _, ok := <-adapter.Events():
		if ok {
			t.Fatal("expected no event for unknown message_type, but received one")
		}
		// Channel closed — no event emitted, which is correct.
	case <-time.After(500 * time.Millisecond):
		// No event within timeout — this is the expected path since the
		// event should be dropped silently.
	}
}
