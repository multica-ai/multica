package weixin

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestNormalizeInboundDirectText(t *testing.T) {
	rawID := json.RawMessage(`12345`)
	got, ok := NormalizeInbound(Message{
		Seq:          7,
		MessageID:    rawID,
		FromUserID:   "user@im.wechat",
		ToUserID:     "bot@im.bot",
		SessionID:    "session-1",
		MessageType:  messageTypeUser,
		MessageState: messageStateFinish,
		ContextToken: "context-secret",
		Items: []MessageItem{
			{Type: messageItemTypeText, Text: &TextItem{Text: "hello"}},
			{Type: messageItemTypeText, Text: &TextItem{Text: "world"}},
		},
	}, "")
	if !ok {
		t.Fatal("NormalizeInbound rejected direct text")
	}
	if got.MessageID != "12345" || got.EventID != "12345" {
		t.Errorf("ids = (%q, %q)", got.MessageID, got.EventID)
	}
	if got.Source.ChannelType != TypeWeixin || got.Source.ChatType != channel.ChatTypeP2P {
		t.Errorf("source = %#v", got.Source)
	}
	if got.Source.ChatID != "user@im.wechat" || got.Source.SenderID != "user@im.wechat" {
		t.Errorf("source = %#v", got.Source)
	}
	if got.Text != "hello\nworld" || got.CommandText != got.Text || got.Type != channel.MsgTypeText {
		t.Errorf("message = %#v", got)
	}
	meta, err := DecodeInboundMetadata(got)
	if err != nil {
		t.Fatalf("DecodeInboundMetadata: %v", err)
	}
	if meta.BotID != "bot@im.bot" || meta.ContextToken != "context-secret" || meta.SessionID != "session-1" || meta.Seq != 7 {
		t.Errorf("metadata = %#v", meta)
	}
}

func TestNormalizeInboundUsesInstallationBotIDFallback(t *testing.T) {
	got, ok := NormalizeInbound(Message{
		MessageID:   json.RawMessage(`"string-id"`),
		FromUserID:  "user@im.wechat",
		MessageType: messageTypeUser,
		Items:       []MessageItem{{Type: messageItemTypeText, Text: &TextItem{Text: "hi"}}},
	}, "installed-bot@im.bot")
	if !ok {
		t.Fatal("NormalizeInbound rejected fallback bot id")
	}
	meta, err := DecodeInboundMetadata(got)
	if err != nil || meta.BotID != "installed-bot@im.bot" {
		t.Fatalf("metadata = %#v, err=%v", meta, err)
	}
}

func TestNormalizeInboundUsesItemIDBeforeDeterministicFallback(t *testing.T) {
	base := Message{
		Seq:          9,
		FromUserID:   "user@im.wechat",
		ToUserID:     "bot@im.bot",
		MessageType:  messageTypeUser,
		CreateTimeMS: 123,
		Items: []MessageItem{{
			Type:  messageItemTypeText,
			MsgID: "item-id",
			Text:  &TextItem{Text: "hello"},
		}},
	}
	got, ok := NormalizeInbound(base, "")
	if !ok || got.MessageID != "item-id" {
		t.Fatalf("message = %#v, ok=%v", got, ok)
	}

	base.Items[0].MsgID = ""
	first, ok := NormalizeInbound(base, "")
	if !ok || first.MessageID == "" {
		t.Fatalf("fallback message = %#v, ok=%v", first, ok)
	}
	second, _ := NormalizeInbound(base, "")
	if first.MessageID != second.MessageID {
		t.Fatalf("fallback is unstable: %q != %q", first.MessageID, second.MessageID)
	}
	base.Items[0].Text.Text = "changed"
	changed, _ := NormalizeInbound(base, "")
	if changed.MessageID == first.MessageID {
		t.Fatal("fallback id did not change with message content")
	}
}

func TestNormalizeInboundAcceptsVoiceTranscriptAsText(t *testing.T) {
	got, ok := NormalizeInbound(Message{
		MessageID:   json.RawMessage(`1`),
		FromUserID:  "user@im.wechat",
		ToUserID:    "bot@im.bot",
		MessageType: messageTypeUser,
		Items: []MessageItem{{
			Type:  messageItemTypeVoice,
			Voice: &VoiceItem{Text: "transcribed words"},
		}},
	}, "")
	if !ok || got.Text != "transcribed words" {
		t.Fatalf("message = %#v, ok=%v", got, ok)
	}
}

func TestNormalizeInboundRejectsUnsupportedTraffic(t *testing.T) {
	valid := Message{
		MessageID:   json.RawMessage(`1`),
		FromUserID:  "user@im.wechat",
		ToUserID:    "bot@im.bot",
		MessageType: messageTypeUser,
		Items:       []MessageItem{{Type: messageItemTypeText, Text: &TextItem{Text: "hello"}}},
	}
	tests := []struct {
		name   string
		mutate func(*Message)
	}{
		{"bot echo", func(m *Message) { m.MessageType = messageTypeBot }},
		{"group", func(m *Message) { m.GroupID = "group-id" }},
		{"generating", func(m *Message) { m.MessageState = 1 }},
		{"missing sender", func(m *Message) { m.FromUserID = "" }},
		{"media only", func(m *Message) { m.Items = []MessageItem{{Type: 2}} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := valid
			tt.mutate(&msg)
			if got, ok := NormalizeInbound(msg, ""); ok {
				t.Fatalf("accepted unsupported message: %#v", got)
			}
		})
	}
}

func TestDecodeInboundMetadataRejectsMalformedRaw(t *testing.T) {
	if _, err := DecodeInboundMetadata(channel.InboundMessage{}); err == nil {
		t.Fatal("empty Raw was accepted")
	}
	if _, err := DecodeInboundMetadata(channel.InboundMessage{Raw: json.RawMessage(`{`)}); err == nil {
		t.Fatal("malformed Raw was accepted")
	}
}
