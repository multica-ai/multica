package lark

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// enricherFakeClient is a programmable APIClient for enricher tests. It
// returns canned GetMessage results keyed by message_id and records the
// ids it was asked for, so tests can assert both the rendered body and
// the network fan-out (e.g. "no call when nothing to enrich").
type enricherFakeClient struct {
	configured bool
	byID       map[string][]LarkMessage
	errByID    map[string]error
	calls      []string

	// ListChatMessages canned results + recorder, keyed by chat id.
	byChat     map[ChatID][]LarkMessage
	errByChat  map[ChatID]error
	listCalls  []ChatID
	listParams []ListMessagesParams

	// BatchGetUsers canned open_id -> name map + recorder. Empty by
	// default, so speakers fall back to positional "User N".
	userNames map[string]string
	usersErr  error
	userCalls [][]string
}

func newEnricherFake() *enricherFakeClient {
	return &enricherFakeClient{
		configured: true,
		byID:       map[string][]LarkMessage{},
		errByID:    map[string]error{},
		byChat:     map[ChatID][]LarkMessage{},
		errByChat:  map[ChatID]error{},
	}
}

func (f *enricherFakeClient) IsConfigured() bool { return f.configured }
func (f *enricherFakeClient) GetMessage(ctx context.Context, creds InstallationCredentials, id string) ([]LarkMessage, error) {
	f.calls = append(f.calls, id)
	if e, ok := f.errByID[id]; ok {
		return nil, e
	}
	return f.byID[id], nil
}
func (f *enricherFakeClient) ListChatMessages(ctx context.Context, creds InstallationCredentials, p ListMessagesParams) ([]LarkMessage, error) {
	f.listCalls = append(f.listCalls, p.ChatID)
	f.listParams = append(f.listParams, p)
	if e, ok := f.errByChat[p.ChatID]; ok {
		return nil, e
	}
	return f.byChat[p.ChatID], nil
}
func (f *enricherFakeClient) BatchGetUsers(ctx context.Context, creds InstallationCredentials, openIDs []string) (map[string]string, error) {
	f.userCalls = append(f.userCalls, openIDs)
	if f.usersErr != nil {
		return nil, f.usersErr
	}
	out := map[string]string{}
	for _, id := range openIDs {
		if name := f.userNames[id]; name != "" {
			out[id] = name
		}
	}
	return out, nil
}

// Unused-by-enricher methods — present only to satisfy APIClient.
func (f *enricherFakeClient) SendInteractiveCard(context.Context, SendCardParams) (string, error) {
	return "", nil
}
func (f *enricherFakeClient) PatchInteractiveCard(context.Context, PatchCardParams) error { return nil }
func (f *enricherFakeClient) SendTextMessage(context.Context, SendTextParams) (string, error) {
	return "", nil
}
func (f *enricherFakeClient) SendDirectTextMessage(context.Context, SendDirectTextParams) (string, error) {
	return "", nil
}
func (f *enricherFakeClient) SendMarkdownCard(context.Context, SendMarkdownCardParams) (string, error) {
	return "", nil
}
func (f *enricherFakeClient) SendBindingPromptCard(context.Context, BindingPromptParams) error {
	return nil
}
func (f *enricherFakeClient) GetBotInfo(context.Context, InstallationCredentials) (BotInfo, error) {
	return BotInfo{}, nil
}
func (f *enricherFakeClient) AddMessageReaction(context.Context, AddReactionParams) (string, error) {
	return "", nil
}
func (f *enricherFakeClient) DeleteMessageReaction(context.Context, DeleteReactionParams) error {
	return nil
}

func textMsg(id, sender, text, createTime string) LarkMessage {
	return LarkMessage{
		MessageID:   id,
		MessageType: "text",
		Content:     `{"text":"` + text + `"}`,
		SenderID:    sender,
		SenderType:  "user",
		CreateTime:  createTime,
	}
}

func enrich(t *testing.T, fake *enricherFakeClient, msg InboundMessage, cfg InboundEnricherConfig) InboundMessage {
	t.Helper()
	e := NewInboundEnricher(fake, cfg)
	return e.Enrich(context.Background(), msg, InstallationCredentials{AppID: "a", AppSecret: "s"})
}

// TestEnrichQuotedReply covers the MUL-2951 quoted-reply example: a text
// reply to a prior text message gets the parent inlined as a
// <quoted_message> block ahead of the user's own prose.
func TestEnrichQuotedReply(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byID["om_parent"] = []LarkMessage{
		textMsg("om_parent", "ou_jiayuan", "做一个删除 issue 的按钮吧", "1000"),
	}
	in := InboundMessage{MessageType: "text", MessageID: "om_child", Body: "去实现", ParentID: "om_parent"}

	out := enrich(t, fake, in, InboundEnricherConfig{})

	want := `<quoted_message message_id="om_parent" sender="User 1" type="text">
做一个删除 issue 的按钮吧
</quoted_message>

去实现`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "om_parent" {
		t.Errorf("expected one GetMessage(om_parent), got %v", fake.calls)
	}
}

// TestEnrichMergeForward covers the merge_forward example: the forwarded
// transcript is fetched via GetMessage(forward_id) — whose items[] are
// [sentinel, child…] — and inlined as a <forwarded_messages> block with
// per-speaker labels. The four original lines must all be present.
func TestEnrichMergeForward(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byID["om_forward"] = []LarkMessage{
		{MessageID: "om_forward", MessageType: "merge_forward", SenderID: "ou_bohan", SenderType: "user", Content: `{"content":"Merged and Forwarded Message"}`},
		textMsg("c1", "ou_jiayuan", "你们线上的 Multica 能用吗", "1000"),
		textMsg("c2", "ou_jiayuan", "我这边无法登录", "2000"),
		textMsg("c3", "ou_bohan", "我这边 web 和 desktop 都能登陆", "3000"),
		{MessageID: "c4", MessageType: "image", SenderID: "ou_jiayuan", SenderType: "user", Content: `{"image_key":"img_x"}`, CreateTime: "4000"},
	}
	in := InboundMessage{MessageType: "merge_forward", MessageID: "om_forward"}

	out := enrich(t, fake, in, InboundEnricherConfig{})

	want := `<forwarded_messages count="4">
[User 1]: 你们线上的 Multica 能用吗
[User 1]: 我这边无法登录
[User 2]: 我这边 web 和 desktop 都能登陆
[User 1]: [Image]
</forwarded_messages>`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
}

// TestEnrichMergeForwardSortsByCreateTime ensures children are ordered
// chronologically regardless of the order Lark returns them.
func TestEnrichMergeForwardSortsByCreateTime(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byID["om_f"] = []LarkMessage{
		{MessageID: "om_f", MessageType: "merge_forward", SenderID: "ou_x", SenderType: "user"},
		textMsg("c2", "ou_a", "second", "2000"),
		textMsg("c1", "ou_a", "first", "1000"),
		textMsg("c3", "ou_a", "third", "3000"),
	}
	out := enrich(t, fake, InboundMessage{MessageType: "merge_forward", MessageID: "om_f"}, InboundEnricherConfig{})
	first := strings.Index(out.Body, "first")
	second := strings.Index(out.Body, "second")
	third := strings.Index(out.Body, "third")
	if !(first < second && second < third) {
		t.Errorf("children not chronologically ordered: %q", out.Body)
	}
}

// TestEnrichMergeForwardCap truncates beyond the configured cap and
// flags how many were dropped.
func TestEnrichMergeForwardCap(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byID["om_f"] = []LarkMessage{
		{MessageID: "om_f", MessageType: "merge_forward", SenderID: "ou_x", SenderType: "user"},
		textMsg("c1", "ou_a", "one", "1000"),
		textMsg("c2", "ou_a", "two", "2000"),
		textMsg("c3", "ou_a", "three", "3000"),
		textMsg("c4", "ou_a", "four", "4000"),
	}
	out := enrich(t, fake, InboundMessage{MessageType: "merge_forward", MessageID: "om_f"}, InboundEnricherConfig{MaxForwardChildren: 2})
	if !strings.Contains(out.Body, "... (2 more truncated)") {
		t.Errorf("expected truncation marker, got %q", out.Body)
	}
	if strings.Contains(out.Body, "three") || strings.Contains(out.Body, "four") {
		t.Errorf("over-cap children should be dropped, got %q", out.Body)
	}
	if !strings.Contains(out.Body, `count="4"`) {
		t.Errorf("count should reflect the true total, got %q", out.Body)
	}
}

// TestEnrichQuotedMergeForwardNests covers a quote-reply whose parent is
// itself a merge_forward: the forwarded transcript renders INSIDE the
// quoted block, using the same single GetMessage response.
func TestEnrichQuotedMergeForwardNests(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byID["om_fwd"] = []LarkMessage{
		{MessageID: "om_fwd", MessageType: "merge_forward", SenderID: "ou_bohan", SenderType: "user"},
		textMsg("c1", "ou_a", "line A", "1000"),
		textMsg("c2", "ou_b", "line B", "2000"),
	}
	in := InboundMessage{MessageType: "text", MessageID: "om_child", Body: "see above", ParentID: "om_fwd"}
	out := enrich(t, fake, in, InboundEnricherConfig{})

	if !strings.Contains(out.Body, `<quoted_message message_id="om_fwd" sender="User 1" type="merge_forward">`) {
		t.Errorf("missing quoted wrapper for merge_forward parent: %q", out.Body)
	}
	if !strings.Contains(out.Body, "<forwarded_messages count=\"2\">") {
		t.Errorf("forwarded block should nest inside quoted: %q", out.Body)
	}
	if !strings.Contains(out.Body, "line A") || !strings.Contains(out.Body, "line B") {
		t.Errorf("nested children missing: %q", out.Body)
	}
	if !strings.HasSuffix(out.Body, "see above") {
		t.Errorf("user prose should follow the quoted block: %q", out.Body)
	}
}

// TestEnrichNestedForwardChildIsPlaceholder bounds HTTP fan-out: a child
// that is itself a forward is NOT recursed into.
func TestEnrichNestedForwardChildIsPlaceholder(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byID["om_f"] = []LarkMessage{
		{MessageID: "om_f", MessageType: "merge_forward", SenderID: "ou_x", SenderType: "user"},
		textMsg("c1", "ou_a", "hello", "1000"),
		{MessageID: "c2", MessageType: "merge_forward", SenderID: "ou_a", SenderType: "user", CreateTime: "2000"},
	}
	out := enrich(t, fake, InboundMessage{MessageType: "merge_forward", MessageID: "om_f"}, InboundEnricherConfig{})
	if !strings.Contains(out.Body, "[nested merge_forward, expand manually]") {
		t.Errorf("nested forward child should be a placeholder: %q", out.Body)
	}
	// Only the top forward should have been fetched.
	if len(fake.calls) != 1 {
		t.Errorf("expected exactly one GetMessage, got %v", fake.calls)
	}
}

// TestEnrichQuotedFetchFailureDegrades verifies a parent fetch failure
// degrades to the documented error block and still keeps the user's
// message — it must not block ingestion.
func TestEnrichQuotedFetchFailureDegrades(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.errByID["om_gone"] = errors.New("not found")
	in := InboundMessage{MessageType: "text", MessageID: "om_child", Body: "ping", ParentID: "om_gone"}
	out := enrich(t, fake, in, InboundEnricherConfig{})

	want := `<quoted_message message_id="om_gone" type="error">[unable to fetch]</quoted_message>

ping`
	if out.Body != want {
		t.Errorf("body\n got = %q\nwant = %q", out.Body, want)
	}
}

func TestEnrichQuotedDeletedParentDegrades(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byID["om_del"] = []LarkMessage{{MessageID: "om_del", MessageType: "text", Deleted: true, SenderID: "ou_a", SenderType: "user"}}
	out := enrich(t, fake, InboundMessage{MessageType: "text", Body: "x", ParentID: "om_del"}, InboundEnricherConfig{})
	if !strings.Contains(out.Body, `type="error"`) {
		t.Errorf("deleted parent should degrade to error block: %q", out.Body)
	}
}

func TestEnrichForwardFetchFailureDegrades(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.errByID["om_f"] = errors.New("boom")
	out := enrich(t, fake, InboundMessage{MessageType: "merge_forward", MessageID: "om_f"}, InboundEnricherConfig{})
	if out.Body != `<forwarded_messages type="error">[unable to fetch]</forwarded_messages>` {
		t.Errorf("forward fetch failure should degrade: %q", out.Body)
	}
}

// TestEnrichNoopWhenNothingAttached: a plain message (no parent, not a
// forward) is returned untouched WITHOUT any network call.
func TestEnrichNoopWhenNothingAttached(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	in := InboundMessage{MessageType: "text", MessageID: "om", Body: "hello"}
	out := enrich(t, fake, in, InboundEnricherConfig{})
	if out.Body != "hello" {
		t.Errorf("body should be unchanged, got %q", out.Body)
	}
	if len(fake.calls) != 0 {
		t.Errorf("no GetMessage should be issued, got %v", fake.calls)
	}
}

// TestEnrichSkipsWhenClientUnconfigured: with the stub/unconfigured
// client we must not stamp a fetch error on every reply — skip silently.
func TestEnrichSkipsWhenClientUnconfigured(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.configured = false
	in := InboundMessage{MessageType: "text", MessageID: "om", Body: "hi", ParentID: "om_parent"}
	out := enrich(t, fake, in, InboundEnricherConfig{})
	if out.Body != "hi" {
		t.Errorf("body should be unchanged when client unconfigured, got %q", out.Body)
	}
	if len(fake.calls) != 0 {
		t.Errorf("no GetMessage when unconfigured, got %v", fake.calls)
	}
}

// TestEnrichPreservesCommandBodyForIssueParsing is the regression guard
// for the quote-reply + /issue interaction: enrichment prepends a
// <quoted_message> block (so the enriched Body no longer parses as a
// command), but CommandBody is left untouched and still parses, so
// `/issue` keeps working when typed as a quote-reply.
func TestEnrichPreservesCommandBodyForIssueParsing(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byID["om_parent"] = []LarkMessage{textMsg("om_parent", "ou_j", "做个删除按钮", "1000")}
	in := InboundMessage{
		MessageType: "text",
		MessageID:   "om_child",
		Body:        "/issue 删除 issue 按钮",
		CommandBody: "/issue 删除 issue 按钮",
		ParentID:    "om_parent",
	}
	out := enrich(t, fake, in, InboundEnricherConfig{})

	// Enriched Body now starts with the quoted block → no longer a command.
	if _, ok := parseIssueCommand(out.Body); ok {
		t.Errorf("enriched Body should not parse as /issue (it is prefixed): %q", out.Body)
	}
	// CommandBody is untouched and still parses with the right title.
	cmd, ok := parseIssueCommand(out.CommandBody)
	if !ok || cmd.Title != "删除 issue 按钮" {
		t.Errorf("CommandBody should still parse /issue: cmd=%+v ok=%v", cmd, ok)
	}
	// And the quoted context did land in the stored Body.
	if !strings.Contains(out.Body, "做个删除按钮") {
		t.Errorf("quoted context missing from Body: %q", out.Body)
	}
}

// TestEnrichResolvesMentionsInChildren: @_user_N placeholders in a
// forwarded child resolve to @name via that child's own mentions array.
func TestEnrichResolvesMentionsInChildren(t *testing.T) {
	t.Parallel()
	fake := newEnricherFake()
	fake.byID["om_f"] = []LarkMessage{
		{MessageID: "om_f", MessageType: "merge_forward", SenderID: "ou_x", SenderType: "user"},
		{
			MessageID:   "c1",
			MessageType: "text",
			Content:     `{"text":"@_user_1 看一下"}`,
			SenderID:    "ou_a",
			SenderType:  "user",
			CreateTime:  "1000",
			Mentions:    []LarkMessageMention{{Key: "@_user_1", ID: "ou_alice", Name: "Alice"}},
		},
	}
	out := enrich(t, fake, InboundMessage{MessageType: "merge_forward", MessageID: "om_f"}, InboundEnricherConfig{})
	if !strings.Contains(out.Body, "@Alice 看一下") {
		t.Errorf("child mention not resolved: %q", out.Body)
	}
}
