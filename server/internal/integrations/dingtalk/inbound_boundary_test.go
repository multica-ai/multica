package dingtalk

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestDingTalkCallbackWireDecodingBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		into func() any
	}{
		{name: "reply content is not an object", raw: `[]`, into: func() any { return &botCallbackRepliedContent{} }},
		{name: "rich text is not an array", raw: `{}`, into: func() any { return new(richTextItems) }},
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

func TestBotCallbackRepliedContentDoesNotRenderSummaryWhenRichTextMalformed(t *testing.T) {
	var reply botCallbackRepliedMessage
	if err := json.Unmarshal([]byte(`{"msgType":"richText","content":{"text":"readable summary","richText":{"unknown":"shape"}}}`), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Content.Text != "readable summary" || len(reply.Content.RichText) != 0 {
		t.Fatalf("malformed optional nodes hid the usable summary: %+v", reply.Content)
	}
	block, media := renderDingTalkQuotedMessage(&reply)
	if block != "> [quoted content unavailable]" || len(media) != 0 {
		t.Fatalf("best-effort quoted body/media = %q / %+v", block, media)
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
		{name: "unknown kind with text", content: botCallbackRepliedContent{Text: "selected"}, want: "> [quoted content unavailable]"},
		{name: "empty unknown", want: "> [quoted content unavailable]"},
		{name: "named file", kind: "file", content: botCallbackRepliedContent{FileName: "notes.txt"}, want: "> [File: notes.txt]"},
		{name: "unnamed file", kind: "file", want: "> [File]"},
		{name: "recognized audio", kind: "audio", content: botCallbackRepliedContent{Recognition: "spoken words"}, want: "> spoken words"},
		{name: "unrecognized audio", kind: "audio", want: "> [Audio message]"},
		{name: "video", kind: "video", want: "> [Video message]"},
		{name: "unavailable picture with summary", kind: "picture", content: botCallbackRepliedContent{Text: "selected caption"}, want: "> [Image unavailable]\n> [quoted content unavailable]"},
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

func TestDingTalkQuotedRichTextMissingMediaKeepsNodeOrder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content botCallbackRepliedContent
		want    string
		indexes []int
	}{
		{name: "empty", want: "[quoted content unavailable]"},
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
			want: "[quoted content unavailable]\n\n[Image]\n\n[Image]", indexes: []int{0, 1},
		},
		{
			name: "summary has one marker",
			content: botCallbackRepliedContent{Text: "caption\n[Image]", RichText: richTextItems{
				{Type: "picture", DownloadCode: "first"}, {Type: "picture", DownloadCode: "second"},
			}},
			want: "[quoted content unavailable]\n\n[Image]\n\n[Image]", indexes: []int{0, 1},
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
