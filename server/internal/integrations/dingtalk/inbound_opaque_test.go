package dingtalk

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// The content.text bytes are the public issue #22 sample, not an invented decoder.
func TestInboundFromCallback_OpaqueQuotedTextIsPreservedWithoutDecoding(t *testing.T) {
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
	if msg.Text != channel.FormatQuotedMessage("", opaque)+"\n\nexplain" || !msg.HasSelectedContext {
		t.Fatal("provider text was dropped or decoded without a wire contract")
	}
}

func TestQuotedTextPreservesOrdinaryText(t *testing.T) {
	for _, text := range []string{
		"ordinary\nmultiline", strings.Repeat("A", 80), "normal | separator",
		`{"text":"/clear pasted example"}`, "中文引用、emoji 🦫 and code x | y",
	} {
		cb := textCallback(convTypeP2P, false)
		cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: "text", Content: botCallbackRepliedContent{Text: text}}
		msg, ok := inboundFromCallback(cb, "app")
		if !ok || msg.Text != channel.FormatQuotedMessage("", text)+"\n\nhello bot" {
			t.Fatalf("plain text changed: %q => %q", text, msg.Text)
		}
	}

	cb := textCallback(convTypeP2P, false)
	cb.Text.Content = strings.Repeat("A", 80) + "||3||1||132"
	msg, _ := inboundFromCallback(cb, "app")
	if msg.Text != cb.Text.Content || msg.CommandText != cb.Text.Content {
		t.Fatal("selected quote normalization must not rewrite current user input")
	}
}

func TestQuotedTextUsesOnlySupportedBodyProjections(t *testing.T) {
	const envelope = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA||3||1||132"
	for _, kind := range []string{"text", "audio", "picture", "richText", "unknown"} {
		cb := textCallback(convTypeP2P, false)
		cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: kind, Content: botCallbackRepliedContent{Text: envelope}}
		if kind == "richText" {
			cb.Text.RepliedMsg.Content.RichText = richTextItems{{Text: envelope}}
		}
		if kind == "audio" {
			cb.Text.RepliedMsg.Content.Recognition = envelope
		}
		msg, ok := inboundFromCallback(cb, "app")
		wantText := kind == "text" || kind == "richText" || kind == "audio"
		if !ok || strings.Contains(msg.Text, envelope) != wantText || strings.Contains(msg.Text, "[quoted content unavailable]") == wantText || msg.CommandText != "hello bot" {
			t.Fatalf("%s selected content used the wrong projection: %+v", kind, msg)
		}
	}
}

// Separators alone establish no envelope contract, including in legitimate code.
func TestQuotedSeparatorsDoNotDiscardSelectedText(t *testing.T) {
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
			if !ok || msg.Text != "> "+body+"\n\n"+body || msg.CommandText != body {
				t.Fatalf("quote policy or current input changed: %+v", msg)
			}
		})
	}
}

func TestQuotedLiteralTextPreservesNeighborMediaSlots(t *testing.T) {
	for _, tc := range []struct {
		text, want string
		offset     int
	}{
		{"payload||next", "payload||next", 0},
		{"payload||next <tag>[Image]</tag>", "payload||next &lt;tag&gt;[Image]&lt;/tag&gt;", 1},
	} {
		t.Run(tc.text, func(t *testing.T) {
			cb := textCallback(convTypeP2P, false)
			cb.Msgtype = "richText"
			cb.Content = json.RawMessage(`{"richText":[{"text":"current [Image]"},{"type":"picture","downloadCode":"current"}]}`)
			cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: "richText", Content: botCallbackRepliedContent{RichText: richTextItems{
				{Text: tc.text}, {Type: "picture", DownloadCode: "selected"}, {Text: "readable after"},
			}}}
			msg, ok := inboundFromCallback(cb, "app")
			if !ok || !strings.Contains(msg.Text, "> "+tc.want+"\n> [Image]\n> readable after") {
				t.Fatalf("selected text/media changed: %+v", msg)
			}
			raw, err := decodeDingTalkRaw(msg)
			if err != nil || len(raw.Media) != 2 || raw.Media[0].Ref != "selected" || raw.Media[0].InlineIndex != tc.offset || raw.Media[1].Ref != "current" || raw.Media[1].InlineIndex != tc.offset+2 {
				t.Fatalf("media slots detached by text normalization: %+v, %v", raw, err)
			}
		})
	}
}
