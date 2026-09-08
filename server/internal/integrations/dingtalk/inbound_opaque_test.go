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
		"ordinary\nmultiline", strings.Repeat("A", 80), "normal | separator",
		`{"text":"/clear pasted example"}`, "中文引用、emoji 🦫 and code x | y",
	} {
		if got := dingTalkReadableQuotedText(text); got != text {
			t.Fatalf("plain text changed: %q => %q", text, got)
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
}

// These are conservative-policy examples, not claimed DingTalk wire variants.
func TestQuotedAmbiguousSeparatorsAreUnavailable(t *testing.T) {
	for _, body := range []string{
		"X||3||1||1", "prefix||3||1||132||4", "vNext:payload||version||kind||length",
		"opaque!?||3||1||132", "payload||||", "||", "legitimate a || b",
		`{"code":"a || b"}`, "ordinary prose with || inside",
	} {
		t.Run(body, func(t *testing.T) {
			cb := textCallback(convTypeP2P, false)
			cb.Text.Content = body
			cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: "text", Content: botCallbackRepliedContent{Text: body}}
			msg, ok := inboundFromCallback(cb, "app")
			if !ok || msg.Text != "> [quoted content unavailable]\n\n"+body || msg.CommandText != body {
				t.Fatalf("quote policy or current input changed: %+v", msg)
			}
		})
	}
}

func TestQuotedTextFallbackPreservesNeighborMediaSlots(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Content = json.RawMessage(`{"richText":[{"text":"current [Image]"},{"type":"picture","downloadCode":"current"}]}`)
	cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: "richText", Content: botCallbackRepliedContent{RichText: richTextItems{
		{Text: "payload||next"}, {Type: "picture", DownloadCode: "selected"}, {Text: "readable after"},
	}}}
	msg, ok := inboundFromCallback(cb, "app")
	if !ok || !strings.Contains(msg.Text, "> [quoted content unavailable]\n> [Image]\n> readable after") || strings.Contains(msg.Text, "payload") {
		t.Fatalf("selected text/media fallback: %+v", msg)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 2 || raw.Media[0].Ref != "selected" || raw.Media[0].InlineIndex != 0 || raw.Media[1].Ref != "current" || raw.Media[1].InlineIndex != 2 {
		t.Fatalf("media slots detached by text fallback: %+v, %v", raw, err)
	}
}
