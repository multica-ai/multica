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
		ChatbotCorpId:    "corp-1",
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

func TestInboundFromCallback_DropsNonTextAndSenderless(t *testing.T) {
	nonText := textCallback(convTypeP2P, false)
	nonText.Msgtype = "picture"
	if _, ok := inboundFromCallback(nonText, "a"); ok {
		t.Error("non-text messages must be dropped")
	}
	senderless := textCallback(convTypeP2P, false)
	senderless.SenderStaffId = ""
	if _, ok := inboundFromCallback(senderless, "a"); ok {
		t.Error("messages with no sender staff id must be dropped")
	}
	if _, ok := inboundFromCallback(nil, "a"); ok {
		t.Error("nil callback must be dropped")
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
