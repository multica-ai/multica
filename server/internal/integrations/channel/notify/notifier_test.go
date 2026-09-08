package notify

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	testWorkspace = "11111111-1111-1111-1111-111111111111"
	testRecipient = "22222222-2222-2222-2222-222222222222"
	testIssue     = "33333333-3333-3333-3333-333333333333"
	testInboxItem = "44444444-4444-4444-4444-444444444444"
	testInstall   = "55555555-5555-5555-5555-555555555555"
)

func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := util.ParseUUID(s)
	if err != nil {
		t.Fatalf("ParseUUID(%q): %v", s, err)
	}
	return u
}

type fakeQueries struct {
	binding    db.ChannelUserBinding
	bindingErr error
	workspace  db.Workspace
	statusKey  string // what Effective should report for a custom status
	created    []db.CreateChannelPushMessageParams
	createErr  error
}

func (f *fakeQueries) FindChannelBindingForMember(context.Context, db.FindChannelBindingForMemberParams) (db.ChannelUserBinding, error) {
	if f.bindingErr != nil {
		return db.ChannelUserBinding{}, f.bindingErr
	}
	return f.binding, nil
}

func (f *fakeQueries) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	return f.workspace, nil
}

func (f *fakeQueries) GetIssueStatusEntryByKey(_ context.Context, arg db.GetIssueStatusEntryByKeyParams) (db.IssueStatus, error) {
	if f.statusKey == "" || arg.Key != f.statusKey {
		return db.IssueStatus{}, pgx.ErrNoRows
	}
	return db.IssueStatus{Key: arg.Key, Category: "in_review"}, nil
}

func (f *fakeQueries) CreateChannelPushMessage(_ context.Context, arg db.CreateChannelPushMessageParams) (db.ChannelPushMessage, error) {
	if f.createErr != nil {
		return db.ChannelPushMessage{}, f.createErr
	}
	f.created = append(f.created, arg)
	return db.ChannelPushMessage{}, nil
}

type fakeAdapter struct {
	result    DeliverResult
	err       error
	noReplies bool
	calls     int
	lastRef   PushRef
	lastTo    db.ChannelUserBinding
	lastTx    string
}

func (a *fakeAdapter) DeliverDM(_ context.Context, ref PushRef, binding db.ChannelUserBinding, text string) (DeliverResult, error) {
	a.calls++
	a.lastRef = ref
	a.lastTo = binding
	a.lastTx = text
	return a.result, a.err
}

func (a *fakeAdapter) AcceptsReplies() bool { return !a.noReplies }

func newTestNotifier(t *testing.T, q *fakeQueries, a *fakeAdapter) *Notifier {
	t.Helper()
	q.binding.InstallationID = mustUUID(t, testInstall)
	q.binding.ChannelType = "lark"
	if q.binding.ChannelUserID == "" {
		q.binding.ChannelUserID = "ou_target"
	}
	n := New(q, slog.Default(), nil)
	n.Register(map[string]DMDeliverer{"lark": a})
	return n
}

func inReviewEvent() events.Event {
	// issue_id is a *string on the wire: inboxItemToResponse builds nullable
	// columns with util.UUIDToPtr. Keep the pointer here — a plain string
	// fixture lets a string-only parser pass while production writes a NULL
	// issue_id.
	issueID := testIssue
	return events.Event{
		Type:        protocol.EventInboxNew,
		WorkspaceID: testWorkspace,
		Payload: map[string]any{"item": map[string]any{
			"id":             testInboxItem,
			"workspace_id":   testWorkspace,
			"recipient_type": "member",
			"recipient_id":   testRecipient,
			"type":           "status_changed",
			"severity":       "info",
			"issue_id":       &issueID,
			"issue_status":   "in_review",
			"title":          "Ship the thing",
		}},
	}
}

func TestNotifierDeliversAndRecordsAnInReviewPush(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_123"}}
	newTestNotifier(t, q, a).HandleInboxNew(inReviewEvent())

	if a.calls != 1 {
		t.Fatalf("adapter calls = %d, want 1", a.calls)
	}
	if a.lastTo.ChannelUserID != "ou_target" {
		t.Errorf("delivered to %q, want ou_target", a.lastTo.ChannelUserID)
	}
	if a.lastTx == "" {
		t.Error("delivered empty text")
	}
	for _, want := range []string{
		"[待你审核] Ship the thing",
		"任务已进入 in_review，等待你的审核。",
		"审核通过可回复「审核通过」；需要修改请直接说明，Multica 会结合任务上下文继续处理。",
	} {
		if !strings.Contains(a.lastTx, want) {
			t.Errorf("push text %q does not contain %q", a.lastTx, want)
		}
	}
	if strings.Contains(a.lastTx, "[状态变更]") {
		t.Errorf("review handoff exposed the transport event instead of the user action: %q", a.lastTx)
	}
	if len(q.created) != 1 {
		t.Fatalf("ledger rows = %d, want 1", len(q.created))
	}
	got := q.created[0]
	if got.ChannelMessageID != "om_123" {
		t.Errorf("ledger ChannelMessageID = %q, want om_123", got.ChannelMessageID)
	}
	if util.UUIDToString(got.IssueID) != testIssue {
		t.Errorf("ledger IssueID = %q, want %q", util.UUIDToString(got.IssueID), testIssue)
	}
	if util.UUIDToString(got.RecipientUserID) != testRecipient {
		t.Errorf("ledger RecipientUserID = %q, want %q", util.UUIDToString(got.RecipientUserID), testRecipient)
	}
}

func TestNotifierCarriesConfirmationContentAndCommentLink(t *testing.T) {
	t.Setenv("MULTICA_APP_URL", "https://app.example.com")
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_context"}}
	e := inReviewEvent()
	item := e.Payload.(map[string]any)["item"].(map[string]any)
	item["body"] = "请确认方案 A，设计稿：https://docs.example.com/design"
	const commentID = "66666666-6666-6666-6666-666666666666"
	item["details"] = json.RawMessage(`{"comment_id":"` + commentID + `"}`)

	newTestNotifier(t, q, a).HandleInboxNew(e)

	for _, want := range []string{
		"请确认方案 A",
		"https://docs.example.com/design",
		"https://app.example.com/acme/issues/" + testIssue + "#comment-" + commentID,
	} {
		if !strings.Contains(a.lastTx, want) {
			t.Errorf("push text %q does not contain %q", a.lastTx, want)
		}
	}
	if a.lastRef.WebURL != "https://app.example.com/acme/issues/"+testIssue+"#comment-"+commentID {
		t.Errorf("WebURL = %q", a.lastRef.WebURL)
	}
	wantDesktop := "multica://issue/" + testIssue + "?comment=" + commentID + "&workspace=" + testWorkspace
	if a.lastRef.DesktopURL != wantDesktop {
		t.Errorf("DesktopURL = %q, want %q", a.lastRef.DesktopURL, wantDesktop)
	}
	if !a.lastRef.StartTopic {
		t.Error("StartTopic = false, want true for a replyable Issue push")
	}
}

// A platform that cannot carry a reply back must not be handed a message
// promising one. WeCom is this case: the hint would tell every WeCom user to
// reply to a push whose reply is discarded, and the deep link is their only
// route. The ledger row goes with it — an id nothing can ever match.
func TestNotifierDoesNotPromiseRepliesAPlatformCannotCarry(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_nr"}, noReplies: true}
	newTestNotifier(t, q, a).HandleInboxNew(inReviewEvent())

	if a.calls != 1 {
		t.Fatalf("adapter calls = %d, want 1: the push still goes out", a.calls)
	}
	if strings.Contains(a.lastTx, replyHint) || strings.Contains(a.lastTx, reviewReplyHint) {
		t.Errorf("pushed the reply hint to a platform that cannot accept replies: %q", a.lastTx)
	}
	if a.lastRef.StartTopic {
		t.Error("StartTopic = true for a platform that cannot accept replies")
	}
	if len(q.created) != 0 {
		t.Errorf("ledger rows = %d, want 0", len(q.created))
	}
}

// The mirror of the case above: where a reply can come back, the recipient has
// to be told, or the whole in-IM decision path is invisible.
func TestNotifierPromisesRepliesWhereTheyWork(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_r"}}
	newTestNotifier(t, q, a).HandleInboxNew(inReviewEvent())

	if !strings.Contains(a.lastTx, reviewReplyHint) {
		t.Errorf("replyable push carries no reply hint: %q", a.lastTx)
	}
}

// The ledger is an index of replyable pushes. A push nobody can reply to must
// not create a row: it would be a permanent miss that only grows the table.
func TestNotifierDoesNotRecordWhenTheresNothingToReplyTo(t *testing.T) {
	tests := []struct {
		name   string
		result DeliverResult
	}{
		{"handed off to another replica", DeliverResult{State: StateHandedOff}},
		{"delivered without a platform message id", DeliverResult{State: StateDelivered}},
		{"adapter does not support DMs", DeliverResult{State: StateUnsupported}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
			a := &fakeAdapter{result: tt.result}
			newTestNotifier(t, q, a).HandleInboxNew(inReviewEvent())
			if len(q.created) != 0 {
				t.Errorf("ledger rows = %d, want 0", len(q.created))
			}
		})
	}
}

// A pushed-but-unreplyable notification type must reach the user and still
// leave no ledger row, even though the adapter handed back a message id.
func TestNotifierDoesNotRecordUnreplyableTypes(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_qc"}}
	e := inReviewEvent()
	item := e.Payload.(map[string]any)["item"].(map[string]any)
	item["type"] = "quick_create_failed"
	delete(item, "issue_id")
	delete(item, "issue_status")

	newTestNotifier(t, q, a).HandleInboxNew(e)

	if a.calls != 1 {
		t.Errorf("adapter calls = %d, want 1 (quick_create_failed still pushes)", a.calls)
	}
	if len(q.created) != 0 {
		t.Errorf("ledger rows = %d, want 0 (no issue to reply into)", len(q.created))
	}
}

func TestNotifierPushesWorkspaceIdleWithoutAReplyLedger(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_idle"}}
	e := inReviewEvent()
	item := e.Payload.(map[string]any)["item"].(map[string]any)
	item["type"] = "workspace_idle"
	item["title"] = "流程停滞"
	item["body"] = "当前仍有 2 个 in_progress 任务，但所有智能体均已空闲，请检查是否需要继续派发。"
	delete(item, "issue_id")
	delete(item, "issue_status")

	newTestNotifier(t, q, a).HandleInboxNew(e)

	if a.calls != 1 {
		t.Fatalf("adapter calls = %d, want 1", a.calls)
	}
	for _, want := range []string{"[工作区状态] 流程停滞", "2 个 in_progress 任务"} {
		if !strings.Contains(a.lastTx, want) {
			t.Errorf("push text %q does not contain %q", a.lastTx, want)
		}
	}
	if strings.Contains(a.lastTx, replyHint) || strings.Contains(a.lastTx, reviewReplyHint) || strings.Contains(a.lastTx, blockedReplyHint) {
		t.Errorf("workspace-level push promised an issue reply: %q", a.lastTx)
	}
	if len(q.created) != 0 {
		t.Errorf("ledger rows = %d, want 0 for workspace-level notification", len(q.created))
	}
}

func TestNotifierDeliversABlockedStatusWithActionableCopy(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_blocked"}}
	e := inReviewEvent()
	item := e.Payload.(map[string]any)["item"].(map[string]any)
	item["issue_status"] = "blocked"

	newTestNotifier(t, q, a).HandleInboxNew(e)

	if a.calls != 1 {
		t.Fatalf("adapter calls = %d, want 1", a.calls)
	}
	for _, want := range []string{
		"[任务受阻] Ship the thing",
		"任务已进入 blocked，需要你的反馈或处理。",
		"请直接回复所需信息或处理意见，Multica 会结合任务上下文继续处理。",
	} {
		if !strings.Contains(a.lastTx, want) {
			t.Errorf("push text %q does not contain %q", a.lastTx, want)
		}
	}
	if len(q.created) != 1 {
		t.Fatalf("ledger rows = %d, want 1 for a replyable blocked push", len(q.created))
	}
}

func TestNotifierSkipsWhatTheWhitelistRejects(t *testing.T) {
	tests := []struct {
		name       string
		notifType  string
		issueState string
	}{
		{"a done transition", "status_changed", "done"},
		{"an in_progress transition", "status_changed", "in_progress"},
		{"a new comment", "new_comment", ""},
		{"a mention", "mentioned", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
			a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_x"}}
			e := inReviewEvent()
			item := e.Payload.(map[string]any)["item"].(map[string]any)
			item["type"] = tt.notifType
			item["issue_status"] = tt.issueState

			newTestNotifier(t, q, a).HandleInboxNew(e)

			if a.calls != 0 {
				t.Errorf("adapter calls = %d, want 0", a.calls)
			}
		})
	}
}

// A workspace's custom status inherits its category's behaviour. Judging the
// literal key would silently exclude every workspace that renamed in_review.
func TestNotifierNormalisesACustomStatusToItsCategory(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}, statusKey: "awaiting_signoff"}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_custom"}}
	e := inReviewEvent()
	e.Payload.(map[string]any)["item"].(map[string]any)["issue_status"] = "awaiting_signoff"

	newTestNotifier(t, q, a).HandleInboxNew(e)

	if a.calls != 1 {
		t.Errorf("adapter calls = %d, want 1 for a custom status in the in_review category", a.calls)
	}
	if !strings.Contains(a.lastTx, "[待你审核]") {
		t.Errorf("custom in_review-category status did not receive review copy: %q", a.lastTx)
	}
}

func TestNotifierIgnoresAgentRecipients(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_x"}}
	e := inReviewEvent()
	e.Payload.(map[string]any)["item"].(map[string]any)["recipient_type"] = "agent"

	newTestNotifier(t, q, a).HandleInboxNew(e)

	if a.calls != 0 {
		t.Errorf("adapter calls = %d, want 0", a.calls)
	}
}

// An unbound member is not an error. They keep seeing the notification in the
// in-app inbox, which is the degradation WeCom's existing path already set.
func TestNotifierIsANoOpForAnUnboundMember(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}, bindingErr: pgx.ErrNoRows}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_x"}}
	newTestNotifier(t, q, a).HandleInboxNew(inReviewEvent())

	if a.calls != 0 {
		t.Errorf("adapter calls = %d, want 0", a.calls)
	}
	if len(q.created) != 0 {
		t.Errorf("ledger rows = %d, want 0", len(q.created))
	}
}

func TestNotifierSurvivesAnAdapterError(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{err: errors.New("platform refused the message")}
	newTestNotifier(t, q, a).HandleInboxNew(inReviewEvent())

	if len(q.created) != 0 {
		t.Errorf("ledger rows = %d, want 0 after a failed send", len(q.created))
	}
}

// The binding names the platform; a workspace bound to a channel we have no
// adapter for must not panic on the map miss.
func TestNotifierIgnoresAChannelWithNoAdapter(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_x"}}
	n := newTestNotifier(t, q, a)
	q.binding.ChannelType = "dingtalk"

	n.HandleInboxNew(inReviewEvent())

	if a.calls != 0 {
		t.Errorf("adapter calls = %d, want 0", a.calls)
	}
}

// blockingAdapter stands in for a real IM send: it holds the caller until
// released, the way an HTTPS round trip to Lark holds it for as long as the
// platform takes.
type blockingAdapter struct {
	entered  chan struct{}
	release  chan struct{}
	fakeOnce bool
}

func (a *blockingAdapter) DeliverDM(context.Context, PushRef, db.ChannelUserBinding, string) (DeliverResult, error) {
	if !a.fakeOnce {
		a.fakeOnce = true
		close(a.entered)
	}
	<-a.release
	return DeliverResult{State: StateDelivered, MessageID: "om_x"}, nil
}

func (a *blockingAdapter) AcceptsReplies() bool { return true }

// The bus dispatches inline on the publishing goroutine, and inbox:new is
// published from inside the HTTP request that changed the issue — once per
// recipient. If the subscribed handler sent the DM inline, a status change to
// in_review would stall that request on an IM round trip. Subscribe therefore
// detaches; this pins that, because the cost is invisible in every test that
// calls HandleInboxNew directly.
func TestSubscribedPushDoesNotBlockThePublisher(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	q.binding.InstallationID = mustUUID(t, testInstall)
	q.binding.ChannelType = "lark"
	q.binding.ChannelUserID = "ou_target"

	a := &blockingAdapter{entered: make(chan struct{}), release: make(chan struct{})}
	defer close(a.release)

	n := New(q, slog.Default(), nil)
	n.Register(map[string]DMDeliverer{"lark": a})

	bus := events.New()
	n.Subscribe(bus)

	done := make(chan struct{})
	go func() {
		bus.Publish(inReviewEvent())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish did not return while the adapter was still sending: the push is inline on the caller's goroutine")
	}

	// And it really did run — a Publish that returns fast because nothing
	// was dispatched would pass the assertion above for the wrong reason.
	select {
	case <-a.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the adapter was never called: the subscription did not dispatch")
	}
}

type recordedPush struct {
	outcome     string
	channelType string
}

type fakeMetrics struct {
	pushes []recordedPush
}

func (m *fakeMetrics) RecordPush(outcome, channelType string) {
	m.pushes = append(m.pushes, recordedPush{outcome, channelType})
}

// Every inbox:new the notifier sees ends in exactly one outcome, and the
// outcome names why. That "exactly one" is what makes delivered/total a ratio
// an operator can read straight off a dashboard — a path that records twice
// inflates the denominator, and one that records nothing makes a whole class
// of failure invisible, which is the state this counter exists to end.
//
// The channel_type half is asserted too: it is empty exactly on the paths that
// give up before a binding is resolved, and attributing a skip to the wrong
// platform would point an investigation at the wrong adapter.
func TestNotifierRecordsExactlyOneOutcomePerEvent(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*fakeQueries, *fakeAdapter)
		event       func() events.Event
		outcome     string
		channelType string
	}{
		{
			name:        "a replyable push that landed",
			outcome:     OutcomeDelivered,
			channelType: "lark",
		},
		{
			name: "relayed to the replica holding the socket",
			setup: func(_ *fakeQueries, a *fakeAdapter) {
				a.result = DeliverResult{State: StateHandedOff}
			},
			outcome:     OutcomeHandedOff,
			channelType: "lark",
		},
		{
			name: "the platform refused the send",
			setup: func(_ *fakeQueries, a *fakeAdapter) {
				a.err = errors.New("platform refused the message")
			},
			outcome:     OutcomeFailed,
			channelType: "lark",
		},
		{
			name: "sent, but the reply ledger write failed",
			setup: func(q *fakeQueries, _ *fakeAdapter) {
				q.createErr = errors.New("connection reset")
			},
			outcome:     OutcomeRecordFailed,
			channelType: "lark",
		},
		{
			name: "a transition the whitelist rejects",
			event: func() events.Event {
				e := inReviewEvent()
				e.Payload.(map[string]any)["item"].(map[string]any)["issue_status"] = "done"
				return e
			},
			outcome: OutcomeNotWhitelisted,
		},
		{
			name: "an agent recipient has no IM identity",
			event: func() events.Event {
				e := inReviewEvent()
				e.Payload.(map[string]any)["item"].(map[string]any)["recipient_type"] = "agent"
				return e
			},
			outcome: OutcomeNotMember,
		},
		{
			name: "the member is not bound to any channel",
			setup: func(q *fakeQueries, _ *fakeAdapter) {
				q.bindingErr = pgx.ErrNoRows
			},
			outcome: OutcomeUnbound,
		},
		{
			name: "bound to a platform this build cannot push to",
			setup: func(q *fakeQueries, _ *fakeAdapter) {
				q.binding.ChannelType = "dingtalk"
			},
			outcome:     OutcomeNoAdapter,
			channelType: "dingtalk",
		},
		{
			name: "the payload is not the shape we publish",
			event: func() events.Event {
				return events.Event{
					Type: protocol.EventInboxNew, WorkspaceID: testWorkspace, Payload: "nonsense",
				}
			},
			outcome: OutcomeMalformed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
			a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_1"}}
			m := &fakeMetrics{}
			n := newTestNotifier(t, q, a)
			n.metrics = m
			// After newTestNotifier, which fills the binding defaults a setup
			// overriding ChannelType has to win against.
			if tt.setup != nil {
				tt.setup(q, a)
			}
			e := inReviewEvent()
			if tt.event != nil {
				e = tt.event()
			}

			n.HandleInboxNew(e)

			if len(m.pushes) != 1 {
				t.Fatalf("recorded %v, want exactly one outcome", m.pushes)
			}
			got := m.pushes[0]
			if got.outcome != tt.outcome || got.channelType != tt.channelType {
				t.Errorf("recorded (%q, %q), want (%q, %q)",
					got.outcome, got.channelType, tt.outcome, tt.channelType)
			}
		})
	}
}

// A nil sink is a supported configuration, not a missing wire: the metrics
// endpoint is optional, and main.go leaves the field nil when it is off.
func TestNotifierPushesWithNoMetricsSink(t *testing.T) {
	q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
	a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_1"}}
	newTestNotifier(t, q, a).HandleInboxNew(inReviewEvent())

	if a.calls != 1 {
		t.Errorf("adapter calls = %d, want 1", a.calls)
	}
}

func TestNotifierIgnoresMalformedPayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload any
	}{
		{"not a map", "nonsense"},
		{"no item key", map[string]any{}},
		{"item is not a map", map[string]any{"item": 42}},
		{"no recipient", map[string]any{"item": map[string]any{
			"workspace_id": testWorkspace, "recipient_type": "member", "type": "task_failed",
		}}},
		{"no workspace", map[string]any{"item": map[string]any{
			"recipient_id": testRecipient, "recipient_type": "member", "type": "task_failed",
		}}},
		{"unparseable recipient", map[string]any{"item": map[string]any{
			"workspace_id": testWorkspace, "recipient_id": "not-a-uuid",
			"recipient_type": "member", "type": "task_failed",
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &fakeQueries{workspace: db.Workspace{Slug: "acme"}}
			a := &fakeAdapter{result: DeliverResult{State: StateDelivered, MessageID: "om_x"}}
			// Must not panic.
			newTestNotifier(t, q, a).HandleInboxNew(events.Event{
				Type: protocol.EventInboxNew, WorkspaceID: testWorkspace, Payload: tt.payload,
			})
			if a.calls != 0 {
				t.Errorf("adapter calls = %d, want 0", a.calls)
			}
		})
	}
}
