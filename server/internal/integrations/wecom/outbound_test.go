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
	// deliveryChannelType is what the turn's channel_task_delivery row says it
	// came in on. Empty means WeCom, which is what all but a couple of these
	// tests are about; set it to another platform's name to model the
	// chat:done every OTHER adapter publishes onto the shared bus, which this
	// subscriber also sees. That is a different thing from sessionErr =
	// pgx.ErrNoRows, which is a channel turn with NO route recorded at all —
	// the two used to be modelled by the same field and the code told them
	// apart nowhere.
	deliveryChannelType string
	installation        db.ChannelInstallation
	installErr          error
	memberBinding       db.ChannelUserBinding
	memberErr           error
	workspace           db.Workspace
	workspaceErr        error
	attachments         []db.Attachment
	attachmentsErr      error

	// userLanguage is what every user row this fake returns says its profile
	// language is, and userBindingID the Multica user a channel user id
	// resolves to. Both zero by default, which is a reader with no profile —
	// the deployment default, and what every test that predates the copy pack
	// expects to keep seeing.
	userLanguage  string
	userBindingID pgtype.UUID
	userErr       error
	userBindErr   error

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

// GetChannelChatSessionBindingBySession is not part of outboundQueries — the
// answer path reads the turn's route off GetChannelTaskDelivery instead. It is
// here because the same fake stands in for chatBindingLookup, which the typing
// indicator uses to address a failure notice when the handle is gone and this
// process has no note of the round (typing_indicator.go, failure_origin_test.go).
func (f *fakeOutboundQueries) GetChannelChatSessionBindingBySession(context.Context, db.GetChannelChatSessionBindingBySessionParams) (db.ChannelChatSessionBinding, error) {
	return f.sessionBinding, f.sessionErr
}

func (f *fakeOutboundQueries) GetChannelTaskDelivery(context.Context, pgtype.UUID) (db.ChannelTaskDelivery, error) {
	if f.sessionErr != nil {
		return db.ChannelTaskDelivery{}, f.sessionErr
	}
	channelType := f.deliveryChannelType
	if channelType == "" {
		channelType = channelTypeWecom
	}
	return db.ChannelTaskDelivery{
		BindingID: f.sessionBinding.ID, InstallationID: f.sessionBinding.InstallationID,
		ChannelType: channelType, ChannelChatID: f.sessionBinding.ChannelChatID,
		ChatType:         f.sessionBinding.ChatType,
		ChannelMessageID: f.sessionBinding.LastMessageID, ChannelThreadID: f.sessionBinding.LastThreadID,
		RouteRevision: f.sessionBinding.RouteRevision, Config: f.sessionBinding.Config,
	}, nil
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
func (f *fakeOutboundQueries) ListAttachmentsByChatMessage(context.Context, db.ListAttachmentsByChatMessageParams) ([]db.Attachment, error) {
	if f.lookupGate != nil {
		f.lookupsEntered.Add(1)
		<-f.lookupGate
	}
	return f.attachments, f.attachmentsErr
}

// GetChannelUserBindingByUserID and GetUser are the languageLookup half of the
// interface: which Multica user a channel userid belongs to, and what language
// that user reads. A fake with no profile set answers "nothing", which is the
// deployment default — the answer every test written before the copy pack
// expects.
func (f *fakeOutboundQueries) GetChannelUserBindingByUserID(context.Context, db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	if f.userBindErr != nil {
		return db.ChannelUserBinding{}, f.userBindErr
	}
	return db.ChannelUserBinding{MulticaUserID: f.userBindingID}, nil
}

func (f *fakeOutboundQueries) GetUser(_ context.Context, id pgtype.UUID) (db.User, error) {
	if f.userErr != nil {
		return db.User{}, f.userErr
	}
	return db.User{ID: id, Language: pgtype.Text{String: f.userLanguage, Valid: f.userLanguage != ""}}, nil
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

func newOutboundWithConn(t *testing.T, q outboundQueries) (*Outbound, pgtype.UUID, *recordingConn) {
	t.Helper()
	o, instID, conn, _ := newOutboundWithConnMetrics(t, q)
	return o, instID, conn
}

// newOutboundWithConnMetrics is newOutboundWithConn plus the counting sink, for
// the cases whose whole subject is a turn that sends nothing. "No frames" is
// the same observation for a completion this adapter declined and for one it
// never reached a decision about — a fixture that stops early at a gate looks
// exactly like a fixture that ran the branch it is named after. The skip reason
// is what tells the two apart.
func newOutboundWithConnMetrics(t *testing.T, q outboundQueries) (*Outbound, pgtype.UUID, *recordingConn, *countingMetrics) {
	t.Helper()
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	conn := &recordingConn{}
	reg.set(instID, conn.autoAck(newWSSender(conn, nil)))
	mx := newCountingMetrics()
	// No round store: these tests read frames, and a turn with no open bubble
	// takes the ordinary message path. The cases about the bubble itself run on
	// newBubbleRig (stream_bubble_test.go), which wires one.
	return NewOutbound(q, reg, nil, slog.Default(), WithOutboundMetrics(mx)), instID, conn, mx
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

// A completion the agent finished with nothing in, on a WeCom round with no
// bubble left to seal: there is nobody to answer and nothing to answer with, so
// the turn ends in silence and says so as skipNothingToSay.
//
// The rig has to carry a task id, a filed task and the origin stamp. Without
// them processEvent returns at chatDoneTaskID / the origin gate, several
// branches short of the one under test — and the frame count is zero either
// way, so the assertion this test used to make held for a run that never got
// near an empty completion. The skip reason is the assertion that cannot be
// satisfied by stopping early: each exit names its own.
//
// NOTE for a reverse check: there is no production line to delete here — the
// guard is against the FIXTURE regressing. Drop channelIngested (or the task
// id) and this goes red on skipped{origin_not_channel} / dropped{task_missing}
// while go build, go vet and gofmt stay silent, which is the shape of the
// defect it replaces.
func TestProcessEvent_EmptyContentIsNoop(t *testing.T) {
	t.Parallel()
	q := &fakeOutboundQueries{
		sessionBinding:  db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1"},
		installation:    db.ChannelInstallation{Status: string(InstallationActive)},
		channelIngested: askedOverWecom(),
	}
	q.fileTask(t, "33333333-3333-3333-3333-333333333333")
	o, instID, conn, mx := newOutboundWithConnMetrics(t, q)
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID

	if err := o.processEvent(context.Background(), events.Event{
		ChatSessionID: "22222222-2222-2222-2222-222222222222",
		Payload:       protocol.ChatDonePayload{Content: "", TaskID: "33333333-3333-3333-3333-333333333333"},
	}); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if len(conn.frames) != 0 {
		t.Errorf("empty completion should send nothing, got %d frames", len(conn.frames))
	}
	if got := mx.get("outbound_skipped:nothing_to_say"); got != 1 {
		t.Errorf("skipped{nothing_to_say} = %d, want 1 — the turn stopped somewhere earlier than the empty completion, so this case is not testing what it is named after. Where it stopped instead: dropped{task_missing}=%d, skipped{origin_not_channel}=%d, skipped{no_delivery_row}=%d, skipped{installation_inactive}=%d",
			got,
			mx.get("outbound_dropped:task_missing"),
			mx.get("outbound_skipped:origin_not_channel"),
			mx.get("outbound_skipped:no_delivery_row"),
			mx.get("outbound_skipped:installation_inactive"))
	}
	if got := mx.get("outbound_dropped"); got != 0 {
		t.Errorf("dropped = %d, want 0 — an agent with nothing to add is not a failed delivery", got)
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
// the round still open, its bubble still unsealed, the store holding no
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
	if rig.streams.has(bubbleSessionID(t), taskUUID(t, "task-2")) {
		t.Fatalf("the store has a record of a browser run — the open list is what the failure " +
			"path reads as proof of where a question was asked, so a refused answer that files " +
			"itself there hands the run permission to speak in the room later")
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
