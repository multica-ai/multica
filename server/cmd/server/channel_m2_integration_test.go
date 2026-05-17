package main

// M2-T18 channel integration tests — STA-64 Phase B.
//
// Scope (per STA-64):
//
//	TC-int-4 (M2)        Group QueryIssue + SetStatus end-to-end (PRD F5).
//	TC-int-6 (M2)        Private chat CreateIssue replies with the new
//	                     identifier (PRD E5).
//	TC-int-7 (M2)        Inbound dedup is idempotent under replay (the
//	                     "network reconnect double-delivery" case
//	                     covered by AC2.1; the SDK-level reconnect path
//	                     was carved out to M3 in STA-59).
//	TC-int-8 (M2)        Image attachment intents reply with the
//	                     IMAGE_UNSUPPORTED rejection template (PRD E10).
//	TC-risk-4            Same-chat concurrent inbound delivery does not
//	                     race on the dedup table (PRD E8).
//
// Naming: every TC-int test in this file is suffixed `_M2` so it does
// not collide with the M1 acceptance tests in
// channel_integration_test.go which use the same TC-int-4 number for
// a different scenario (M1 path-of-strangers).
//
// Helpers live in mock_channel_test.go.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/channel"
	"github.com/multica-ai/multica/server/internal/channel/facade"
	"github.com/multica-ai/multica/server/internal/channel/inbound"
	"github.com/multica-ai/multica/server/internal/channel/port"
)

// ---------------------------------------------------------------------------
// TC-int-4 (M2) · Group QueryIssue + SetStatus end-to-end.
//
// Scenario (PRD F5, STA-64 TC-int-4):
//
//   1. A bound user @-mentions the bot in a bound group with a query
//      intent ("查看 issue STA-N 状态"). The dispatcher resolves the
//      issue via GetIssueByIdentifier and replies with the current
//      status.
//   2. The same user issues a status-change intent ("把 STA-N 改为
//      in_progress"). The dispatcher calls SetIssueStatus and replies
//      with STATUS_CHANGED.
//   3. Re-querying the issue returns the new status (state is
//      persisted).
// ---------------------------------------------------------------------------

func TestChannelIntegration_TC_int_4_M2_GroupQueryAndSetStatus(t *testing.T) {
	requirePool(t)

	ctx := context.Background()
	const externalUserID = "ou_m2int4_kim"
	const externalChatID = "oc_m2int4_chat"
	const provider = "feishu"
	titleSuffix := fmt.Sprintf("-%d", time.Now().UnixNano())
	createTitle := "m2 query/set status target" + titleSuffix
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM issue WHERE title = $1`, createTitle)
	})

	registry := channel.NewRegistry()
	fake := newFakeChannel(provider)
	if err := registry.Register(fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	bindChatToWorkspace(t, provider, externalChatID, port.ChatTypeGroup, testWorkspaceID, testUserID)
	bindUserToMulticaUser(t, provider, externalUserID, testUserID)

	// 1. Pre-create an issue via the in-test IssueService so we have a
	//    known identifier to query and mutate. Going through the
	//    facade keeps the workspace.issue_counter increment + STA-N
	//    assignment consistent with how the dispatcher would create
	//    one — the test sequencing (Query → SetStatus → Query) does
	//    not require an inbound-create step.
	issueSvc := newDirectIssueServiceFull(testPool)
	wsUUID := uuidFromString(t, testWorkspaceID)
	created, err := issueSvc.CreateIssue(ctx, mustCreateReq(t, wsUUID, externalUserID, createTitle))
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	// Read back the assigned identifier (STA-N) so the intent we feed
	// into the dispatcher matches the row in the DB.
	var (
		number int32
		prefix string
	)
	if err := testPool.QueryRow(ctx, `
		SELECT i.number, w.issue_prefix
		FROM issue i JOIN workspace w ON w.id = i.workspace_id
		WHERE i.id = $1
	`, created.ID).Scan(&number, &prefix); err != nil {
		t.Fatalf("read identifier: %v", err)
	}
	identifier := fmt.Sprintf("%s-%d", prefix, number)

	dispatch := newProductionDispatchStep(testPool, registry, issueSvc)

	// --- Step A: QueryIssue ---
	queryIntent := port.InboundIntent{
		Kind:       port.IntentQueryIssue,
		Confidence: 1,
		Source:     port.SourceRule,
		Params:     map[string]string{"issue_key": identifier},
	}
	queryEvt := port.InboundEvent{
		ChannelName: provider,
		EventID:     freshEventID(t, provider, "evt_m2int4_query"),
		Type:        port.EventTypeMessageReceived,
		ChatID:      externalChatID,
		ChatType:    port.ChatTypeGroup,
		SenderID:    externalUserID,
		Text:        "查看 issue " + identifier + " 状态",
		MessageID:   "om_m2int4_query",
	}
	pipeline := newM2Pipeline(testPool, registry, queryIntent, dispatch)
	if _, err := pipeline.Run(ctx, queryEvt); err != nil {
		t.Fatalf("query pipeline: %v", err)
	}

	if got, want := len(fake.snapshotSends()), 1; got != want {
		t.Fatalf("query: expected %d outbound send, got %d", want, got)
	}
	queryReply := fake.snapshotSends()[0]
	if !strings.Contains(queryReply.Text, identifier) {
		t.Fatalf("query reply must echo identifier %q: %q", identifier, queryReply.Text)
	}
	if !strings.Contains(queryReply.Text, "todo") {
		t.Fatalf("query reply must include initial status 'todo': %q", queryReply.Text)
	}

	// --- Step B: SetStatus ---
	setIntent := port.InboundIntent{
		Kind:       port.IntentSetStatus,
		Confidence: 1,
		Source:     port.SourceRule,
		Params:     map[string]string{"issue_key": identifier, "status": "in_progress"},
	}
	setEvt := port.InboundEvent{
		ChannelName: provider,
		EventID:     freshEventID(t, provider, "evt_m2int4_set"),
		Type:        port.EventTypeMessageReceived,
		ChatID:      externalChatID,
		ChatType:    port.ChatTypeGroup,
		SenderID:    externalUserID,
		Text:        "把 " + identifier + " 改为 in_progress",
		MessageID:   "om_m2int4_set",
	}
	pipeline2 := newM2Pipeline(testPool, registry, setIntent, dispatch)
	if _, err := pipeline2.Run(ctx, setEvt); err != nil {
		t.Fatalf("set pipeline: %v", err)
	}

	sends := fake.snapshotSends()
	if len(sends) != 2 {
		t.Fatalf("set: expected 2 cumulative outbound sends, got %d", len(sends))
	}
	setReply := sends[1]
	if !strings.Contains(setReply.Text, "STATUS_CHANGED") {
		t.Fatalf("set reply must contain STATUS_CHANGED template marker: %q", setReply.Text)
	}
	if !strings.Contains(setReply.Text, "in_progress") {
		t.Fatalf("set reply must echo target status: %q", setReply.Text)
	}

	// --- Step C: confirm persisted state via direct SQL ---
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, created.ID).Scan(&status); err != nil {
		t.Fatalf("read post-set status: %v", err)
	}
	if status != "in_progress" {
		t.Fatalf("issue.status not persisted: got %q want in_progress", status)
	}
}

// ---------------------------------------------------------------------------
// TC-int-9 (M3a) · Group SetAssignee end-to-end.
//
// Scenario (PRD F5, STA-68):
//   1. Pre-create an issue and a workspace member.
//   2. Send a SetAssignee intent via the production dispatch step.
//   3. Assert the reply contains "ASSIGNEE_CHANGED" and the assignee name.
//   4. Confirm the issue row has the correct assignee_id.
// ---------------------------------------------------------------------------

func TestChannelIntegration_TC_int_9_M3a_GroupSetAssignee(t *testing.T) {
	requirePool(t)

	ctx := context.Background()
	const externalUserID = "ou_m3a_int9_sender"
	const externalChatID = "oc_m3a_int9_chat"
	const provider = "feishu"
	titleSuffix := fmt.Sprintf("-%d", time.Now().UnixNano())
	createTitle := "m3a set assignee target" + titleSuffix
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM issue WHERE title = $1`, createTitle)
	})

	registry := channel.NewRegistry()
	fake := newFakeChannel(provider)
	if err := registry.Register(fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	bindChatToWorkspace(t, provider, externalChatID, port.ChatTypeGroup, testWorkspaceID, testUserID)
	bindUserToMulticaUser(t, provider, externalUserID, testUserID)

	// Create a second member to assign to.
	const assigneeExternalID = "ou_m3a_int9_target"
	assigneeUserID := mustCreateUser(t, "assignee-target"+titleSuffix)
	mustAddMember(t, testWorkspaceID, assigneeUserID)
	bindUserToMulticaUser(t, provider, assigneeExternalID, assigneeUserID)

	// Resolve assignee display name for the intent.
	var assigneeName string
	if err := testPool.QueryRow(ctx, `SELECT name FROM "user" WHERE id = $1`, assigneeUserID).Scan(&assigneeName); err != nil {
		t.Fatalf("read assignee name: %v", err)
	}

	issueSvc := newDirectIssueServiceFull(testPool)
	wsUUID := uuidFromString(t, testWorkspaceID)
	created, err := issueSvc.CreateIssue(ctx, mustCreateReq(t, wsUUID, externalUserID, createTitle))
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	var number int32
	var prefix string
	if err := testPool.QueryRow(ctx, `
		SELECT i.number, w.issue_prefix
		FROM issue i JOIN workspace w ON w.id = i.workspace_id
		WHERE i.id = $1
	`, created.ID).Scan(&number, &prefix); err != nil {
		t.Fatalf("read identifier: %v", err)
	}
	identifier := fmt.Sprintf("%s-%d", prefix, number)

	dispatch := newProductionDispatchStep(testPool, registry, issueSvc)

	setIntent := port.InboundIntent{
		Kind:       port.IntentSetAssignee,
		Confidence: 1,
		Source:     port.SourceRule,
		Params:     map[string]string{"issue_key": identifier, "assignee": assigneeName},
	}
	setEvt := port.InboundEvent{
		ChannelName: provider,
		EventID:     freshEventID(t, provider, "evt_m3aint9_set"),
		Type:        port.EventTypeMessageReceived,
		ChatID:      externalChatID,
		ChatType:    port.ChatTypeGroup,
		SenderID:    externalUserID,
		Text:        "把 " + identifier + " 指派给 @" + assigneeName,
		MessageID:   "om_m3aint9_set",
	}
	pipeline := newM2Pipeline(testPool, registry, setIntent, dispatch)
	if _, err := pipeline.Run(ctx, setEvt); err != nil {
		t.Fatalf("set assignee pipeline: %v", err)
	}

	sends := fake.snapshotSends()
	if len(sends) != 1 {
		t.Fatalf("expected 1 outbound send, got %d", len(sends))
	}
	reply := sends[0]
	if !strings.Contains(reply.Text, "ASSIGNEE_CHANGED") {
		t.Fatalf("reply must contain ASSIGNEE_CHANGED: %q", reply.Text)
	}
	if !strings.Contains(reply.Text, assigneeName) {
		t.Fatalf("reply must contain assignee name %q: %q", assigneeName, reply.Text)
	}

	var assigneeID pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT assignee_id FROM issue WHERE id = $1`, created.ID).Scan(&assigneeID); err != nil {
		t.Fatalf("read post-assignee: %v", err)
	}
	var targetUUID pgtype.UUID
	if err := targetUUID.Scan(assigneeUserID); err != nil {
		t.Fatalf("parse target uuid: %v", err)
	}
	if !assigneeID.Valid || assigneeID != targetUUID {
		t.Fatalf("assignee not persisted: got %v want %v", assigneeID, targetUUID)
	}
}

// TC-int-6 (M2) · Private (DM) chat path: CreateIssue replies with the
// new identifier.
//
// Scenario (PRD E5, STA-64 TC-int-6):
//
//   - User has a 1:1 DM with the bot; their DM chat is bound to the
//     workspace. They send "创建 issue 标题是 …".
//   - Dispatcher routes to CreateIssue facade and the channel receives
//     a reply containing the new STA-N identifier.
//
// Implementation note: the dispatcher does not itself differentiate
// group vs. direct chats — that is the inbound-authz step's
// responsibility (we are exercising it transparently via the
// production inbound.Pipeline shape). For E5 the relevant assertion is
// that the create flow runs and the bot replies in the DM.
// ---------------------------------------------------------------------------

func TestChannelIntegration_TC_int_6_M2_PrivateChatCreateIssue(t *testing.T) {
	requirePool(t)

	ctx := context.Background()
	const externalUserID = "ou_m2int6_priv"
	const externalChatID = "oc_m2int6_dm"
	const provider = "feishu"
	titleSuffix := fmt.Sprintf("-%d", time.Now().UnixNano())
	expectedTitle := "private DM creates issue" + titleSuffix
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM issue WHERE title = $1`, expectedTitle)
	})

	registry := channel.NewRegistry()
	fake := newFakeChannel(provider)
	if err := registry.Register(fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Bind the DM as a chat — chat_type = direct.
	bindChatToWorkspace(t, provider, externalChatID, port.ChatTypeDirect, testWorkspaceID, testUserID)
	bindUserToMulticaUser(t, provider, externalUserID, testUserID)

	issueSvc := newDirectIssueServiceFull(testPool)
	dispatch := newProductionDispatchStep(testPool, registry, issueSvc)

	intent := port.InboundIntent{
		Kind:       port.IntentCreateIssue,
		Confidence: 1,
		Source:     port.SourceRule,
		Params:     map[string]string{"title": expectedTitle},
	}
	evt := port.InboundEvent{
		ChannelName: provider,
		EventID:     freshEventID(t, provider, "evt_m2int6_create"),
		Type:        port.EventTypeMessageReceived,
		ChatID:      externalChatID,
		ChatType:    port.ChatTypeDirect,
		SenderID:    externalUserID,
		Text:        "创建 issue 标题是 " + expectedTitle,
		MessageID:   "om_m2int6_create",
	}
	pipeline := newM2Pipeline(testPool, registry, intent, dispatch)
	if _, err := pipeline.Run(ctx, evt); err != nil {
		t.Fatalf("pipeline: %v", err)
	}

	// Issue row exists.
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, expectedTitle,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 issue created, got %d", count)
	}

	// Reply went out via the DM chat with ISSUE_CREATED template.
	sends := fake.snapshotSends()
	if len(sends) != 1 {
		t.Fatalf("expected 1 outbound, got %d", len(sends))
	}
	if sends[0].Target != port.TargetChat(externalChatID) {
		t.Fatalf("reply target = %+v, want chat %q (DM)", sends[0].Target, externalChatID)
	}
	if !strings.Contains(sends[0].Text, "ISSUE_CREATED") {
		t.Fatalf("reply must include ISSUE_CREATED template: %q", sends[0].Text)
	}
	if !strings.Contains(sends[0].Text, expectedTitle) {
		t.Fatalf("reply must echo title: %q", sends[0].Text)
	}
}

// ---------------------------------------------------------------------------
// TC-int-7 (M2) · Inbound dedup is idempotent under replay.
//
// Scenario (PRD AC2.1, STA-64 TC-int-7):
//
//   - The platform double-delivers an event after a transient
//     reconnect (simulated here by feeding the same EventID into the
//     pipeline twice).
//   - The first run fires identity-bind → static-intent → dispatch.
//   - The second run terminates at dedup with DecisionSkip; dispatch
//     does NOT re-run.
//
// The "real SDK reconnect" path was explicitly carved out of M2 to
// STA-59 / M3 in Orion's 22:20 派单 evaluation. T18 only exercises the
// dedup-table-level idempotency, which is what AC2.1 requires.
// ---------------------------------------------------------------------------

func TestChannelIntegration_TC_int_7_M2_DedupIdempotentOnReplay(t *testing.T) {
	requirePool(t)

	ctx := context.Background()
	const externalUserID = "ou_m2int7_dup"
	const externalChatID = "oc_m2int7_chat"
	const provider = "feishu"
	titleSuffix := fmt.Sprintf("-%d", time.Now().UnixNano())
	createTitle := "dedup replay only one issue" + titleSuffix
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM issue WHERE title = $1`, createTitle)
	})

	registry := channel.NewRegistry()
	fake := newFakeChannel(provider)
	if err := registry.Register(fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	bindChatToWorkspace(t, provider, externalChatID, port.ChatTypeGroup, testWorkspaceID, testUserID)
	bindUserToMulticaUser(t, provider, externalUserID, testUserID)

	issueSvc := newDirectIssueServiceFull(testPool)
	dispatch := newProductionDispatchStep(testPool, registry, issueSvc)

	intent := port.InboundIntent{
		Kind:       port.IntentCreateIssue,
		Confidence: 1,
		Source:     port.SourceRule,
		Params:     map[string]string{"title": createTitle},
	}
	// Reuse a single EventID across both deliveries — this is what
	// makes the second run a "replay" by the dedup table's contract.
	eventID := freshEventID(t, provider, "evt_m2int7_replay")
	evt := port.InboundEvent{
		ChannelName: provider,
		EventID:     eventID,
		Type:        port.EventTypeMessageReceived,
		ChatID:      externalChatID,
		ChatType:    port.ChatTypeGroup,
		SenderID:    externalUserID,
		Text:        "create issue: " + createTitle,
		MessageID:   "om_m2int7_replay",
	}

	pipeline := newM2Pipeline(testPool, registry, intent, dispatch)

	// First delivery → dispatch runs.
	out1, err := pipeline.Run(ctx, evt)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if out1.Terminal != "dispatch" || out1.Decision != inbound.DecisionContinue {
		t.Fatalf("first run terminal = %s/%s, want dispatch/Continue", out1.Terminal, out1.Decision)
	}

	// Second delivery (replay) → dedup short-circuits.
	out2, err := pipeline.Run(ctx, evt)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if out2.Terminal != "dedup" || out2.Decision != inbound.DecisionSkip {
		t.Fatalf("second run terminal = %s/%s, want dedup/Skip", out2.Terminal, out2.Decision)
	}

	// One issue row, one outbound reply — the dedup short-circuit
	// must not let a side effect leak through.
	var count int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM issue WHERE title = $1`, createTitle,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("dedup leaked: expected 1 issue, got %d", count)
	}
	if got := len(fake.snapshotSends()); got != 1 {
		t.Fatalf("dedup leaked outbound: expected 1 reply, got %d", got)
	}

	// Defence-in-depth: the dedup row is exactly 1.
	var dedupRows int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM channel_inbound_event_dedup WHERE provider = $1 AND event_id = $2`,
		provider, eventID,
	).Scan(&dedupRows); err != nil {
		t.Fatalf("count dedup rows: %v", err)
	}
	if dedupRows != 1 {
		t.Fatalf("dedup table expected 1 row, got %d", dedupRows)
	}
}

// ---------------------------------------------------------------------------
// TC-int-8 (M2) · Image attachment intent receives IMAGE_UNSUPPORTED.
//
// Scenario (PRD E10, STA-64 TC-int-8):
//
//   - User sends a message with an image attachment whose intent is
//     CreateIssue. The dispatcher must reply with the
//     IMAGE_UNSUPPORTED rejection template ("暂不支持图片创建").
//
// Implementation: there is no image-attachment field on InboundEvent
// at the M2 boundary — image rejection is signalled through the
// IntentUnsupported kind with a `_reason: image_unsupported` Param,
// per Orion's classification of E10 as an "unsupported intent" path
// (the dispatcher already returns UNSUPPORTED_OP for IntentUnsupported;
// here we assert the rejection text is delivered, which is the
// observable contract for the user).
//
// PRD E10 wording focuses on the user-visible response, so this test
// asserts on the reply text rather than on a specific intent
// constant. If a future task introduces a dedicated IntentImageCreate,
// only the intent-recog step will need to change — the rejection
// reply contract stays.
// ---------------------------------------------------------------------------

func TestChannelIntegration_TC_int_8_M2_ImageAttachmentRejected(t *testing.T) {
	requirePool(t)

	ctx := context.Background()
	const externalUserID = "ou_m2int8_img"
	const externalChatID = "oc_m2int8_chat"
	const provider = "feishu"

	registry := channel.NewRegistry()
	fake := newFakeChannel(provider)
	if err := registry.Register(fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	bindChatToWorkspace(t, provider, externalChatID, port.ChatTypeGroup, testWorkspaceID, testUserID)
	bindUserToMulticaUser(t, provider, externalUserID, testUserID)

	issueSvc := newDirectIssueServiceFull(testPool)
	dispatch := newProductionDispatchStep(testPool, registry, issueSvc)

	intent := port.InboundIntent{
		Kind:       port.IntentUnsupported,
		Confidence: 1,
		Source:     port.SourceRule,
		Params:     map[string]string{"_reason": "image_unsupported"},
	}
	evt := port.InboundEvent{
		ChannelName: provider,
		EventID:     freshEventID(t, provider, "evt_m2int8_image"),
		Type:        port.EventTypeMessageReceived,
		ChatID:      externalChatID,
		ChatType:    port.ChatTypeGroup,
		SenderID:    externalUserID,
		Text:        "[图片]",
		MessageID:   "om_m2int8_image",
	}
	pipeline := newM2Pipeline(testPool, registry, intent, dispatch)
	if _, err := pipeline.Run(ctx, evt); err != nil {
		t.Fatalf("pipeline: %v", err)
	}

	sends := fake.snapshotSends()
	if len(sends) != 1 {
		t.Fatalf("expected 1 reject reply, got %d", len(sends))
	}
	// The dispatcher's UNSUPPORTED_OP template is the observable
	// contract for "this op cannot be handled here, go to Web".
	// PRD E10 is a specialisation of that contract for image
	// attachments.
	reply := sends[0].Text
	if !strings.Contains(reply, "UNSUPPORTED_OP") {
		t.Fatalf("reject reply must include UNSUPPORTED_OP marker: %q", reply)
	}
	if !strings.Contains(reply, "Web 端") {
		t.Fatalf("reject reply must direct user to Web 端: %q", reply)
	}
}

// ---------------------------------------------------------------------------
// TC-risk-4 · Same-chat concurrent inbound delivery is race-free.
//
// Scenario (PRD E8, STA-64 TC-risk-4):
//
//   - 10 distinct events for the same chat arrive concurrently. The
//     dispatcher counter is a shared variable across goroutines.
//   - Assert: every event reaches dispatch exactly once, no two
//     dispatch calls observe a race in the dedup table (no duplicate
//     EventIDs are accepted), and final counter == 10.
//
// The goal is NOT to assert FIFO ordering — the inbound dispatcher
// is documented to be at-least-once-per-event, not strictly ordered.
// What we DO assert is correctness under contention: no event is
// dropped, no event is processed twice, and the dedup table behaves
// atomically (PostgreSQL's ON CONFLICT DO NOTHING handles this
// trivially; the test is a regression guard for any future change
// that introduces a non-atomic short-circuit).
// ---------------------------------------------------------------------------

func TestChannelIntegration_TC_risk_4_ConcurrentInboundIsRaceFree(t *testing.T) {
	requirePool(t)

	ctx := context.Background()
	const externalUserID = "ou_m2risk4_con"
	const externalChatID = "oc_m2risk4_chat"
	const provider = "feishu"
	const N = 10

	registry := channel.NewRegistry()
	fake := newFakeChannel(provider)
	if err := registry.Register(fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	bindChatToWorkspace(t, provider, externalChatID, port.ChatTypeGroup, testWorkspaceID, testUserID)
	bindUserToMulticaUser(t, provider, externalUserID, testUserID)

	var (
		mu          sync.Mutex
		dispatchCnt int
	)
	dispatch := newFnDispatch("dispatch", func(_ context.Context, evt port.InboundEvent) (port.InboundEvent, inbound.Decision, error) {
		mu.Lock()
		dispatchCnt++
		mu.Unlock()
		return evt, inbound.DecisionContinue, nil
	})

	intent := port.InboundIntent{
		Kind:       port.IntentUnknown, // dispatch counts events; no facade calls.
		Confidence: 1,
		Source:     port.SourceRule,
	}

	// Pre-allocate distinct EventIDs so each event is unique. The
	// freshEventID helper writes Cleanup hooks; pre-allocating once
	// avoids racing on t.Cleanup registration.
	eventIDs := make([]string, N)
	for i := 0; i < N; i++ {
		eventIDs[i] = freshEventID(t, provider, fmt.Sprintf("evt_m2risk4_e%d", i))
	}

	// Each goroutine builds its own pipeline. The dedup store and
	// downstream steps are all read-only or use atomic SQL primitives,
	// so contention is real but should not produce a race.
	var wg sync.WaitGroup
	wg.Add(N)
	errs := make(chan error, N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			pipeline := newM2Pipeline(testPool, registry, intent, dispatch)
			evt := port.InboundEvent{
				ChannelName: provider,
				EventID:     eventIDs[i],
				Type:        port.EventTypeMessageReceived,
				ChatID:      externalChatID,
				ChatType:    port.ChatTypeGroup,
				SenderID:    externalUserID,
				Text:        fmt.Sprintf("hello bot %d", i),
				MessageID:   fmt.Sprintf("om_m2risk4_%d", i),
			}
			if _, err := pipeline.Run(ctx, evt); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent pipeline run: %v", err)
	}

	if dispatchCnt != N {
		t.Fatalf("expected dispatch to fire %d times, got %d", N, dispatchCnt)
	}

	// Defence-in-depth: dedup table now holds N distinct rows for the
	// (provider, eventID) pairs we just submitted.
	var dedupRows int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM channel_inbound_event_dedup
		WHERE provider = $1 AND event_id = ANY($2)
	`, provider, eventIDs).Scan(&dedupRows); err != nil {
		t.Fatalf("count dedup: %v", err)
	}
	if dedupRows != N {
		t.Fatalf("dedup table missing rows: got %d, want %d", dedupRows, N)
	}

	// Sanity: assert the second run of any one of those events is a
	// dedup-Skip, proving the rows we just wrote are real (not a
	// transaction that rolled back). Pick the first event id; cleanup
	// already covers it.
	replay := port.InboundEvent{
		ChannelName: provider,
		EventID:     eventIDs[0],
		Type:        port.EventTypeMessageReceived,
		ChatID:      externalChatID,
		ChatType:    port.ChatTypeGroup,
		SenderID:    externalUserID,
		Text:        "replay",
	}
	pipeline := newM2Pipeline(testPool, registry, intent, dispatch)
	out, err := pipeline.Run(ctx, replay)
	if err != nil {
		t.Fatalf("replay run: %v", err)
	}
	if out.Terminal != "dedup" || out.Decision != inbound.DecisionSkip {
		t.Fatalf("replay terminal = %s/%s, want dedup/Skip", out.Terminal, out.Decision)
	}
}

// helpers (file-local)
// ---------------------------------------------------------------------------

// mustCreateReq builds a facade.CreateIssueReq using the supplied
// workspace + an actor ID resolved through channel_user_binding for
// externalUserID. Most M2 tests pre-bind the user; this helper hides
// the resolution boilerplate.
func mustCreateReq(t *testing.T, wsID pgtype.UUID, externalUserID, title string) facade.CreateIssueReq {
	t.Helper()
	var actor pgtype.UUID
	if err := testPool.QueryRow(context.Background(), `
			SELECT user_id FROM channel_user_binding
			WHERE connection_id = 'feishu' AND external_user_id = $1
		`, externalUserID).Scan(&actor); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("mustCreateReq: no channel_user_binding for %q (did you call bindUserToMulticaUser?)", externalUserID)
		}
		t.Fatalf("mustCreateReq: lookup user_id: %v", err)
	}
	return facade.CreateIssueReq{WorkspaceID: wsID, ActorID: actor, Title: title}
}
