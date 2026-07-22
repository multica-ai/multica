package dingtalk

import (
	"encoding/json"
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
	if msg.Text != "hello bot" {
		t.Errorf("text = %q, want trimmed 'hello bot'", msg.Text)
	}
	if msg.MessageID != "msg-1" || msg.Source.SenderID != "staff-9" || msg.Source.ChatID != "cid-123" {
		t.Errorf("routing fields wrong: %+v", msg)
	}
	var raw dingtalkRawEvent
	if err := json.Unmarshal(msg.Raw, &raw); err != nil || raw.AppID != "appkey-A" {
		t.Errorf("Raw must carry the stamped app id: %q (%v)", raw.AppID, err)
	}
}

func TestInboundFromCallback_GroupAddressedOnlyWhenMentioned(t *testing.T) {
	if msg, ok := inboundFromCallback(textCallback(convTypeGroup, true), "a"); !ok || !msg.AddressedToBot {
		t.Errorf("group @mention must be addressed: ok=%v addressed=%v", ok, msg.AddressedToBot)
	}
	msg, ok := inboundFromCallback(textCallback(convTypeGroup, false), "a")
	if !ok {
		t.Fatal("group message should still be ingested (addressing decided downstream)")
	}
	if msg.Source.ChatType != channel.ChatTypeGroup || msg.AddressedToBot {
		t.Errorf("group without @mention must not be addressed: %+v", msg)
	}
}

func TestInboundFromCallback_UnreadableMediaReachesEngine(t *testing.T) {
	// A picture/richText the adapter cannot download (over-quota errorCode
	// 20001 strips content; a codeless or unparsable payload) must NOT be
	// dropped pre-engine — it reaches the core as a MediaUnreadable image so
	// the engine can refuse it with identity-gated feedback.
	cases := []struct {
		name    string
		msgtype string
		content json.RawMessage
	}{
		{"picture over-quota (no content)", "picture", nil},
		{"picture without any download code", "picture", json.RawMessage(`{}`)},
		{"unparsable richText", "richText", json.RawMessage(`not-json`)},
		{"richText over-quota (no content)", "richText", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := textCallback(convTypeP2P, false)
			cb.Msgtype = tc.msgtype
			cb.Content = tc.content
			msg, ok := inboundFromCallback(cb, "a")
			if !ok {
				t.Fatal("unreadable media must reach the engine, not be dropped")
			}
			if !msg.MediaUnreadable {
				t.Error("unreadable media must be flagged MediaUnreadable")
			}
			if msg.Type != channel.MsgTypeImage {
				t.Errorf("type = %v, want image (so it never trips the unsupported-kind gate)", msg.Type)
			}
			if len(msg.PendingMedia) != 0 {
				t.Errorf("no downloadable references expected, got %+v", msg.PendingMedia)
			}
		})
	}
}

func TestInboundFromCallback_DropsSenderless(t *testing.T) {
	// Only genuinely non-user events (no sender staff id, nil callback) are
	// dropped before the engine.
	senderless := textCallback(convTypeP2P, false)
	senderless.SenderStaffId = ""
	if _, ok := inboundFromCallback(senderless, "a"); ok {
		t.Error("messages with no sender staff id must be dropped")
	}
	if _, ok := inboundFromCallback(nil, "a"); ok {
		t.Error("nil callback must be dropped")
	}
}

func TestInboundFromCallback_Picture(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "picture"
	cb.Text = botCallbackText{}
	cb.Content = json.RawMessage(`{"downloadCode":"dl-1","pictureDownloadCode":"pdl-1"}`)
	msg, ok := inboundFromCallback(cb, "a")
	if !ok {
		t.Fatal("expected a picture message to be ingested")
	}
	if msg.Type != channel.MsgTypeImage || msg.Text != "" {
		t.Errorf("type/text = %v/%q", msg.Type, msg.Text)
	}
	if len(msg.PendingMedia) != 1 || msg.PendingMedia[0].Ref != "dl-1" || msg.PendingMedia[0].Alt != "pdl-1" {
		t.Errorf("pending media = %+v", msg.PendingMedia)
	}
	if len(msg.Segments) != 1 || msg.Segments[0].Text != "" || msg.Segments[0].MediaIdx != 0 {
		t.Errorf("segments = %+v", msg.Segments)
	}

	altOnly := textCallback(convTypeP2P, false)
	altOnly.Msgtype = "picture"
	altOnly.Content = json.RawMessage(`{"pictureDownloadCode":"pdl-2"}`)
	msg, ok = inboundFromCallback(altOnly, "a")
	if !ok || msg.PendingMedia[0].Ref != "pdl-2" || msg.PendingMedia[0].Alt != "" {
		t.Errorf("picture with only pictureDownloadCode must promote it to Ref: ok=%v %+v", ok, msg.PendingMedia)
	}
}

func TestInboundFromCallback_RichTextInterleaved(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Text = botCallbackText{}
	cb.Content = json.RawMessage(`{"richText":[
		{"text":"look at this "},
		{"type":"picture","downloadCode":"dl-1","pictureDownloadCode":"pdl-1"},
		{"text":" and that "},
		{"type":"picture","downloadCode":"dl-2"},
		{"unknownKind":true},
		{"type":"picture"}
	]}`)
	msg, ok := inboundFromCallback(cb, "a")
	if !ok {
		t.Fatal("expected a richText message to be ingested")
	}
	if msg.Type != channel.MsgTypeImage {
		t.Errorf("type = %v, want image", msg.Type)
	}
	if msg.Text != "look at this  and that" {
		t.Errorf("text = %q, want the concatenated trimmed runs", msg.Text)
	}
	// Unknown items and code-less picture items are skipped.
	if len(msg.PendingMedia) != 2 || msg.PendingMedia[0].Ref != "dl-1" || msg.PendingMedia[1].Ref != "dl-2" {
		t.Errorf("pending media = %+v", msg.PendingMedia)
	}
	wantSegs := []channel.Segment{
		{Text: "look at this ", MediaIdx: -1},
		{MediaIdx: 0},
		{Text: " and that ", MediaIdx: -1},
		{MediaIdx: 1},
	}
	if len(msg.Segments) != len(wantSegs) {
		t.Fatalf("segments = %+v", msg.Segments)
	}
	for i, want := range wantSegs {
		if msg.Segments[i] != want {
			t.Errorf("segment[%d] = %+v, want %+v", i, msg.Segments[i], want)
		}
	}
}

// A single richText item that carries BOTH a text run and a picture download
// code must surface both — the image must not be swallowed by the text branch.
func TestInboundFromCallback_RichTextCombinedTextAndPictureItem(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Text = botCallbackText{}
	cb.Content = json.RawMessage(`{"richText":[{"text":"see this ","downloadCode":"dl-1"}]}`)
	msg, ok := inboundFromCallback(cb, "a")
	if !ok {
		t.Fatal("expected a richText message to be ingested")
	}
	if msg.Type != channel.MsgTypeImage {
		t.Errorf("type = %v, want image", msg.Type)
	}
	if msg.Text != "see this" {
		t.Errorf("text = %q, want the item's text run", msg.Text)
	}
	if len(msg.PendingMedia) != 1 || msg.PendingMedia[0].Ref != "dl-1" {
		t.Fatalf("pending media = %+v, want the combined item's image", msg.PendingMedia)
	}
	wantSegs := []channel.Segment{
		{Text: "see this ", MediaIdx: -1},
		{MediaIdx: 0},
	}
	if len(msg.Segments) != len(wantSegs) {
		t.Fatalf("segments = %+v, want text run then image", msg.Segments)
	}
	for i, want := range wantSegs {
		if msg.Segments[i] != want {
			t.Errorf("segment[%d] = %+v, want %+v", i, msg.Segments[i], want)
		}
	}
}

func TestInboundFromCallback_RichTextAllTextFallsBackToTextPath(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Text = botCallbackText{}
	cb.Content = json.RawMessage(`{"richText":[{"text":"  /new start over  "}]}`)
	msg, ok := inboundFromCallback(cb, "a")
	if !ok {
		t.Fatal("expected an all-text richText to be ingested")
	}
	if msg.Type != channel.MsgTypeText || len(msg.PendingMedia) != 0 || len(msg.Segments) != 0 {
		t.Errorf("all-text richText must degrade to the plain-text shape: %+v", msg)
	}
	if !msg.ForceFresh || msg.Text != "start over" {
		t.Errorf("the /new normalization must apply: fresh=%v text=%q", msg.ForceFresh, msg.Text)
	}
}

func TestInboundFromCallback_RichTextWithImagesNormalizesFreshCommand(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Text = botCallbackText{}
	cb.Content = json.RawMessage(`{"richText":[{"text":"/new hello "},{"type":"picture","downloadCode":"dl-1"}]}`)
	msg, ok := inboundFromCallback(cb, "a")
	if !ok {
		t.Fatal("expected ingest")
	}
	if !msg.ForceFresh {
		t.Error("/new on a media-carrying turn must set ForceFresh")
	}
	if msg.BareFresh {
		t.Error("a media-carrying /new is never a bare reset: the images are the prompt")
	}
	if msg.Text != "hello" {
		t.Errorf("text = %q, want the /new prefix stripped", msg.Text)
	}
	if len(msg.Segments) != 2 || msg.Segments[0].Text != "hello" || msg.Segments[1].MediaIdx != 0 {
		t.Errorf("segments = %+v, want the stripped text run then the image", msg.Segments)
	}
}

func TestInboundFromCallback_RichTextWithImagesNewIssueCombo(t *testing.T) {
	// "/new /issue <title>" + image: stripping /new must leave a body whose
	// first line is a parseable /issue command, in Text AND Segments alike.
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Text = botCallbackText{}
	cb.Content = json.RawMessage(`{"richText":[{"text":"/new /issue fix login"},{"type":"picture","downloadCode":"dl-1"}]}`)
	msg, ok := inboundFromCallback(cb, "a")
	if !ok {
		t.Fatal("expected ingest")
	}
	if !msg.ForceFresh || msg.BareFresh {
		t.Errorf("ForceFresh=%v BareFresh=%v, want a non-bare fresh turn", msg.ForceFresh, msg.BareFresh)
	}
	if msg.Text != "/issue fix login" {
		t.Errorf("text = %q, want the /issue command exposed after stripping /new", msg.Text)
	}
	if len(msg.Segments) != 2 || msg.Segments[0].Text != "/issue fix login" {
		t.Errorf("segments = %+v, want the stripped run kept in sync with Text", msg.Segments)
	}
}

func TestInboundFromCallback_RichTextLoneFreshWithImage(t *testing.T) {
	// A lone "/new" run plus an image: the directive segment disappears and
	// the image remains the turn's content — NOT a bare reset.
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Text = botCallbackText{}
	cb.Content = json.RawMessage(`{"richText":[{"text":"/new"},{"type":"picture","downloadCode":"dl-1"}]}`)
	msg, ok := inboundFromCallback(cb, "a")
	if !ok {
		t.Fatal("expected ingest")
	}
	if !msg.ForceFresh || msg.BareFresh {
		t.Errorf("ForceFresh=%v BareFresh=%v, want fresh but not bare", msg.ForceFresh, msg.BareFresh)
	}
	if msg.Text != "" {
		t.Errorf("text = %q, want empty after stripping a lone /new", msg.Text)
	}
	if len(msg.Segments) != 1 || msg.Segments[0].MediaIdx != 0 {
		t.Errorf("segments = %+v, want only the image left", msg.Segments)
	}
}

func TestInboundFromCallback_RichTextFreshNotOnFirstLineStaysLiteral(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Msgtype = "richText"
	cb.Text = botCallbackText{}
	cb.Content = json.RawMessage(`{"richText":[{"text":"hello\n/new x"},{"type":"picture","downloadCode":"dl-1"}]}`)
	msg, ok := inboundFromCallback(cb, "a")
	if !ok {
		t.Fatal("expected ingest")
	}
	if msg.ForceFresh || msg.BareFresh {
		t.Error("/new below the first line must stay literal")
	}
	if msg.Text != "hello\n/new x" {
		t.Errorf("text = %q, want untouched", msg.Text)
	}
}

func TestInboundFromCallback_UnsupportedKindsReachEngine(t *testing.T) {
	cases := []struct {
		msgtype string
		want    channel.MsgType
	}{
		{"audio", channel.MsgTypeAudio},
		{"video", channel.MsgTypeVideo},
		{"file", channel.MsgTypeFile},
		{"someFutureKind", channel.MsgTypeUnknown},
	}
	for _, tc := range cases {
		cb := textCallback(convTypeP2P, false)
		cb.Msgtype = tc.msgtype
		cb.Text = botCallbackText{}
		msg, ok := inboundFromCallback(cb, "a")
		if !ok {
			t.Errorf("%s must reach the engine for the capability notice", tc.msgtype)
			continue
		}
		if msg.Type != tc.want || msg.Text != "" || len(msg.PendingMedia) != 0 {
			t.Errorf("%s → %+v, want Type=%v with no text/media", tc.msgtype, msg, tc.want)
		}
	}
}

func TestCapabilitiesIncludeAttachment(t *testing.T) {
	c := (&dingtalkChannel{}).Capabilities()
	if !c.Has(channel.CapText | channel.CapAttachment) {
		t.Errorf("capabilities = %v, want text|attachment", c)
	}
}

func TestInboundFromCallback_FreshCommandSetsFlagAndStripsPrefix(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Text = botCallbackText{Content: "  /new let's start over  "}
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok {
		t.Fatal("expected a /new message to be ingested")
	}
	if !msg.ForceFresh {
		t.Error("/new must set ForceFresh on the inbound message")
	}
	if msg.Text != "let's start over" {
		t.Errorf("text = %q, want the /new prefix stripped", msg.Text)
	}
	if msg.BareFresh {
		t.Error("/new WITH a prompt must not be a bare reset")
	}
}

func TestInboundFromCallback_BareFreshSetsBareFlag(t *testing.T) {
	cb := textCallback(convTypeP2P, false)
	cb.Text = botCallbackText{Content: "  /new  "}
	msg, ok := inboundFromCallback(cb, "appkey-A")
	if !ok {
		t.Fatal("expected a lone /new message to be ingested")
	}
	if !msg.ForceFresh || !msg.BareFresh {
		t.Errorf("a lone /new must set ForceFresh and BareFresh; got ForceFresh=%v BareFresh=%v", msg.ForceFresh, msg.BareFresh)
	}
	if msg.Text != "" {
		t.Errorf("text = %q, want empty for a lone /new", msg.Text)
	}
}

func TestInboundFromCallback_PlainMessageIsNotFresh(t *testing.T) {
	msg, ok := inboundFromCallback(textCallback(convTypeP2P, false), "a")
	if !ok {
		t.Fatal("expected ingest")
	}
	if msg.ForceFresh {
		t.Error("a plain message must not set ForceFresh")
	}
}
