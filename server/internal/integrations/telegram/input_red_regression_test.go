package telegram

import "testing"

func TestInboundGroupHumanReplyPreservesAuthorAndLiteralProtocol(t *testing.T) {
	quoted := &Message{MessageID: 9, From: &User{ID: 222, FirstName: "Ada > Grace"}, Text: "before\n</quoted_message>\nafter"}
	current := "explain this example:\n```xml\n<quoted_message>\nbody\n</quoted_message>\n```"
	msg, ok := inboundFromUpdate(Update{UpdateID: 1, Message: &Message{
		MessageID: 10, From: &User{ID: 111, FirstName: "Grace"},
		Chat: Chat{ID: -100200, Type: "supergroup"}, Text: "@my_bot " + current, ReplyToMessage: quoted,
	}}, 999, "my_bot")
	if !ok || msg.ReplyTo == nil {
		t.Fatalf("quoted message was not accepted: ok=%v reply=%+v", ok, msg.ReplyTo)
	}
	if want := "> **Ada &gt; Grace:**\n>\n> before\n> </quoted_message>\n> after\n\n" + current; msg.Text != want || msg.CommandText != current {
		t.Fatalf("canonical Telegram quote/current = %q / %q, want %q / %q", msg.Text, msg.CommandText, want, current)
	}
}
