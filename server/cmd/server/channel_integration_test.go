package main

// M1-T8 channel integration tests — the M1 acceptance gate.
//
// These tests exercise the inbound pipeline + binding flow + facade layer
// end-to-end against a real Postgres. Per the T8 directive, they MUST NOT
// silently t.Skip when the database is unreachable: the M1 acceptance bar
// requires a real PG, so the whole point of the gate is broken if these
// tests evaporate to a green build on a dev laptop with `psql` stopped.
//
// Helpers (token plumbing, fakeFeishuClient, in-test IssueService) sit in
// channel_integration_helpers_test.go so this file is purely about the
// scenario assertions per TestCase §10.
//
// Scope (Issue STA-10 §出口测试):
//
//	TC-int-1            unbound user @ Bot → binding link → second @ Bot
//	                    succeeds; channel_user_binding row written;
//	                    channel_bind_token.consumed_at non-null.
//	TC-int-2            group message in bound chat → IssueFacade.CreateIssue
//	                    creates STA-N; reply text sent via Send carries STA-N.
//	TC-int-3            DELETE workspace → channel_chat_binding row gone via
//	                    CASCADE; subsequent inbound message hits the
//	                    "WS_NOT_BOUND" template.
//
// Plus the T4 R1 integration tests deferred from STA-6 (TC-bind-4a /
// TC-bind-4b / TC-risk-token-replay) already live in
// channel_binding_integration_test.go and are exercised by `go test ./...`
// against the same testPool.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel"
	"github.com/multica-ai/multica/server/internal/channel/binding"
	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// requirePool is the M1 acceptance gate: T8 is the milestone exit, so a
// missing testPool is not a "skip" — it's a hard failure. CI / local
// runs without Postgres see a noisy failure pointing at the cause
// instead of a deceptively-green run.
func requirePool(t *testing.T) {
	t.Helper()
	if testPool == nil {
		t.Fatal("M1 acceptance test requires a real Postgres (DATABASE_URL); set up the dev DB before running T8 tests")
	}
}

// TC-int-1 · Binding flow end-to-end.
//
// Scenario (PRD AC3.1–3.4, TestCase §10 TC-int-1):
//
//  1. A new external user (open_id "ou_int1_alice") @-mentions the bot in
//     a *bound* group chat. Identity-bind misses → it issues a binding
//     token, sends the one-shot link via the registered fake channel,
//     and returns Skip so the rest of the pipeline does not fire.
//  2. The user "clicks" the link — simulated here by calling
//     binding.TokenConsumer.Consume with the plaintext we captured from
//     the outbound Send call, then writing the resulting
//     channel_user_binding row exactly the way the real M2 link-handler
//     will (provider, external_user_id, user_id).
//  3. The same user @-mentions the bot a second time. Identity-bind now
//     hits, populates evt.SenderID with the Multica user_id, and the
//     pipeline reaches the dispatch step. We assert the dispatch step
//     ran (Outcome.Terminal == "dispatch") instead of short-circuiting.
//
// PRD ACs covered:
//
//   - AC3.1 (one binding link per platform user)
//   - AC3.2 (link is one-shot)
//   - AC3.3 (binding writes channel_user_binding)
//   - AC3.4 (link expires after 10 minutes — inherited from
//     binding.DefaultTokenTTL; not re-asserted here)
func TestChannelIntegration_TC_int_1_BindingFlow_EndToEnd(t *testing.T) {
	requirePool(t)

	ctx := context.Background()
	const externalUserID = "ou_int1_alice"
	const externalChatID = "oc_int1_chat"
	const provider = "feishu"

	queries := db.New(testPool)
	registry := channel.NewRegistry()
	fake := newFakeChannel(provider)
	if err := registry.Register(fake); err != nil {
		t.Fatalf("register fake channel: %v", err)
	}

	// Bind chat → workspace ahead of time so the dispatcher has a
	// workspace to attribute the message to. This row is the M2-style
	// "owner pair-bound the group" state.
	bindChatToWorkspace(t, provider, externalChatID, port.ChatTypeGroup, testWorkspaceID, testUserID)

	// Wire identity-bind + reply-with-binding-link real implementations.
	// Real-T11 dispatch is replaced by an assert-only step that records
	// what events reached it, so we can prove identity-bind fired Skip
	// the first time and Continue the second time without depending on
	// any service.
	tokenConsumer := binding.NewTokenConsumer(queries)

	dispatch := newRecordingStep("dispatch")
	pipeline := newChannelTestPipeline(testPool, registry, dispatch)

	// First @ Bot: unbound. Pipeline must Skip at identity-bind, push a
	// binding link via the fake channel, and leave consumed_at NULL.
	first := port.InboundEvent{
		ChannelName: provider,
		EventID:     freshEventID(t, provider, "evt_int1_first"),
		Type:        port.EventTypeMessageReceived,
		ChatID:      externalChatID,
		ChatType:    port.ChatTypeGroup,
		SenderID:    externalUserID,
		Text:        "hi bot",
		MessageID:   "om_int1_first",
	}
	out, err := pipeline.Run(ctx, first)
	if err != nil {
		t.Fatalf("first pipeline run: %v", err)
	}
	if out.Terminal != "identity-bind" || out.Decision != inbound.DecisionSkip {
		t.Fatalf("first event: expected identity-bind Skip, got %s/%s", out.Terminal, out.Decision)
	}
	if dispatch.callCount() != 0 {
		t.Fatalf("first event: dispatch must NOT run for unbound user; call count = %d", dispatch.callCount())
	}

	// Capture the binding link the fake channel received. Real adapters
	// would render a card; for M1 the link is plumbed verbatim in the
	// outbound message body so the test can extract it.
	sends := fake.snapshotSends()
	if len(sends) != 1 {
		t.Fatalf("expected exactly 1 outbound binding-link message, got %d", len(sends))
	}
	plaintextToken := extractPlaintextFromBindingMessage(t, sends[0].Text)

	// Verify the token row is present and unconsumed (AC3.2 setup).
	var (
		tokenRowExists bool
		consumedAt     pgtype.Timestamptz
	)
	if err := testPool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM channel_bind_token WHERE provider=$1 AND external_user_id=$2)`,
		provider, externalUserID,
	).Scan(&tokenRowExists); err != nil {
		t.Fatalf("query channel_bind_token: %v", err)
	}
	if !tokenRowExists {
		t.Fatal("channel_bind_token row missing after identity-bind issued the link")
	}
	if err := testPool.QueryRow(ctx,
		`SELECT consumed_at FROM channel_bind_token WHERE provider=$1 AND external_user_id=$2`,
		provider, externalUserID,
	).Scan(&consumedAt); err != nil {
		t.Fatalf("query consumed_at: %v", err)
	}
	if consumedAt.Valid {
		t.Fatalf("channel_bind_token.consumed_at must be NULL before user clicks the link; got %v", consumedAt.Time)
	}

	// Simulate the user clicking the link: consume the token + write the
	// channel_user_binding row. This mirrors what the future M2 web
	// handler will do server-side after the user authenticates with
	// the existing Multica session.
	consumed, err := tokenConsumer.Consume(ctx, plaintextToken)
	if err != nil {
		t.Fatalf("consume binding token: %v", err)
	}
	if !consumed.ConsumedAt.Valid {
		t.Fatal("token row's consumed_at must be Valid after Consume")
	}
	bindUserToMulticaUser(t, provider, externalUserID, testUserID)

	// Second @ Bot: bound now. Pipeline must Continue past identity-bind
	// and reach dispatch.
	second := port.InboundEvent{
		ChannelName: provider,
		EventID:     freshEventID(t, provider, "evt_int1_second"),
		Type:        port.EventTypeMessageReceived,
		ChatID:      externalChatID,
		ChatType:    port.ChatTypeGroup,
		SenderID:    externalUserID,
		Text:        "hi bot, again",
		MessageID:   "om_int1_second",
	}
	out2, err := pipeline.Run(ctx, second)
	if err != nil {
		t.Fatalf("second pipeline run: %v", err)
	}
	if out2.Terminal != "dispatch" || out2.Decision != inbound.DecisionContinue {
		t.Fatalf("second event: expected dispatch Continue, got %s/%s", out2.Terminal, out2.Decision)
	}
	if dispatch.callCount() != 1 {
		t.Fatalf("second event: dispatch must run exactly once for bound user; call count = %d", dispatch.callCount())
	}
	got := dispatch.lastEvent()
	if got.SenderID == "" {
		t.Fatal("identity-bind must populate evt.SenderID from channel_user_binding before reaching dispatch")
	}
}

// TC-int-2 · Group-bound chat creates an issue end-to-end.
//
// Scenario (PRD AC4.1–4.3, TestCase §10 TC-int-2):
//
//   - A bound chat (provider=feishu, chat=oc_int2_chat → testWorkspaceID)
//     receives a group message from a bound user (ou_int2_carol →
//     testUserID). The dispatch step calls IssueFacade.CreateIssue with
//     the workspace + actor resolved from the two binding tables.
//   - The reply step formats "STA-N created" and pushes via Send.
//   - We assert: (a) an `issue` row exists with the expected workspace +
//     created-by, and (b) the fake channel received a Send call carrying
//     the STA-N identifier text.
func TestChannelIntegration_TC_int_2_GroupCreatesIssue(t *testing.T) {
	requirePool(t)

	ctx := context.Background()
	const externalUserID = "ou_int2_carol"
	const externalChatID = "oc_int2_chat"
	const provider = "feishu"
	// Title is unique per test run so a re-run does not collide on the
	// "exactly 1 issue" assertion. The Cleanup deletes the row regardless.
	titleSuffix := fmt.Sprintf("-%d", time.Now().UnixNano())
	expectedTitle := "integration test creates issue from group" + titleSuffix
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM issue WHERE title = $1`, expectedTitle)
	})

	registry := channel.NewRegistry()
	fake := newFakeChannel(provider)
	if err := registry.Register(fake); err != nil {
		t.Fatalf("register fake channel: %v", err)
	}

	// Pre-bind the chat → workspace and the user → multica_user.
	bindChatToWorkspace(t, provider, externalChatID, port.ChatTypeGroup, testWorkspaceID, testUserID)
	bindUserToMulticaUser(t, provider, externalUserID, testUserID)

	// Real-impl dispatch: parses the message and calls IssueFacade.CreateIssue.
	issueSvc := newDirectIssueService(testPool)
	issueFacade := facade.NewIssueFacade(issueSvc)

	dispatch := newCreateIssueDispatchStep(testPool, registry, issueFacade)

	pipeline := newChannelTestPipeline(testPool, registry, dispatch)

	evt := port.InboundEvent{
		ChannelName: provider,
		EventID:     freshEventID(t, provider, "evt_int2_create"),
		Type:        port.EventTypeMessageReceived,
		ChatID:      externalChatID,
		ChatType:    port.ChatTypeGroup,
		SenderID:    externalUserID,
		Text:        "create issue: " + expectedTitle,
		MessageID:   "om_int2_create",
	}
	out, err := pipeline.Run(ctx, evt)
	if err != nil {
		t.Fatalf("pipeline run: %v", err)
	}
	if out.Terminal != "dispatch" || out.Decision != inbound.DecisionContinue {
		t.Fatalf("expected dispatch Continue, got %s/%s", out.Terminal, out.Decision)
	}

	// Assert the issue row exists.
	var (
		issueCount   int
		latestNumber int32
		issuePrefix  string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM issue
		WHERE workspace_id = $1 AND title = $2
	`, testWorkspaceID, expectedTitle).Scan(&issueCount); err != nil {
		t.Fatalf("count created issues: %v", err)
	}
	if issueCount != 1 {
		t.Fatalf("expected exactly 1 issue created with title %q, got %d", expectedTitle, issueCount)
	}

	if err := testPool.QueryRow(ctx, `
		SELECT i.number, w.issue_prefix
		FROM issue i JOIN workspace w ON w.id = i.workspace_id
		WHERE i.workspace_id = $1 AND i.title = $2
	`, testWorkspaceID, expectedTitle).Scan(&latestNumber, &issuePrefix); err != nil {
		t.Fatalf("read created issue identifier: %v", err)
	}
	expectedIdent := fmt.Sprintf("%s-%d", issuePrefix, latestNumber)

	// Assert the fake channel got a "STA-N created" reply containing the
	// identifier we just looked up.
	sends := fake.snapshotSends()
	if len(sends) != 1 {
		t.Fatalf("expected exactly 1 outbound reply, got %d", len(sends))
	}
	if !strings.Contains(sends[0].Text, expectedIdent) {
		t.Fatalf("reply text %q must contain identifier %q", sends[0].Text, expectedIdent)
	}
	if sends[0].ChatID != externalChatID {
		t.Fatalf("reply chat id mismatch: got %q want %q", sends[0].ChatID, externalChatID)
	}
}

// TC-int-3 · Workspace deletion CASCADEs to channel_chat_binding and
// subsequent inbound messages hit the WS_NOT_BOUND template.
//
// Scenario (PRD AC4.1, AC7.x, TestCase §10 TC-int-3):
//
//  1. Create a fresh, dedicated workspace (so we don't blow away the
//     shared testWorkspaceID used by other tests). Bind a chat to it.
//  2. DELETE the workspace. ON DELETE CASCADE on channel_chat_binding
//     means the binding row goes away atomically.
//  3. Send a group message addressed to the now-orphan chat. The
//     dispatch step must classify the chat as unbound and reply with
//     the WS_NOT_BOUND template; no issue rows must be created.
func TestChannelIntegration_TC_int_3_UnbindCascadeAndStopResponse(t *testing.T) {
	requirePool(t)

	ctx := context.Background()
	const externalUserID = "ou_int3_dave"
	const externalChatID = "oc_int3_chat"
	const provider = "feishu"
	const wsSlug = "integration-tests-channel-cascade"
	rejectedTitle := fmt.Sprintf("should be rejected-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM issue WHERE title = $1`, rejectedTitle)
	})

	// Best-effort cleanup of any leftover from a previous run.
	cleanupChatBindings(ctx, provider, externalChatID)
	cleanupChannelUserBinding(ctx, provider, externalUserID)
	if _, err := testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, wsSlug); err != nil {
		t.Fatalf("pre-clean workspace: %v", err)
	}

	// 1. Create the dedicated workspace + member + bind the chat.
	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description)
		VALUES ($1, $2, $3)
		RETURNING id
	`, "Integration Tests Channel Cascade", wsSlug, "TC-int-3 workspace").Scan(&wsID); err != nil {
		t.Fatalf("create scratch workspace: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, wsID, testUserID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	bindChatToWorkspace(t, provider, externalChatID, port.ChatTypeGroup, wsID, testUserID)
	bindUserToMulticaUser(t, provider, externalUserID, testUserID)

	registry := channel.NewRegistry()
	fake := newFakeChannel(provider)
	if err := registry.Register(fake); err != nil {
		t.Fatalf("register fake channel: %v", err)
	}

	issueSvc := newDirectIssueService(testPool)
	issueFacade := facade.NewIssueFacade(issueSvc)
	dispatch := newCreateIssueDispatchStep(testPool, registry, issueFacade)
	pipeline := newChannelTestPipeline(testPool, registry, dispatch)

	// 2. Confirm the chat binding exists, then DELETE the workspace and
	//    confirm CASCADE removed the binding.
	var beforeCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM channel_chat_binding WHERE provider=$1 AND external_chat_id=$2`,
		provider, externalChatID,
	).Scan(&beforeCount); err != nil {
		t.Fatalf("pre-cascade count: %v", err)
	}
	if beforeCount != 1 {
		t.Fatalf("pre-cascade: expected 1 chat binding, got %d", beforeCount)
	}

	if _, err := testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID); err != nil {
		t.Fatalf("delete workspace: %v", err)
	}

	var afterCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM channel_chat_binding WHERE provider=$1 AND external_chat_id=$2`,
		provider, externalChatID,
	).Scan(&afterCount); err != nil {
		t.Fatalf("post-cascade count: %v", err)
	}
	if afterCount != 0 {
		t.Fatalf("ON DELETE CASCADE failed: expected 0 chat bindings after workspace delete, got %d", afterCount)
	}

	// 3. Send a message in the now-orphan chat. Dispatch must produce a
	//    WS_NOT_BOUND reply and create no issue rows.
	evt := port.InboundEvent{
		ChannelName: provider,
		EventID:     freshEventID(t, provider, "evt_int3_orphan"),
		Type:        port.EventTypeMessageReceived,
		ChatID:      externalChatID,
		ChatType:    port.ChatTypeGroup,
		SenderID:    externalUserID,
		Text:        "create issue: " + rejectedTitle,
		MessageID:   "om_int3_orphan",
	}
	if _, err := pipeline.Run(ctx, evt); err != nil {
		t.Fatalf("pipeline run: %v", err)
	}

	sends := fake.snapshotSends()
	if len(sends) != 1 {
		t.Fatalf("expected exactly 1 outbound WS_NOT_BOUND reply, got %d", len(sends))
	}
	if !strings.Contains(sends[0].Text, "WS_NOT_BOUND") {
		t.Fatalf("reply text %q must contain WS_NOT_BOUND template marker", sends[0].Text)
	}

	// And no issue rows should have leaked through.
	var issueCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue WHERE title = $1`, rejectedTitle,
	).Scan(&issueCount); err != nil {
		t.Fatalf("count post-cascade issues: %v", err)
	}
	if issueCount != 0 {
		t.Fatalf("orphan chat must NOT create issues; got %d", issueCount)
	}

	// Cleanup any residual binding rows the failed bind path may have
	// left around (defence-in-depth — most should be gone already via
	// CASCADE).
	cleanupChannelUserBinding(ctx, provider, externalUserID)
	cleanupChatBindings(ctx, provider, externalChatID)
}

// TC-int-4 · Path-of-strangers protection (PRD AC7.1).
//
// A user who has no channel_user_binding row tries to drive the bot in
// a bound chat. Identity-bind must Skip and the dispatch step must
// never see the event. This is the "passer-by cannot create issues"
// guarantee that anchors AC7.1.
func TestChannelIntegration_TC_int_4_StrangerCannotDispatch(t *testing.T) {
	requirePool(t)

	ctx := context.Background()
	const externalUserID = "ou_int4_eve_stranger"
	const externalChatID = "oc_int4_chat"
	const provider = "feishu"
	strangerTitle := fmt.Sprintf("stranger trying to file-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM issue WHERE title = $1`, strangerTitle)
	})

	registry := channel.NewRegistry()
	fake := newFakeChannel(provider)
	if err := registry.Register(fake); err != nil {
		t.Fatalf("register fake channel: %v", err)
	}
	// Chat is bound to a workspace, but the user is NOT bound.
	bindChatToWorkspace(t, provider, externalChatID, port.ChatTypeGroup, testWorkspaceID, testUserID)

	dispatch := newRecordingStep("dispatch")
	pipeline := newChannelTestPipeline(testPool, registry, dispatch)

	evt := port.InboundEvent{
		ChannelName: provider,
		EventID:     freshEventID(t, provider, "evt_int4_stranger"),
		Type:        port.EventTypeMessageReceived,
		ChatID:      externalChatID,
		ChatType:    port.ChatTypeGroup,
		SenderID:    externalUserID,
		Text:        "create issue: " + strangerTitle,
		MessageID:   "om_int4_stranger",
	}
	out, err := pipeline.Run(ctx, evt)
	if err != nil {
		t.Fatalf("pipeline run: %v", err)
	}
	if out.Terminal != "identity-bind" || out.Decision != inbound.DecisionSkip {
		t.Fatalf("stranger event: expected identity-bind Skip, got %s/%s", out.Terminal, out.Decision)
	}
	if dispatch.callCount() != 0 {
		t.Fatalf("stranger event: dispatch must NOT run; call count = %d", dispatch.callCount())
	}

	// And — defence in depth — no issue with the stranger's title was
	// created.
	var issueCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue WHERE title = $1`, strangerTitle,
	).Scan(&issueCount); err != nil {
		t.Fatalf("count post-stranger issues: %v", err)
	}
	if issueCount != 0 {
		t.Fatalf("stranger must not create issues; got %d", issueCount)
	}
}

// recordingStep is a dispatch test double used by tests that want to
// observe whether the pipeline reached the dispatch slot. It records
// each event it sees and always returns Continue so the pipeline keeps
// rolling (subsequent steps, if any, still run). It is concurrency-safe
// because the integration tests share a single pipeline across no
// goroutines.
type recordingStep struct {
	name string
	mu   sync.Mutex
	seen []port.InboundEvent
}

func newRecordingStep(name string) *recordingStep {
	return &recordingStep{name: name}
}

func (s *recordingStep) Name() string { return s.name }

func (s *recordingStep) Run(_ context.Context, evt port.InboundEvent) (port.InboundEvent, inbound.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, evt)
	return evt, inbound.DecisionContinue, nil
}

func (s *recordingStep) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

func (s *recordingStep) lastEvent() port.InboundEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seen) == 0 {
		return port.InboundEvent{}
	}
	return s.seen[len(s.seen)-1]
}

// extractPlaintextFromBindingMessage finds the `token=<plaintext>`
// substring the test-side identity-bind step plants in the outbound
// link message. Real M2 link delivery will put the token inside a URL
// query parameter; the test format is intentionally trivial so the
// assertion stays focused on flow rather than URL templating.
func extractPlaintextFromBindingMessage(t *testing.T, body string) string {
	t.Helper()
	const marker = "token="
	_, after, ok := strings.Cut(body, marker)
	if !ok {
		t.Fatalf("binding-link message missing %q marker: %q", marker, body)
	}
	return strings.TrimSpace(after)
}
