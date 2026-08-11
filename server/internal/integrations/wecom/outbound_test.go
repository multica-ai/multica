package wecom

// outbound_test.go — the EventChatDone reply path and the inbox:new delivery
// path, both driven through a fake outboundQueries (the interface Outbound
// depends on) and a recording enqueue store, so no database is required.
// These are the paths that put an agent's words back in front of the WeCom
// user, and the "deliver via bot only when bound" contract the inbox
// notification rests on.
//
// The observation point is the queue row, not a WebSocket frame: Outbound no
// longer writes to WeCom at all. It enqueues, and the replica holding the bot's
// socket delivers (see outbox_sender_test.go).
//
// Original inbox-delivery design and review: seacen (PR #5833).

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel/outbox"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const testTaskID = "55555555-5555-5555-5555-555555555555"

// fakeOutboundQueries is an in-memory stand-in for the queries Outbound
// uses. A nil error field returns the row; a non-nil one is returned as-is
// (use pgx.ErrNoRows to exercise the "not a wecom session" / "no binding"
// branches).
type fakeOutboundQueries struct {
	sessionBinding db.ChannelChatSessionBinding
	sessionErr     error
	installation   db.ChannelInstallation
	installErr     error
	memberBinding  db.ChannelUserBinding
	memberErr      error
	workspace      db.Workspace
	workspaceErr   error
	// tasks answers the retry-clone lookup: the round is bound under the turn
	// that owns the input batch, and a clone reaches it through
	// chat_input_task_id. A task with no row here reads as pgx.ErrNoRows —
	// cancelled and reaped while its ending was in flight.
	tasks    map[string]db.AgentTaskQueue
	taskErr  error
	taskGets int
	// channelIngested is the channel_ingested stamp on the input batch the
	// task owns: askedOverWecom for a question typed in the room,
	// askedInTheWebUI for one typed in Multica.
	//
	// It is a pointer, and it has no default on purpose. Two gates read this
	// one stamp in opposite directions — the answer path delivers only when it
	// is set, and the failure-notice path #6606 adds delivers unless it is — so
	// either zero value would let one of them pass a test that never said where
	// the question came from. Left unset the fake ends the test naming the
	// omission, which is what keeps this rig usable by whichever of the two
	// lands second.
	//
	// originAskedFor records which id the stamp was read for, which is the
	// whole of the retry-clone question.
	channelIngested *bool
	originErr       error
	originAskedFor  []string
	// t is who an unset stamp is reported to. fileTask sets it, and a filed
	// row is the only route to the origin gate, so it is always there by the
	// time the gate reads.
	t testing.TB
}

// askedOverWecom and askedInTheWebUI are the two answers to "where was this
// question asked". One of them belongs in every rig whose run reaches the
// origin gate; there is no third answer and no default.
func askedOverWecom() *bool  { asked := true; return &asked }
func askedInTheWebUI() *bool { asked := false; return &asked }

func (f *fakeOutboundQueries) GetChannelChatSessionBindingBySession(context.Context, db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	return f.sessionBinding, f.sessionErr
}
func (f *fakeOutboundQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return f.installation, f.installErr
}
func (f *fakeOutboundQueries) FindChannelBindingForMember(context.Context, db.FindChannelBindingForMemberParams) (db.ChannelUserBinding, error) {
	return f.memberBinding, f.memberErr
}
func (f *fakeOutboundQueries) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	return f.workspace, f.workspaceErr
}
func (f *fakeOutboundQueries) GetAgentTask(_ context.Context, id pgtype.UUID) (db.AgentTaskQueue, error) {
	f.taskGets++
	if f.taskErr != nil {
		return db.AgentTaskQueue{}, f.taskErr
	}
	task, ok := f.tasks[util.UUIDToString(id)]
	if !ok {
		return db.AgentTaskQueue{}, pgx.ErrNoRows
	}
	return task, nil
}
func (f *fakeOutboundQueries) TaskHasChannelIngestedMessages(_ context.Context, taskID pgtype.UUID) (bool, error) {
	f.originAskedFor = append(f.originAskedFor, util.UUIDToString(taskID))
	if f.originErr != nil {
		return false, f.originErr
	}
	if f.channelIngested == nil {
		f.failStampNotSet(util.UUIDToString(taskID))
		return false, nil // unreachable: failStampNotSet ends the test
	}
	return *f.channelIngested, nil
}

// failStampNotSet ends the test naming what the rig left out, instead of
// letting a zero value answer for it three layers away.
//
// It has to be t.Fatalf rather than a panic: the failure path arrives here
// through events.Bus.Publish, which recovers panics in listeners and logs
// them, so a panic would be swallowed and the test would fail on an assertion
// that says nothing about what was missing. Fatalf's runtime.Goexit is not
// recoverable, so it survives the bus.
func (f *fakeOutboundQueries) failStampNotSet(taskID string) {
	msg := "fakeOutboundQueries: the origin gate read the channel_ingested stamp for task " +
		taskID + ", but this rig never set channelIngested. Say where the question was asked: " +
		"channelIngested: askedOverWecom() for one typed in the room, askedInTheWebUI() for one " +
		"typed in Multica. There is no default — the answer path delivers only when the stamp is " +
		"set and the failure path delivers unless it is, so either zero value would let one of " +
		"those two pass a test that never stated what it meant."
	if f.t == nil {
		panic(msg)
	}
	f.t.Fatalf("%s", msg)
}

// fileTask records the agent_task_queue row GetAgentTask answers with, for a
// task that owns its own input batch — which every chat round's task has done
// since MUL-4351. id is the task id the ending event carries.
func (f *fakeOutboundQueries) fileTask(t testing.TB, id string) {
	t.Helper()
	f.fileRetryClone(t, id, id)
}

// fileRetryClone files FailTask's retry child: a fresh task id inheriting the
// parent's input batch, its own id owning nothing. This is the row that makes
// the batch owner the only id worth asking the stamp about.
func (f *fakeOutboundQueries) fileRetryClone(t testing.TB, id, owner string) {
	t.Helper()
	f.t = t
	taskID := mustParseTaskUUID(t, id)
	ownerID := mustParseTaskUUID(t, owner)
	if f.tasks == nil {
		f.tasks = map[string]db.AgentTaskQueue{}
	}
	f.tasks[util.UUIDToString(taskID)] = db.AgentTaskQueue{ID: taskID, ChatInputTaskID: ownerID}
}

func mustParseTaskUUID(t testing.TB, id string) pgtype.UUID {
	t.Helper()
	parsed, err := util.ParseUUID(id)
	if err != nil || !parsed.Valid {
		t.Fatalf("parse task id %q: %v", id, err)
	}
	return parsed
}

// originAsked is the ids the provenance stamp was read for, in order.
func (f *fakeOutboundQueries) originAsked() []string { return f.originAskedFor }

// recordingEnqueueStore captures every enqueue so a test can assert on the row
// that would have been written.
type recordingEnqueueStore struct {
	rows []db.EnqueueChannelOutboundParams
	err  error
}

func (s *recordingEnqueueStore) EnqueueChannelOutbound(_ context.Context, arg db.EnqueueChannelOutboundParams) (db.ChannelOutboundQueue, error) {
	if s.err != nil {
		return db.ChannelOutboundQueue{}, s.err
	}
	s.rows = append(s.rows, arg)
	return db.ChannelOutboundQueue{
		InstallationID: arg.InstallationID,
		WorkspaceID:    arg.WorkspaceID,
		ChannelType:    arg.ChannelType,
		SourceKind:     arg.SourceKind,
		SourceID:       arg.SourceID,
	}, nil
}

// payload decodes the queue payload of row i.
func (s *recordingEnqueueStore) payload(t *testing.T, i int) outboundPayload {
	t.Helper()
	if i >= len(s.rows) {
		t.Fatalf("no enqueued row at index %d (have %d)", i, len(s.rows))
	}
	var p outboundPayload
	if err := json.Unmarshal(s.rows[i].Payload, &p); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return p
}

func newOutboundWithQueue(t *testing.T, q outboundQueries) (*Outbound, pgtype.UUID, *recordingEnqueueStore) {
	t.Helper()
	store := &recordingEnqueueStore{}
	producer, err := outbox.NewProducer(channelTypeWecom, store, nil, nil)
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	return NewOutbound(q, newSendersRegistry(), producer, slog.Default()), mustTestUUID(t), store
}

func chatDoneEvent(content string) events.Event {
	return events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload: protocol.ChatDonePayload{
			TaskID:  testTaskID,
			Content: content,
		},
	}
}

func TestProcessEvent_EnqueuesChatReplyForBoundChat(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding:  db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1", ChatType: "group"},
		installation:    db.ChannelInstallation{Status: string(InstallationActive)},
		channelIngested: askedOverWecom(), // the question came in over WeCom
	}
	q.fileTask(t, testTaskID)
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	q.installation.WorkspaceID = mustTestUUID(t)

	if err := o.processEvent(context.Background(), chatDoneEvent("the agent reply")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("enqueued %d rows, want 1", len(store.rows))
	}
	row := store.rows[0]
	if row.TargetChatID != "CHAT_1" {
		t.Errorf("target_chat_id = %q, want CHAT_1", row.TargetChatID)
	}
	if row.TargetChatType != int16(chatTypeGroupInt) {
		t.Errorf("target_chat_type = %d, want group (%d)", row.TargetChatType, chatTypeGroupInt)
	}
	if row.ChannelType != channelTypeWecom {
		t.Errorf("channel_type = %q, want %q", row.ChannelType, channelTypeWecom)
	}
	if row.SourceKind != sourceKindChatDone {
		t.Errorf("source_kind = %q, want %q", row.SourceKind, sourceKindChatDone)
	}
	// The business key must be the task, so a redelivered event dedupes
	// against the row the first delivery wrote.
	if row.SourceID != testTaskID {
		t.Errorf("source_id = %q, want the task id %q", row.SourceID, testTaskID)
	}
	if got := store.payload(t, 0).Content; got != "the agent reply" {
		t.Errorf("payload content = %q, want the agent reply", got)
	}
}

// The task id lives in ChatDonePayload, not on the event envelope
// (service.broadcastChatDone leaves Event.TaskID empty). Reading only the
// envelope would skip every realtime enqueue and silently demote delivery to
// the deliberately-lagged reconciler.
func TestProcessEvent_TakesTaskIDFromPayloadNotEnvelope(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding:  db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"},
		installation:    db.ChannelInstallation{Status: string(InstallationActive)},
		channelIngested: askedOverWecom(),
	}
	q.fileTask(t, testTaskID)
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	q.installation.WorkspaceID = mustTestUUID(t)

	event := chatDoneEvent("hi")
	if event.TaskID != "" {
		t.Fatal("fixture must mirror production: envelope TaskID is empty")
	}
	if err := o.processEvent(context.Background(), event); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("enqueued %d rows, want 1 — the payload task id must be used", len(store.rows))
	}
}

func TestProcessEvent_MapPayloadTaskIDIsUsed(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding:  db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"},
		installation:    db.ChannelInstallation{Status: string(InstallationActive)},
		channelIngested: askedOverWecom(),
	}
	q.fileTask(t, testTaskID)
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	q.installation.WorkspaceID = mustTestUUID(t)

	// Post-serialization shape, as a Redis-relayed event arrives.
	err := o.processEvent(context.Background(), events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload:       map[string]any{"task_id": testTaskID, "content": "mapped reply"},
	})
	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("enqueued %d rows, want 1", len(store.rows))
	}
	if store.rows[0].SourceID != testTaskID {
		t.Errorf("source_id = %q, want %q", store.rows[0].SourceID, testTaskID)
	}
}

func TestProcessEvent_MissingTaskIDIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"},
		installation:   db.ChannelInstallation{Status: string(InstallationActive)},
	}
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	q.installation.WorkspaceID = mustTestUUID(t)

	err := o.processEvent(context.Background(), events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload:       protocol.ChatDonePayload{Content: "no task id"},
	})
	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 0 {
		t.Errorf("expected no enqueue without a business key, got %d rows", len(store.rows))
	}
}

func TestProcessEvent_NonWecomSessionIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{sessionErr: pgx.ErrNoRows}
	o, _, store := newOutboundWithQueue(t, q)
	if err := o.processEvent(context.Background(), chatDoneEvent("x")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 0 {
		t.Errorf("expected no enqueue for a non-wecom session, got %d rows", len(store.rows))
	}
}

func TestProcessEvent_EmptyContentIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"}}
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	if err := o.processEvent(context.Background(), chatDoneEvent("")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 0 {
		t.Errorf("empty completion should enqueue nothing, got %d rows", len(store.rows))
	}
}

// The origin gate now runs ahead of the installation lookup, so a completion
// has to carry a task id AND a WeCom origin to reach the revocation check at
// all. Without both, this test passes on the gate's refusal and never
// exercises the branch it is named after.
func TestProcessEvent_RevokedInstallationIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding:  db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"},
		installation:    db.ChannelInstallation{Status: "revoked"},
		channelIngested: askedOverWecom(), // so revocation is the only thing left to stop it
	}
	q.fileTask(t, testTaskID)
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	q.installation.WorkspaceID = mustTestUUID(t)
	if err := o.processEvent(context.Background(), chatDoneEvent("hi")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 0 {
		t.Errorf("revoked installation should enqueue nothing, got %d rows", len(store.rows))
	}
}

func TestTryEnqueueInbox_TargetsBoundMemberPrivately(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		memberBinding: db.ChannelUserBinding{ChannelUserID: "T_USER_1"},
		workspace:     db.Workspace{Slug: "acme"},
	}
	o, instID, store := newOutboundWithQueue(t, q)
	q.memberBinding.InstallationID = instID

	const itemID = "66666666-6666-6666-6666-666666666666"
	item := map[string]any{
		"id":             itemID,
		"recipient_type": "member",
		"recipient_id":   "33333333-3333-3333-3333-333333333333",
		"workspace_id":   "44444444-4444-4444-4444-444444444444",
		"type":           "issue_assigned",
		"title":          "New issue",
	}
	if !o.tryEnqueueInbox(context.Background(), item, itemID, "33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444") {
		t.Fatal("tryEnqueueInbox returned false; expected an enqueue for a bound member")
	}
	if len(store.rows) != 1 {
		t.Fatalf("enqueued %d rows, want 1", len(store.rows))
	}
	row := store.rows[0]
	if row.TargetChatID != "T_USER_1" {
		t.Errorf("target_chat_id = %q, want the member's bound userid", row.TargetChatID)
	}
	if row.TargetChatType != int16(chatTypeSingleInt) {
		t.Errorf("target_chat_type = %d, want single (%d)", row.TargetChatType, chatTypeSingleInt)
	}
	// Keyed on the inbox item so a redelivered inbox:new cannot notify twice.
	if row.SourceKind != sourceKindInboxNotify || row.SourceID != itemID {
		t.Errorf("business key = (%q, %q), want (%q, %q)", row.SourceKind, row.SourceID, sourceKindInboxNotify, itemID)
	}
	// The row must carry no chat_session_id: an inbox push is addressed to a
	// user, not a conversation, so session fencing must not apply to it.
	if row.ChatSessionID.Valid {
		t.Error("inbox push must not be fenced on a chat session")
	}
}

func TestTryEnqueueInbox_NoBindingIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{memberErr: pgx.ErrNoRows}
	o, _, store := newOutboundWithQueue(t, q)
	if o.tryEnqueueInbox(context.Background(), map[string]any{}, "66666666-6666-6666-6666-666666666666", "33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444") {
		t.Error("expected false when the member has no wecom binding")
	}
	if len(store.rows) != 0 {
		t.Errorf("no binding should enqueue nothing, got %d rows", len(store.rows))
	}
}

func TestHandleInboxNew_IgnoresNonMemberRecipient(t *testing.T) {
	t.Parallel()
	// memberErr set so that if it somehow reached the query it would no-op;
	// the recipient_type guard should return before any query.
	q := &fakeOutboundQueries{memberErr: errors.New("must not be called")}
	o, _, store := newOutboundWithQueue(t, q)
	o.handleInboxNew(events.Event{Payload: map[string]any{
		"item": map[string]any{"id": "x", "recipient_type": "agent", "recipient_id": "x", "workspace_id": "y"},
	}})
	if len(store.rows) != 0 {
		t.Errorf("agent recipient should not be enqueued for, got %d rows", len(store.rows))
	}
}

func TestChatDoneContent(t *testing.T) {
	t.Parallel()
	if got := chatDoneContent(protocol.ChatDonePayload{Content: "typed"}); got != "typed" {
		t.Errorf("typed payload = %q, want typed", got)
	}
	roundTripped := map[string]any{"content": "mapped"}
	if got := chatDoneContent(roundTripped); got != "mapped" {
		t.Errorf("map payload = %q, want mapped", got)
	}
	if got := chatDoneContent(map[string]any{"other": 1}); got != "" {
		t.Errorf("payload without content = %q, want empty", got)
	}
	if got := chatDoneContent(json.RawMessage(`{}`)); got != "" {
		t.Errorf("unknown payload type = %q, want empty", got)
	}
}

// TestProcessEvent_DoesNotPushAWebUIAnswerIntoTheRoom is the privacy case. A
// session that originated in WeCom can be continued from the Multica web UI,
// and that answer belongs only in Multica. Without the origin gate it is
// pushed to the bound chat — which in a group means in front of everyone in
// the room, an answer to a question none of them saw asked.
func TestProcessEvent_DoesNotPushAWebUIAnswerIntoTheRoom(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding:  db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1", ChatType: "group"},
		installation:    db.ChannelInstallation{Status: string(InstallationActive)},
		channelIngested: askedInTheWebUI(), // asked in the web UI, not over WeCom
	}
	q.fileTask(t, testTaskID)
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	q.installation.WorkspaceID = mustTestUUID(t)

	err := o.processEvent(context.Background(), events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload: protocol.ChatDonePayload{
			Content: "something the room was never meant to read",
			TaskID:  testTaskID,
		},
	})
	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	// Enqueueing counts as delivering: a row is a durable promise that the lease
	// holder will put this in the room.
	if len(store.rows) != 0 {
		t.Fatalf("a web-UI answer was enqueued for the WeCom chat (%d row(s))", len(store.rows))
	}
}

// An origin that cannot be established must fail closed — silence is
// recoverable, a leak is not.
func TestProcessEvent_FailsClosedWhenTheTaskIdIsMissing(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1", ChatType: "group"},
		installation:   db.ChannelInstallation{Status: string(InstallationActive)},
		// Stamped as a WeCom question, and deliberately so: the gate would say
		// deliver if it were ever consulted. Nothing here is refused for want
		// of a permissive origin — it is refused for want of an id to ask
		// about. No task row is filed, because there is no id to file one under.
		channelIngested: askedOverWecom(),
	}
	o, instID, store := newOutboundWithQueue(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	q.installation.WorkspaceID = mustTestUUID(t)

	// No TaskID anywhere: the envelope's is empty and the payload carries none.
	err := o.processEvent(context.Background(), events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload:       protocol.ChatDonePayload{Content: "unattributable"},
	})
	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("enqueued a completion whose origin could not be established (%d row(s))", len(store.rows))
	}
}

// The id is also the queue row's business key, so where it is read from decides
// whether the realtime path enqueues at all: broadcastChatDone leaves the
// envelope empty and carries the id in the payload, which is why the fallback is
// the live path rather than the exceptional one.
func TestChatDoneTaskID(t *testing.T) {
	t.Parallel()
	if _, ok := chatDoneTaskID(events.Event{TaskID: testTaskID}); !ok {
		t.Error("envelope id was not read")
	}
	if _, ok := chatDoneTaskID(events.Event{Payload: protocol.ChatDonePayload{TaskID: testTaskID}}); !ok {
		t.Error("typed payload id was not read")
	}
	if _, ok := chatDoneTaskID(events.Event{Payload: map[string]any{"task_id": testTaskID}}); !ok {
		t.Error("map payload id was not read — this is the shape a relayed event arrives in")
	}
	// Not a task id: must report absence rather than a zero UUID that would
	// become a business key every unattributable completion collided on.
	if _, ok := chatDoneTaskID(events.Event{Payload: json.RawMessage(`{}`)}); ok {
		t.Error("an unknown payload shape reported a usable task id")
	}
	if _, ok := chatDoneTaskID(events.Event{TaskID: "not-a-uuid"}); ok {
		t.Error("a malformed id reported as usable")
	}
}
