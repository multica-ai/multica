package wecom

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func mustFrame(t *testing.T, body any) Frame {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return Frame{Cmd: CmdMsgCallback, Body: raw}
}

func TestInboundFromMsgCallback_TextGroupStripsAt(t *testing.T) {
	msg, ok := InboundFromMsgCallback(mustFrame(t, MsgCallbackBody{
		MsgID:    "m1",
		AIBotID:  "bot1",
		ChatID:   "chat_group",
		ChatType: ChatTypeGroup,
		From:     MsgFrom{UserID: "u1"},
		MsgType:  "text",
		Text:     &TextBody{Content: "@BotName hello world"},
	}))
	if !ok {
		t.Fatal("expected ok")
	}
	if msg.CommandText != "hello world" {
		t.Fatalf("CommandText = %q, want %q", msg.CommandText, "hello world")
	}
	if msg.Text != "hello world" {
		t.Fatalf("Text = %q, want %q", msg.Text, "hello world")
	}
	if msg.Source.ChatID != "chat_group" {
		t.Fatalf("ChatID = %q, want group chatid", msg.Source.ChatID)
	}
	if msg.Source.ChatType != channel.ChatTypeGroup {
		t.Fatalf("ChatType = %q, want group", msg.Source.ChatType)
	}
	if msg.ReplyTo != nil {
		t.Fatal("ReplyTo must be nil")
	}
	if msg.AddressedToBot != true {
		t.Fatal("AddressedToBot must be true")
	}
}

func TestInboundFromMsgCallback_TextP2PChatID(t *testing.T) {
	msg, ok := InboundFromMsgCallback(mustFrame(t, MsgCallbackBody{
		MsgID:    "m2",
		AIBotID:  "bot1",
		ChatType: ChatTypeSingle,
		From:     MsgFrom{UserID: "u1"},
		MsgType:  "text",
		Text:     &TextBody{Content: "hi"},
	}))
	if !ok {
		t.Fatal("expected ok")
	}
	if msg.Source.ChatID != "u1" {
		t.Fatalf("p2p ChatID = %q, want sender userid", msg.Source.ChatID)
	}
	if msg.Source.ChatType != channel.ChatTypeP2P {
		t.Fatalf("ChatType = %q, want p2p", msg.Source.ChatType)
	}
}

func TestInboundFromMsgCallback_QuoteEnrichesTextOnly(t *testing.T) {
	msg, ok := InboundFromMsgCallback(mustFrame(t, MsgCallbackBody{
		MsgID:    "m3",
		AIBotID:  "bot1",
		ChatType: ChatTypeSingle,
		From:     MsgFrom{UserID: "u1"},
		MsgType:  "text",
		Text:     &TextBody{Content: "follow up"},
		Quote: &QuoteBody{
			MsgType: "text",
			Text:    &TextBody{Content: "earlier"},
		},
	}))
	if !ok {
		t.Fatal("expected ok")
	}
	if msg.CommandText != "follow up" {
		t.Fatalf("CommandText = %q, must exclude quote", msg.CommandText)
	}
	if msg.Text != "<quoted_message type=\"text\">earlier</quoted_message>\nfollow up" {
		t.Fatalf("Text = %q", msg.Text)
	}
}

func TestInboundFromMsgCallback_Mixed(t *testing.T) {
	msg, ok := InboundFromMsgCallback(mustFrame(t, MsgCallbackBody{
		MsgID:    "m4",
		AIBotID:  "bot1",
		ChatType: ChatTypeGroup,
		ChatID:   "g1",
		From:     MsgFrom{UserID: "u1"},
		MsgType:  "mixed",
		Mixed: &MixedBody{
			MsgItem: []MixedItem{
				{MsgType: "text", Text: &TextBody{Content: "@Bot hi "}},
				{MsgType: "image"},
			},
		},
	}))
	if !ok {
		t.Fatal("expected ok")
	}
	if msg.CommandText != "hi" {
		t.Fatalf("CommandText = %q, want stripped hi", msg.CommandText)
	}
}

func TestInboundFromMsgCallback_Voice(t *testing.T) {
	msg, ok := InboundFromMsgCallback(mustFrame(t, MsgCallbackBody{
		MsgID:    "m5",
		AIBotID:  "bot1",
		ChatType: ChatTypeSingle,
		From:     MsgFrom{UserID: "u1"},
		MsgType:  "voice",
		Voice:    &VoiceBody{Content: "transcribed"},
	}))
	if !ok {
		t.Fatal("expected ok")
	}
	if msg.CommandText != "transcribed" || msg.Text != "transcribed" {
		t.Fatalf("voice text = %q / %q", msg.CommandText, msg.Text)
	}
}

func TestStripLeadingAtMentions(t *testing.T) {
	if got := stripLeadingAtMentions("@A @B hello"); got != "hello" {
		t.Fatalf("got %q", got)
	}
}
