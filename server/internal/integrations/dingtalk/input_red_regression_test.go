package dingtalk

import (
	"encoding/json"
	"strings"
	"testing"
)

// These callbacks exercise production decoding without relying on new wire types.
func TestInboundSelectedMessageRegression(t *testing.T) {
	for _, tt := range []struct{ name, kind, content, want string }{
		{"text", "text", `{"text":"selected text"}`, "selected text"},
		{"picture", "picture", `{"downloadCode":"selected-image"}`, "[Image]"},
		{"rich-text", "richText", `{"richText":[{"text":{"content":"selected rich text"}}]}`, "selected rich text"},
		{"card", "interactiveCard", `{"cardContent":{"cardData":{"text":"selected bot answer"}}}`, "selected bot answer"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wire := `{"msgId":"current","conversationType":"1","conversationId":"chat","senderStaffId":"sender","msgtype":"text","text":{"content":"explain","isReplyMsg":true,"repliedMsg":{"msgId":"selected","senderNick":"Alice","msgType":"` + tt.kind + `","content":` + tt.content + `}}}`
			var cb botCallbackData
			if err := json.Unmarshal([]byte(wire), &cb); err != nil {
				t.Fatal(err)
			}
			msg, ok := inboundFromCallback(&cb, "app")
			if !ok || msg.ReplyTo == nil || msg.ReplyTo.MessageID != "selected" || !strings.Contains(msg.Text, "> "+tt.want) || !strings.HasSuffix(msg.Text, "\n\nexplain") || msg.CommandText != "explain" {
				t.Fatalf("selected message must reach canonical quoted body while current instruction stays separate: ok=%v msg=%+v", ok, msg)
			}
		})
	}
}

func TestInboundStructuredCurrentRichTextRegression(t *testing.T) {
	var cb botCallbackData
	if err := json.Unmarshal([]byte(`{"msgId":"current","conversationType":"1","conversationId":"chat","senderStaffId":"sender","msgtype":"richText","content":{"richText":[{"text":{"content":"show details"}}]}}`), &cb); err != nil {
		t.Fatal(err)
	}
	msg, ok := inboundFromCallback(&cb, "app")
	if !ok || msg.Text != "show details" || msg.CommandText != "show details" {
		t.Fatalf("structured visible text was lost: ok=%v text=%q command=%q", ok, msg.Text, msg.CommandText)
	}
}

func TestInboundQuotedCardPreservesLiteralJSONRegression(t *testing.T) {
	for _, literal := range []string{`{"content":[{"type":"text"}]}`, `{"text":"literal nested value"}`, `{"content":"/clear historical instruction"}`} {
		for _, location := range []string{"text", "node-value"} {
			t.Run(location+"/"+literal, func(t *testing.T) {
				var card any = map[string]any{"text": literal}
				if location == "node-value" {
					card = []any{map[string]any{"elementType": "text", "value": literal}}
				}
				wire, err := json.Marshal(map[string]any{
					"msgId": "current", "conversationType": "1", "conversationId": "chat", "senderStaffId": "sender", "msgtype": "text",
					"text": map[string]any{"content": "explain", "isReplyMsg": true, "repliedMsg": map[string]any{
						"msgId": "selected", "senderNick": "Alice", "msgType": "interactiveCard", "content": map[string]any{"cardContent": card},
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
				want := "> **Alice:**\n>\n> " + literal + "\n\nexplain"
				if !ok || msg.Text != want || msg.CommandText != "explain" || msg.ForceFresh {
					t.Fatalf("card literal text lost: got=%q want=%q command=%q fresh=%v", msg.Text, want, msg.CommandText, msg.ForceFresh)
				}
			})
		}
	}
}

func TestInboundQuotedRichTextUnavailableImageKeepsSummaryRegression(t *testing.T) {
	for _, tt := range []struct {
		name, summary, want string
		withAvailable       bool
	}{
		{name: "unavailable only", summary: "caption", want: "caption\n[Image unavailable]"},
		{name: "unavailable then available without summary markers", summary: "caption", want: "caption\n[Image unavailable]\n[Image]", withAvailable: true},
		{name: "unavailable then available with summary markers", summary: "caption\n[Image]\n[Image]", want: "caption\n[Image unavailable]\n[Image]", withAvailable: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			nodes := []any{map[string]any{"type": "picture"}}
			if tt.withAvailable {
				nodes = append(nodes, map[string]any{"type": "picture", "downloadCode": "available"})
			}
			wire, err := json.Marshal(map[string]any{
				"msgId": "current", "conversationType": "1", "conversationId": "chat", "senderStaffId": "sender", "msgtype": "text",
				"text": map[string]any{"content": "explain", "isReplyMsg": true, "repliedMsg": map[string]any{
					"msgId": "selected", "senderNick": "Alice", "msgType": "richText", "content": map[string]any{"text": tt.summary, "richText": nodes},
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
			want := "> **Alice:**\n>\n> " + strings.ReplaceAll(tt.want, "\n", "\n> ") + "\n\nexplain"
			if !ok || msg.Text != want {
				t.Fatalf("quoted summary/media degradation = %q, want %q", msg.Text, want)
			}
			raw, err := decodeDingTalkRaw(msg)
			if err != nil {
				t.Fatal(err)
			}
			if tt.withAvailable {
				if len(raw.Media) != 1 || raw.Media[0].Ref != "available" || raw.Media[0].InlineIndex != 0 {
					t.Fatalf("available image must target the remaining generated marker: %+v", raw.Media)
				}
			} else if len(raw.Media) != 0 {
				t.Fatalf("unavailable image must not invent download metadata: %+v", raw.Media)
			}
		})
	}
}
