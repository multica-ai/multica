package dingtalk

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQuotedRichTextWrappersPreserveTextAroundPictures(t *testing.T) {
	for _, node := range []string{
		`{"text":{"text":"111"}}`,
		`{"text":{"content":"111"}}`,
		`{"msgType":"text","content":"111"}`,
		`{"msgType":"text","content":{"text":"111"}}`,
		`{"type":"text","content":{"content":"111"}}`,
	} {
		t.Run(node, func(t *testing.T) {
			wire := `{"senderStaffId":"sender","conversationId":"group","conversationType":"2","isInAtList":true,"msgId":"current","msgtype":"richText","content":{"richText":[{"text":"222"},{"type":"picture","downloadCode":"current-image"},{"text":"333"}],"repliedMsg":{"msgType":"richText","msgId":"selected","content":{"richText":[` + node + `,{"msgType":"picture","downloadCode":"quoted-image"},{"msgType":"text","content":{"text":"/clear historical question"}}]}}}}`
			var cb botCallbackData
			if err := json.Unmarshal([]byte(wire), &cb); err != nil {
				t.Fatal(err)
			}
			msg, ok := inboundFromCallback(&cb, "app")
			const want = "> 111\n> [Image]\n> /clear historical question\n\n222\n[Image]\n333"
			if !ok || msg.Text != want || msg.CommandText != "222333" || msg.ForceFresh {
				t.Fatalf("quoted rich text lost text/order or executed a historical command: %+v, want %q", msg, want)
			}
			raw, err := decodeDingTalkRaw(msg)
			if err != nil || len(raw.Media) != 2 || raw.Media[0].Ref != "quoted-image" || raw.Media[0].InlineIndex != 0 || raw.Media[1].Ref != "current-image" || raw.Media[1].InlineIndex != 1 {
				t.Fatalf("quoted/current image positions changed: %+v, %v", raw, err)
			}
		})
	}
}

func TestQuotedRichTextWrappersDoNotDecodeArbitraryEnvelopes(t *testing.T) {
	for _, node := range []string{
		`{"msgType":"text","content":{"data":{"text":"hidden"}}}`,
		`{"msgType":"text","content":{"text":{"text":"hidden"}}}`,
		`{"msgType":42,"content":"hidden"}`,
		`{"msgType":"audio","content":"hidden"}`,
		`{"msgType":"text","content":["hidden"]}`,
	} {
		t.Run(node, func(t *testing.T) {
			var content botCallbackRepliedContent
			if err := json.Unmarshal([]byte(`{"richText":[`+node+`,{"text":"valid neighbor"}]}`), &content); err != nil {
				t.Fatal(err)
			}
			body, _ := renderDingTalkQuotedMessage(&botCallbackRepliedMessage{MsgType: "richText", Content: content})
			if strings.Contains(body, "hidden") || !strings.Contains(body, "[rich-text content unavailable]") || !strings.Contains(body, "valid neighbor") {
				t.Fatalf("unsupported wrapper changed valid neighbors: %q", body)
			}
		})
	}
}

func TestInboundQuotedRichTextNodeTextBoundaries(t *testing.T) {
	for _, tc := range []struct{ node, want string }{
		{`{"text":{"content":"/clear historical"}}`, "/clear historical"},
		{`{"text":{"text":"/new historical"}}`, "/new historical"},
		{`{"type":"text","content":"/issue historical"}`, "/issue historical"},
		{`{"msgType":"text","content":{"text":"selected [Image]"}}`, "selected [Image]"},
		{`{"msgType":"text","content":{"content":"selected text"}}`, "selected text"},
		{`{"msgType":"text","content":"{\"text\":\"/clear literal JSON\"}"}`, `{"text":"/clear literal JSON"}`},
		{`{"msgType":"text","text":"primary","content":"secondary"}`, "primary"},
		{`{"msgType":"text","text":"","content":"not selected"}`, ""},
		{`{"msgType":"audio","downloadCode":"not-an-image","content":"hidden"}`, "[rich-text content unavailable]"},
		{`{"msgType":42,"text":"hidden"}`, "[rich-text content unavailable]"},
		{`{"msgType":"text","content":{"data":{"text":"hidden"}}}`, "[rich-text content unavailable]"},
		{`{"msgType":"text","content":{"text":{"content":"hidden"}}}`, "[rich-text content unavailable]"},
		{`{"msgType":"text","content":[{"text":"hidden"}]}`, "[rich-text content unavailable]"},
		{`{"msgType":"text","content":42}`, "[rich-text content unavailable]"},
		{`{"data":{"text":"hidden"}}`, "[rich-text content unavailable]"},
	} {
		t.Run(tc.node, func(t *testing.T) {
			var cb botCallbackData
			wire := `{"senderStaffId":"sender","conversationType":"1","msgtype":"text","text":{"content":"explain","repliedMsg":{"msgId":"selected","msgType":"richText","content":{"richText":[` + tc.node + `,{"msgType":"picture","downloadCode":"quoted-image"},{"text":"caption"}]}}}}`
			if err := json.Unmarshal([]byte(wire), &cb); err != nil {
				t.Fatal(err)
			}
			msg, ok := inboundFromCallback(&cb, "app")
			want := "> [Image]\n> caption\n\nexplain"
			if tc.want != "" {
				want = "> " + tc.want + "\n" + want
			}
			if !ok || msg.Text != want || msg.CommandText != "explain" || msg.ForceFresh || !msg.HasSelectedContext || msg.ReplyTo == nil || msg.ReplyTo.MessageID != "selected" {
				t.Fatalf("selected text changed content or command meaning: %+v; want body=%q", msg, want)
			}
			raw, err := decodeDingTalkRaw(msg)
			if err != nil || len(raw.Media) != 1 || raw.Media[0].Ref != "quoted-image" || raw.Media[0].InlineIndex != strings.Count(tc.want, "[Image]") || raw.CurrentText != "explain" {
				t.Fatalf("selected text changed media position or current preview: %+v; err=%v", raw, err)
			}
		})
	}
}
