package wecom

// ws_frame_test.go — pure codec guards: the single-vs-group source mapping
// (which feeds the #1 security fix — a group message must carry the group
// chatid in Source.ChatID and the real person in Source.SenderID), the
// msgtype normalization, and the outbound send-body shape.

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

func TestChannelMessageFromCallback_GroupKeepsSenderDistinctFromChat(t *testing.T) {
	t.Parallel()
	mc := aibotMsgCallback{
		MsgID:    "m1",
		ChatID:   "GROUP_CHAT_ID",
		ChatType: "group",
		MsgType:  "text",
	}
	mc.From.UserID = "SENDER_USERID"
	mc.Text.Content = "hello"

	msg := channelMessageFromCallback("bot-1", mc, "req-1")

	if msg.Source.ChatType != channel.ChatTypeGroup {
		t.Errorf("chat type = %v, want group", msg.Source.ChatType)
	}
	if msg.Source.ChatID != "GROUP_CHAT_ID" {
		t.Errorf("Source.ChatID = %q, want the group chatid", msg.Source.ChatID)
	}
	// The security fix depends on this: the sender is addressable separately
	// from the room, so the binding token can go to the person privately.
	if msg.Source.SenderID != "SENDER_USERID" {
		t.Errorf("Source.SenderID = %q, want the sender userid", msg.Source.SenderID)
	}
}

func TestChannelMessageFromCallback_P2PFallsBackChatIDToSender(t *testing.T) {
	t.Parallel()
	mc := aibotMsgCallback{MsgID: "m2", ChatID: "", ChatType: "single", MsgType: "text"}
	mc.From.UserID = "USER_A"

	msg := channelMessageFromCallback("bot-1", mc, "req-2")

	if msg.Source.ChatType != channel.ChatTypeP2P {
		t.Errorf("chat type = %v, want p2p", msg.Source.ChatType)
	}
	if msg.Source.ChatID != "USER_A" {
		t.Errorf("p2p ChatID = %q, want fallback to sender USER_A", msg.Source.ChatID)
	}
}

func TestChannelMsgType_NonTextIsUnknown(t *testing.T) {
	t.Parallel()
	cases := map[string]channel.MsgType{
		"text":  channel.MsgTypeText,
		"image": channel.MsgTypeImage,
		"file":  channel.MsgTypeFile,
		"voice": channel.MsgTypeAudio,
		"audio": channel.MsgTypeAudio,
		"video": channel.MsgTypeVideo,
		// "mixed" must NOT map to Text: dispatchFrame drops non-text before
		// normalization, so mapping it to Text was dead and misleading.
		"mixed":     channel.MsgTypeUnknown,
		"":          channel.MsgTypeUnknown,
		"greetings": channel.MsgTypeUnknown,
	}
	for in, want := range cases {
		if got := channelMsgType(in); got != want {
			t.Errorf("channelMsgType(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSendMsgTextBody_ShapeAndChatTypeValidation(t *testing.T) {
	t.Parallel()

	body, err := sendMsgTextBody("chat-1", chatTypeSingleInt, "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if body["chatid"] != "chat-1" || body["chat_type"] != chatTypeSingleInt || body["msgtype"] != "markdown" {
		t.Errorf("unexpected body: %#v", body)
	}

	if _, err := sendMsgTextBody("", chatTypeSingleInt, "hi"); err == nil {
		t.Error("empty chatid should error")
	}
	if _, err := sendMsgTextBody("chat-1", 0, "hi"); err == nil {
		t.Error("chat_type 0 should be rejected (must be 1 or 2)")
	}
	if _, err := sendMsgTextBody("chat-1", 3, "hi"); err == nil {
		t.Error("chat_type 3 should be rejected (must be 1 or 2)")
	}
}
