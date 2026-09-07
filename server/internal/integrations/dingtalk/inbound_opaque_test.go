package dingtalk

import (
	"encoding/json"
	"strings"
	"testing"
)

// The content.text bytes are the public issue #22 sample, not an invented decoder.
func TestInboundFromCallback_OpaqueQuotedTextDoesNotReachAgent(t *testing.T) {
	const opaque = "vJCRdgNhBZ/leSxHkTuYF+BWa+7kxCUXN4tJUE30cCDYqcnXH8wxZV+CRcfg+qVjDuO\n3CaGDQCiZkYV9zU22l/EUwlTXBckN7vCeSiK3yXDWRCxxE4Zvi3xyKh0jMaHVnq5Qi\ng3ishGYrFSSvLujbT1parH5jWjNLrErP6UxR/J3zCHDw7Slef69EFJvny152iByE\n||3||1||132"
	wire, err := json.Marshal(map[string]any{
		"msgId": "current", "conversationType": "1", "conversationId": "chat", "senderStaffId": "sender", "msgtype": "text",
		"text": map[string]any{"content": "explain", "isReplyMsg": true, "repliedMsg": map[string]any{
			"msgId": "selected", "msgType": "text", "content": map[string]any{"text": opaque},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var cb botCallbackData
	if err := json.Unmarshal(wire, &cb); err != nil {
		t.Fatal(err)
	}
	msg, ok := inboundFromCallback(&cb, "app")
	if !ok || msg.CommandText != "explain" {
		t.Fatal("current input not retained")
	}
	if msg.Text != "> [quoted content unavailable]\n\nexplain" || !msg.HasSelectedContext {
		t.Fatal("opaque provider payload passed through as quoted user text")
	}
}

func TestQuotedOpaqueDetectionPreservesOrdinaryText(t *testing.T) {
	for _, text := range []string{
		"ordinary\nmultiline", "example||1||2||3", strings.Repeat("A", 80),
		strings.Repeat("A", 80) + "||one||2||3", strings.Repeat("A", 80) + "||1||2||3||4",
		`{"text":"/clear pasted example"}`, strings.Repeat("x", 80) + " words||1||2||3",
	} {
		if got := dingTalkReadableQuotedText(text); got != text {
			t.Fatalf("ordinary text changed: %q => %q", text, got)
		}
	}
	cb := textCallback(convTypeP2P, false)
	cb.Text.Content = strings.Repeat("A", 80) + "||3||1||132"
	msg, _ := inboundFromCallback(cb, "app")
	if msg.Text != cb.Text.Content || msg.CommandText != cb.Text.Content {
		t.Fatal("opaque detector must not rewrite current user input")
	}
}

func TestOpaqueQuoteFallbackAcrossReadableBodyProjections(t *testing.T) {
	const envelope = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA||3||1||132"
	for _, kind := range []string{"text", "picture", "richText", "unknown"} {
		cb := textCallback(convTypeP2P, false)
		cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: kind, Content: botCallbackRepliedContent{Text: envelope}}
		if kind == "richText" {
			cb.Text.RepliedMsg.Content.RichText = richTextItems{{Text: envelope}}
		}
		msg, ok := inboundFromCallback(cb, "app")
		if !ok || !strings.Contains(msg.Text, "[quoted content unavailable]") || strings.Contains(msg.Text, "AAAAAAAA") || msg.CommandText != "hello bot" {
			t.Fatalf("opaque %s projection was not degraded: %+v", kind, msg)
		}
	}
	for _, text := range []string{strings.Repeat("A", 64) + "||||1||132", strings.Repeat("A", 63) + "||3||1||132"} {
		if dingTalkReadableQuotedText(text) != text {
			t.Fatalf("outside the bounded envelope should be preserved: %q", text)
		}
	}
}
