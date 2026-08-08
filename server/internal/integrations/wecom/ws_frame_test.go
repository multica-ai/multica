package wecom

// ws_frame_test.go — pure codec guards: the single-vs-group source mapping
// (which feeds the #1 security fix — a group message must carry the group
// chatid in Source.ChatID and the real person in Source.SenderID), the
// msgtype normalization, and the outbound send-body shape.

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
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

	msg := channelMessageFromCallback("bot-1", "", mc, "req-1")

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

	msg := channelMessageFromCallback("bot-1", "", mc, "req-2")

	if msg.Source.ChatType != channel.ChatTypeP2P {
		t.Errorf("chat type = %v, want p2p", msg.Source.ChatType)
	}
	if msg.Source.ChatID != "USER_A" {
		t.Errorf("p2p ChatID = %q, want fallback to sender USER_A", msg.Source.ChatID)
	}
}

// TestChannelMessageFromCallback_P2PMentionIsProseNotACommand: the mention
// strip is for groups, where the @ is the only way to reach the bot. In a 1:1
// nobody has to address anyone, so an @ at the front is the sender naming a
// colleague inside their own sentence.
//
// What a person experiences when this is not scoped: they type "@李雷 /issue
// 帮我问问他" to the bot, meaning "ask 李雷 about the issue" — and an issue
// titled 帮我问问他 is filed that they never asked for. SkipAgentRun is read
// off the same line, so the bot also says nothing back. A stray issue and
// silence, from a message that was a question.
func TestChannelMessageFromCallback_P2PMentionIsProseNotACommand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, content string }{
		{"issue directive after a colleague's name", "@李雷 /issue 帮我问问他"},
		{"fresh-session directive after a colleague's name", "@李雷 /new 的排期你问一下"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mc := aibotMsgCallback{MsgID: "m-p2p", ChatID: "", ChatType: "single", MsgType: "text"}
			mc.From.UserID = "USER_A"
			mc.Text.Content = tc.content

			// A configured display name must not change this either: it is the
			// chat type that decides, not whose name is at the front.
			msg := channelMessageFromCallback("bot-1", "Multica Bot", mc, "req-p2p")

			if msg.CommandText != tc.content {
				t.Errorf("CommandText = %q, want %q untouched — in a 1:1 the leading @ is a colleague's "+
					"name in the sender's sentence, not addressing the bot", msg.CommandText, tc.content)
			}
			if cmd, ok := engine.ParseIssueCommand(msg.CommandText); ok {
				t.Errorf("an issue titled %q would be filed from %q, which the sender never asked for",
					cmd.Title, tc.content)
			}
			if _, ok := engine.ParseFreshSessionCommand(msg.CommandText); ok {
				t.Errorf("%q would throw away the running session the sender was in the middle of", tc.content)
			}
			if msg.SkipAgentRun {
				t.Errorf("SkipAgentRun = true for %q; the sender's question gets no reply at all", tc.content)
			}
		})
	}
}

// TestChannelMessageFromCallback_P2PCommandStillWorks is the other side of the
// gate: scoping the strip to groups must not stop a plain 1:1 slash command
// from being one. This is the path that worked before the mention strip
// existed, and it has to keep working after it.
func TestChannelMessageFromCallback_P2PCommandStillWorks(t *testing.T) {
	t.Parallel()
	mc := aibotMsgCallback{MsgID: "m-p2p-cmd", ChatID: "", ChatType: "single", MsgType: "text"}
	mc.From.UserID = "USER_A"
	mc.Text.Content = "/issue 登录失败"

	msg := channelMessageFromCallback("bot-1", "Multica Bot", mc, "req-p2p-cmd")

	cmd, ok := engine.ParseIssueCommand(msg.CommandText)
	if !ok {
		t.Fatalf("CommandText = %q — a 1:1 /issue stopped being a command", msg.CommandText)
	}
	if cmd.Title != "登录失败" {
		t.Errorf("issue title = %q, want 登录失败", cmd.Title)
	}
	if !msg.SkipAgentRun {
		t.Error("SkipAgentRun = false for a pure 1:1 /issue; the agent would answer the command text too")
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
