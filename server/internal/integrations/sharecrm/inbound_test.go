package sharecrm

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestInboundFromEvent_Direct(t *testing.T) {
	data := &botMessageData{
		MessageID: "m1",
		ChatID:    "0:fs:session123:",
		ChatType:  "direct",
		From:      botSender{ID: "E.fs.7618", Name: "7618"},
		Text:      "hello",
		Message:   &botTextMessage{Type: "text", Content: "hello"},
		EA:        "fs",
		BotFullID: "B.fs.bot_demo",
	}
	msg, ok := inboundFromEvent(data, "app-1", "B.fs.bot_demo")
	if !ok {
		t.Fatal("expected ok")
	}
	if msg.Source.ChannelType != TypeShareCRM {
		t.Fatalf("channel type = %q", msg.Source.ChannelType)
	}
	if msg.Source.ChatType != channel.ChatTypeP2P {
		t.Fatalf("chat type = %q", msg.Source.ChatType)
	}
	if msg.Source.SenderID != "E.fs.7618" {
		t.Fatalf("sender = %q", msg.Source.SenderID)
	}
	if msg.Source.ChatID != "0:fs:session123:" {
		t.Fatalf("chat_id = %q", msg.Source.ChatID)
	}
	if msg.Text != "hello" {
		t.Fatalf("text = %q", msg.Text)
	}
	if !msg.AddressedToBot {
		t.Fatal("expected addressed")
	}
	var raw sharecrmRawEvent
	if err := json.Unmarshal(msg.Raw, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.AppID != "app-1" {
		t.Fatalf("raw app_id = %q", raw.AppID)
	}
}

func TestInboundFromEvent_GroupStripsMention(t *testing.T) {
	data := &botMessageData{
		MessageID: "m2",
		ChatID:    "0:fs:group1:",
		ChatType:  "group",
		From:      botSender{ID: "7618", Name: "7618"},
		Text:      "@B.fs.bot_demo  fix the login",
		EA:        "fs",
		BotFullID: "B.fs.bot_demo",
	}
	msg, ok := inboundFromEvent(data, "app-1", "B.fs.bot_demo")
	if !ok {
		t.Fatal("expected ok")
	}
	if msg.Source.ChatType != channel.ChatTypeGroup {
		t.Fatalf("chat type = %q", msg.Source.ChatType)
	}
	if msg.Source.SenderID != "E.fs.7618" {
		t.Fatalf("sender normalized = %q", msg.Source.SenderID)
	}
	if msg.Text != "fix the login" {
		t.Fatalf("text after strip = %q", msg.Text)
	}
}

func TestInboundFromEvent_DropNoSender(t *testing.T) {
	_, ok := inboundFromEvent(&botMessageData{ChatID: "0:fs:s:", Text: "x"}, "app", "")
	if ok {
		t.Fatal("expected drop")
	}
}

func TestStripMarkdown(t *testing.T) {
	in := "**bold** and [link](https://ex.com)"
	out := stripMarkdown(in)
	if out != "bold and link (https://ex.com)" {
		t.Fatalf("got %q", out)
	}
}
