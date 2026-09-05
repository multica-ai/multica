package dingtalk

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func textCallback(convType string, inAtList bool) *botCallbackData {
	return &botCallbackData{
		MsgId:            "msg-1",
		Msgtype:          "text",
		ConversationId:   "cid-123",
		ConversationType: convType,
		SenderStaffId:    "staff-9",
		IsInAtList:       inAtList,
		Text:             botCallbackText{Content: "  hello bot  "},
	}
}

func TestInboundFromCallback_P2PAddressedAndTrimmed(t *testing.T) {
	msg, ok := inboundFromCallback(textCallback(convTypeP2P, false), "appkey-A")
	if !ok {
		t.Fatal("expected a 1:1 text message to be ingested")
	}
	if msg.Source.ChatType != channel.ChatTypeP2P || !msg.AddressedToBot {
		t.Errorf("1:1 must be p2p + addressed: %+v", msg.Source)
	}
	if msg.Text != "hello bot" || msg.CommandText != msg.Text {
		t.Errorf("text/command = %q/%q", msg.Text, msg.CommandText)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || raw.AppID != "appkey-A" {
		t.Fatalf("raw routing context = %+v, err=%v", raw, err)
	}
}

func TestInboundFromCallback_GroupAddressing(t *testing.T) {
	callback := textCallback(convTypeGroup, true)
	callback.ConversationTitle = "  Platform team  "
	if msg, ok := inboundFromCallback(callback, "a"); !ok || !msg.AddressedToBot {
		t.Fatalf("group mention must be addressed: ok=%v msg=%+v", ok, msg)
	} else if raw, err := decodeDingTalkRaw(msg); err != nil || raw.ConversationTitle != "Platform team" {
		t.Fatalf("group title metadata = %q, err=%v", raw.ConversationTitle, err)
	}
	msg, ok := inboundFromCallback(textCallback(convTypeGroup, false), "a")
	if !ok || msg.AddressedToBot {
		t.Fatalf("unmentioned group message must reach the shared filter unaddressed: ok=%v msg=%+v", ok, msg)
	}
}

func TestInboundFromCallback_LeavesFreshCommandForSharedRouter(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Text.Content = "  /clear start over  "
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok {
		t.Fatal("expected /clear message")
	}
	if msg.Text != "/clear start over" || msg.CommandText != msg.Text || msg.ForceFresh {
		t.Fatalf("DingTalk must leave /clear classification to the shared Router: %+v", msg)
	}
}

func TestInboundFromCallback_BareFreshStaysForSharedPendingPath(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Text.Content = " /clear "
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.Text != "/clear" || msg.CommandText != "/clear" || msg.ForceFresh {
		t.Fatalf("bare /clear must remain visible to the shared Router: ok=%v msg=%+v", ok, msg)
	}
}

func TestInboundFromCallback_DoesNotPrivatelyComposeFreshAndIssue(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Text.Content = "/clear /issue calculate 1+2"
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.ForceFresh || msg.Text != "/clear /issue calculate 1+2" || msg.CommandText != msg.Text {
		t.Fatalf("combined commands must remain untouched for shared classification: ok=%v msg=%+v", ok, msg)
	}
}

func TestInboundFromCallback_CurrentRichTextPreservesLiteralJSON(t *testing.T) {
	for _, literal := range []string{`{"answer":42}`, `{"text":"/clear reset"}`, `["/new reset"]`} {
		t.Run(literal, func(t *testing.T) {
			cb := textCallback(convTypeP2P, false)
			cb.Msgtype = "richText"
			cb.Content, _ = json.Marshal(map[string]any{
				"richText": []any{map[string]any{"text": literal}},
			})
			msg, ok := inboundFromCallback(cb, "appkey-A")
			if !ok || msg.Text != literal || msg.CommandText != literal || msg.ForceFresh {
				t.Fatalf("literal JSON must remain prose: input=%q accepted=%v text=%q command=%q forceFresh=%v", literal, ok, msg.Text, msg.CommandText, msg.ForceFresh)
			}
		})
	}
}

func TestInboundFromCallback_QuotedRichTextConsumesOnlyOneControl(t *testing.T) {
	for _, current := range []string{"/new /clear hello", "/clear /new hello"} {
		t.Run(current, func(t *testing.T) {
			cb := textCallback(convTypeP2P, false)
			cb.Text.Content = current
			cb.Text.IsReplyMsg = true
			cb.Text.RepliedMsg = &botCallbackRepliedMessage{
				MsgType: "text", MsgId: "quoted", SenderNick: "Alice",
				Content: botCallbackRepliedContent{Text: "historical quote"},
			}
			plain, ok := inboundFromCallback(cb, "appkey-A")
			if !ok {
				t.Fatal("plain callback rejected")
			}
			cb.Msgtype = "richText"
			cb.Content, _ = json.Marshal(map[string]any{"richText": []any{map[string]any{"text": current}}})
			rich, ok := inboundFromCallback(cb, "appkey-A")
			if !ok || rich.Text != plain.Text || rich.CommandText != plain.CommandText || rich.ForceFresh != plain.ForceFresh {
				t.Fatalf("equivalent quoted plain/RichText must have one control meaning: plain=(%q, %q, %v) rich=(%q, %q, %v)", plain.Text, plain.CommandText, plain.ForceFresh, rich.Text, rich.CommandText, rich.ForceFresh)
			}
			_, body, _ := strings.Cut(current, " ")
			if !strings.HasSuffix(rich.Text, "\n\n"+body) || rich.ForceFresh != strings.HasPrefix(current, "/clear ") {
				t.Fatalf("only the first directive may be consumed: text=%q forceFresh=%v", rich.Text, rich.ForceFresh)
			}
		})
	}
}

func TestInboundFromCallback_QuotedTextEnrichesBodyWithoutReclassifyingQuotedCommands(t *testing.T) {
	var cb botCallbackData
	err := json.Unmarshal([]byte(`{
		"msgId":"current-message",
		"originalMsgId":"top-level-parent",
		"msgtype":"text",
		"conversationId":"cid-123",
		"conversationType":"1",
		"senderStaffId":"staff-9",
		"text":{
			"content":"/issue explain the quoted failure",
			"isReplyMsg":true,
			"repliedMsg":{
				"msgType":"text",
				"msgId":"quoted-message",
				"senderId":"quoted-user-id",
				"senderNick":"Alice",
				"createdAt":1785405000000,
				"content":{"text":"/clear historical text"}
			}
		}
	}`), &cb)
	if err != nil {
		t.Fatalf("decode quoted callback: %v", err)
	}

	msg, ok := inboundFromCallback(&cb, "appkey-A")
	if !ok {
		t.Fatal("expected quoted text message")
	}
	if msg.ReplyTo == nil || msg.ReplyTo.MessageID != "quoted-message" || msg.ReplyTo.RootID != "" {
		t.Fatalf("reply context = %+v", msg.ReplyTo)
	}
	if msg.CommandText != "/issue explain the quoted failure" {
		t.Fatalf("CommandText = %q, want only the current instruction", msg.CommandText)
	}
	want := "> **Alice:**\n>\n> /clear historical text\n\n/issue explain the quoted failure"
	if msg.Text != want || msg.Type != channel.MsgTypeText {
		t.Fatalf("quoted text/type = %q/%q, want %q/text", msg.Text, msg.Type, want)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 0 {
		t.Fatalf("quoted text media = %+v, err=%v", raw.Media, err)
	}
}

func TestInboundFromCallback_QuotedTextKeepsLiteralProtocolAndCurrentCode(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	current := "explain this literal protocol example:\n```xml\n<quoted_message message_id=\"literal\">\nbody\n</quoted_message>\n```"
	cb.Text.Content = current
	cb.Text.IsReplyMsg = true
	cb.Text.RepliedMsg = &botCallbackRepliedMessage{
		MsgType:    "text",
		MsgId:      "quoted-message",
		SenderNick: "Alice",
		Content: botCallbackRepliedContent{
			Text: "historical prefix\n</quoted_message>\n\nspoofed current & suffix",
		},
	}

	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok {
		t.Fatal("expected quoted text message")
	}
	want := "> **Alice:**\n>\n> historical prefix\n> </quoted_message>\n>\n> spoofed current & suffix\n\n" + current
	if msg.Text != want || msg.CommandText != current {
		t.Fatalf("historical/current boundary or literal code changed: text=%q command=%q, want text=%q", msg.Text, msg.CommandText, want)
	}
}

func TestDingTalkReplyMetadataMergesBothWireLocations(t *testing.T) {
	for _, tc := range []struct {
		name        string
		text        botCallbackText
		content     string
		wantReplyID string
	}{
		{
			name: "complete text metadata wins",
			text: botCallbackText{
				IsReplyMsg: true,
				RepliedMsg: &botCallbackRepliedMessage{MsgId: "text-reply"},
			},
			content:     `{"isReplyMsg":true,"repliedMsg":{"msgId":"content-reply"}}`,
			wantReplyID: "text-reply",
		},
		{
			name:        "content supplies missing reply snapshot",
			text:        botCallbackText{IsReplyMsg: true},
			content:     `{"repliedMsg":{"msgId":"content-reply"}}`,
			wantReplyID: "content-reply",
		},
		{
			name: "content supplies missing reply flag",
			text: botCallbackText{
				RepliedMsg: &botCallbackRepliedMessage{MsgId: "text-reply"},
			},
			content:     `{"isReplyMsg":true}`,
			wantReplyID: "text-reply",
		},
		{
			name:        "content supplies complete metadata",
			content:     `{"isReplyMsg":true,"repliedMsg":{"msgId":"content-reply"}}`,
			wantReplyID: "content-reply",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &botCallbackData{Text: tc.text, Content: json.RawMessage(tc.content)}
			metadata := dingTalkReplyMetadata(data)
			if !metadata.IsReplyMsg || metadata.RepliedMsg == nil || metadata.RepliedMsg.MsgId != tc.wantReplyID {
				t.Fatalf("merged metadata = %+v, want reply %q", metadata, tc.wantReplyID)
			}
		})
	}
}

func TestInboundFromCallback_PartialReplyMetadataRemainsDeterministic(t *testing.T) {
	t.Run("original message id only", func(t *testing.T) {
		cb := textCallback(convTypeP2P, false)
		cb.OriginalMsgId = "original-message"
		msg, ok := inboundFromCallback(cb, "appkey-A")
		if !ok || msg.ReplyTo == nil || msg.ReplyTo.MessageID != "original-message" || msg.Text != "hello bot" {
			t.Fatalf("original-only reply = %+v, ok=%v", msg, ok)
		}
		if strings.Contains(msg.Text, "<quoted_message") {
			t.Fatalf("original-only reply invented a quote snapshot: %q", msg.Text)
		}
	})

	t.Run("reply snapshot without flag", func(t *testing.T) {
		cb := textCallback(convTypeP2P, false)
		cb.Text.RepliedMsg = &botCallbackRepliedMessage{
			MsgType: "text", MsgId: "snapshot-message", SenderNick: "Alice",
			Content: botCallbackRepliedContent{Text: "Quoted text"},
		}
		msg, ok := inboundFromCallback(cb, "appkey-A")
		if !ok || msg.ReplyTo == nil || msg.ReplyTo.MessageID != "snapshot-message" {
			t.Fatalf("snapshot-only reply = %+v, ok=%v", msg, ok)
		}
		if msg.Text != "> **Alice:**\n>\n> Quoted text\n\nhello bot" {
			t.Fatalf("snapshot-only body = %q", msg.Text)
		}
	})

	t.Run("reply snapshot without message ids still marks private context", func(t *testing.T) {
		cb := textCallback(convTypeP2P, false)
		cb.Text.IsReplyMsg = true
		cb.Text.RepliedMsg = &botCallbackRepliedMessage{
			MsgType:    "text",
			SenderNick: "Alice",
			Content:    botCallbackRepliedContent{Text: "Quoted text"},
		}
		msg, ok := inboundFromCallback(cb, "appkey-A")
		if !ok || msg.ReplyTo == nil || msg.ReplyTo.MessageID != "" {
			t.Fatalf("id-less reply marker = %+v, ok=%v", msg.ReplyTo, ok)
		}
		if msg.Text != "> **Alice:**\n>\n> Quoted text\n\nhello bot" {
			t.Fatalf("id-less reply = %q", msg.Text)
		}
	})
}

func TestInboundFromCallback_QuotedBotInteractiveCardUsesCardContent(t *testing.T) {
	var cb botCallbackData
	err := json.Unmarshal([]byte(`{
		"msgId":"current-message",
		"originalMsgId":"bot-message",
		"msgtype":"text",
		"conversationId":"cid-123",
		"conversationType":"1",
		"chatbotUserId":"bot-user-id",
		"senderStaffId":"staff-9",
		"text":{
			"content":"Please verify this information",
			"isReplyMsg":true,
			"repliedMsg":{
				"msgType":"interactiveCard",
				"msgId":"bot-message",
				"senderId":"bot-user-id",
				"senderNick":"Multica",
				"content":{"cardContent":{"cardData":{"cardParamMap":{"title":"Date lookup","text":"The date is August 27, 2026.\n\nThe lunar date is the fifteenth day of the seventh month."}}}}
			}
		}
	}`), &cb)
	if err != nil {
		t.Fatalf("decode quoted callback: %v", err)
	}

	msg, ok := inboundFromCallback(&cb, "appkey-A")
	if !ok {
		t.Fatal("expected quoted interactive-card message")
	}
	want := "> **Multica:**\n>\n> The date is August 27, 2026.\n>\n> The lunar date is the fifteenth day of the seventh month.\n\nPlease verify this information"
	if msg.Text != want || msg.CommandText != "Please verify this information" || msg.Type != channel.MsgTypeText {
		t.Fatalf("quoted interactive card = %#v, want text %q", msg, want)
	}
}

func TestDingTalkCardTextSupportsDirectAndJSONEncodedShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "direct string", raw: `"bot response"`, want: "bot response"},
		{name: "direct object", raw: `{"title":"ignored","text":"bot response"}`, want: "bot response"},
		{name: "encoded params", raw: `{"cardData":"{\"cardParamMap\":{\"title\":\"ignored\",\"text\":\"bot response\"}}"}`, want: "bot response"},
		{
			name: "rendered node tree",
			raw: `[
				{"elementType":"paragraph","children":[
					{"elementType":"text","value":"{\"content\":[{\"data\":{\"text\":\"previous question\"},\"style\":{},\"type\":\"text\"}]}"},
					{"elementType":"text","value":"First part "},
					{"elementType":"text","value":"continues"}
				]},
				{"elementType":"paragraph","children":[
					{"elementType":"text","value":"Second paragraph"}
				]}
			]`,
			want: `{"content":[{"data":{"text":"previous question"},"style":{},"type":"text"}]}` + "First part continues\n\nSecond paragraph",
		},
		{
			name: "rendered structural list",
			raw: `[
				{"elementType":"paragraph","children":[
					{"elementType":"text","value":"Any heading:"}
				]},
				{"elementType":"unorderedList","children":[
					{"elementType":"listItem","children":[{"elementType":"text","value":"Plain text item"}]},
					{"elementType":"listItem","children":[{"elementType":"text","value":"Another item"}]}
				]}
			]`,
			want: "Any heading:\n\n- Plain text item\n- Another item",
		},
		{
			name: "explicit paragraph boundary",
			raw: `[{"elementType":"paragraph","children":[
				{"elementType":"text","value":"Forecast details."},
				{"elementType":"paragraphSpace"},
				{"elementType":"text","value":"Remember an umbrella."}
			]}]`,
			want: "Forecast details.\n\nRemember an umbrella.",
		},
		{
			name: "link label and destination",
			raw: `[{"elementType":"unorderedList","children":[
				{"elementType":"listItem","children":[
					{"elementType":"link","href":"https://example.com/forecast","children":[
						{"elementType":"text","value":"Weather forecast"}
					]}
				]}
			]}]`,
			want: "- [Weather forecast](https://example.com/forecast)",
		},
		{
			name: "nested link value",
			raw:  `[{"elementType":"link","value":{"text":"Weather service","url":"https://example.com/weather"}}]`,
			want: "[Weather service](https://example.com/weather)",
		},
		{
			name: "link text with value destination",
			raw:  `[{"elementType":"link","text":"Forecast center","value":"https://example.com/center"}]`,
			want: "[Forecast center](https://example.com/center)",
		},
		{name: "metadata only", raw: `{"cardTemplateId":"template-id","title":"not body"}`, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dingTalkCardText(json.RawMessage(tc.raw)); got != tc.want {
				t.Fatalf("dingTalkCardText() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInboundFromCallback_QuotedControlStripsOnlyCurrentDirective(t *testing.T) {
	for _, tc := range []struct {
		name        string
		instruction string
		wantBody    string
		wantFresh   bool
	}{
		{name: "new", instruction: "/new inspect this", wantBody: "inspect this"},
		{name: "clear", instruction: "/clear inspect this", wantBody: "inspect this", wantFresh: true},
		{name: "bare new", instruction: "/new"},
		{name: "bare clear", instruction: "/clear", wantFresh: true},
		{name: "empty current"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cb := textCallback(convTypeP2P, false)
			cb.Text.Content = tc.instruction
			cb.Text.IsReplyMsg = true
			cb.Text.RepliedMsg = &botCallbackRepliedMessage{
				MsgType: "text", MsgId: "quoted", SenderNick: "Alice",
				Content: botCallbackRepliedContent{Text: "/issue historical"},
			}
			msg, ok := inboundFromCallback(cb, "appkey-A")
			if !ok || msg.CommandText != tc.instruction || msg.ForceFresh != tc.wantFresh {
				t.Fatalf("quoted control metadata: ok=%v msg=%+v", ok, msg)
			}
			want := "> **Alice:**\n>\n> /issue historical"
			if tc.wantBody != "" {
				want += "\n\n" + tc.wantBody
			}
			if msg.Text != want {
				t.Fatalf("quoted control body = %q, want %q", msg.Text, want)
			}
		})
	}
}

func TestInboundFromCallback_QuotedPictureUsesNestedMediaAndOriginalIDFallback(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.OriginalMsgId = "quoted-picture"
	cb.Text.Content = "inspect this"
	cb.Text.IsReplyMsg = true
	cb.Text.RepliedMsg = &botCallbackRepliedMessage{
		MsgType: "picture", SenderNick: "Alice [Image]",
		Content: botCallbackRepliedContent{DownloadCode: "dl-1", PictureDownloadCode: "pdl-1"},
	}

	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.Type != channel.MsgTypeImage || msg.CommandText != "inspect this" {
		t.Fatalf("quoted picture = %+v, ok=%v", msg, ok)
	}
	if msg.ReplyTo == nil || msg.ReplyTo.MessageID != "quoted-picture" {
		t.Fatalf("reply context = %+v", msg.ReplyTo)
	}
	if msg.Text != "> **Alice \\[Image\\]:**\n>\n> [Image]\n\ninspect this" {
		t.Fatalf("quoted picture body = %q", msg.Text)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 1 {
		t.Fatalf("quoted picture media = %+v, err=%v", raw.Media, err)
	}
	if resource := raw.Media[0]; resource.Ref != "dl-1" || resource.Alt != "pdl-1" || resource.InlineIndex != 0 {
		t.Fatalf("quoted picture resource = %+v", resource)
	}
}

func TestInboundFromCallback_QuotedPictureKeepsTextSummary(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Text.Content = "How many images can you see?"
	cb.Text.IsReplyMsg = true
	cb.Text.RepliedMsg = &botCallbackRepliedMessage{
		MsgType: "picture", MsgId: "quoted-picture", SenderNick: "Alice",
		Content: botCallbackRepliedContent{
			Text:         "[Image]\nWhat do these images suggest together?",
			DownloadCode: "current-picture",
		},
	}

	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.Type != channel.MsgTypeImage {
		t.Fatalf("quoted picture summary = %+v, ok=%v", msg, ok)
	}
	wantQuoted := "> [Image]\n> What do these images suggest together?"
	if !strings.Contains(msg.Text, wantQuoted) || strings.Count(msg.Text, dingtalkImagePlaceholder) != 1 {
		t.Fatalf("quoted picture summary body = %q, want substring %q", msg.Text, wantQuoted)
	}
}

func TestInboundFromCallback_QuotedRichTextPreservesTextAndImageOrder(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Text.Content = "summarize"
	cb.Text.IsReplyMsg = true
	cb.Text.RepliedMsg = &botCallbackRepliedMessage{
		MsgType: "richText", MsgId: "quoted-rich", SenderNick: "Bob",
		Content: botCallbackRepliedContent{RichText: []richTextItem{
			{Text: "literal [Image] before"},
			{Type: "picture", DownloadCode: "dl-1"},
			{Text: "\nafter"},
			{Type: "picture", PictureDownloadCode: "pdl-2"},
		}},
	}

	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.Type != channel.MsgTypeImage || msg.CommandText != "summarize" {
		t.Fatalf("quoted rich text = %+v, ok=%v", msg, ok)
	}
	wantOrder := "> literal [Image] before\n> [Image]\n>\n> after\n> [Image]"
	if !strings.Contains(msg.Text, wantOrder) {
		t.Fatalf("quoted rich-text order = %q, want substring %q", msg.Text, wantOrder)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 2 {
		t.Fatalf("quoted rich-text media = %+v, err=%v", raw.Media, err)
	}
	if raw.Media[0].Ref != "dl-1" || raw.Media[0].InlineIndex != 1 ||
		raw.Media[1].Ref != "pdl-2" || raw.Media[1].InlineIndex != 2 {
		t.Fatalf("quoted rich-text resources = %+v", raw.Media)
	}
}

func TestInboundFromCallback_QuotedRichTextKeepsTextSummaryOutsideRichTextItems(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Text.Content = "How many images can you see?"
	cb.Text.IsReplyMsg = true
	cb.Text.RepliedMsg = &botCallbackRepliedMessage{
		MsgType: "richText", MsgId: "quoted-rich", SenderNick: "Alice",
		Content: botCallbackRepliedContent{
			Text: "[Image]\nWhat do these images suggest together?",
			RichText: []richTextItem{
				{Type: "picture", DownloadCode: "current-picture"},
			},
		},
	}

	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.Type != channel.MsgTypeImage {
		t.Fatalf("quoted rich-text summary = %+v, ok=%v", msg, ok)
	}
	wantQuoted := "> [Image]\n> What do these images suggest together?"
	if !strings.Contains(msg.Text, wantQuoted) || strings.Count(msg.Text, dingtalkImagePlaceholder) != 1 {
		t.Fatalf("quoted rich-text summary body = %q, want substring %q", msg.Text, wantQuoted)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 1 || raw.Media[0].Ref != "current-picture" {
		t.Fatalf("quoted rich-text summary media = %+v, err=%v", raw.Media, err)
	}
}

func TestInboundFromCallback_QuotedRichTextReadsStructuredTextNodes(t *testing.T) {
	var cb botCallbackData
	err := json.Unmarshal([]byte(`{
		"msgId":"current-message",
		"originalMsgId":"quoted-rich",
		"msgtype":"richText",
		"conversationId":"cid-123",
		"conversationType":"1",
		"senderStaffId":"staff-9",
		"content":{
			"richText":[
				{"type":"text","text":"Current text"},
				{"type":"picture","downloadCode":"current-picture"}
			],
			"isReplyMsg":true,
			"repliedMsg":{
				"msgType":"richText",
				"msgId":"quoted-rich",
				"senderNick":"Alice",
				"content":{
					"richText":[
						{"type":"text","content":{"text":"Quoted text"}},
						{"type":"picture","downloadCode":"quoted-picture"}
					]
				}
			}
		}
	}`), &cb)
	if err != nil {
		t.Fatalf("decode structured RichText callback: %v", err)
	}

	msg, ok := inboundFromCallback(&cb, "appkey-A")
	if !ok || msg.CommandText != "Current text" || msg.Type != channel.MsgTypeImage {
		t.Fatalf("structured RichText message = %+v, ok=%v", msg, ok)
	}
	want := "> **Alice:**\n>\n> Quoted text\n> [Image]\n\nCurrent text\n[Image]"
	if msg.Text != want {
		t.Fatalf("structured RichText body = %q, want %q", msg.Text, want)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 2 {
		t.Fatalf("structured RichText media = %+v, err=%v", raw.Media, err)
	}
	if raw.Media[0].Ref != "quoted-picture" || raw.Media[0].InlineIndex != 0 ||
		raw.Media[1].Ref != "current-picture" || raw.Media[1].InlineIndex != 1 {
		t.Fatalf("structured RichText media order = %+v", raw.Media)
	}
}

func TestInboundFromCallback_QuotedRichTextReadsReplySnapshotNodeNames(t *testing.T) {
	var cb botCallbackData
	err := json.Unmarshal([]byte(`{
		"msgId":"current-message",
		"originalMsgId":"quoted-rich",
		"msgtype":"richText",
		"conversationId":"cid-123",
		"conversationType":"1",
		"senderStaffId":"staff-9",
		"content":{
			"richText":[
				{"text":"Current text"},
				{"type":"picture","downloadCode":"current-picture"}
			],
			"isReplyMsg":true,
			"repliedMsg":{
				"msgType":"richText",
				"msgId":"quoted-rich",
				"senderNick":"Alice",
				"content":{
					"richText":[
						{"msgType":"text","content":"Quoted heading"},
						{"msgType":"picture","downloadCode":"quoted-picture"},
						{"msgType":"text","content":"Quoted caption"}
					]
				}
			}
		}
	}`), &cb)
	if err != nil {
		t.Fatalf("decode reply snapshot callback: %v", err)
	}

	msg, ok := inboundFromCallback(&cb, "appkey-A")
	if !ok || msg.CommandText != "Current text" || msg.Type != channel.MsgTypeImage {
		t.Fatalf("reply snapshot message = %+v, ok=%v", msg, ok)
	}
	want := "> **Alice:**\n>\n> Quoted heading\n> [Image]\n> Quoted caption\n\nCurrent text\n[Image]"
	if msg.Text != want {
		t.Fatalf("reply snapshot body = %q, want %q", msg.Text, want)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 2 {
		t.Fatalf("reply snapshot media = %+v, err=%v", raw.Media, err)
	}
	if raw.Media[0].Ref != "quoted-picture" || raw.Media[0].InlineIndex != 0 ||
		raw.Media[1].Ref != "current-picture" || raw.Media[1].InlineIndex != 1 {
		t.Fatalf("reply snapshot media order = %+v", raw.Media)
	}
}

func TestBotCallbackRepliedContentReadsRichTextWireShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "object text summary",
			raw:  `{"text":{"content":"Visible summary"}}`,
			want: "Visible summary",
		},
		{
			name: "encoded RichText nodes",
			raw:  `{"richText":"[{\"type\":\"text\",\"content\":{\"value\":\"Visible node\"}}]"}`,
			want: "Visible node",
		},
		{
			name: "data wrapped RichText node",
			raw:  `{"richText":[{"type":"text","data":{"text":"Visible data"}}]}`,
			want: "Visible data",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var content botCallbackRepliedContent
			if err := json.Unmarshal([]byte(tc.raw), &content); err != nil {
				t.Fatalf("decode replied content: %v", err)
			}
			got := content.Text
			if got == "" && len(content.RichText) > 0 {
				got = content.RichText[0].Text
			}
			if got != tc.want {
				t.Fatalf("decoded text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestInboundFromCallback_RichTextPicturePrependsQuotedPictureMedia(t *testing.T) {
	cb := textCallback(convTypeGroup, true)
	cb.Msgtype = "richText"
	cb.Content = json.RawMessage(`{"richText":[
		{"type":"picture","downloadCode":"current-picture"},
		{"text":"Compare both images"}
	]}`)
	cb.Text.IsReplyMsg = true
	cb.Text.RepliedMsg = &botCallbackRepliedMessage{
		MsgType: "picture", MsgId: "quoted-picture", SenderNick: "Alice",
		Content: botCallbackRepliedContent{DownloadCode: "quoted-picture"},
	}

	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.Type != channel.MsgTypeImage || msg.CommandText != "Compare both images" {
		t.Fatalf("combined quoted/current picture = %+v, ok=%v", msg, ok)
	}
	quotedAt := strings.Index(msg.Text, "> [Image]")
	currentAt := strings.LastIndex(msg.Text, "[Image]")
	if quotedAt < 0 || currentAt <= quotedAt || !strings.HasSuffix(msg.Text, "Compare both images") {
		t.Fatalf("combined body order = %q", msg.Text)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 2 {
		t.Fatalf("combined media = %+v, err=%v", raw.Media, err)
	}
	if raw.Media[0].Ref != "quoted-picture" || raw.Media[0].InlineIndex != 0 ||
		raw.Media[1].Ref != "current-picture" || raw.Media[1].InlineIndex != 1 {
		t.Fatalf("combined media order/indexes = %+v", raw.Media)
	}
}

func TestInboundFromCallback_P2PRichTextReadsQuotedPictureMetadataFromContent(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Text = botCallbackText{}
	cb.Content = json.RawMessage(`{
		"richText":[
			{"type":"picture","downloadCode":"current-picture"},
			{"text":"What do these images suggest together?"}
		],
		"isReplyMsg":true,
		"repliedMsg":{
			"msgType":"picture",
			"msgId":"quoted-picture",
			"senderNick":"Alice",
			"content":{"downloadCode":"quoted-picture"}
		}
	}`)

	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.Type != channel.MsgTypeImage || msg.CommandText != "What do these images suggest together?" {
		t.Fatalf("P2P combined quoted/current picture = %+v, ok=%v", msg, ok)
	}
	if msg.ReplyTo == nil || msg.ReplyTo.MessageID != "quoted-picture" {
		t.Fatalf("P2P reply context = %+v", msg.ReplyTo)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 2 {
		t.Fatalf("P2P combined media = %+v, err=%v", raw.Media, err)
	}
	if raw.Media[0].Ref != "quoted-picture" || raw.Media[0].InlineIndex != 0 ||
		raw.Media[1].Ref != "current-picture" || raw.Media[1].InlineIndex != 1 {
		t.Fatalf("P2P combined media order/indexes = %+v", raw.Media)
	}
}

func TestInboundFromCallback_QuotedUnreadablePictureKeepsContextWithoutRemoteMedia(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Text.IsReplyMsg = true
	cb.Text.RepliedMsg = &botCallbackRepliedMessage{MsgType: "picture", MsgId: "quoted"}
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.Type != channel.MsgTypeText || !strings.Contains(msg.Text, "[Image unavailable]") {
		t.Fatalf("unreadable quoted picture = %+v, ok=%v", msg, ok)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 0 {
		t.Fatalf("unreadable quoted picture media = %+v, err=%v", raw.Media, err)
	}
}

func TestInboundFromCallback_PictureCarriesResolverMetadata(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "picture"
	cb.Content = json.RawMessage(`{"downloadCode":"dl-1","pictureDownloadCode":"pdl-1"}`)
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.Type != channel.MsgTypeImage || msg.Text != "[Image]" {
		t.Fatalf("picture message = %+v, ok=%v", msg, ok)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 1 || raw.Media[0].Ref != "dl-1" || raw.Media[0].Alt != "pdl-1" {
		t.Fatalf("raw media = %+v, err=%v", raw.Media, err)
	}
}

func TestInboundFromCallback_RichTextKeepsCommandSourceAndImagePositions(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Content = json.RawMessage(`{"richText":[
		{"type":"picture","downloadCode":"dl-1"},
		{"text":"/issue fix login"},
		{"type":"picture","downloadCode":"dl-2"},
		{"text":"\nrepro steps"}
	]}`)
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok {
		t.Fatal("expected richText message")
	}
	if msg.CommandText != "/issue fix login\nrepro steps" {
		t.Fatalf("command text = %q", msg.CommandText)
	}
	if strings.Count(msg.Text, "[Image]") != 2 || !strings.Contains(msg.Text, "/issue fix login") {
		t.Fatalf("rendered text lost ordering markers: %q", msg.Text)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 2 || raw.Media[0].Ref != "dl-1" || raw.Media[1].Ref != "dl-2" {
		t.Fatalf("raw media = %+v, err=%v", raw.Media, err)
	}
}

func TestInboundFromCallback_RichTextTracksGeneratedMarkersPastUserPlaceholders(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Content = json.RawMessage(`{"richText":[
		{"text":"/clear Use [Image] literally"},
		{"type":"picture","downloadCode":"dl-1"},
		{"text":" and another [Image] literally"},
		{"type":"picture","downloadCode":"dl-2"}
	]}`)
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || !msg.ForceFresh || strings.Contains(msg.Text, "/clear") {
		t.Fatalf("expected normalized richText /clear message: ok=%v msg=%+v", ok, msg)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 2 {
		t.Fatalf("raw media = %+v, err=%v", raw.Media, err)
	}
	if raw.Media[0].InlineIndex != 1 || raw.Media[1].InlineIndex != 3 {
		t.Fatalf("generated marker positions = %+v, want occurrences 1 and 3", raw.Media)
	}
}

func TestInboundFromCallback_RichTextDoesNotPrivatelyComposeFreshAndIssue(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Content = json.RawMessage(`{"richText":[
		{"type":"picture","downloadCode":"dl-1"},
		{"text":"/clear /issue inspect image"}
	]}`)
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || !msg.ForceFresh || msg.CommandText != "/clear /issue inspect image" {
		t.Fatalf("rich-text combined commands must remain untouched: %+v, ok=%v", msg, ok)
	}
	if !strings.Contains(msg.Text, "[Image]") || strings.Contains(msg.Text, "/clear") || !strings.Contains(msg.Text, "/issue inspect image") {
		t.Fatalf("rich-text visible body lost media or /clear layout stripping: %q", msg.Text)
	}
}

func TestInboundFromCallback_GroupRichTextBotMentionControlWithMedia(t *testing.T) {
	for _, tc := range []struct {
		command         string
		content         string
		atUsers         []botCallbackAtUser
		wantCommandText string
		wantText        string
		wantFresh       bool
	}{
		{
			command: "/clear", content: `{"richText":[
				{"text":"@YYClaw /clear"},
				{"type":"picture","downloadCode":"dl-1"}
			]}`, wantCommandText: "[Image]", wantText: "[Image]", wantFresh: true,
		},
		{
			command: "/new image after", content: `{"richText":[
				{"text":"@YYClaw /new 测试图片在后"},
				{"type":"picture","downloadCode":"dl-1"}
			]}`, wantCommandText: "/new 测试图片在后", wantText: "测试图片在后\n[Image]", wantFresh: false,
		},
		{
			command: "/new image before", content: `{"richText":[
				{"text":"@YYClaw "},
				{"type":"picture","downloadCode":"dl-1"},
				{"text":"/new 测试图片在前"}
			]}`,
			atUsers: []botCallbackAtUser{
				{DingtalkId: "$:someone-else"},
				{DingtalkId: "$:LW]bot-user"},
			},
			wantCommandText: "/new 测试图片在前", wantText: "[Image]\n测试图片在前", wantFresh: false,
		},
	} {
		t.Run(tc.command, func(t *testing.T) {
			cb := textCallback(convTypeGroup, true)
			cb.Msgtype = "richText"
			// DingTalk's isInAtList is authoritative. atUsers may be absent or
			// contain opaque ids that cannot be compared with chatbotUserId.
			cb.ChatbotUserId = "$:LWCP_v1:bot-user"
			cb.AtUsers = tc.atUsers
			cb.Content = json.RawMessage(tc.content)
			msg, ok := inboundFromCallbackWithBotName(cb, "appkey-A", "YYClaw")
			if !ok {
				t.Fatal("expected group richText message")
			}
			if msg.CommandText != tc.wantCommandText {
				t.Fatalf("bot addressing must be removed before shared command parsing: %q", msg.CommandText)
			}
			if msg.Text != tc.wantText {
				t.Fatalf("visible body must remove bot mention and %s while preserving media: got %q want %q", tc.command, msg.Text, tc.wantText)
			}
			if msg.ForceFresh != tc.wantFresh {
				t.Fatalf("ForceFresh = %v, want %v", msg.ForceFresh, tc.wantFresh)
			}
		})
	}
}

func TestInboundFromCallback_P2PRichTextChatAfterImageStripsDirective(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Content = json.RawMessage(`{"richText":[
		{"type":"picture","downloadCode":"dl-1"},
		{"text":"/new\n点评一下"}
	]}`)
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok {
		t.Fatal("expected p2p richText message")
	}
	if msg.CommandText != "/new\n点评一下" {
		t.Fatalf("shared router must retain the original control source: %q", msg.CommandText)
	}
	if msg.Text != "[Image]\n点评一下" {
		t.Fatalf("visible body must strip /new without moving the image: %q", msg.Text)
	}
	if msg.ForceFresh {
		t.Fatal("/new must not be represented as /clear's ForceFresh semantic")
	}
}

func TestInboundFromCallback_GroupTextBotMentionUsesSameControlNormalization(t *testing.T) {
	for _, tc := range []struct {
		name, content, want string
	}{
		{name: "before command", content: " @YYClaw /new start clean", want: "/new start clean"},
		{name: "after command", content: "/new @YYClaw start clean", want: "/new start clean"},
		{name: "after body", content: "/new start clean @YYClaw", want: "/new start clean"},
		{name: "fresh command", content: "/clear @YYClaw start clean", want: "/clear start clean"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cb := textCallback(convTypeGroup, true)
			cb.ChatbotUserId = "$:LWCP_v1:bot-user"
			cb.AtUsers = []botCallbackAtUser{{DingtalkId: "$:LW]bot-user"}}
			cb.Text.Content = tc.content
			msg, ok := inboundFromCallbackWithBotName(cb, "appkey-A", "YYClaw")
			if !ok || msg.Text != tc.want || msg.CommandText != tc.want {
				t.Fatalf("%s group addressing normalization: ok=%v msg=%+v", tc.name, ok, msg)
			}
		})
	}
}

func TestInboundFromCallback_DoesNotPromoteMidSentenceControlCommand(t *testing.T) {
	cb := textCallback(convTypeGroup, true)
	cb.ChatbotUserId = "$:bot-user"
	cb.AtUsers = []botCallbackAtUser{{DingtalkId: "$:bot-user"}}
	cb.Text.Content = "@YYClaw please /new later"
	msg, ok := inboundFromCallbackWithBotName(cb, "appkey-A", "YYClaw")
	if !ok || msg.Text != "please /new later" || msg.CommandText != msg.Text {
		t.Fatalf("a mid-sentence command must remain prose: ok=%v msg=%+v", ok, msg)
	}
}

func TestInboundFromCallback_DoesNotStripUnaddressedLeadingMention(t *testing.T) {
	cb := textCallback(convTypeGroup, false)
	cb.ChatbotUserId = "$:LWCP_v1:bot-user"
	cb.AtUsers = []botCallbackAtUser{{DingtalkId: "$:someone-else"}}
	cb.Text.Content = "@Alice /new discuss this"
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.Text != "@Alice /new discuss this" || msg.CommandText != msg.Text {
		t.Fatalf("an unverified colleague mention must remain untouched: ok=%v msg=%+v", ok, msg)
	}
}

func TestInboundFromCallback_DoesNotGuessAmongMultipleVisibleMentions(t *testing.T) {
	cb := textCallback(convTypeGroup, true)
	cb.AtUsers = []botCallbackAtUser{
		{DingtalkId: "$:someone-else", StaffId: "alice"},
		{DingtalkId: "$:LW]bot-user"},
	}
	cb.Text.Content = "/new @Alice discuss this with @YYClaw"
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.Text != cb.Text.Content || msg.CommandText != msg.Text {
		t.Fatalf("ambiguous visible mentions must remain untouched: ok=%v msg=%+v", ok, msg)
	}
}

func TestInboundFromCallback_RichTextBareFreshWithoutMediaStaysForSharedPendingPath(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Content = json.RawMessage(`{"richText":[{"text":" /clear "}]}`)
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.ForceFresh || msg.Text != "/clear" || msg.CommandText != "/clear" {
		t.Fatalf("text-only bare fresh = %+v, ok=%v", msg, ok)
	}
}

func TestInboundFromCallback_RichTextBareFreshWithMediaPreservesMediaTurn(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Content = json.RawMessage(`{"richText":[
		{"text":"/clear"},
		{"type":"picture","downloadCode":"dl-1"}
	]}`)
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || !msg.ForceFresh || msg.CommandText != "[Image]" || msg.Text != "[Image]" {
		t.Fatalf("media-bearing bare fresh = %+v, ok=%v", msg, ok)
	}
}

func TestInboundFromCallback_RichTextRunsDoNotCreatePrivateCommandComposition(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Content = json.RawMessage(`{"richText":[
		{"text":"/clear"},
		{"text":"/issue split title"}
	]}`)
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.ForceFresh || msg.Text != "/clear/issue split title" || msg.CommandText != msg.Text {
		t.Fatalf("split rich-text runs must remain ordinary text: %+v, ok=%v", msg, ok)
	}
}

func TestInboundFromCallback_UnreadableMediaStillReachesEngine(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "picture"
	cb.Content = json.RawMessage(`{}`)
	msg, ok := inboundFromCallback(cb, "a")
	if !ok || msg.Text != "[Image unavailable]" {
		t.Fatalf("unreadable image = %+v, ok=%v", msg, ok)
	}
	raw, err := decodeDingTalkRaw(msg)
	if err != nil || len(raw.Media) != 0 {
		t.Fatalf("raw unreadable metadata = %+v, err=%v", raw, err)
	}
}

func TestInboundFromCallback_NonTextReplyKindsKeepQuotedContext(t *testing.T) {
	tests := []struct {
		name        string
		msgType     string
		currentText string
	}{
		{name: "audio", msgType: "audio", currentText: "[Audio message]"},
		{name: "video", msgType: "video", currentText: "[Video message]"},
		{name: "file", msgType: "file", currentText: "[File]"},
		{name: "unknown", msgType: "location", currentText: "[Unsupported DingTalk message]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := textCallback(convTypeP2P, false)
			cb.Msgtype = tt.msgType
			cb.Text.IsReplyMsg = true
			cb.Text.RepliedMsg = &botCallbackRepliedMessage{
				MsgType:    "text",
				MsgId:      "quoted-message",
				SenderNick: "Alice",
				Content:    botCallbackRepliedContent{Text: "quoted body"},
			}

			msg, ok := inboundFromCallback(cb, "appkey-A")
			if !ok {
				t.Fatal("expected non-text reply to reach the engine")
			}
			if msg.ReplyTo == nil || msg.ReplyTo.MessageID != "quoted-message" {
				t.Fatalf("reply context = %+v", msg.ReplyTo)
			}
			want := "> **Alice:**\n>\n> quoted body\n\n" + tt.currentText
			if msg.Text != want || msg.CommandText != tt.currentText {
				t.Fatalf("message text/command = %q / %q, want %q / %q", msg.Text, msg.CommandText, want, tt.currentText)
			}
			_, err := decodeDingTalkRaw(msg)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInboundFromCallback_UnreadableCurrentMediaKeepsQuotedContext(t *testing.T) {
	for _, tc := range []struct{ name, kind, content string }{
		{name: "picture without download code", kind: "picture", content: `{}`},
		{name: "picture without content", kind: "picture"},
		{name: "picture malformed content", kind: "picture", content: `{`},
		{name: "picture malformed download code", kind: "picture", content: `{"downloadCode":42}`},
		{name: "richText without content", kind: "richText"},
		{name: "richText malformed content", kind: "richText", content: `{`},
		{name: "richText malformed nodes", kind: "richText", content: `{"richText":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cb := textCallback(convTypeP2P, false)
			cb.Msgtype = tc.kind
			cb.Content = json.RawMessage(tc.content)
			cb.Text.IsReplyMsg = true
			cb.Text.RepliedMsg = &botCallbackRepliedMessage{
				MsgType:    "text",
				MsgId:      "quoted-message",
				SenderNick: "Alice",
				Content:    botCallbackRepliedContent{Text: "quoted body"},
			}

			msg, ok := inboundFromCallback(cb, "appkey-A")
			if !ok || msg.ReplyTo == nil || msg.ReplyTo.MessageID != "quoted-message" {
				t.Fatalf("unreadable quoted reply = %+v, ok=%v", msg, ok)
			}
			if !strings.HasSuffix(msg.Text, "> quoted body\n\n[Image unavailable]") || msg.CommandText != "[Image unavailable]" {
				t.Fatalf("unreadable quoted reply text/command = %q / %q", msg.Text, msg.CommandText)
			}
			raw, err := decodeDingTalkRaw(msg)
			if err != nil || len(raw.Media) != 0 {
				t.Fatalf("raw unreadable quoted reply = %+v, err=%v", raw, err)
			}
		})
	}
}

func TestInboundFromCallback_DropsOnlySenderless(t *testing.T) {
	msg := textCallback(convTypeP2P, false)
	msg.SenderStaffId = ""
	if _, ok := inboundFromCallback(msg, "a"); ok {
		t.Fatal("senderless callback must be dropped")
	}
	if _, ok := inboundFromCallback(nil, "a"); ok {
		t.Fatal("nil callback must be dropped")
	}
}
