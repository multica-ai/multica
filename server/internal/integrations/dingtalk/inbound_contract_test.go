package dingtalk

import (
	"encoding/json"
	"strings"
	"testing"
)

// Synthetic boundary fixtures, not captured callbacks. The supported node
// schema comes from https://open.dingtalk.com/document/orgapp/receive-message.
// Its public contract does not describe repliedMsg, structured text aliases,
// encoded richText arrays, or template-independent cardContent rendering.
func TestInboundRichTextContractKeepsSupportedNeighbors(t *testing.T) {
	for _, node := range []string{
		`{"text":{"content":"/clear hidden"}}`,
		`{"content":"/new hidden"}`, `{"data":{"text":"/issue hidden"}}`,
		`{"msgType":"text","content":"hidden"}`, `{"type":42}`,
		`{"type":"audio","downloadCode":"not-an-image"}`, `42`, `null`,
	} {
		t.Run(node, func(t *testing.T) {
			cb := textCallback(convTypeP2P, false)
			cb.Msgtype = "richText"
			cb.Content = json.RawMessage(`{"richText":[` + node + `,{"text":" current question"},{"type":"picture","downloadCode":"known-image"}]}`)
			msg, ok := inboundFromCallback(cb, "app")
			if !ok || msg.Text != "[rich-text content unavailable] current question\n[Image]" || msg.CommandText != "[rich-text content unavailable] current question" || msg.ForceFresh {
				t.Fatalf("unsupported node changed supported input or command meaning: %+v", msg)
			}
			raw, err := decodeDingTalkRaw(msg)
			if err != nil || len(raw.Media) != 1 || raw.Media[0].Ref != "known-image" || raw.Media[0].InlineIndex != 0 {
				t.Fatalf("neighbor picture lost: %+v, %v", raw, err)
			}
		})
	}
}

func TestInboundQuotedUnsupportedStructuresDegrade(t *testing.T) {
	for _, tt := range []struct{ kind, content string }{
		{"text", `{"text":{"content":"hidden"}}`},
		{"richText", `{"text":"preview [Image]","richText":"[{\"text\":\"hidden\"}]"}`},
		{"richText", `{"text":"preview [Image]","richText":{"text":"hidden"}}`},
		{"interactiveCard", `{"text":"hidden","cardContent":{"cardData":{"cardParamMap":{"markdown_content":"hidden","text":"different hidden"}}}}`},
		{"interactiveCard", `{"cardContent":"{\"text\":\"hidden\"}"}`},
		{"interactiveCard", `{"cardContent":[{"elementType":"paragraph","children":[{"value":"hidden"}]}]}`},
	} {
		t.Run(tt.kind+tt.content, func(t *testing.T) {
			var cb botCallbackData
			wire := `{"senderStaffId":"sender","conversationType":"1","msgtype":"text","text":{"content":"current question","repliedMsg":{"msgId":"selected","senderNick":"Alice","msgType":"` + tt.kind + `","content":` + tt.content + `}}}`
			if err := json.Unmarshal([]byte(wire), &cb); err != nil {
				t.Fatal(err)
			}
			msg, ok := inboundFromCallback(&cb, "app")
			if !ok || !msg.HasSelectedContext || msg.CommandText != "current question" || msg.ReplyTo == nil || msg.ReplyTo.MessageID != "selected" || !strings.HasSuffix(msg.Text, "\n\ncurrent question") || strings.Contains(msg.Text, "hidden") || strings.Contains(msg.Text, "preview") {
				t.Fatalf("unsupported quote leaked inferred text or lost current input: %+v", msg)
			}
			if !strings.Contains(msg.Text, "unavailable]") && !strings.Contains(msg.Text, "unsupported message]") {
				t.Fatalf("missing explicit fallback: %q", msg.Text)
			}
		})
	}
}

func TestInboundBareControlMarksSelectedContext(t *testing.T) {
	for _, command := range []string{"/new", "/clear"} {
		for _, kind := range []string{"text", "picture", "interactiveCard"} {
			t.Run(command+kind, func(t *testing.T) {
				cb := textCallback(convTypeP2P, false)
				cb.Text.Content = command
				cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgId: "selected", MsgType: kind, Content: botCallbackRepliedContent{Text: "/issue historical", DownloadCode: "quoted-picture"}}
				msg, ok := inboundFromCallback(cb, "app")
				if !ok || !msg.HasSelectedContext || msg.CommandText != command || msg.ForceFresh != (command == "/clear") || strings.Contains(msg.Text, command) || msg.Text == "" {
					t.Fatalf("selected control input = %+v", msg)
				}
			})
		}
	}
	cb := textCallback(convTypeP2P, false)
	cb.OriginalMsgId = "thread-only"
	msg, _ := inboundFromCallback(cb, "app")
	if msg.HasSelectedContext {
		t.Fatal("reply coordinates alone must not claim projected content")
	}
}

func TestInboundRichTextUnsupportedEnvelopeKeepsQuote(t *testing.T) {
	for _, content := range []string{`{}`, `{"richText":null}`, `{"richText":[]}`, `{"richText":{}}`, `{"richText":"[{\"text\":\"/clear hidden\"}]"}`} {
		cb := textCallback(convTypeP2P, false)
		cb.Msgtype = "richText"
		cb.Content = json.RawMessage(content)
		cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: "text", Content: botCallbackRepliedContent{Text: "selected"}}
		msg, ok := inboundFromCallback(cb, "app")
		if !ok || msg.Text != "> selected\n\n[rich-text content unavailable]" || msg.CommandText != "[rich-text content unavailable]" || msg.ForceFresh {
			t.Fatalf("unavailable envelope lost quote or invented current content: %+v", msg)
		}
	}
}

func TestInboundExplicitQuoteWithoutSnapshotIsUnavailableInput(t *testing.T) {
	for _, command := range []string{"/new", "/clear"} {
		cb := textCallback(convTypeP2P, false)
		cb.Text.Content = command
		cb.Text.IsReplyMsg = true
		msg, ok := inboundFromCallback(cb, "app")
		if !ok || !msg.HasSelectedContext || msg.Text != "> [quoted content unavailable]" || msg.CommandText != command || msg.ForceFresh != (command == "/clear") {
			t.Fatalf("explicit empty quote = %+v", msg)
		}
	}
}

func TestInboundMalformedOptionalQuoteNeverRejectsCurrentMessage(t *testing.T) {
	for _, tc := range []struct{ snapshot, body string }{
		{`42`, "[quoted content unavailable]"},
		{`[]`, "[quoted content unavailable]"},
		{`{"msgType":"text","msgId":42,"senderId":{},"senderNick":[],"content":{"text":"readable selected text"}}`, "readable selected text"},
		{`{"msgType":42,"content":{"text":"not a verified text kind"}}`, "[quoted content unavailable]"},
		{`{"msgType":"text","content":42}`, "[quoted content unavailable]"},
	} {
		t.Run(tc.snapshot, func(t *testing.T) {
			var cb botCallbackData
			wire := `{"senderStaffId":"sender","conversationType":"1","msgtype":"text","text":{"content":"current question","isReplyMsg":true,"repliedMsg":` + tc.snapshot + `}}`
			if err := json.Unmarshal([]byte(wire), &cb); err != nil {
				t.Fatalf("optional quote rejected current message: %v", err)
			}
			msg, ok := inboundFromCallback(&cb, "app")
			if !ok || !msg.HasSelectedContext || msg.CommandText != "current question" || msg.Text != "> "+tc.body+"\n\ncurrent question" {
				t.Fatalf("optional quote destroyed current input: %+v", msg)
			}
		})
	}
}
