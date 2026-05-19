package feishu

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/channel/port"
)

func TestNormaliseMessageReceive_BotMentioned_Group(t *testing.T) {
	t.Parallel()
	const botID = "ou_bot_1"
	raw := RawEvent{
		EventID:   "evt-1",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-1"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m1",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      `{"text":"@_user_1 hi"}`,
					"mentions": []map[string]any{
						{
							"key":  "@_user_1",
							"name": "Bot",
							"id":   map[string]any{"open_id": botID},
						},
					},
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", botID, raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if !ev.BotMentioned {
		t.Fatal("expected BotMentioned true when bot is in mentions")
	}
	if ev.Text != "hi" {
		t.Fatalf("text = %q", ev.Text)
	}
}

func TestNormaliseMessageReceive_BotMentioned_GroupNoMention(t *testing.T) {
	t.Parallel()
	const botID = "ou_bot_1"
	raw := RawEvent{
		EventID:   "evt-2",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-2"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m2",
					"chat_id":      "oc_g1",
					"chat_type":    "group",
					"message_type": "text",
					"content":      `{"text":"hello"}`,
					"mentions":     []map[string]any{},
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", botID, raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.BotMentioned {
		t.Fatal("expected BotMentioned false without @bot")
	}
}

func TestNormaliseMessageReceive_BotMentioned_Direct(t *testing.T) {
	t.Parallel()
	const botID = "ou_bot_1"
	raw := RawEvent{
		EventID:   "evt-3",
		EventType: "im.message.receive_v1",
		Payload: mustJSON(t, map[string]any{
			"header": map[string]any{"event_id": "evt-3"},
			"event": map[string]any{
				"sender": map[string]any{
					"sender_id":   map[string]any{"open_id": "ou_user"},
					"sender_type": "user",
				},
				"message": map[string]any{
					"message_id":   "m3",
					"chat_id":      "oc_p2p",
					"chat_type":    "p2p",
					"message_type": "text",
					"content":      `{"text":"dm hi"}`,
					"mentions":     []map[string]any{},
				},
			},
		}),
	}
	ev, ok, err := normaliseEvent("feishu", botID, raw)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if ev.ChatType != port.ChatTypeDirect {
		t.Fatalf("chat type = %v", ev.ChatType)
	}
	if !ev.BotMentioned {
		t.Fatal("expected BotMentioned true for direct chat")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
