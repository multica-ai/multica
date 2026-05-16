package feishu

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TC-10.1: 飞书 thread 消息解析
// ---------------------------------------------------------------------------

func TestNormaliseMessageReceive_ThreadMessage(t *testing.T) {
	t.Parallel()
	raw := RawEvent{
		EventID:   "evt-thread-1",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-thread-1"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m_thread_1",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      `{"text":"hello in thread"}`,
					"mentions":     []map[string]any{},
					"root_id":      "m_root_1",
					"parent_id":    "m_root_1",
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", "", raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.ThreadID != "m_root_1" {
		t.Fatalf("ThreadID = %q, want %q", ev.ThreadID, "m_root_1")
	}
	// parent_id == message_id (thread root) → ReplyToMessageID should be empty
	if ev.ReplyToMessageID != "" {
		t.Fatalf("ReplyToMessageID = %q, want empty (thread root)", ev.ReplyToMessageID)
	}
}

// ---------------------------------------------------------------------------
// TC-10.3: 非 thread 消息（root_id 为空）
// ---------------------------------------------------------------------------

func TestNormaliseMessageReceive_NonThreadMessage(t *testing.T) {
	t.Parallel()
	raw := RawEvent{
		EventID:   "evt-plain-1",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-plain-1"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m_plain_1",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      `{"text":"hello"}`,
					"mentions":     []map[string]any{},
					"root_id":      "",
					"parent_id":    "",
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", "", raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.ThreadID != "" {
		t.Fatalf("ThreadID = %q, want empty", ev.ThreadID)
	}
	if ev.ReplyToMessageID != "" {
		t.Fatalf("ReplyToMessageID = %q, want empty", ev.ReplyToMessageID)
	}
}

// ---------------------------------------------------------------------------
// TC-9.1: 飞书 reply 消息解析
// ---------------------------------------------------------------------------

func TestNormaliseMessageReceive_ReplyMessage(t *testing.T) {
	t.Parallel()
	raw := RawEvent{
		EventID:   "evt-reply-1",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-reply-1"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m_reply_1",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      `{"text":"replying to you"}`,
					"mentions":     []map[string]any{},
					"root_id":      "m_root_1",
					"parent_id":    "m_parent_1",
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", "", raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.ThreadID != "m_root_1" {
		t.Fatalf("ThreadID = %q, want %q", ev.ThreadID, "m_root_1")
	}
	if ev.ReplyToMessageID != "m_parent_1" {
		t.Fatalf("ReplyToMessageID = %q, want %q", ev.ReplyToMessageID, "m_parent_1")
	}
}

// ---------------------------------------------------------------------------
// TC-9.3: thread root 消息（parent_id == message_id）
// ---------------------------------------------------------------------------

func TestNormaliseMessageReceive_ThreadRootMessage(t *testing.T) {
	t.Parallel()
	raw := RawEvent{
		EventID:   "evt-root-1",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-root-1"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m_root_1",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      `{"text":"thread root"}`,
					"mentions":     []map[string]any{},
					"root_id":      "m_root_1",
					"parent_id":    "m_root_1",
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", "", raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.ThreadID != "m_root_1" {
		t.Fatalf("ThreadID = %q, want %q", ev.ThreadID, "m_root_1")
	}
	if ev.ReplyToMessageID != "" {
		t.Fatalf("ReplyToMessageID = %q, want empty (parent_id == message_id)", ev.ReplyToMessageID)
	}
}

// ---------------------------------------------------------------------------
// TC-9.4: parent_id 为空串
// ---------------------------------------------------------------------------

func TestNormaliseMessageReceive_EmptyParentID(t *testing.T) {
	t.Parallel()
	raw := RawEvent{
		EventID:   "evt-empty-parent",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-empty-parent"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m_empty_parent",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      `{"text":"no parent"}`,
					"mentions":     []map[string]any{},
					"root_id":      "",
					"parent_id":    "",
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", "", raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.ReplyToMessageID != "" {
		t.Fatalf("ReplyToMessageID = %q, want empty", ev.ReplyToMessageID)
	}
}

// ---------------------------------------------------------------------------
// TC-8.1: 飞书标准 quote 消息解析
// ---------------------------------------------------------------------------

func TestNormaliseMessageReceive_QuoteMessage(t *testing.T) {
	t.Parallel()
	raw := RawEvent{
		EventID:   "evt-quote-1",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-quote-1"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m_quote_1",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      `{"text":"I agree","quote":{"message_id":"m_quoted_1","text":"Original message text"}}`,
					"mentions":     []map[string]any{},
					"root_id":      "",
					"parent_id":    "",
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", "", raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.QuotedMessageID != "m_quoted_1" {
		t.Fatalf("QuotedMessageID = %q, want %q", ev.QuotedMessageID, "m_quoted_1")
	}
	if ev.QuotedText != "Original message text" {
		t.Fatalf("QuotedText = %q, want %q", ev.QuotedText, "Original message text")
	}
}

// ---------------------------------------------------------------------------
// TC-8.3: quote 文本恰好 200 rune
// ---------------------------------------------------------------------------

func TestNormaliseMessageReceive_QuoteTextExactly200(t *testing.T) {
	t.Parallel()
	text200 := strings.Repeat("a", 200)
	raw := RawEvent{
		EventID:   "evt-quote-200",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-quote-200"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m_quote_200",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      mustJSONString(t, map[string]any{"text": "ok", "quote": map[string]any{"message_id": "m_q", "text": text200}}),
					"mentions":     []map[string]any{},
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", "", raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len([]rune(ev.QuotedText)) != 200 {
		t.Fatalf("QuotedText rune count = %d, want 200", len([]rune(ev.QuotedText)))
	}
	if strings.HasSuffix(ev.QuotedText, "…") {
		t.Fatal("QuotedText should not have ellipsis at exactly 200 runes")
	}
}

// ---------------------------------------------------------------------------
// TC-8.4: quote 文本 201 rune
// ---------------------------------------------------------------------------

func TestNormaliseMessageReceive_QuoteTextTruncatedAt201(t *testing.T) {
	t.Parallel()
	text201 := strings.Repeat("b", 201)
	raw := RawEvent{
		EventID:   "evt-quote-201",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-quote-201"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m_quote_201",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      mustJSONString(t, map[string]any{"text": "ok", "quote": map[string]any{"message_id": "m_q", "text": text201}}),
					"mentions":     []map[string]any{},
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", "", raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if len([]rune(ev.QuotedText)) != 201 {
		t.Fatalf("QuotedText rune count = %d, want 201 (200 + ellipsis)", len([]rune(ev.QuotedText)))
	}
	if !strings.HasSuffix(ev.QuotedText, "…") {
		t.Fatalf("QuotedText should end with ellipsis, got %q", ev.QuotedText)
	}
	wantPrefix := strings.Repeat("b", 200)
	if !strings.HasPrefix(ev.QuotedText, wantPrefix) {
		t.Fatal("QuotedText prefix mismatch after truncation")
	}
}

// ---------------------------------------------------------------------------
// TC-8.5: quote 块无 text 字段
// ---------------------------------------------------------------------------

func TestNormaliseMessageReceive_QuoteNoText(t *testing.T) {
	t.Parallel()
	raw := RawEvent{
		EventID:   "evt-quote-notext",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-quote-notext"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m_quote_notext",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      `{"text":"reply","quote":{"message_id":"m_quoted_notext"}}`,
					"mentions":     []map[string]any{},
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", "", raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.QuotedMessageID != "m_quoted_notext" {
		t.Fatalf("QuotedMessageID = %q, want %q", ev.QuotedMessageID, "m_quoted_notext")
	}
	if ev.QuotedText != "" {
		t.Fatalf("QuotedText = %q, want empty", ev.QuotedText)
	}
}

// ---------------------------------------------------------------------------
// TC-8.6: quote 块含控制字符
// ---------------------------------------------------------------------------

func TestNormaliseMessageReceive_QuoteWithControlChars(t *testing.T) {
	t.Parallel()
	raw := RawEvent{
		EventID:   "evt-quote-ctrl",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-quote-ctrl"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m_quote_ctrl",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      `{"text":"reply","quote":{"message_id":"m_q","text":"line1\nline2\t tab"}}`,
					"mentions":     []map[string]any{},
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", "", raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.QuotedMessageID != "m_q" {
		t.Fatalf("QuotedMessageID = %q, want %q", ev.QuotedMessageID, "m_q")
	}
	// Control characters should be filtered/kept as-is depending on implementation;
	// the key requirement is: no panic, and the text is preserved in some form.
	if ev.QuotedText == "" {
		t.Fatal("QuotedText should not be empty")
	}
}

// ---------------------------------------------------------------------------
// TC-8.8: 普通文本消息无 quote/reply/thread
// ---------------------------------------------------------------------------

func TestNormaliseMessageReceive_PlainTextNoSignals(t *testing.T) {
	t.Parallel()
	raw := RawEvent{
		EventID:   "evt-plain",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-plain"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m_plain",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      `{"text":"just a message"}`,
					"mentions":     []map[string]any{},
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", "", raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
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
}

// ---------------------------------------------------------------------------
// TC-int.1: 完整显式信号透传链路（quote + reply + thread）
// ---------------------------------------------------------------------------

func TestNormaliseMessageReceive_FullSignals(t *testing.T) {
	t.Parallel()
	raw := RawEvent{
		EventID:   "evt-full",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-full"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m_full",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      `{"text":"I agree with this","quote":{"message_id":"m_quoted","text":"quoted text"}}`,
					"mentions":     []map[string]any{},
					"root_id":      "m_root",
					"parent_id":    "m_parent",
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", "", raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.ThreadID != "m_root" {
		t.Fatalf("ThreadID = %q, want %q", ev.ThreadID, "m_root")
	}
	if ev.ReplyToMessageID != "m_parent" {
		t.Fatalf("ReplyToMessageID = %q, want %q", ev.ReplyToMessageID, "m_parent")
	}
	if ev.QuotedMessageID != "m_quoted" {
		t.Fatalf("QuotedMessageID = %q, want %q", ev.QuotedMessageID, "m_quoted")
	}
	if ev.QuotedText != "quoted text" {
		t.Fatalf("QuotedText = %q, want %q", ev.QuotedText, "quoted text")
	}
}

func mustJSONString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
