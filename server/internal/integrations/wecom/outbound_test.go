package wecom

// outbound_test.go — the EventChatDone reply path and the inbox:new delivery
// path, both driven through a fake outboundQueries (the interface Outbound
// depends on) and a recording wsConn, so no database is required. These are
// the paths that put an agent's words back in front of the WeCom user, and
// the "deliver via bot only when bound" contract the inbox notification rests
// on.
//
// Original inbox-delivery design and review: seacen (PR #5833).

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// fakeOutboundQueries is an in-memory stand-in for the queries Outbound
// uses. A nil error field returns the row; a non-nil one is returned as-is
// (use pgx.ErrNoRows to exercise the "not a wecom session" / "no binding"
// branches).
type fakeOutboundQueries struct {
	sessionBinding db.ChannelChatSessionBinding
	sessionErr     error
	// The installation row and the two knobs that shape a read of it, all
	// guarded by mu: an answer's retries read this row from their own timer
	// goroutines, and a test that withdraws permission mid-delivery writes it
	// from a third.
	installation   db.ChannelInstallation
	installErr     error
	installErrFor  int // 1-based read to fail; 0 fails none
	installReads   int
	memberBinding  db.ChannelUserBinding
	memberErr      error
	workspace      db.Workspace
	workspaceErr   error
	attachments    []db.Attachment
	attachmentsErr error
	// lookupGate holds every attachment lookup open until it is closed, which
	// is what a slow database looks like from in here. lookupsEntered counts
	// the arrivals, so a test can ask how many deliveries got as far as the
	// query rather than how many the counter was told about.
	lookupGate     chan struct{}
	lookupsEntered atomic.Int64
	// tasks answers the retry-clone lookup: the round is bound under the turn
	// that owns the input batch, and a clone reaches it through
	// chat_input_task_id. A task with no row here reads as pgx.ErrNoRows —
	// cancelled and reaped while its ending was in flight.
	tasks   map[string]db.AgentTaskQueue
	taskErr error
	// mu guards the counters, the installation row above and the hook below.
	// task:failed has two publishers and the bus is synchronous, so a test
	// modelling both of them runs this fake on two goroutines at once.
	mu       sync.Mutex
	taskGets int
	// atBinding runs on every chat-binding lookup, which is the first round trip
	// a delivery with no bubble left has to make. It is where a test says "and
	// by the time the delivery got here, the caller's budget was gone".
	atBinding func()
	// channelIngested is the channel_ingested stamp on the input batch the
	// task owns: askedOverWecom for a question typed in the room,
	// askedInTheWebUI for one typed in Multica.
	//
	// It is a pointer, and it has no default on purpose. Two gates read this
	// one stamp in opposite directions — the answer path in outbound.go
	// delivers only when it is set, the failure-notice path in
	// typing_indicator.go delivers unless it is — so either zero value would
	// let one of them pass a test that never said where the question came
	// from. Left unset the fake ends the test naming the omission.
	//
	// originAskedFor records which id the stamp was read for, which is the
	// whole of the retry-clone question. See failure_origin_test.go.
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
	f.mu.Lock()
	at := f.atBinding
	f.mu.Unlock()
	if at != nil {
		at()
	}
	return f.sessionBinding, f.sessionErr
}

// GetChannelTaskDelivery mirrors the binding double: the same row, as the
// snapshot the answer path reads, honouring the same error and the same
// atBinding hook so tests that time the lookup keep working on either path.
func (f *fakeOutboundQueries) GetChannelTaskDelivery(context.Context, pgtype.UUID) (db.ChannelTaskDelivery, error) {
	f.mu.Lock()
	at := f.atBinding
	f.mu.Unlock()
	if at != nil {
		at()
	}
	if f.sessionErr != nil {
		return db.ChannelTaskDelivery{}, f.sessionErr
	}
	return db.ChannelTaskDelivery{
		BindingID: f.sessionBinding.ID, InstallationID: f.sessionBinding.InstallationID,
		ChannelType: channelTypeWecom, ChannelChatID: f.sessionBinding.ChannelChatID,
		ChatType:         f.sessionBinding.ChatType,
		ChannelMessageID: f.sessionBinding.LastMessageID, ChannelThreadID: f.sessionBinding.LastThreadID,
		RouteRevision: f.sessionBinding.RouteRevision, Config: f.sessionBinding.Config,
	}, nil
}
func (f *fakeOutboundQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// installErrFor lets a test fail one read and not the rest, which is the
	// shape a transient database blip actually has — the retry behind it has to
	// find the row there.
	f.installReads++
	if n := f.installErrFor; n > 0 && f.installReads == n {
		return db.ChannelInstallation{}, errors.New("wecom test: transient database error")
	}
	return f.installation, f.installErr
}
func (f *fakeOutboundQueries) FindChannelBindingForMember(context.Context, db.FindChannelBindingForMemberParams) (db.ChannelUserBinding, error) {
	return f.memberBinding, f.memberErr
}
func (f *fakeOutboundQueries) GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error) {
	return f.workspace, f.workspaceErr
}
func (f *fakeOutboundQueries) ListAttachmentsByChatMessage(context.Context, db.ListAttachmentsByChatMessageParams) ([]db.Attachment, error) {
	if f.lookupGate != nil {
		f.lookupsEntered.Add(1)
		<-f.lookupGate
	}
	return f.attachments, f.attachmentsErr
}
func (f *fakeOutboundQueries) GetAgentTask(_ context.Context, id pgtype.UUID) (db.AgentTaskQueue, error) {
	f.mu.Lock()
	f.taskGets++
	f.mu.Unlock()
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
	f.mu.Lock()
	f.originAskedFor = append(f.originAskedFor, util.UUIDToString(taskID))
	f.mu.Unlock()
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
		"typed in Multica. There is no default — the failure-notice path delivers unless the " +
		"stamp is set, and the answer path delivers only when it is, so either zero value " +
		"would let one of those two pass a test that never stated what it meant."
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
func (f *fakeOutboundQueries) originAsked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.originAskedFor...)
}

// taskRowReads is how many times the task row was read.
func (f *fakeOutboundQueries) taskRowReads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.taskGets
}

// revoke withdraws the installation the way a person does, and from whichever
// goroutine the test is on. Every reader of this row runs somewhere else — an
// attachment delivery on its own goroutine, an answer retry on a timer — so the
// write goes under the same lock the read takes.
func (f *fakeOutboundQueries) revoke() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installation.Status = "revoked"
}

func newOutboundWithConn(t *testing.T, q outboundQueries) (*Outbound, pgtype.UUID, *recordingConn) {
	t.Helper()
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	conn := &recordingConn{}
	reg.set(instID, conn.autoAck(newWSSender(conn, nil)))
	return NewOutbound(q, reg, nil, slog.Default()), instID, conn
}

func TestProcessEvent_DeliversChatReplyToBoundChat(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding:  db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1", ChatType: "group"},
		installation:    db.ChannelInstallation{Status: string(InstallationActive)},
		channelIngested: askedOverWecom(), // the question came in over WeCom
	}
	q.fileTask(t, "33333333-3333-3333-3333-333333333333")
	o, instID, conn := newOutboundWithConn(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID

	err := o.processEvent(context.Background(), events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload: protocol.ChatDonePayload{
			Content: "the agent reply",
			TaskID:  "33333333-3333-3333-3333-333333333333",
		},
	})
	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	body := conn.sendBody(t, 0)
	if body["chatid"] != "CHAT_1" {
		t.Errorf("reply chatid = %v, want CHAT_1", body["chatid"])
	}
	if body["chat_type"] != float64(chatTypeGroupInt) {
		t.Errorf("reply chat_type = %v, want group", body["chat_type"])
	}
	md, _ := body["markdown"].(map[string]any)
	if md == nil || md["content"] != "the agent reply" {
		t.Errorf("reply content = %v, want the agent reply", body["markdown"])
	}
}

func TestProcessEvent_NonWecomSessionIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{sessionErr: pgx.ErrNoRows}
	o, _, conn := newOutboundWithConn(t, q)
	if err := o.processEvent(context.Background(), events.Event{ChatSessionID: "22222222-2222-2222-2222-222222222222", Payload: protocol.ChatDonePayload{Content: "x"}}); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(conn.frames) != 0 {
		t.Errorf("expected no send for a non-wecom session, got %d frames", len(conn.frames))
	}
}

func TestProcessEvent_EmptyContentIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"}}
	o, instID, conn := newOutboundWithConn(t, q)
	q.sessionBinding.InstallationID = instID
	if err := o.processEvent(context.Background(), events.Event{ChatSessionID: "22222222-2222-2222-2222-222222222222", Payload: protocol.ChatDonePayload{Content: ""}}); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(conn.frames) != 0 {
		t.Errorf("empty completion should send nothing, got %d frames", len(conn.frames))
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
	q.fileTask(t, "33333333-3333-3333-3333-333333333333")
	o, instID, conn := newOutboundWithConn(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	if err := o.processEvent(context.Background(), events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload: protocol.ChatDonePayload{
			Content: "hi",
			TaskID:  "33333333-3333-3333-3333-333333333333",
		},
	}); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(conn.frames) != 0 {
		t.Errorf("revoked installation should send nothing, got %d frames", len(conn.frames))
	}
}

func TestTryDeliverInbox_PushesToBoundMemberPrivately(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		memberBinding: db.ChannelUserBinding{ChannelUserID: "T_USER_1"},
		workspace:     db.Workspace{Slug: "acme"},
	}
	o, instID, conn := newOutboundWithConn(t, q)
	q.memberBinding.InstallationID = instID

	item := map[string]any{
		"recipient_type": "member",
		"recipient_id":   "33333333-3333-3333-3333-333333333333",
		"workspace_id":   "44444444-4444-4444-4444-444444444444",
		"type":           "issue_assigned",
		"title":          "New issue",
	}
	if !o.tryDeliverInbox(context.Background(), item, "33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444") {
		t.Fatal("tryDeliverInbox returned false; expected delivery to a bound member")
	}
	body := conn.sendBody(t, 0)
	if body["chatid"] != "T_USER_1" {
		t.Errorf("inbox push chatid = %v, want the member's bound userid", body["chatid"])
	}
	if body["chat_type"] != float64(chatTypeSingleInt) {
		t.Errorf("inbox push chat_type = %v, want single (1)", body["chat_type"])
	}
}

func TestTryDeliverInbox_NoBindingIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{memberErr: pgx.ErrNoRows}
	o, _, conn := newOutboundWithConn(t, q)
	if o.tryDeliverInbox(context.Background(), map[string]any{}, "33333333-3333-3333-3333-333333333333", "44444444-4444-4444-4444-444444444444") {
		t.Error("expected false when the member has no wecom binding")
	}
	if len(conn.frames) != 0 {
		t.Errorf("no binding should push nothing, got %d frames", len(conn.frames))
	}
}

func TestHandleInboxNew_IgnoresNonMemberRecipient(t *testing.T) {
	t.Parallel()
	// memberErr set so that if it somehow reached the query it would no-op;
	// the recipient_type guard should return before any query.
	q := &fakeOutboundQueries{memberErr: errors.New("must not be called")}
	o, _, conn := newOutboundWithConn(t, q)
	o.handleInboxNew(events.Event{Payload: map[string]any{
		"item": map[string]any{"recipient_type": "agent", "recipient_id": "x", "workspace_id": "y"},
	}})
	if len(conn.frames) != 0 {
		t.Errorf("agent recipient should not be pushed to, got %d frames", len(conn.frames))
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
	q.fileTask(t, "33333333-3333-3333-3333-333333333333")
	o, instID, conn := newOutboundWithConn(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID

	err := o.processEvent(context.Background(), events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload: protocol.ChatDonePayload{
			Content: "something the room was never meant to read",
			TaskID:  "33333333-3333-3333-3333-333333333333",
		},
	})
	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	conn.mu.Lock()
	n := len(conn.frames)
	conn.mu.Unlock()
	if n != 0 {
		t.Fatalf("a web-UI answer was pushed into the WeCom chat (%d frame(s) written)", n)
	}
}

// TestAWebUIAnswerDoesNotConsumeTheRoomsBubble is the other half of the case
// above, and the reason the gate sits where it does.
//
// That test pins that nothing is SENT. It would still pass with the gate moved
// below sayEnding, because sealing a bubble is not sending a message: the
// asker in the room would lose the reply they are watching for and no
// assertion there would notice. This one is about what the answer TAKES on its
// way to being refused.
//
// So the room has a live question of its own here — a bubble open, waiting on
// an answer — and the installer's browser question finishes first against the
// session they share. Everything WeCom-side has to come out of it untouched:
// the round still open, its bubble still unsealed, the ledger holding no
// record of a run this adapter never ingested, and not a word in the chat.
func TestAWebUIAnswerDoesNotConsumeTheRoomsBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	// The room asks something. Its bubble is open and its round is waiting.
	rig.ran(t, "REQ-1", 1, "task-1")

	// The installer asks the same session something in their browser, and that
	// run finishes first.
	rig.q.fileTask(t, taskUUID(t, "task-2"))
	rig.q.channelIngested = askedInTheWebUI()
	if err := rig.out.processEvent(context.Background(), events.Event{
		ChatSessionID: bubbleSession,
		TaskID:        taskUUID(t, "task-2"),
		Payload:       protocol.ChatDonePayload{Content: "the salary band for that role is 42k"},
	}); err != nil {
		t.Fatalf("a browser question's answer is refused at the gate, so nothing is attempted "+
			"and there is nothing to report: %v", err)
	}

	if rig.streams.depth() != 1 {
		t.Fatalf("the room holds %d open rounds, want 1 — a browser question's answer retired the "+
			"round the room is still waiting on, so the answer to the question they actually "+
			"asked now has nowhere to land", rig.streams.depth())
	}
	frames := rig.conn.streamFrames(t)
	if len(frames) != 1 {
		t.Fatalf("the room's bubble has %d frames, want the 1 that opened it — a browser "+
			"question's answer wrote into a bubble the room opened for its own question", len(frames))
	}
	if frames[0]["finish"] == true {
		t.Fatalf("the room's bubble was sealed by a browser question's answer — the asker is " +
			"looking at an ending to a question they never asked, and the reply they were " +
			"waiting for has nowhere left to go")
	}
	if got := pushedTexts(t, rig.conn); len(got) != 0 {
		t.Fatalf("the room was told %q about a question typed in Multica", got)
	}
	if rig.streams.knowsRound(bubbleSessionID(t), taskUUID(t, "task-2")) {
		t.Fatalf("the ledger has a record of a browser run — owed and the open list are what " +
			"the failure path reads as proof of where a question was asked, so a refused answer " +
			"that files itself there hands the run permission to speak in the room later")
	}
}

// TestAGateThatCannotAnswerDoesNotCostTheAskerTheirBubble is what holds the
// ordering in place, and it is the room's OWN question that pays if it slips.
//
// The test above cannot do that job on its own. A browser run carries its own
// task id, the round matcher is strict about ids, and so a browser answer
// finds no round to take whichever side of sayEnding the gate sits on — it
// passes either way. The case where the take actually lands on the room's
// bubble is the case where the id DOES match, and the gate still refuses:
// fails-closed, when the stamp cannot be read at all.
//
// Put the gate after the take and this is what the asker gets. sayEnding
// consumes the round, the gate then refuses, and the delivery that would have
// sealed the bubble never runs — leaving a spinner on screen that nothing owns
// any more, for the one question in this session that really was asked in the
// room. Ahead of the take, the refusal costs nothing: the round is still open
// and the next publisher of its ending can still speak for it.
func TestAGateThatCannotAnswerDoesNotCostTheAskerTheirBubble(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)

	// The room asks, and this is its own round — the id on the answer is the
	// one the flush bound, so the take will match it.
	rig.ran(t, "REQ-1", 1, "task-1")

	// The database stops answering the one question the gate asks.
	rig.q.originErr = errors.New("connection reset by peer")
	if err := rig.out.processEvent(context.Background(), events.Event{
		ChatSessionID: bubbleSession,
		TaskID:        taskUUID(t, "task-1"),
		Payload:       protocol.ChatDonePayload{Content: "the answer the room is waiting for"},
	}); err == nil {
		t.Fatalf("an origin the gate could not establish was treated as an answer it could deliver")
	}

	if rig.streams.depth() != 1 {
		t.Fatalf("the room holds %d open rounds, want 1 — the gate refused AFTER the take, so the "+
			"asker's bubble was consumed by a delivery that then never ran, and they are left "+
			"watching a spinner no ending can reach", rig.streams.depth())
	}
	frames := rig.conn.streamFrames(t)
	if len(frames) != 1 || frames[0]["finish"] == true {
		t.Fatalf("the room's bubble is %v, want the single unsealed frame that opened it — a "+
			"refusal must leave the round exactly as it found it", frames)
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
	o, instID, conn := newOutboundWithConn(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID

	// No TaskID anywhere: the envelope's is empty and the payload carries none.
	err := o.processEvent(context.Background(), events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload:       protocol.ChatDonePayload{Content: "unattributable"},
	})
	if err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	conn.mu.Lock()
	n := len(conn.frames)
	conn.mu.Unlock()
	if n != 0 {
		t.Fatalf("delivered a completion whose origin could not be established (%d frame(s))", n)
	}
}
