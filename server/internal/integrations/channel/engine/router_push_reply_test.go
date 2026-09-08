package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakePushReplies is the PushReplyPoster test double. LookupPush is keyed by
// the platform message id the caller passes; lookedUpWith records the
// installation the router scoped the lookup to, which is the other half of the
// real (installationID, channelMessageID) key and the isolation boundary
// between two installations that saw the same platform message id.
type fakePushReplies struct {
	byMessageID  map[string]db.ChannelPushMessage
	posted       []string
	lookedUpWith []pgtype.UUID
	result       PushReplyResult
	lookupErr    error
	postErr      error
}

func (f *fakePushReplies) LookupPush(_ context.Context, installationID pgtype.UUID, id string) (db.ChannelPushMessage, bool, error) {
	f.lookedUpWith = append(f.lookedUpWith, installationID)
	if f.lookupErr != nil {
		return db.ChannelPushMessage{}, false, f.lookupErr
	}
	row, ok := f.byMessageID[id]
	return row, ok, nil
}

func (f *fakePushReplies) PostPushReplyComment(_ context.Context, _ db.ChannelPushMessage, _ pgtype.UUID, content string) (PushReplyResult, error) {
	if f.postErr != nil {
		return PushReplyResult{}, f.postErr
	}
	f.posted = append(f.posted, content)
	return f.result, nil
}

// pushRow is a ledger row as the notifier would have written it: in the same
// workspace the harness's installation serves. Tests that care about the
// workspace boundary override it.
func pushRow(t *testing.T) db.ChannelPushMessage {
	t.Helper()
	return db.ChannelPushMessage{WorkspaceID: activeResolved(t).WorkspaceID}
}

// newHarnessWithPushReplies rebuilds the router with a PushReplies poster
// wired in, reusing every other fake exactly as newHarness configured them.
// Mirrors the existing pattern of overriding h.router with a custom
// RouterConfig (see TestRouter_MediaResolverTimeoutAppendsOriginalMessage).
func newHarnessWithPushReplies(t *testing.T, f PushReplyPoster) *harness {
	t.Helper()
	h := newHarness(t)
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{
		Logger: discardLogger(), Lifecycle: h.lifecycle, PushReplies: f,
	})
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst,
		Identity:     h.ident,
		Dedup:        h.dedup,
		Session:      h.binder,
		Audit:        h.audit,
		Replier:      h.replier,
		Typing:       h.typing,
		Media:        h.media,
		OriginType:   "lark_chat",
	})
	return h
}

// lastResult waits for the detached replier to receive a Result and returns
// the most recent one. The replier is always invoked off the ACK path (see
// Router.scheduleReply), so every push-reply assertion needs this instead of
// reading Handle's return value.
func lastResult(t *testing.T, h *harness) Result {
	t.Helper()
	var res Result
	if !waitFor(time.Second, func() bool {
		calls := h.replier.calls()
		if len(calls) == 0 {
			return false
		}
		res = calls[len(calls)-1]
		return true
	}) {
		t.Fatal("replier never received a Result")
	}
	return res
}

// The core routing claim: a reply to a push leaves the chat pipeline.
func TestPushReplyDoesNotTouchTheChatPipeline(t *testing.T) {
	f := &fakePushReplies{
		byMessageID: map[string]db.ChannelPushMessage{"om_push_1": pushRow(t)},
		result:      PushReplyResult{Posted: true, Message: "已记录"},
	}
	h := newHarnessWithPushReplies(t, f)

	msg := p2pMessage(t)
	msg.Text = "确认审核"
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	res := lastResult(t, h)

	if res.Outcome != OutcomePushReply {
		t.Fatalf("Outcome = %q, want %q", res.Outcome, OutcomePushReply)
	}
	if res.PushReplyText != "已记录" {
		t.Errorf("PushReplyText = %q", res.PushReplyText)
	}
	if len(f.posted) != 1 || f.posted[0] != "确认审核" {
		t.Errorf("posted = %v, want one entry with the reply text", f.posted)
	}
	// The two things the spec forbids on this path.
	if h.binder.ensureCalls != 0 || h.binder.startCalls != 0 {
		t.Errorf("session resolution ran: ensure=%d start=%d", h.binder.ensureCalls, h.binder.startCalls)
	}
	if h.issues.called {
		t.Error("issue creation ran; /issue must not be parsed on this path")
	}
}

// A "/issue Foo" typed as a reply to a push is a decision, not a command.
func TestPushReplyDoesNotParseIssueCommand(t *testing.T) {
	f := &fakePushReplies{
		byMessageID: map[string]db.ChannelPushMessage{"om_push_1": pushRow(t)},
		result:      PushReplyResult{Posted: true, Message: "已记录"},
	}
	h := newHarnessWithPushReplies(t, f)

	msg := p2pMessage(t)
	msg.Text = "/issue 顺便再建一个"
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	res := lastResult(t, h)

	if res.Outcome != OutcomePushReply {
		t.Fatalf("Outcome = %q, want %q", res.Outcome, OutcomePushReply)
	}
	if h.issues.called {
		t.Error("an /issue command was parsed out of a push reply")
	}
}

// Slack reports only a thread-level id, so RootID is the fallback key.
func TestPushReplyFallsBackToRootID(t *testing.T) {
	f := &fakePushReplies{
		byMessageID: map[string]db.ChannelPushMessage{"root_1": pushRow(t)},
		result:      PushReplyResult{Posted: true, Message: "已记录"},
	}
	h := newHarnessWithPushReplies(t, f)

	msg := p2pMessage(t)
	msg.Text = "确认审核"
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "not_a_push", RootID: "root_1"}

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res := lastResult(t, h); res.Outcome != OutcomePushReply {
		t.Fatalf("Outcome = %q, want the RootID fallback to hit", res.Outcome)
	}
}

// A reply to an ordinary agent message must behave exactly as before.
func TestReplyToANonPushTakesTheChatPath(t *testing.T) {
	f := &fakePushReplies{byMessageID: map[string]db.ChannelPushMessage{}}
	h := newHarnessWithPushReplies(t, f)

	msg := p2pMessage(t)
	msg.Text = "再补充一句"
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_ordinary"}

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	res := lastResult(t, h)
	if res.Outcome == OutcomePushReply || res.Outcome == OutcomePushReplyDenied {
		t.Fatalf("Outcome = %q; a non-push reply must stay on the chat path", res.Outcome)
	}
	if h.binder.ensureCalls == 0 {
		t.Error("chat path did not run")
	}
}

// No ReplyTo at all: the lookup must not even be attempted.
func TestNoReplyToSkipsTheLookup(t *testing.T) {
	f := &fakePushReplies{lookupErr: errors.New("LookupPush must not be called")}
	h := newHarnessWithPushReplies(t, f)

	msg := p2pMessage(t)
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res := lastResult(t, h); res.Outcome == OutcomePushReply {
		t.Fatal("a message with no ReplyTo entered the push path")
	}
}

// A denial still reaches the user; silence would look like the bot is broken.
func TestPushReplyDenialRepliesAndWritesNothing(t *testing.T) {
	f := &fakePushReplies{
		byMessageID: map[string]db.ChannelPushMessage{"om_push_1": pushRow(t)},
		result:      PushReplyResult{Posted: false, Message: "你没有权限回复这条推送。"},
	}
	h := newHarnessWithPushReplies(t, f)

	msg := p2pMessage(t)
	msg.Text = "确认审核"
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	res := lastResult(t, h)
	if res.Outcome != OutcomePushReplyDenied {
		t.Fatalf("Outcome = %q, want %q", res.Outcome, OutcomePushReplyDenied)
	}
	if res.PushReplyText == "" {
		t.Error("denial carried no text; the user would see silence")
	}
	if h.binder.ensureCalls != 0 {
		t.Error("a denied reply still ran the chat path")
	}
}

// A ledger row names its own workspace, and everything downstream — the
// membership check, the issue lookup, the comment write — trusts that name. The
// lookup key is only (installation, message id), so nothing in the query itself
// proves the row belongs to the workspace this installation serves. If the two
// ever disagree, the reply must not be acted on.
func TestPushReplyRefusesARowFromAnotherWorkspace(t *testing.T) {
	foreign := uuidFromString(t, "77777777-7777-7777-7777-777777777777")
	f := &fakePushReplies{
		byMessageID: map[string]db.ChannelPushMessage{
			"om_push_1": {WorkspaceID: foreign},
		},
		result: PushReplyResult{Posted: true, Message: "已记录"},
	}
	h := newHarnessWithPushReplies(t, f)

	msg := p2pMessage(t)
	msg.Text = "确认审核"
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

	if err := h.router.Handle(context.Background(), msg); err == nil {
		t.Fatal("Handle returned nil for a push row outside the installation's workspace")
	}
	if len(f.posted) != 0 {
		t.Errorf("posted %v; a cross-workspace row must not reach the comment write", f.posted)
	}
	if h.binder.ensureCalls != 0 {
		t.Error("a cross-workspace row fell through to the chat path")
	}
}

// /new and /clear are chat-session controls with no meaning on a path that
// never touches a session. Telegram hands CommandText over with the directive
// still attached (inbound.go sets commandText before stripping, so downstream
// classifiers can see the original source) — so reading CommandText raw would
// put "/clear 确认审核" into the issue thread as the member's words.
func TestPushReplyStripsAControlDirective(t *testing.T) {
	for _, directive := range []string{"/clear", "/new"} {
		t.Run(directive, func(t *testing.T) {
			f := &fakePushReplies{
				byMessageID: map[string]db.ChannelPushMessage{"om_push_1": pushRow(t)},
				result:      PushReplyResult{Posted: true, Message: "已记录"},
			}
			h := newHarnessWithPushReplies(t, f)

			msg := p2pMessage(t)
			msg.Text = "确认审核"
			msg.CommandText = directive + " 确认审核"
			msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

			if err := h.router.Handle(context.Background(), msg); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			lastResult(t, h)

			if len(f.posted) != 1 || f.posted[0] != "确认审核" {
				t.Fatalf("posted = %v, want the directive stripped", f.posted)
			}
		})
	}
}

// The feature must be inert until Task 8 wires it: newHarness never sets
// RouterConfig.PushReplies, so it defaults to nil.
func TestNilPushRepliesLeavesTheOldPathUntouched(t *testing.T) {
	h := newHarness(t)

	msg := p2pMessage(t)
	msg.Text = "确认审核"
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if res := lastResult(t, h); res.Outcome == OutcomePushReply {
		t.Fatal("push path ran with no poster configured")
	}
}

// The comment body is the sender's own words, not the platform's rendering of
// the conversation. Lark's enricher prepends a <quoted_message> block to Text
// whenever ParentID is set (inbound_enricher.go), and a push reply always has
// ParentID set — so on the one adapter that closes this loop, Text is the push
// we ourselves sent with the reply stapled underneath. CommandText is the
// pre-enrichment body (lark/ws_frame_decoder.go copies it before the enricher
// runs), and every other adapter populates it the same way.
//
// Posting Text would put our own deep link and 「直接回复本条消息即可处理。」
// into the issue thread as if a human had written them, and hand the woken
// agent its own push back as new instruction.
func TestPushReplyPostsTheSendersOwnWordsNotTheQuotedPush(t *testing.T) {
	f := &fakePushReplies{
		byMessageID: map[string]db.ChannelPushMessage{"om_push_1": pushRow(t)},
		result:      PushReplyResult{Posted: true, Message: "已记录"},
	}
	h := newHarnessWithPushReplies(t, f)

	msg := p2pMessage(t)
	msg.Text = "<quoted_message>\n**[状态变更] Ship it**\nhttps://app.example.com/acme/issues/x\n" +
		"直接回复本条消息即可处理。\n</quoted_message>\n\n确认审核"
	msg.CommandText = "确认审核"
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	lastResult(t, h)

	if len(f.posted) != 1 {
		t.Fatalf("posted %d comments, want 1", len(f.posted))
	}
	if f.posted[0] != "确认审核" {
		t.Errorf("posted %q, want the sender's own words alone", f.posted[0])
	}
}

// An image-only or sticker reply carries no words. The router must not treat
// whatever the adapter put in CommandText as the member's decision: the shape
// Lark actually sends is a bracketed placeholder, not an empty string
// (ws_frame_decoder.go copies the flattened body into CommandBody verbatim,
// and content_flatten.go renders an image as "[Image]"). Posting that would
// put "[Image]" into the issue thread as a human verdict and wake the agent
// on it.
//
// Both shapes are covered: the placeholder Lark sends, and the empty string a
// stricter adapter would send. Both must reach PostPushReplyComment with
// empty content, whose own no-words precondition denies them — proven
// separately by TestPostPushReplyCommentRejectsEmptyContent in
// internal/handler.
func TestPushReplyWithNoTypedWordsPostsEmptyContent(t *testing.T) {
	cases := []struct {
		name        string
		msgType     channel.MsgType
		commandText string
	}{
		{"lark image placeholder", channel.MsgTypeImage, "[Image]"},
		{"lark sticker placeholder", channel.MsgTypeUnknown, "[Sticker]"},
		{"adapter sends nothing", channel.MsgTypeImage, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakePushReplies{
				byMessageID: map[string]db.ChannelPushMessage{"om_push_1": pushRow(t)},
				result:      PushReplyResult{Posted: true, Message: "已记录"},
			}
			h := newHarnessWithPushReplies(t, f)

			msg := p2pMessage(t)
			msg.Type = tc.msgType
			msg.Text = "<quoted_message>\n**[状态变更] Ship it**\n</quoted_message>"
			msg.CommandText = tc.commandText
			msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

			if err := h.router.Handle(context.Background(), msg); err != nil {
				t.Fatalf("Handle: %v", err)
			}
			lastResult(t, h)

			if len(f.posted) != 1 || f.posted[0] != "" {
				t.Fatalf("posted = %v, want a single empty-content post — a wordless reply became a decision", f.posted)
			}
		})
	}
}

// The ledger key is (installation, platform message id). Two installations can
// see the same id, so the router must scope every lookup to the installation it
// resolved for this message — passing pgtype.UUID{} or some other installation
// would let a reply in one workspace's IM find another workspace's push.
func TestPushReplyLooksUpUnderTheResolvedInstallation(t *testing.T) {
	f := &fakePushReplies{
		byMessageID: map[string]db.ChannelPushMessage{"om_push_1": pushRow(t)},
		result:      PushReplyResult{Posted: true, Message: "已记录"},
	}
	h := newHarnessWithPushReplies(t, f)

	msg := p2pMessage(t)
	msg.Text = "确认审核"
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	lastResult(t, h)

	if len(f.lookedUpWith) == 0 {
		t.Fatal("LookupPush was never called")
	}
	want := h.inst.inst.ID
	for i, got := range f.lookedUpWith {
		if got != want {
			t.Errorf("lookup %d scoped to installation %v, want %v", i, got, want)
		}
	}
}

// A database fault is not a verdict. It leaves Handle as a real error so the
// adapter retries, and the sender is told nothing — answering a fault with any
// of the product messages would report a decision that was never recorded.
func TestPushReplyPostFaultIsAnErrorAndSaysNothing(t *testing.T) {
	f := &fakePushReplies{
		byMessageID: map[string]db.ChannelPushMessage{"om_push_1": pushRow(t)},
		postErr:     errors.New("connection reset"),
	}
	h := newHarnessWithPushReplies(t, f)

	msg := p2pMessage(t)
	msg.Text = "确认审核"
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

	if err := h.router.Handle(context.Background(), msg); err == nil {
		t.Fatal("Handle returned nil for an infrastructure fault")
	}
	if calls := h.replier.calls(); len(calls) != 0 {
		t.Errorf("replied %v; a fault must not reach the sender", calls)
	}
	if h.binder.ensureCalls != 0 {
		t.Error("a faulted push reply fell through to the chat path")
	}
}

// A lookup fault is the same: the reply is neither posted nor answered, and it
// must not silently become an ordinary chat turn.
func TestPushReplyLookupFaultDoesNotFallThroughToChat(t *testing.T) {
	f := &fakePushReplies{lookupErr: errors.New("connection reset")}
	h := newHarnessWithPushReplies(t, f)

	msg := p2pMessage(t)
	msg.Text = "确认审核"
	msg.ReplyTo = &channel.ReplyCtx{MessageID: "om_push_1"}

	if err := h.router.Handle(context.Background(), msg); err == nil {
		t.Fatal("Handle returned nil for a lookup fault")
	}
	if h.binder.ensureCalls != 0 {
		t.Error("a failed lookup fell through to the chat path")
	}
}
