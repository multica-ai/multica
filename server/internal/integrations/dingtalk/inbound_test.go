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
	callback.ConversationTitle = "Platform team"
	if msg, ok := inboundFromCallback(callback, "a"); !ok || !msg.AddressedToBot {
		t.Fatalf("group mention must be addressed: ok=%v msg=%+v", ok, msg)
	} else if raw, err := decodeDingTalkRaw(msg); err != nil || raw.ConversationTitle != "Platform team" {
		t.Fatalf("group title = %q, err=%v", raw.ConversationTitle, err)
	}
	msg, ok := inboundFromCallback(textCallback(convTypeGroup, false), "a")
	if !ok || msg.AddressedToBot {
		t.Fatalf("unmentioned group message must reach the shared filter unaddressed: ok=%v msg=%+v", ok, msg)
	}
}

func TestInboundFromCallback_LeavesFreshCommandForSharedRouter(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Text.Content = "  /new start over  "
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok {
		t.Fatal("expected /new message")
	}
	if msg.Text != "/new start over" || msg.CommandText != msg.Text || msg.ForceFresh {
		t.Fatalf("DingTalk must leave /new classification to the shared Router: %+v", msg)
	}
}

func TestInboundFromCallback_BareFreshStaysForSharedPendingPath(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Text.Content = " /new "
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.Text != "/new" || msg.CommandText != "/new" || msg.ForceFresh {
		t.Fatalf("bare /new must remain visible to the shared Router: ok=%v msg=%+v", ok, msg)
	}
}

func TestInboundFromCallback_DoesNotPrivatelyComposeFreshAndIssue(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Text.Content = "/new /issue calculate 1+2"
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.ForceFresh || msg.Text != "/new /issue calculate 1+2" || msg.CommandText != msg.Text {
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
		{"text":"/new Use [Image] literally"},
		{"type":"picture","downloadCode":"dl-1"},
		{"text":" and another [Image] literally"},
		{"type":"picture","downloadCode":"dl-2"}
	]}`)
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || !msg.ForceFresh || strings.Contains(msg.Text, "/new") {
		t.Fatalf("expected normalized richText /new message: ok=%v msg=%+v", ok, msg)
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
		{"text":"/new /issue inspect image"}
	]}`)
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || !msg.ForceFresh || msg.CommandText != "/new /issue inspect image" {
		t.Fatalf("rich-text combined commands must remain untouched: %+v, ok=%v", msg, ok)
	}
	if !strings.Contains(msg.Text, "[Image]") || strings.Contains(msg.Text, "/new") || !strings.Contains(msg.Text, "/issue inspect image") {
		t.Fatalf("rich-text visible body lost media or /new layout stripping: %q", msg.Text)
	}
}

func TestInboundFromCallback_RichTextBareFreshWithoutMediaStaysForSharedPendingPath(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Content = json.RawMessage(`{"richText":[{"text":" /new "}]}`)
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.ForceFresh || msg.Text != "/new" || msg.CommandText != "/new" {
		t.Fatalf("text-only bare fresh = %+v, ok=%v", msg, ok)
	}
}

func TestInboundFromCallback_RichTextBareFreshWithMediaPreservesMediaTurn(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Content = json.RawMessage(`{"richText":[
		{"text":"/new"},
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
		{"text":"/new"},
		{"text":"/issue split title"}
	]}`)
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok || msg.ForceFresh || msg.Text != "/new/issue split title" || msg.CommandText != msg.Text {
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

func TestCapabilitiesIncludeAttachment(t *testing.T) {
	if c := (&dingtalkChannel{}).Capabilities(); !c.Has(channel.CapText | channel.CapAttachment) {
		t.Fatalf("capabilities = %v", c)
	}
}
