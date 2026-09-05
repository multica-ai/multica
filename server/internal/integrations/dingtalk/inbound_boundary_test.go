package dingtalk

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestDingTalkCallbackWireDecodingBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		into func() any
	}{
		{name: "reply metadata has wrong scalar type", raw: `{"msgId":42}`, into: func() any { return &botCallbackRepliedMessage{} }},
		{name: "reply content is not an object", raw: `[]`, into: func() any { return &botCallbackRepliedContent{} }},
		{name: "rich text is not an array", raw: `{}`, into: func() any { return new(richTextItems) }},
		{name: "rich text member has wrong scalar type", raw: `[{"type":42}]`, into: func() any { return new(richTextItems) }},
		{name: "text node has wrong scalar type", raw: `{"type":42}`, into: func() any { return &richTextItem{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tc.raw), tc.into()); err == nil {
				t.Fatalf("invalid wire shape %s was silently accepted", tc.raw)
			}
		})
	}
	for _, raw := range []string{"null", "[]"} {
		items := richTextItems{{Text: "old content"}}
		if err := json.Unmarshal([]byte(raw), &items); err != nil || len(items) != 0 {
			t.Fatalf("empty RichText should clear old nodes: raw=%q nodes=%+v err=%v", raw, items, err)
		}
	}
}

func TestBotCallbackRepliedContentKeepsTextWhenRichTextMalformed(t *testing.T) {
	var reply botCallbackRepliedMessage
	if err := json.Unmarshal([]byte(`{"msgType":"richText","content":{"text":"readable summary","richText":{"unknown":"shape"}}}`), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Content.Text != "readable summary" || len(reply.Content.RichText) != 0 {
		t.Fatalf("malformed optional nodes hid the usable summary: %+v", reply.Content)
	}
	block, media := renderDingTalkQuotedMessage(&reply)
	if block != "> readable summary" || len(media) != 0 {
		t.Fatalf("best-effort quoted body/media = %q / %+v", block, media)
	}
}

func TestDingTalkRichTextNodeTextBoundaries(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{name: "absent"},
		{name: "null", raw: "null"},
		{name: "invalid", raw: "{"},
		{name: "unsupported scalar", raw: "42"},
		{name: "structural array", raw: `[{"text":"first "},{"content":{"value":"second"}},42]`, want: "first second"},
		{name: "literal object text", raw: `"{\"text\":\"/clear example\"}"`, want: `{"text":"/clear example"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dingTalkRichTextNodeText(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDingTalkReplyOptionalContextBoundaries(t *testing.T) {
	msg := channel.InboundMessage{Text: "current", CommandText: "current"}
	raw := dingtalkRawEvent{}
	data := &botCallbackData{}
	applyDingTalkReplyContext(nil, &msg, &raw)
	applyDingTalkReplyContext(data, nil, &raw)
	applyDingTalkReplyContext(data, &msg, nil)
	if msg.Text != "current" || msg.ReplyTo != nil {
		t.Fatalf("missing context mutated current turn: %+v / %+v", msg, raw)
	}
	data.Text.IsReplyMsg = true
	data.Content = json.RawMessage(`{`)
	metadata := dingTalkReplyMetadata(data)
	if !metadata.IsReplyMsg || metadata.RepliedMsg != nil {
		t.Fatalf("unreadable content hid valid text metadata: %+v", metadata)
	}
	if block, media := renderDingTalkQuotedMessage(nil); block != "" || len(media) != 0 {
		t.Fatalf("missing reply invented context: %q / %+v", block, media)
	}
}

func TestInboundFromCallback_QuotedFallbackKinds(t *testing.T) {
	for _, tc := range []struct {
		name, kind, senderID, want string
		content                    botCallbackRepliedContent
	}{
		{name: "sender id omitted without nickname", kind: "text", senderID: "platform author", content: botCallbackRepliedContent{Text: "selected"}, want: "> selected"},
		{name: "unknown kind with text", content: botCallbackRepliedContent{Text: "selected"}, want: "> selected"},
		{name: "empty unknown", want: "> [empty or unsupported message]"},
		{name: "named file", kind: "file", content: botCallbackRepliedContent{FileName: "notes.txt"}, want: "> [File: notes.txt]"},
		{name: "unnamed file", kind: "file", want: "> [File]"},
		{name: "recognized audio", kind: "audio", content: botCallbackRepliedContent{Recognition: "spoken words"}, want: "> spoken words"},
		{name: "unrecognized audio", kind: "audio", want: "> [Audio message]"},
		{name: "video", kind: "video", want: "> [Video message]"},
		{name: "unavailable picture with summary", kind: "picture", content: botCallbackRepliedContent{Text: "selected caption"}, want: "> [Image unavailable]\n> selected caption"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cb := textCallback(convTypeP2P, false)
			cb.Text.Content = "current"
			cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: tc.kind, SenderId: tc.senderID, Content: tc.content}
			msg, ok := inboundFromCallback(cb, "app-key")
			if !ok || msg.Text != tc.want+"\n\ncurrent" || msg.CommandText != "current" || msg.ReplyTo == nil {
				t.Fatalf("quoted fallback = %+v, want %q then current", msg, tc.want)
			}
		})
	}
}

func TestDingTalkQuotedRichTextMissingMediaAndSummaryLayout(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content botCallbackRepliedContent
		want    string
		indexes []int
	}{
		{name: "empty"},
		{name: "literal unavailable text remains prose", content: botCallbackRepliedContent{Text: "provider preview", RichText: richTextItems{{Text: "[Image unavailable]"}, {Type: "picture"}}}, want: "[Image unavailable]\n[Image unavailable]"},
		{name: "unavailable first image", content: botCallbackRepliedContent{RichText: richTextItems{{Type: "picture"}}}, want: "[Image unavailable]"},
		{
			name: "unavailable middle image",
			content: botCallbackRepliedContent{RichText: richTextItems{
				{Text: "before"}, {Type: "picture"}, {Type: "picture", DownloadCode: "available"},
			}},
			want: "before\n[Image unavailable]\n[Image]", indexes: []int{0},
		},
		{
			name: "summary has no markers",
			content: botCallbackRepliedContent{Text: "two views", RichText: richTextItems{
				{Type: "picture", DownloadCode: "first"}, {Type: "picture", DownloadCode: "second"},
			}},
			want: "two views\n[Image]\n\n[Image]", indexes: []int{0, 1},
		},
		{
			name: "summary has one marker",
			content: botCallbackRepliedContent{Text: "caption\n[Image]", RichText: richTextItems{
				{Type: "picture", DownloadCode: "first"}, {Type: "picture", DownloadCode: "second"},
			}},
			want: "caption\n[Image]\n[Image]", indexes: []int{0, 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, media := renderDingTalkQuotedRichText(tc.content, 0)
			var indexes []int
			for _, resource := range media {
				indexes = append(indexes, resource.InlineIndex)
			}
			if body != tc.want || !reflect.DeepEqual(indexes, tc.indexes) {
				t.Fatalf("layout/indexes = %q / %v, want %q / %v", body, indexes, tc.want, tc.indexes)
			}
		})
	}
}

func TestDingTalkCardTextBoundaryShapes(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{name: "absent"},
		{name: "null", raw: "null"},
		{name: "malformed", raw: "{"},
		{name: "blank", raw: `"  "`},
		{name: "blank child between visible nodes", raw: `[{"elementType":"text","value":"first"},{"elementType":"text","value":"   "},{"elementType":"text","value":"second"}]`, want: "first\n\nsecond"},
		{name: "plain leaf", raw: `{"value":"plain body"}`, want: "plain body"},
		{name: "wrapped leaf", raw: `{"value":{"text":"nested body"}}`, want: "nested body"},
		{name: "structured body", raw: `{"text":{"value":"nested body"}}`, want: "nested body"},
		{name: "structured node value", raw: `{"elementType":"text","value":{"text":"nested body"}}`, want: "nested body"},
		{name: "empty unknown node", raw: `{"elementType":"unknown"}`},
		{name: "scalar list children", raw: `{"elementType":"unorderedList","children":["first","second"]}`, want: "- first\n- second"},
		{name: "scalar inline children", raw: `{"elementType":"paragraph","children":["first","second"]}`, want: "firstsecond"},
		{name: "untyped children", raw: `{"children":[{"value":"first "},{"value":"second"}]}`, want: "first second"},
		{name: "literal JSON leaf with node signature", raw: `{"value":"{\"content\":[{\"data\":{\"text\":\"previous question\"},\"style\":{},\"type\":\"text\"}]}"}`, want: `{"content":[{"data":{"text":"previous question"},"style":{},"type":"text"}]}`},
		{name: "ordinary JSON leaf", raw: `{"value":"{\"answer\":42}"}`, want: `{"answer":42}`},
		{name: "node Markdown fallback", raw: `{"elementType":"text","markdown":"fallback body"}`, want: "fallback body"},
		{name: "scalar children", raw: `{"elementType":"paragraph","children":"body"}`, want: "body"},
		{name: "invalid list children", raw: `{"elementType":"unorderedList","children":{}}`},
		{name: "empty list item", raw: `{"elementType":"unorderedList","children":[{"elementType":"listItem","children":[]},{"elementType":"listItem","children":[{"value":"visible"}]}]}`, want: "- visible"},
		{name: "ordered list", raw: `{"elementType":"orderedList","children":[{"elementType":"listItem","children":[{"value":"first"}]},{"elementType":"listItem","children":[{"value":"second"}]}]}`, want: "1. first\n2. second"},
		{name: "link leaf supplies label", raw: `{"elementType":"link","href":"https://example.com","value":"label"}`, want: "[label](https://example.com)"},
		{name: "link without href", raw: `{"elementType":"link","text":"label"}`, want: "label"},
		{name: "link without label", raw: `{"elementType":"link","href":"https://example.com"}`, want: "[https://example.com](https://example.com)"},
		{name: "link Markdown escaping", raw: `{"elementType":"link","href":"https://example.com/a)b","text":"[label]"}`, want: `[\[label\]](https://example.com/a%29b)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dingTalkCardText(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("card body = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInboundQuotedCardPreservesInlineWhitespace(t *testing.T) {
	for _, tc := range []struct{ name, children, want string }{
		{
			name:     "separate space between words",
			children: `[{"elementType":"text","value":"first"},{"elementType":"text","value":" "},{"elementType":"text","value":"second"}]`,
			want:     "first second",
		},
		{
			name:     "spaces around a link",
			children: `[{"elementType":"text","value":"see"},{"elementType":"text","value":" "},{"elementType":"link","href":"https://example.com","text":"details"},{"elementType":"text","value":" "},{"elementType":"text","value":"today"}]`,
			want:     "see [details](https://example.com) today",
		},
		{
			name:     "space after paragraph break",
			children: `[{"elementType":"text","value":"first"},{"elementType":"paragraphBreak"},{"elementType":"text","value":" "},{"elementType":"text","value":"second"}]`,
			want:     "first\n\nsecond",
		},
		{
			name:     "empty block between words",
			children: `[{"elementType":"text","value":"first"},{"elementType":"paragraph","children":[{"value":" "}]},{"elementType":"text","value":"second"}]`,
			want:     "firstsecond",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cb := textCallback(convTypeP2P, false)
			cb.Text.RepliedMsg = &botCallbackRepliedMessage{
				MsgType: "interactiveCard", SenderNick: "Alice",
				Content: botCallbackRepliedContent{CardContent: json.RawMessage(`{"elementType":"paragraph","children":` + tc.children + `}`)},
			}
			msg, ok := inboundFromCallback(cb, "app-key")
			want := "> **Alice:**\n>\n> " + strings.ReplaceAll(tc.want, "\n\n", "\n>\n> ") + "\n\nhello bot"
			if !ok || msg.Text != want || msg.CommandText != "hello bot" {
				t.Fatalf("quoted card = %q, command = %q; want %q and unchanged current command", msg.Text, msg.CommandText, want)
			}
		})
	}
}

func TestInboundQuotedCardNestedRenderingAllocationBudget(t *testing.T) {
	var card any = map[string]any{"elementType": "text", "value": "selected answer"}
	for range 16 {
		card = map[string]any{"elementType": "paragraph", "children": []any{card}}
	}
	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	cb := textCallback(convTypeP2P, false)
	cb.Text.RepliedMsg = &botCallbackRepliedMessage{
		MsgType: "interactiveCard", SenderNick: "Alice",
		Content: botCallbackRepliedContent{CardContent: raw},
	}
	allocations := testing.AllocsPerRun(1, func() {
		msg, ok := inboundFromCallback(cb, "app-key")
		if !ok || msg.Text != "> **Alice:**\n>\n> selected answer\n\nhello bot" || msg.CommandText != "hello bot" {
			t.Fatalf("nested card lost selected/current text: %+v", msg)
		}
	})
	// A sub-kilobyte card should stay well below this deliberately generous
	// budget. Repeated subtree traversal previously needed over 500,000
	// allocations; a wall-clock limit would be sensitive to machine load.
	if allocations > 10_000 {
		t.Fatalf("small nested card allocated %.0f times, want at most 10000", allocations)
	}
}

func TestDingTalkQuotedSummaryMediaSlotBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, summary, want string
		available           []bool
		indexes             []int
	}{
		{name: "available then unavailable", summary: "caption\n[Image]\n[Image]", available: []bool{true, false}, want: "caption\n[Image]\n[Image unavailable]", indexes: []int{2}},
		{name: "extra summary marker remains literal", summary: "caption [Image] [Image] [Image]", available: []bool{false, true}, want: "caption [Image unavailable] [Image] [Image]", indexes: []int{2}},
		{name: "partial summary markers", summary: "caption [Image]", available: []bool{false, true}, want: "caption [Image unavailable]\n[Image]", indexes: []int{2}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body, resources := renderDingTalkQuotedSummaryMedia(tt.summary, tt.available, []dingtalkMediaResource{{Ref: "image"}}, 2)
			if body != tt.want || len(resources) != len(tt.indexes) {
				t.Fatalf("summary layout/resources = %q/%+v, want %q/%v", body, resources, tt.want, tt.indexes)
			}
			for i, index := range tt.indexes {
				if resources[i].InlineIndex != index {
					t.Fatalf("resource index=%d want=%d", resources[i].InlineIndex, index)
				}
			}
		})
	}
}
