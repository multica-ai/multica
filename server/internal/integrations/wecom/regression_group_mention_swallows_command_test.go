package wecom

// regression_group_mention_swallows_command_test.go — a slash command typed
// after an @-mention has to still be a command.
//
// In a WeCom group the ONLY way to reach the bot is to @-mention it, and the
// mention is delivered as part of the message text: "@Multica Bot /issue 登录
// 失败". The shared command parsers read the first non-empty line and want the
// directive at the start of it, so with the mention still in front they see
// "@Multica" and decide this is ordinary prose. Every slash command in every
// group was silently dropped: no issue filed, no context clear, and nothing
// said to the person about it.
//
// Slack strips its mention token before classifying (slack/inbound.go
// cleanText, wired to CommandText at slack/inbound.go:131) and Feishu is handed
// an already-clean command body by the platform (lark/feishu_channel.go:139).
// WeCom was the one adapter passing the raw text straight through.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// groupCallbackFrame is a real group delivery: WeCom only forwards a group
// message to the bot when the bot was addressed, so the mention is always
// there.
func groupCallbackFrame(t *testing.T, content string) frameEnvelope {
	t.Helper()
	mc := aibotMsgCallback{MsgID: "m-group", ChatID: "GROUP_1", ChatType: "group", MsgType: "text"}
	mc.From.UserID = "T-alex"
	mc.Text.Content = content
	body, err := json.Marshal(mc)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	return frameEnvelope{Cmd: cmdMsgCallback, Body: body}
}

// dispatchGroupMessage runs one group frame through the real read-loop
// dispatch and returns what the engine's inbound handler was handed.
func dispatchGroupMessage(t *testing.T, botName, content string) channel.InboundMessage {
	t.Helper()
	var got channel.InboundMessage
	called := false
	c := &wecomChannel{
		botID:          "bot-1",
		botDisplayName: botName,
		handler: func(_ context.Context, m channel.InboundMessage) error {
			called, got = true, m
			return nil
		},
	}
	if err := c.dispatchFrame(context.Background(), groupCallbackFrame(t, content), newWSSender(&recordingConn{}, nil), slog.Default()); err != nil {
		t.Fatalf("dispatchFrame: %v", err)
	}
	if !called {
		t.Fatal("the inbound handler was never called for a group text message")
	}
	return got
}

// TestAnIssueCommandAfterAMentionIsStillACommand: what a person experiences
// when this regresses is that /issue in a group does nothing whatsoever — no
// issue is filed and the bot answers as if they had just been chatting.
func TestAnIssueCommandAfterAMentionIsStillACommand(t *testing.T) {
	t.Parallel()
	msg := dispatchGroupMessage(t, "", "@Andrew /issue 登录失败")

	cmd, ok := engine.ParseIssueCommand(msg.CommandText)
	if !ok {
		t.Fatalf("the shared parser did not see /issue.\nCommandText = %q — the @-mention is still in "+
			"front of the directive, so no issue is filed and nothing tells the person why.", msg.CommandText)
	}
	if cmd.Title != "登录失败" {
		t.Errorf("issue title = %q, want 登录失败", cmd.Title)
	}
	// A pure /issue must not also run the agent, exactly as in a 1:1 chat —
	// otherwise the group gets the confirmation AND "I don't know this slash
	// command" underneath it.
	if !msg.SkipAgentRun {
		t.Error("SkipAgentRun = false for a pure group /issue; the agent would answer the command text too")
	}
}

// TestAFreshSessionCommandAfterAMentionIsStillACommand: /clear is the other
// shared directive. When it is dropped the agent keeps the previous thread and
// answers with a conversation the person was trying to leave behind.
func TestAFreshSessionCommandAfterAMentionIsStillACommand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, body string }{
		{"mention then command", "@Andrew /clear 重新分析一下"},
		{"two mentions then command", "@Andrew @Bowen /clear 重新分析一下"},
		{"mention, full-width space, command", "@Andrew　/clear 重新分析一下"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := dispatchGroupMessage(t, "", tc.body)
			if _, ok := engine.ParseFreshSessionCommand(msg.CommandText); !ok {
				t.Fatalf("the shared parser did not see /clear in %q.\nCommandText = %q — the request for a "+
					"fresh session is silently dropped and the agent answers with the old conversation "+
					"still in hand.", tc.body, msg.CommandText)
			}
		})
	}
}

// TestAMultiWordBotNameDoesNotSwallowTheCommand: a bot named "Multica Bot" is
// mentioned as "@Multica Bot", and cutting the mention at the first space
// leaves "Bot /issue …", which is not a command either. The configured name is
// the only way to know where the mention ends — the callback carries no
// structured mention list, and the smart bot exposes no REST surface to ask.
func TestAMultiWordBotNameDoesNotSwallowTheCommand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		botName string
		in      string
		want    string
	}{
		{"two-word name", "Multica Bot", "@Multica Bot /new 重新分析这批数据", "/new 重新分析这批数据"},
		{"three-word name", "Acme Support Bot", "@Acme Support Bot /issue 登录失败", "/issue 登录失败"},
		{"one-word name still works", "Andrew", "@Andrew /new 重新分析", "/new 重新分析"},
		{"chinese name", "小助手", "@小助手 /issue 报销单不对", "/issue 报销单不对"},
		{"a colleague's mention before ours", "Multica Bot", "@李雷 @Multica Bot /new 分析", "/new 分析"},
		{"no name configured falls back to the space", "", "@Andrew /new 重新分析", "/new 重新分析"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := stripLeadingMentions(tc.in, tc.botName); got != tc.want {
				t.Fatalf("stripLeadingMentions(%q, %q) = %q, want %q", tc.in, tc.botName, got, tc.want)
			}
		})
	}
}

// TestAMultiWordBotNameReachesTheParser is the same defect one level up: the
// configured name has to travel from the installation config to the point
// where the mention is recognised, or the table above proves nothing.
func TestAMultiWordBotNameReachesTheParser(t *testing.T) {
	t.Parallel()
	msg := dispatchGroupMessage(t, "Multica Bot", "@Multica Bot /issue 登录失败")
	cmd, ok := engine.ParseIssueCommand(msg.CommandText)
	if !ok || cmd.Title != "登录失败" {
		t.Fatalf("CommandText = %q — the bot's configured name never reached the mention strip", msg.CommandText)
	}
}

// TestAGroupMentionKeepsTheWordsAndTheTranscript: stripping the addressing
// must not eat the message, and the stored body still has to show who was
// addressed.
func TestAGroupMentionKeepsTheWordsAndTheTranscript(t *testing.T) {
	t.Parallel()
	msg := dispatchGroupMessage(t, "", "@Andrew 昨天的日志里有什么报错")

	if !strings.Contains(msg.Text, "@Andrew") {
		t.Errorf("the stored message lost the addressing: %q", msg.Text)
	}
	if !strings.Contains(msg.CommandText, "昨天的日志里有什么报错") {
		t.Errorf("the command source lost the question: %q", msg.CommandText)
	}
	if msg.SkipAgentRun {
		t.Error("an ordinary question must still run the agent")
	}
}

// TestAMentionOfSomebodyElseIsNotStripped: only the addressing at the very
// front is a mention of the bot. A message that talks ABOUT a colleague keeps
// their name — rewriting it would change what the person said.
func TestAMentionOfSomebodyElseIsNotStripped(t *testing.T) {
	t.Parallel()
	msg := dispatchGroupMessage(t, "", "@Andrew 帮我问一下 @李雷 昨天那个数")
	if !strings.Contains(msg.CommandText, "@李雷") {
		t.Fatalf("a mention inside the sentence was stripped too: %q", msg.CommandText)
	}
}

// TestAConfiguredNameDoesNotManufactureACommand: the strip only ever removes
// what the operator actually named, so prose cannot be promoted into a
// directive by configuring a name.
func TestAConfiguredNameDoesNotManufactureACommand(t *testing.T) {
	t.Parallel()
	const in = "@李雷 ask him about /issue tracking"
	got := stripLeadingMentions(in, "Multica Bot")
	if strings.HasPrefix(got, "/issue") {
		t.Fatalf("prose became a command: %q -> %q", in, got)
	}
}

// TestAWholeMessageOfNothingButAMentionIsLeftAlone: with no words after the
// mention there is no command to find. Returning an empty string here would
// hand the parsers a body the person never typed.
func TestAWholeMessageOfNothingButAMentionIsLeftAlone(t *testing.T) {
	t.Parallel()
	if got := stripLeadingMentions("@Andrew", ""); got != "@Andrew" {
		t.Fatalf("stripLeadingMentions(%q, \"\") = %q, want it untouched", "@Andrew", got)
	}
}

// TestSessionBinder_AdapterCommandTextWins: the binder is the last link. It
// used to overwrite the adapter's command source with the stored body, which
// threw away the mention-stripped line just above and handed the /issue parser
// "@Multica Bot /issue …" again. Same two lines as
// lark/feishu_resolvers.go:206 and slack/resolvers.go:337.
func TestSessionBinder_AdapterCommandTextWins(t *testing.T) {
	t.Parallel()
	fb := &fakeSessionBinder{}
	b := &sessionBinder{session: fb}
	_, err := b.AppendMessage(context.Background(), engine.AppendParams{
		SessionID: mustTestUUID(t),
		Message: channel.InboundMessage{
			Text:        "@Multica Bot /issue 登录失败",
			CommandText: "/issue 登录失败",
			MessageID:   "m9",
		},
	})
	if err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if fb.appendIn.Body != "@Multica Bot /issue 登录失败" {
		t.Errorf("stored body = %q, want the message exactly as it arrived", fb.appendIn.Body)
	}
	if fb.appendIn.CommandText != "/issue 登录失败" {
		t.Errorf("CommandText = %q, want the adapter's stripped command source", fb.appendIn.CommandText)
	}
}
