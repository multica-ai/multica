package lark

import (
	"context"
	"errors"
	"strings"
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

// threadTextMsg is textMsg tagged with a Lark topic (话题) id, so tests
// can seed a chat whose returned window interleaves several topics.
func threadTextMsg(id, sender, text, createTime, threadID string) LarkMessage {
	m := textMsg(id, sender, text, createTime)
	m.ThreadID = threadID
	return m
}

// cardMsg builds a Bot-sent interactive card (sender_type "app",
// msg_type "interactive") — the shape the Bot's markdown replies take,
// which flattens to a zero-signal "[interactive card]" placeholder.
func cardMsg(id, createTime string) LarkMessage {
	return LarkMessage{
		MessageID:   id,
		MessageType: "interactive",
		Content:     `{"type":"template"}`,
		SenderID:    "cli_bot",
		SenderType:  "app",
		CreateTime:  createTime,
	}
}

func assertNoRecentContextFetchPlaceholder(t *testing.T, body string) {
	t.Helper()
	if strings.Contains(body, `<recent_context type="error">`) ||
		strings.Contains(body, "[unable to fetch recent context]") {
		t.Fatalf("recent context fetch placeholder leaked into body: %q", body)
	}
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
	// A non-topic group @-mention uses the chat container, never a thread
	// scope — the #5835 topic path must not touch normal-group behavior.
	if got := fake.listParams[0].ThreadID; got != "" {
		t.Errorf("chat-level fetch must not set ThreadID, got %q", got)
	}
}

// TestEnrichRecentContextTopicExcludesOtherTopics is the #5835 core: an
// @-mention inside a Lark topic (话题) must see ONLY that topic's messages,
// never a sibling topic that shares the chat_id. The fetch is topic-scoped
// (ThreadID set, no end_time), and even though the fake returns interleaved
// sibling-topic items the enricher's fail-closed thread_id filter drops
// them, so no sibling content can leak into this topic's context (and,
// downstream, its persisted turn).
func TestEnrichRecentContextTopicExcludesOtherTopics(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byChat["oc_g"] = []LarkMessage{
		threadTextMsg("om_trigger", "ou_user", "总结一下", "3000", "th_a"),
		threadTextMsg("om_b2", "ou_carol", "话题B的机密", "2500", "th_b"),
		threadTextMsg("om_a2", "ou_bob", "话题A第二条", "2000", "th_a"),
		threadTextMsg("om_b1", "ou_dave", "话题B第一条", "1500", "th_b"),
		threadTextMsg("om_a1", "ou_alice", "话题A第一条", "1000", "th_a"),
	}
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "总结一下",
		CreateTime:     "3000",
		ThreadID:       "th_a",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `<recent_context count="2">
[User 1]: 话题A第一条
[User 2]: 话题A第二条
</recent_context>

总结一下`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	// Exactly one fetch, scoped to the trigger's topic, with no end_time
	// (the thread container rejects it).
	if len(fake.listParams) != 1 {
		t.Fatalf("expected one ListChatMessages call, got %d", len(fake.listParams))
	}
	if got := fake.listParams[0].ThreadID; got != "th_a" {
		t.Errorf("list ThreadID = %q, want th_a (thread-scoped fetch)", got)
	}
	if got := fake.listParams[0].EndTime; got != 0 {
		t.Errorf("thread fetch must omit end_time, got EndTime=%d", got)
	}
}

// TestEnrichRecentContextTopicFailsClosedOnThreadID pins the second line of
// defense: if Lark's thread container ever returns an item whose thread_id
// is missing or does not match the trigger's topic, the enricher drops it
// rather than trust it. Only the exact-match item survives.
func TestEnrichRecentContextTopicFailsClosedOnThreadID(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byChat["oc_g"] = []LarkMessage{
		threadTextMsg("om_trigger", "ou_user", "怎么办", "3000", "th_a"),
		threadTextMsg("om_match", "ou_alice", "本话题内容", "2000", "th_a"),
		threadTextMsg("om_other", "ou_bob", "别的话题内容", "1800", "th_b"),
		threadTextMsg("om_missing", "ou_carol", "缺少话题字段", "1500", ""),
	}
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "怎么办",
		CreateTime:     "3000",
		ThreadID:       "th_a",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `<recent_context count="1">
[User 1]: 本话题内容
</recent_context>

怎么办`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
}

// TestEnrichRecentContextTopicDropsMessagesAfterTrigger covers the
// client-side time anchor the topic path needs because the thread
// container can't be bounded by end_time: an item created strictly after
// the @-mention is dropped, keeping the window "up to the @-mention" like
// the chat path.
func TestEnrichRecentContextTopicDropsMessagesAfterTrigger(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byChat["oc_g"] = []LarkMessage{
		threadTextMsg("om_after", "ou_bob", "触发之后才发的", "4000", "th_a"),
		threadTextMsg("om_trigger", "ou_user", "看下上面", "3000", "th_a"),
		threadTextMsg("om_before", "ou_alice", "触发之前的", "2000", "th_a"),
	}
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "看下上面",
		CreateTime:     "3000",
		ThreadID:       "th_a",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `<recent_context count="1">
[User 1]: 触发之前的
</recent_context>

看下上面`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
}

// TestEnrichRecentContextTopicFetchErrorNoChatFallback locks the fail-safe:
// when a topic-scoped fetch fails, the enricher degrades to the readable
// note and NEVER retries as a chat-wide fetch — a chat fallback would
// re-open the exact cross-topic leak this change closes.
func TestEnrichRecentContextTopicFetchErrorNoChatFallback(t *testing.T) {
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
		ThreadID:       "th_a",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `[Recent Lark context unavailable; continuing with the latest message.]

在干嘛`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	assertNoRecentContextFetchPlaceholder(t, out.Body)
	if len(fake.listParams) != 1 {
		t.Fatalf("expected exactly one ListChatMessages call, got %d", len(fake.listParams))
	}
	// The single fetch stayed thread-scoped — no chat-wide fallback.
	if fake.listParams[0].ThreadID != "th_a" {
		t.Errorf("the single fetch must stay thread-scoped, got ThreadID=%q", fake.listParams[0].ThreadID)
	}
}

// TestEnrichRecentContextExcludesBotInteractiveCards pins the #5835
// acceptance item that the Bot's own interactive-card replies (which
// flatten to a useless "[interactive card]" placeholder) are excluded from
// the recent-context window rather than rendered as noise.
func TestEnrichRecentContextExcludesBotInteractiveCards(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byChat["oc_g"] = []LarkMessage{
		textMsg("om_trigger", "ou_user", "总结一下", "3000"),
		cardMsg("om_card", "2500"),
		textMsg("om_a", "ou_alice", "我改完了登录页", "1000"),
	}
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "总结一下",
		CreateTime:     "3000",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `<recent_context count="1">
[User 1]: 我改完了登录页
</recent_context>

总结一下`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	if strings.Contains(out.Body, "[interactive card]") {
		t.Errorf("bot interactive card placeholder leaked into recent_context: %q", out.Body)
	}
}

// TestEnrichRecentContextRendersDeletedItems keeps deleted messages
// user-readable when Lark returns them inside the recent window.
func TestEnrichRecentContextRendersDeletedItems(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byChat["oc_g"] = []LarkMessage{
		textMsg("om_trigger", "ou_user", "总结一下", "3000"),
		{MessageID: "om_deleted", MessageType: "text", Deleted: true, SenderID: "ou_alice", SenderType: "user", CreateTime: "1000"},
	}
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "总结一下",
		CreateTime:     "3000",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `<recent_context count="1">
[User 1]: [deleted message]
</recent_context>

总结一下`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
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

// TestEnrichRecentContextFetchError degrades to a plain, user-readable
// note on a list failure without leaking the old internal XML placeholder
// or dropping the user's body.
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

	want := `[Recent Lark context unavailable; continuing with the latest message.]

在干嘛`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	assertNoRecentContextFetchPlaceholder(t, out.Body)
	if len(fake.listCalls) != 1 {
		t.Errorf("unknown error should not be retried; got calls %v", fake.listCalls)
	}
}

// TestEnrichRecentContextRetriesTransientNetworkError pins the bounded
// retry on a transient failure that leaves the shared enrichment budget
// intact: one retry, and a successful retry renders the real
// recent_context block instead of degrading. It deliberately does NOT use
// context.DeadlineExceeded — that error means the shared ctx is already
// spent, so a second attempt could never recover in production (see
// TestEnrichRecentContextDoneContextDoesNotRetry).
func TestEnrichRecentContextRetriesTransientNetworkError(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.errSeqChat["oc_g"] = []error{errors.New("connection reset by peer"), nil}
	fake.byChat["oc_g"] = []LarkMessage{
		textMsg("om_trigger", "ou_user", "总结一下", "3000"),
		textMsg("om_a", "ou_alice", "我改完了登录页", "1000"),
	}
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "总结一下",
		CreateTime:     "3000",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `<recent_context count="1">
[User 1]: 我改完了登录页
</recent_context>

总结一下`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	if len(fake.listCalls) != 2 {
		t.Errorf("transient network error should retry once; got calls %v", fake.listCalls)
	}
}

// TestEnrichRecentContextDoneContextDoesNotRetry locks Must-fix #1: when
// the shared enrichment budget is already spent, a retryable first failure
// must degrade after ONE attempt rather than fire a second call that can
// only fail again. A pre-cancelled context stands in for that exhausted
// budget (the production entry point wraps Enrich in a ~2s deadline).
func TestEnrichRecentContextDoneContextDoesNotRetry(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	// Would normally be retried (transient), but the context is done.
	fake.errByChat["oc_g"] = errors.New("connection reset by peer")
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "在干嘛",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := NewInboundEnricher(fake, groupCfg())
	out := e.Enrich(ctx, in, InstallationCredentials{AppID: "a", AppSecret: "s"})

	want := `[Recent Lark context temporarily unavailable; continuing with the latest message.]

在干嘛`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	assertNoRecentContextFetchPlaceholder(t, out.Body)
	if len(fake.listCalls) != 1 {
		t.Errorf("a done context must not retry; got calls %v", fake.listCalls)
	}
}

// TestEnrichRecentContextRateLimitedDoesNotRetry locks Must-fix #2: a rate
// limit degrades immediately instead of retrying. It uses the PRODUCTION
// error shape — ListChatMessages returns a plain wrapped error string, not
// an *APIError — so the string classifier path (the one prod actually
// hits) is what gets pinned.
func TestEnrichRecentContextRateLimitedDoesNotRetry(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.errByChat["oc_g"] = errors.New(`lark http client: list chat messages: code=230020 msg="rate limit exceeded"`)
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "在干嘛",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `[Recent Lark context temporarily unavailable; continuing with the latest message.]

在干嘛`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	assertNoRecentContextFetchPlaceholder(t, out.Body)
	if len(fake.listCalls) != 1 {
		t.Errorf("rate limit should not retry; got calls %v", fake.listCalls)
	}
}

// TestEnrichRecentContextProductionErrorShapes covers the classifier on
// the real error shape ListChatMessages returns: a plain wrapped error
// string ("...: code=%d msg=%q"), NOT an *APIError. The typed-APIError
// tests above exercise classifyRecentContextAPIError, but production
// traffic only ever reaches the string path, so pin that too — including
// that token errors DO retry (client refreshes the token) while permission
// and deleted errors do not.
func TestEnrichRecentContextProductionErrorShapes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		err       error
		wantLine  string
		wantCalls int
	}{
		{
			name:      "permission_denied",
			err:       errors.New(`lark http client: list chat messages: code=99991002 msg="no permission"`),
			wantLine:  "[Recent Lark context unavailable: the bot cannot read this chat history. Continuing with the latest message.]",
			wantCalls: 1,
		},
		{
			name:      "message_deleted",
			err:       errors.New(`lark http client: list chat messages: code=230110 msg="message has been deleted"`),
			wantLine:  "[Recent Lark context unavailable: the referenced chat history is deleted or no longer visible. Continuing with the latest message.]",
			wantCalls: 1,
		},
		{
			name:      "token_expired_retries_then_degrades",
			err:       errors.New(`lark http client: list chat messages: code=99991663 msg="access token expired"`),
			wantLine:  "[Recent Lark context temporarily unavailable; continuing with the latest message.]",
			wantCalls: 2,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := newEnricherFake()
			fake.errByChat["oc_g"] = tc.err
			in := InboundMessage{
				MessageType:    "text",
				MessageID:      "om_trigger",
				ChatID:         "oc_g",
				ChatType:       ChatTypeGroup,
				AddressedToBot: true,
				Body:           "在干嘛",
			}

			out := enrich(t, fake, in, groupCfg())

			want := tc.wantLine + "\n\n在干嘛"
			if out.Body != want {
				t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
			}
			assertNoRecentContextFetchPlaceholder(t, out.Body)
			if len(fake.listCalls) != tc.wantCalls {
				t.Errorf("%s: got calls %v want %d", tc.name, fake.listCalls, tc.wantCalls)
			}
		})
	}
}

func TestEnrichRecentContextPermissionDeniedDoesNotRetry(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.errByChat["oc_g"] = &APIError{Op: "list chat messages", Code: 99991002, Msg: "no permission"}
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "查一下上下文",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `[Recent Lark context unavailable: the bot cannot read this chat history. Continuing with the latest message.]

查一下上下文`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	assertNoRecentContextFetchPlaceholder(t, out.Body)
	if len(fake.listCalls) != 1 {
		t.Errorf("permission denial should not retry; got calls %v", fake.listCalls)
	}
}

func TestEnrichRecentContextDeletedOrInvisibleError(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.errByChat["oc_g"] = &APIError{Op: "list chat messages", Code: 230110, Msg: "message has been deleted"}
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatID:         "oc_g",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "接着处理",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `[Recent Lark context unavailable: the referenced chat history is deleted or no longer visible. Continuing with the latest message.]

接着处理`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	assertNoRecentContextFetchPlaceholder(t, out.Body)
	if len(fake.listCalls) != 1 {
		t.Errorf("deleted/invisible error should not retry; got calls %v", fake.listCalls)
	}
}

func TestEnrichRecentContextMissingChatBinding(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	in := InboundMessage{
		MessageType:    "text",
		MessageID:      "om_trigger",
		ChatType:       ChatTypeGroup,
		AddressedToBot: true,
		Body:           "查一下上下文",
	}

	out := enrich(t, fake, in, groupCfg())

	want := `[Recent Lark context unavailable: chat binding is missing. Continuing with the latest message.]

查一下上下文`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	assertNoRecentContextFetchPlaceholder(t, out.Body)
	if len(fake.listCalls) != 0 {
		t.Errorf("missing chat binding should not call ListChatMessages; got %v", fake.listCalls)
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
