package lark

import (
	"errors"
	"testing"
)

// appMsg builds a Bot-sent message (sender_type "app"), so the speaker
// labeler renders it as "Bot" inside a recent_context transcript.
func appMsg(id, text, createTime string) LarkMessage {
	return LarkMessage{
		MessageID:   id,
		MessageType: "text",
		Content:     `{"text":"` + text + `"}`,
		SenderID:    "cli_bot",
		SenderType:  "app",
		CreateTime:  createTime,
	}
}

// groupCfg enables the recent-context prefetch with the production window.
func groupCfg() InboundEnricherConfig {
	return InboundEnricherConfig{RecentContextSize: DefaultRecentContextSize}
}

// TestEnrichRecentContextGroupMention is the MUL-3084 core: a bare @-bot
// mention in a group (no quote, no forward) gets the surrounding
// conversation inlined as a <recent_context> block ahead of the user's
// own message. The trigger message is excluded; speakers are labeled
// positionally with Bot replies labeled "Bot"; oldest-first ordering.
func TestEnrichRecentContextGroupMention(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	// Lark returns newest-first; include the trigger itself to prove it
	// is filtered back out.
	fake.byChat["oc_g"] = []LarkMessage{
		textMsg("om_trigger", "ou_user", "总结一下", "3000"),
		appMsg("om_bot", "你好", "2500"),
		textMsg("om_b", "ou_bob", "明天发布", "2000"),
		textMsg("om_a", "ou_alice", "我改完了登录页", "1000"),
	}
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "总结一下",
		CreateTime:     "3000", // 3000ms -> end_time 3s
	}

	out := enrich(t, fake, in, groupCfg())

	want := `<recent_context count="3">
[User 1]: 我改完了登录页
[User 2]: 明天发布
[Bot]: 你好
</recent_context>

总结一下`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	if len(fake.listCalls) != 1 || fake.listCalls[0] != "oc_g" {
		t.Errorf("expected one ListChatMessages(oc_g), got %v", fake.listCalls)
	}
	if len(fake.calls) != 0 {
		t.Errorf("no GetMessage expected, got %v", fake.calls)
	}
	// The window uses the production default size and is anchored to the
	// trigger's time (millis -> seconds).
	if got := fake.listParams[0].PageSize; got != DefaultRecentContextSize {
		t.Errorf("page size = %d, want %d", got, DefaultRecentContextSize)
	}
	if got := fake.listParams[0].EndTime; got != 3 {
		t.Errorf("end_time = %d, want 3 (3000ms -> 3s)", got)
	}
}

// TestEnrichRecentContextResolvesNames covers the MUL-3084 follow-up:
// speakers in <recent_context> show real display names (not User 1/2),
// and the user's own @-message is labeled with the sender's name so the
// agent knows WHO @-mentioned it.
func TestEnrichRecentContextResolvesNames(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.userNames = map[string]string{
		"ou_alice":   "Alice",
		"ou_bob":     "Bob",
		"ou_charlie": "Charlie",
	}
	fake.byChat["oc_g"] = []LarkMessage{
		textMsg("om_trigger", "ou_charlie", "总结一下", "3000"),
		textMsg("om_b", "ou_bob", "明天发布", "2000"),
		textMsg("om_a", "ou_alice", "我改完了登录页", "1000"),
	}
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		SenderOpenID:   "ou_charlie",
		Body:           "总结一下",
		CreateTime:     "3000",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `<recent_context count="2">
[Alice]: 我改完了登录页
[Bob]: 明天发布
</recent_context>

[Charlie]: 总结一下`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	if len(fake.userCalls) != 1 {
		t.Fatalf("expected one BatchGetUsers call, got %d", len(fake.userCalls))
	}
	// The batch must include the surrounding speakers AND the trigger sender.
	got := map[string]bool{}
	for _, id := range fake.userCalls[0] {
		got[id] = true
	}
	for _, want := range []string{"ou_alice", "ou_bob", "ou_charlie"} {
		if !got[want] {
			t.Errorf("BatchGetUsers missing id %q (got %v)", want, fake.userCalls[0])
		}
	}
}

// TestEnrichRecentContextNameFallback pins the mixed case: a sender whose
// name resolved shows the name; one that did not falls back to positional
// "User N"; and an unresolved trigger sender leaves the core unlabeled.
func TestEnrichRecentContextNameFallback(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.userNames = map[string]string{"ou_alice": "Alice"} // bob + charlie unresolved
	fake.byChat["oc_g"] = []LarkMessage{
		textMsg("om_trigger", "ou_charlie", "总结一下", "3000"),
		textMsg("om_b", "ou_bob", "明天发布", "2000"),
		textMsg("om_a", "ou_alice", "我改完了登录页", "1000"),
	}
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		SenderOpenID:   "ou_charlie",
		Body:           "总结一下",
		CreateTime:     "3000",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `<recent_context count="2">
[Alice]: 我改完了登录页
[User 1]: 明天发布
</recent_context>

总结一下`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
}

// TestEnrichRecentContextWithQuotedReply composes both expansions: the
// recent_context block comes first (broadest), then the quoted parent,
// then the user's prose. The quoted parent is excluded from the
// recent_context window so it isn't duplicated.
//
// It also pins the MUL-3084 review fix: the quoted parent's sender
// (ou_alice) is NOT in the recent window, yet still resolves to a real
// name ("Alice") — i.e. quoted/forwarded senders are folded into the same
// Contact batch as the recent-window senders, not left as "User N".
func TestEnrichRecentContextWithQuotedReply(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.userNames = map[string]string{"ou_alice": "Alice", "ou_bob": "Bob"}
	fake.byID["om_parent"] = []LarkMessage{
		textMsg("om_parent", "ou_alice", "删除按钮加一下", "1000"),
	}
	fake.byChat["oc_g"] = []LarkMessage{
		textMsg("om_trigger", "ou_user", "去做", "3000"),
		textMsg("om_x", "ou_bob", "顺便看下样式", "2000"),
		textMsg("om_parent", "ou_alice", "删除按钮加一下", "1000"),
	}
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "去做",
		ParentID:       "om_parent",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `<recent_context count="1">
[Bob]: 顺便看下样式
</recent_context>

<quoted_message message_id="om_parent" sender="Alice" type="text">
删除按钮加一下
</quoted_message>

去做`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	if len(fake.listCalls) != 1 || fake.listCalls[0] != "oc_g" {
		t.Errorf("expected one ListChatMessages(oc_g), got %v", fake.listCalls)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "om_parent" {
		t.Errorf("expected one GetMessage(om_parent), got %v", fake.calls)
	}
	// The single name batch must include the quoted parent's sender even
	// though it is not in the recent window.
	if len(fake.userCalls) != 1 {
		t.Fatalf("expected one BatchGetUsers call, got %d", len(fake.userCalls))
	}
	found := false
	for _, id := range fake.userCalls[0] {
		if id == "ou_alice" {
			found = true
		}
	}
	if !found {
		t.Errorf("BatchGetUsers must include quoted parent sender ou_alice, got %v", fake.userCalls[0])
	}
}

// TestEnrichForwardedResolvesNames proves the review fix also covers the
// forwarded transcript: in a group, merge_forward children are folded
// into the same Contact batch and render with real names. Recent prefetch
// is disabled here to isolate the forwarded path; name resolution still
// runs because it is a group chat.
func TestEnrichForwardedResolvesNames(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.userNames = map[string]string{"ou_jiayuan": "Jiayuan", "ou_bohan": "Bohan"}
	fake.byID["om_forward"] = []LarkMessage{
		{MessageID: "om_forward", MessageType: "merge_forward", SenderID: "ou_bohan", SenderType: "user", Content: `{"content":"Merged and Forwarded Message"}`},
		textMsg("c1", "ou_jiayuan", "你们线上的 Multica 能用吗", "1000"),
		textMsg("c2", "ou_bohan", "我这边都能登陆", "2000"),
	}
	in := InboundMessage{
		MessageType:    "merge_forward",
		MessageID:      "om_forward",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		SenderOpenID:   "ou_bohan",
	}

	out := enrich(t, fake, in, InboundEnricherConfig{})

	want := `<forwarded_messages count="2">
[Jiayuan]: 你们线上的 Multica 能用吗
[Bohan]: 我这边都能登陆
</forwarded_messages>`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	if len(fake.userCalls) != 1 {
		t.Fatalf("expected one BatchGetUsers call, got %d", len(fake.userCalls))
	}
}

// TestEnrichRecentContextFetchError degrades to a visible placeholder on
// a list failure, without blocking ingestion or dropping the user's body.
func TestEnrichRecentContextFetchError(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.errByChat["oc_g"] = errors.New("boom")
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "在干嘛",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `<recent_context type="error">[unable to fetch recent context]</recent_context>

在干嘛`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
}

// TestEnrichRecentContextEmptyWindow emits NO block (not an empty one)
// when the only message in the window is the trigger itself.
func TestEnrichRecentContextEmptyWindow(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byChat["oc_g"] = []LarkMessage{
		textMsg("om_trigger", "ou_user", "在吗", "1000"),
	}
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "在吗",
	}

	out := enrich(t, fake, in, groupCfg())

	if out.Body != "在吗" {
		t.Errorf("body = %q, want unchanged %q", out.Body, "在吗")
	}
	if len(fake.listCalls) != 1 {
		t.Errorf("expected one ListChatMessages, got %v", fake.listCalls)
	}
}

// TestEnrichRecentContextSkippedCases pins the three conditions under
// which the prefetch must NOT fire: p2p chats, group messages not
// addressed to the Bot, and a disabled window (size 0). In all three the
// body is untouched and no list call is made.
func TestEnrichRecentContextSkippedCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		msg  InboundMessage
		cfg  InboundEnricherConfig
	}{
		{
			name: "p2p chat",
			msg:  InboundMessage{MessageType: "text", MessageID: "om1", ChatID: "oc_p", ChatType: ChatTypeP2P, AddressedToBot: true, Body: "hi"},
			cfg:  groupCfg(),
		},
		{
			name: "group but not addressed",
			msg:  InboundMessage{MessageType: "text", MessageID: "om1", ChatID: "oc_g", ChatType: ChatTypeGroup, AddressedToBot: false, Body: "闲聊"},
			cfg:  groupCfg(),
		},
		{
			name: "prefetch disabled (size 0)",
			msg:  InboundMessage{MessageType: "text", MessageID: "om1", ChatID: "oc_g", ChatType: ChatTypeGroup, AddressedToBot: true, Body: "在吗"},
			cfg:  InboundEnricherConfig{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := newEnricherFake()
			out := enrich(t, fake, tc.msg, tc.cfg)
			if out.Body != tc.msg.Body {
				t.Errorf("body = %q, want unchanged %q", out.Body, tc.msg.Body)
			}
			if len(fake.listCalls) != 0 {
				t.Errorf("expected no ListChatMessages, got %v", fake.listCalls)
			}
		})
	}
}
