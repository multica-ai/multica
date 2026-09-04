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
		t.Fatalf("raw app id = %q, err=%v", raw.AppID, err)
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
