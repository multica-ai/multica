package wecom

// inbound_media_test.go — what a media callback becomes on the way in:
// whether it reaches the handler at all, what the stored body says, and what
// the resolver is handed to download.
//
// The frames here are the shapes WeCom's aibot long-connection docs specify
// (developer.work.weixin.qq.com/document/path/101463): a standalone
// image/file/video carries {url, aeskey} under its own key, and a 图文混排
// carries mixed.msg_item, each item typed like a standalone message.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// mediaFrame builds an aibot_msg_callback frame from a raw body map, going
// through the real JSON decoder so a test cannot drift from the wire.
func mediaFrame(t *testing.T, msgType string, extra map[string]any) frameEnvelope {
	t.Helper()
	full := map[string]any{
		"msgid":    "MSGID-1",
		"aibotid":  "bot-1",
		"chatid":   "CHAT_1",
		"chattype": "single",
		"from":     map[string]any{"userid": "USER_A"},
		"msgtype":  msgType,
	}
	for k, v := range extra {
		full[k] = v
	}
	body, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	return frameEnvelope{Cmd: cmdMsgCallback, Body: body}
}

// dispatchOne runs one frame through dispatchFrame and reports what the
// handler saw plus what went back over the socket.
func dispatchOne(t *testing.T, env frameEnvelope) (channel.InboundMessage, bool, *recordingConn) {
	t.Helper()
	return dispatchOneAs(t, env, "")
}

// dispatchOneAs is dispatchOne with the bot's configured display name — the
// one thing stripLeadingMentions has to match a group's addressing against.
func dispatchOneAs(t *testing.T, env frameEnvelope, botDisplayName string) (channel.InboundMessage, bool, *recordingConn) {
	t.Helper()
	var got channel.InboundMessage
	called := false
	c := testChannel(func(_ context.Context, m channel.InboundMessage) error {
		called, got = true, m
		return nil
	})
	c.botDisplayName = botDisplayName
	conn := &recordingConn{}
	if err := c.dispatchFrame(context.Background(), env, newWSSender(conn, slog.Default()), slog.Default()); err != nil {
		t.Fatalf("dispatchFrame: %v", err)
	}
	return got, called, conn
}

// mixedFrame builds a 图文混排 callback from its runs, in composition order.
func mixedFrame(t *testing.T, extra map[string]any, items ...map[string]any) frameEnvelope {
	t.Helper()
	full := map[string]any{"mixed": map[string]any{"msg_item": items}}
	for k, v := range extra {
		full[k] = v
	}
	return mediaFrame(t, "mixed", full)
}

func textRun(content string) map[string]any {
	return map[string]any{"msgtype": "text", "text": map[string]any{"content": content}}
}

func imageRun(url string) map[string]any {
	return map[string]any{"msgtype": "image", "image": map[string]any{"url": url, "aeskey": "k"}}
}

// voiceRun is a spoken run: WeCom sends the transcript it recognised, never
// audio, so there is no url and no key on it.
func voiceRun(transcript string) map[string]any {
	return map[string]any{"msgtype": "voice", "voice": map[string]any{"content": transcript}}
}

// TestDispatchFrame_MediaCallbacksReachTheHandler is the defect this file
// exists for. Before inbound media, dispatchFrame routed only msgtype ==
// "text" and answered every other kind with a receipt, so a photo, a file, a
// video and a 图文混排 all ended at the socket: no chat_message, no session,
// no agent run, nothing in the web UI either.
func TestDispatchFrame_MediaCallbacksReachTheHandler(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		msgType  string
		extra    map[string]any
		wantText string
		wantType channel.MsgType
		wantURLs []string
	}{
		{
			name:     "image",
			msgType:  "image",
			extra:    map[string]any{"image": map[string]any{"url": "https://cos.example.com/i", "aeskey": "k1"}},
			wantText: "[Image]",
			wantType: channel.MsgTypeImage,
			wantURLs: []string{"https://cos.example.com/i"},
		},
		{
			name:     "file",
			msgType:  "file",
			extra:    map[string]any{"file": map[string]any{"url": "https://cos.example.com/f", "aeskey": "k2"}},
			wantText: "[File]",
			wantType: channel.MsgTypeFile,
			wantURLs: []string{"https://cos.example.com/f"},
		},
		{
			name:     "video",
			msgType:  "video",
			extra:    map[string]any{"video": map[string]any{"url": "https://cos.example.com/v", "aeskey": "k3"}},
			wantText: "[Video]",
			wantType: channel.MsgTypeVideo,
			wantURLs: []string{"https://cos.example.com/v"},
		},
		{
			name:    "mixed keeps composition order",
			msgType: "mixed",
			extra: map[string]any{"mixed": map[string]any{"msg_item": []map[string]any{
				{"msgtype": "text", "text": map[string]any{"content": "看下这个报错"}},
				{"msgtype": "image", "image": map[string]any{"url": "https://cos.example.com/m1", "aeskey": "k4"}},
				{"msgtype": "text", "text": map[string]any{"content": "还有这个"}},
				{"msgtype": "image", "image": map[string]any{"url": "https://cos.example.com/m2", "aeskey": "k5"}},
			}}},
			wantText: "看下这个报错\n[Image]\n还有这个\n[Image]",
			wantType: channel.MsgTypeText,
			wantURLs: []string{"https://cos.example.com/m1", "https://cos.example.com/m2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, called, conn := dispatchOne(t, mediaFrame(t, tc.msgType, tc.extra))
			if !called {
				t.Fatalf("a %s message never reached the handler — it was answered and dropped", tc.msgType)
			}
			if len(conn.frames) != 0 {
				t.Errorf("a routable %s message should not also get a receipt, got %d frames", tc.msgType, len(conn.frames))
			}
			if got.Text != tc.wantText {
				t.Errorf("stored body = %q, want %q", got.Text, tc.wantText)
			}
			if got.Type != tc.wantType {
				t.Errorf("normalized type = %v, want %v", got.Type, tc.wantType)
			}
			var wm InboundMessage
			if err := json.Unmarshal(got.Raw, &wm); err != nil {
				t.Fatalf("decode Raw: %v", err)
			}
			if len(wm.Media) != len(tc.wantURLs) {
				t.Fatalf("Raw carried %d attachments, want %d", len(wm.Media), len(tc.wantURLs))
			}
			for i, want := range tc.wantURLs {
				if wm.Media[i].URL != want {
					t.Errorf("attachment %d url = %q, want %q (order must be composition order)", i, wm.Media[i].URL, want)
				}
			}
		})
	}
}

// TestMediaPlaceholders_MatchLarkAndDingTalk pins the placeholder strings to
// their siblings byte for byte. An agent reads every channel through the same
// prompt, so a wecom-only spelling is a wecom-only thing for it to learn.
//
// Sources: lark/content_flatten.go flattenContent ("[Image]" / "[File]" /
// "[Video]") and dingtalk/inbound.go dingtalkImagePlaceholder = "[Image]"
// with "[File]" beside it.
func TestMediaPlaceholders_MatchLarkAndDingTalk(t *testing.T) {
	t.Parallel()
	cases := map[channel.MsgType]string{
		channel.MsgTypeImage: "[Image]",
		channel.MsgTypeFile:  "[File]",
		channel.MsgTypeVideo: "[Video]",
	}
	for kind, want := range cases {
		if got := mediaPlaceholder(kind); got != want {
			t.Errorf("mediaPlaceholder(%v) = %q, want %q — lark and dingtalk emit %q", kind, got, want, want)
		}
	}
}

// TestOwnText_MixedDropsRunsItCannotRead: a run of a kind the adapter does
// not model contributes nothing, rather than a stray placeholder for an
// attachment nobody will ever download.
func TestOwnText_MixedDropsRunsItCannotRead(t *testing.T) {
	t.Parallel()
	env := mediaFrame(t, "mixed", map[string]any{"mixed": map[string]any{"msg_item": []map[string]any{
		{"msgtype": "text", "text": map[string]any{"content": "hi"}},
		{"msgtype": "location", "location": map[string]any{"name": "office"}},
		{"msgtype": "image", "image": map[string]any{"url": "", "aeskey": "k"}},
	}}})
	got, called, _ := dispatchOne(t, env)
	if !called {
		t.Fatal("a mixed message with one readable run must still be routed")
	}
	if got.Text != "hi" {
		t.Errorf("body = %q, want just the readable run", got.Text)
	}
	var wm InboundMessage
	if err := json.Unmarshal(got.Raw, &wm); err != nil {
		t.Fatalf("decode Raw: %v", err)
	}
	if len(wm.Media) != 0 {
		t.Errorf("an image run with no url produced %d attachments, want 0 — an intent row for an object that can never exist", len(wm.Media))
	}
}

// TestOwnText_MixedWithNothingReadableTakesTheReceipt: a mixed message whose
// every run is a kind we cannot read is not an empty message to ingest.
func TestOwnText_MixedWithNothingReadableTakesTheReceipt(t *testing.T) {
	t.Parallel()
	env := mediaFrame(t, "mixed", map[string]any{"mixed": map[string]any{"msg_item": []map[string]any{
		{"msgtype": "location", "location": map[string]any{"name": "office"}},
	}}})
	_, called, conn := dispatchOne(t, env)
	if called {
		t.Error("a mixed message with nothing readable must not be ingested as an empty turn")
	}
	if len(conn.frames) != 1 {
		t.Fatalf("expected one receipt, got %d frames", len(conn.frames))
	}
}

// TestUnsupportedReceipt_DoesNotClaimTextOnly: the receipt used to say the
// bot only handles text. It now routes photos, files, videos and 图文混排, so
// a person who just watched it answer a screenshot must not then be told it
// handles text only.
func TestUnsupportedReceipt_DoesNotClaimTextOnly(t *testing.T) {
	t.Parallel()
	if strings.Contains(unsupportedMsgTypeReceipt, "只能处理文字") {
		t.Errorf("receipt %q still claims text-only while image/file/video/mixed route", unsupportedMsgTypeReceipt)
	}
}

// TestAVoiceNoteIsReadWhereverItArrives: WeCom runs the speech recognition on
// its side and hands over the transcript, so a spoken sentence needs no
// download and no key — it is words, and it is read like words whether it
// arrives on its own or as one run of a 图文混排. An empty transcript is the
// one exception: background noise and a half-second press produce nothing to
// route, and an empty body would reach the agent as a turn with nothing in it.
func TestAVoiceNoteIsReadWhereverItArrives(t *testing.T) {
	t.Parallel()

	standalone := mediaFrame(t, "voice", map[string]any{"voice": map[string]any{"content": "把报表发我"}})
	got, called, conn := dispatchOne(t, standalone)
	if !called {
		t.Fatal("a voice note with a transcript was refused — the sentence was already in hand")
	}
	if got.Text != "把报表发我" {
		t.Errorf("body = %q, want the transcript", got.Text)
	}
	if len(conn.frames) != 0 {
		t.Errorf("a readable voice note got %d receipt(s); it was answered instead of routed", len(conn.frames))
	}

	empty := mediaFrame(t, "voice", map[string]any{"voice": map[string]any{"content": "  "}})
	if _, called, conn := dispatchOne(t, empty); called || len(conn.frames) != 1 {
		t.Errorf("an empty transcript should take the receipt path (called=%v, frames=%d)", called, len(conn.frames))
	}

	inMixed := mediaFrame(t, "mixed", map[string]any{"mixed": map[string]any{"msg_item": []map[string]any{
		{"msgtype": "voice", "voice": map[string]any{"content": "把报表发我"}},
		{"msgtype": "file", "file": map[string]any{"url": "https://cos.example.com/x", "aeskey": "k"}},
	}}})
	got, called, _ = dispatchOne(t, inMixed)
	if !called {
		t.Fatal("a mixed message with a voice run must be routed")
	}
	if got.Text != "把报表发我\n[File]" {
		t.Errorf("body = %q, want the transcript above the file placeholder", got.Text)
	}
}

// TestDispatchFrame_TheCommandSourceHasNoPlaceholdersInIt is the ordering
// defect. A person reporting a bug attaches the screenshot and then types
// "/issue 登录坏了" — that order, because you pick the picture while you are
// still deciding what to say. WeCom sends it as one 图文混排, ownText renders
// it "[Image]\n/issue 登录坏了", and engine.ParseIssueCommand looks at the
// first non-empty line and only the first: it sees "[Image]", declines, and no
// issue is filed. Nothing is said about it either, in the chat or the log. The
// same two things typed the other way round work. Nobody can be expected to
// know that.
//
// So the command source is the runs the sender authored and nothing else,
// while the stored body keeps the placeholders — the agent still has to be
// able to see that a picture was attached and where.
func TestDispatchFrame_TheCommandSourceHasNoPlaceholdersInIt(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		items       []map[string]any
		wantBody    string
		wantCommand string
	}{
		{
			name:        "image before the command — the ordering that was broken",
			items:       []map[string]any{imageRun("https://cos.example.com/a"), textRun("/issue 登录坏了")},
			wantBody:    "[Image]\n/issue 登录坏了",
			wantCommand: "/issue 登录坏了",
		},
		{
			name:        "image after the command — the ordering that happened to work",
			items:       []map[string]any{textRun("/issue 登录坏了"), imageRun("https://cos.example.com/a")},
			wantBody:    "/issue 登录坏了\n[Image]",
			wantCommand: "/issue 登录坏了",
		},
		{
			name:        "image between two runs the sender typed",
			items:       []map[string]any{imageRun("https://cos.example.com/a"), textRun("/issue 登录坏了"), imageRun("https://cos.example.com/b"), textRun("复现步骤见图")},
			wantBody:    "[Image]\n/issue 登录坏了\n[Image]\n复现步骤见图",
			wantCommand: "/issue 登录坏了\n复现步骤见图",
		},
		{
			name:        "a spoken command counts, WeCom already transcribed it",
			items:       []map[string]any{imageRun("https://cos.example.com/a"), map[string]any{"msgtype": "voice", "voice": map[string]any{"content": "/issue 登录坏了"}}},
			wantBody:    "[Image]\n/issue 登录坏了",
			wantCommand: "/issue 登录坏了",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, called, _ := dispatchOne(t, mixedFrame(t, nil, tc.items...))
			if !called {
				t.Fatal("the message never reached the handler")
			}
			if got.Text != tc.wantBody {
				t.Errorf("stored body = %q, want %q — the placeholders belong in the body", got.Text, tc.wantBody)
			}
			if got.CommandText != tc.wantCommand {
				t.Errorf("CommandText = %q, want %q — a placeholder in the command source is a line the sender never typed", got.CommandText, tc.wantCommand)
			}
			cmd, ok := engine.ParseIssueCommand(got.CommandText)
			if !ok {
				t.Fatalf("engine.ParseIssueCommand(%q) declined — the sender attached a screenshot, typed /issue, and got no issue and no explanation", got.CommandText)
			}
			if cmd.Title != "登录坏了" {
				t.Errorf("issue title = %q, want 登录坏了", cmd.Title)
			}
			if !got.SkipAgentRun {
				t.Error("SkipAgentRun = false on a pure /issue; the agent would answer the slash command back at the sender")
			}
		})
	}
}

// TestDispatchFrame_GroupMentionedMixedCommand is the two defects together,
// and it is the one that has to keep passing for both of them to stay fixed.
//
// A group is the only place the bot can be reached by @-mentioning it, so the
// mention arrives glued to the front of the text run: "@Multica Bot /issue
// 登录坏了". main strips that (stripLeadingMentions) and this PR removes the
// placeholders, and the command source needs BOTH — drop the strip and the
// parser sees "@Multica Bot", drop the placeholder removal and it sees
// "[Image]". Either way the sender gets nothing.
func TestDispatchFrame_GroupMentionedMixedCommand(t *testing.T) {
	t.Parallel()
	group := map[string]any{"chattype": "group", "chatid": "GROUP_1"}
	env := mixedFrame(t, group,
		imageRun("https://cos.example.com/a"),
		textRun("@Multica Bot /issue 登录坏了"),
	)
	got, called, _ := dispatchOneAs(t, env, "Multica Bot")
	if !called {
		t.Fatal("the group message never reached the handler")
	}
	if got.Source.ChatType != channel.ChatTypeGroup {
		t.Fatalf("chat type = %v, want group — the mention strip is gated on it", got.Source.ChatType)
	}
	// The transcript keeps what was said, including who was addressed.
	if want := "[Image]\n@Multica Bot /issue 登录坏了"; got.Text != want {
		t.Errorf("stored body = %q, want %q", got.Text, want)
	}
	if want := "/issue 登录坏了"; got.CommandText != want {
		t.Errorf("CommandText = %q, want %q — the placeholder and the addressing both have to come off", got.CommandText, want)
	}
	cmd, ok := engine.ParseIssueCommand(got.CommandText)
	if !ok {
		t.Fatalf("engine.ParseIssueCommand(%q) declined — an @-mentioned /issue with a screenshot files nothing", got.CommandText)
	}
	if cmd.Title != "登录坏了" {
		t.Errorf("issue title = %q, want 登录坏了", cmd.Title)
	}
	if !got.SkipAgentRun {
		t.Error("SkipAgentRun = false; the group would get the slash command read back to it")
	}
}

func TestDispatchFrame_MixedSessionControlsShareNormalization(t *testing.T) {
	tests := []struct {
		command   string
		wantFresh bool
	}{
		{command: "/clear 点评一下", wantFresh: true},
		{command: "/new 点评一下", wantFresh: false},
	}
	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			got, called, _ := dispatchOne(t, mixedFrame(t, nil,
				imageRun("https://cos.example.com/a"),
				textRun(tc.command),
			))
			if !called {
				t.Fatal("the mixed control message never reached the handler")
			}
			if got.Text != "[Image]\n点评一下" {
				t.Fatalf("visible body = %q, want media with stripped directive", got.Text)
			}
			if got.CommandText != tc.command {
				t.Fatalf("CommandText = %q, want original control source %q", got.CommandText, tc.command)
			}
			if got.ForceFresh != tc.wantFresh {
				t.Fatalf("ForceFresh = %v, want %v", got.ForceFresh, tc.wantFresh)
			}
		})
	}
}

func TestDispatchFrame_GroupMentionedMixedChatStripsAddressingAndDirective(t *testing.T) {
	group := map[string]any{"chattype": "group", "chatid": "GROUP_1"}
	got, called, _ := dispatchOneAs(t, mixedFrame(t, group,
		imageRun("https://cos.example.com/a"),
		textRun("@Multica Bot /new 点评一下"),
	), "Multica Bot")
	if !called {
		t.Fatal("the group message never reached the handler")
	}
	if got.Text != "[Image]\n点评一下" || got.CommandText != "/new 点评一下" {
		t.Fatalf("Text/CommandText = %q/%q", got.Text, got.CommandText)
	}
}

// TestDispatchFrame_GroupMentionedSpokenIssueWithAnImage is the intersection
// of all three decisions that meet in this function, and the only test that
// holds them together.
//
// Someone in a group photographs the broken screen, holds the mic and says
// "@Multica Bot /issue 登录坏了". WeCom delivers one 图文混排 with an image run
// and a voice run, and the command has to survive three separate strips:
//
//   - the placeholder — the image run is first, and the parser reads the first
//     non-empty line and only that one, so a body opening with "[Image]" files
//     nothing;
//   - the transcript — the spoken words live in voice.content, and a command
//     source built from the typed field is empty, at which point the engine
//     falls back to the body and reads "[Image]" anyway;
//   - the addressing — an @-mention is the only way to reach a bot in a group,
//     so it arrives glued to the front of the words.
//
// Take any one side of the merge wholesale and one of the three is missing.
// The sender then gets no issue and no explanation, and the message they would
// have to send to report it is the one that does not work.
func TestDispatchFrame_GroupMentionedSpokenIssueWithAnImage(t *testing.T) {
	t.Parallel()
	group := map[string]any{"chattype": "group", "chatid": "GROUP_1"}

	for _, tc := range []struct {
		name     string
		items    []map[string]any
		wantBody string
	}{
		{
			// Everything spoken: the mention rides in front of the transcript,
			// which is how it arrives when the whole message is said out loud.
			name:     "the mention and the command are both in the voice run",
			items:    []map[string]any{imageRun("https://cos.example.com/a"), voiceRun("@Multica Bot /issue 登录坏了")},
			wantBody: "[Image]\n@Multica Bot /issue 登录坏了",
		},
		{
			// Mention typed, command spoken — the composer's own order when
			// someone picks the bot from the @ menu and then talks.
			name:     "the mention is typed and the command is spoken",
			items:    []map[string]any{textRun("@Multica Bot"), imageRun("https://cos.example.com/a"), voiceRun("/issue 登录坏了")},
			wantBody: "@Multica Bot\n[Image]\n/issue 登录坏了",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, called, _ := dispatchOneAs(t, mixedFrame(t, group, tc.items...), "Multica Bot")
			if !called {
				t.Fatal("the group message never reached the handler")
			}
			if got.Source.ChatType != channel.ChatTypeGroup {
				t.Fatalf("chat type = %v, want group — the mention strip is gated on it", got.Source.ChatType)
			}
			// The stored body keeps everything: the placeholder marks where the
			// screenshot goes, and the addressing is part of what was said.
			if got.Text != tc.wantBody {
				t.Errorf("stored body = %q, want %q", got.Text, tc.wantBody)
			}
			cmd, ok := engine.ParseIssueCommand(got.CommandText)
			if !ok {
				t.Fatalf("engine.ParseIssueCommand(%q) declined — a spoken, @-mentioned /issue with a screenshot files nothing and says nothing", got.CommandText)
			}
			if cmd.Title != "登录坏了" {
				t.Errorf("issue title = %q, want 登录坏了 — the title is what the strips left behind", cmd.Title)
			}
			if !got.SkipAgentRun {
				t.Error("SkipAgentRun = false; the group would get the issue AND the agent reading the slash command back at it")
			}
			// The picture still travels. Losing it here would file the issue
			// without the one thing the sender attached to explain it.
			wm, err := wecomMsgFromRaw(got)
			if err != nil {
				t.Fatalf("decode raw: %v", err)
			}
			if len(wm.Media) != 1 || wm.Media[0].URL != "https://cos.example.com/a" {
				t.Errorf("attachments = %+v, want the one image the sender took", wm.Media)
			}
		})
	}
}

// A standalone photo has no words in it. Its whole body is a placeholder, so
// the command source is empty rather than "[Image]" — the engine's own
// fallback then reads the body, which is the behaviour every adapter has, and
// "[Image]" is not a command in any of them.
func TestDispatchFrame_AStandalonePhotoCarriesNoCommand(t *testing.T) {
	t.Parallel()
	env := mediaFrame(t, "image", map[string]any{
		"image": map[string]any{"url": "https://cos.example.com/a", "aeskey": "k"},
	})
	got, called, _ := dispatchOne(t, env)
	if !called {
		t.Fatal("the photo never reached the handler")
	}
	if got.CommandText != "" {
		t.Errorf("CommandText = %q, want empty — the sender typed nothing", got.CommandText)
	}
	if got.SkipAgentRun {
		t.Error("SkipAgentRun = true on a bare photo; the agent would never see the picture it was sent")
	}
}
