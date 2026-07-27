package telegram

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestInboundFromUpdate_PrivateIssueCommand(t *testing.T) {
	u := Update{UpdateID: 1, Message: &Message{
		MessageID: 77,
		From:      &User{ID: 900, Username: "alice"},
		Chat:      Chat{ID: 555, Type: "private"},
		Text:      "/issue Fix login\nit crashes",
	}}
	msg, ok := InboundFromUpdate(u, "123", "acme_bot")
	if !ok {
		t.Fatal("expected ok")
	}
	if msg.Source.ChannelType != TypeTelegram || msg.Source.ChatType != channel.ChatTypeP2P {
		t.Fatalf("bad source %+v", msg.Source)
	}
	if msg.Source.SenderID != "900" || msg.Source.ChatID != "555" || msg.MessageID != "77" {
		t.Fatalf("bad ids %+v", msg.Source)
	}
	if !msg.AddressedToBot || msg.Text != "/issue Fix login\nit crashes" {
		t.Fatalf("bad text/addressed: %q %v", msg.Text, msg.AddressedToBot)
	}
}

func TestInboundFromUpdate_GroupRequiresMention(t *testing.T) {
	base := Message{MessageID: 5, From: &User{ID: 1}, Chat: Chat{ID: -100, Type: "supergroup"}}
	noMention := base
	noMention.Text = "hello team"
	if msg, ok := InboundFromUpdate(Update{Message: &noMention}, "123", "acme_bot"); !ok || msg.AddressedToBot {
		t.Fatalf("group without mention should be ok but not addressed; got ok=%v addressed=%v", ok, msg.AddressedToBot)
	}
	withMention := base
	withMention.Text = "@acme_bot /issue Ship it"
	withMention.Entities = []Entity{{Type: "mention", Offset: 0, Length: 9}}
	msg, ok := InboundFromUpdate(Update{Message: &withMention}, "123", "acme_bot")
	if !ok || !msg.AddressedToBot {
		t.Fatalf("group with mention should be addressed; got ok=%v addressed=%v", ok, msg.AddressedToBot)
	}
	if msg.Text != "/issue Ship it" {
		t.Fatalf("mention not stripped: %q", msg.Text)
	}
}

func TestInboundFromUpdate_DropsBotAndEmptyAndChannel(t *testing.T) {
	if _, ok := InboundFromUpdate(Update{Message: &Message{From: &User{ID: 1, IsBot: true}, Chat: Chat{Type: "private"}, Text: "hi"}}, "123", "b"); ok {
		t.Fatal("bot sender should drop")
	}
	if _, ok := InboundFromUpdate(Update{Message: &Message{From: &User{ID: 1}, Chat: Chat{Type: "private"}, Text: "   "}}, "123", "b"); ok {
		t.Fatal("empty text should drop")
	}
	if _, ok := InboundFromUpdate(Update{Message: &Message{From: &User{ID: 1}, Chat: Chat{Type: "channel"}, Text: "x"}}, "123", "b"); ok {
		t.Fatal("channel post should drop")
	}
	if _, ok := InboundFromUpdate(Update{Message: nil}, "123", "b"); ok {
		t.Fatal("no message should drop")
	}
}

func TestInboundFromUpdate_StripsCommandBotSuffix(t *testing.T) {
	u := Update{Message: &Message{MessageID: 9, From: &User{ID: 2}, Chat: Chat{ID: 5, Type: "private"}, Text: "/issue@acme_bot Do the thing"}}
	msg, _ := InboundFromUpdate(u, "123", "acme_bot")
	if msg.Text != "/issue Do the thing" {
		t.Fatalf("command suffix not normalized: %q", msg.Text)
	}
}
