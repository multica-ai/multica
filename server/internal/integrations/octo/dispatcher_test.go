package octo

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// --- fakes -----------------------------------------------------------------

type fakeQueries struct {
	inst       db.OctoInstallation
	instErr    error
	claimErr   error // returned by ClaimOctoInboundDedup (pgx.ErrNoRows = duplicate)
	binding    db.OctoUserBinding
	bindingErr error

	markRows    int64
	releaseRows int64
	marked      bool
	released    bool
	// claimCalled records whether ClaimOctoInboundDedup was invoked, so the
	// empty-MessageID dedup-skip path can be asserted.
	claimCalled bool
}

func (f *fakeQueries) GetOctoInstallationByRobotID(ctx context.Context, robotID string) (db.OctoInstallation, error) {
	return f.inst, f.instErr
}
func (f *fakeQueries) ClaimOctoInboundDedup(ctx context.Context, arg db.ClaimOctoInboundDedupParams) (db.OctoInboundDedup, error) {
	f.claimCalled = true
	if f.claimErr != nil {
		return db.OctoInboundDedup{}, f.claimErr
	}
	return db.OctoInboundDedup{InstallationID: arg.InstallationID, MessageID: arg.MessageID, ClaimToken: validUUID(1)}, nil
}
func (f *fakeQueries) MarkOctoInboundDedupProcessed(ctx context.Context, arg db.MarkOctoInboundDedupProcessedParams) (int64, error) {
	f.marked = true
	return f.markRows, nil
}
func (f *fakeQueries) ReleaseOctoInboundDedup(ctx context.Context, arg db.ReleaseOctoInboundDedupParams) (int64, error) {
	f.released = true
	return f.releaseRows, nil
}
func (f *fakeQueries) GetOctoUserBindingByUID(ctx context.Context, arg db.GetOctoUserBindingByUIDParams) (db.OctoUserBinding, error) {
	return f.binding, f.bindingErr
}

type fakeChat struct {
	session   db.ChatSession
	ensureErr error
	// ensureParams captures the last EnsureChatSession call so tests can assert
	// the creator-selection rule (installer for groups, sender for DMs).
	ensureParams EnsureChatSessionParams
	appendResult AppendResult
	appendErr    error
	// appendParams captures the last AppendUserMessage call so tests can assert
	// the body and dedup claim token reach the chat layer.
	appendParams AppendUserMessageParams
}

func (f *fakeChat) EnsureChatSession(ctx context.Context, p EnsureChatSessionParams) (db.ChatSession, error) {
	f.ensureParams = p
	return f.session, f.ensureErr
}
func (f *fakeChat) AppendUserMessage(ctx context.Context, p AppendUserMessageParams) (AppendResult, error) {
	f.appendParams = p
	return f.appendResult, f.appendErr
}

type fakeEnqueuer struct {
	task db.AgentTaskQueue
	err  error
	// called records whether EnqueueChatTask was invoked.
	called bool
	// forceFresh captures the forceFreshSession arg from the last call so
	// /new-directive tests can assert it propagated through the dispatcher.
	forceFresh bool
}

func (f *fakeEnqueuer) EnqueueChatTask(ctx context.Context, session db.ChatSession, initiatorUserID pgtype.UUID, forceFreshSession bool) (db.AgentTaskQueue, error) {
	f.called = true
	f.forceFresh = forceFreshSession
	return f.task, f.err
}

type fakeAudit struct {
	reasons []DropReason
}

func (f *fakeAudit) RecordDrop(ctx context.Context, p AuditDropParams) error {
	f.reasons = append(f.reasons, p.Reason)
	return nil
}

func validUUID(b byte) pgtype.UUID {
	var u pgtype.UUID
	for i := range u.Bytes {
		u.Bytes[i] = b
	}
	u.Valid = true
	return u
}

// activeInstallation returns a ready-to-route installation row.
func activeInstallation() db.OctoInstallation {
	return db.OctoInstallation{
		ID:              validUUID(0xAA),
		WorkspaceID:     validUUID(0xBB),
		AgentID:         validUUID(0xCC),
		InstallerUserID: validUUID(0xDD),
		RobotID:         "robot_x",
		Status:          "active",
	}
}

func boundUser() db.OctoUserBinding {
	return db.OctoUserBinding{
		ID:             validUUID(0xEE),
		MulticaUserID:  validUUID(0x11),
		InstallationID: validUUID(0xAA),
	}
}

func dmMessage() InboundMessage {
	return InboundMessage{
		RobotID:     "robot_x",
		MessageID:   "msg_1",
		SenderUID:   "uid_1",
		ChannelID:   "ch_1",
		ChannelType: ChannelDM,
		Body:        "hello",
	}
}

// newDispatcher wires a dispatcher over the supplied fakes.
func newDispatcher(q *fakeQueries, c *fakeChat, e *fakeEnqueuer, a *fakeAudit) *Dispatcher {
	return &Dispatcher{Queries: q, Chat: c, TaskService: e, Audit: a}
}

// --- tests -----------------------------------------------------------------

func TestHandle_UnknownRobot_Drops(t *testing.T) {
	q := &fakeQueries{instErr: pgx.ErrNoRows}
	a := &fakeAudit{}
	d := newDispatcher(q, &fakeChat{}, &fakeEnqueuer{}, a)

	res, err := d.Handle(context.Background(), dmMessage())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Outcome != OutcomeDropped || res.DropReason != DropReasonInvalidEvent {
		t.Errorf("got %+v, want dropped/invalid_event", res)
	}
}

func TestHandle_RevokedInstallation_Drops(t *testing.T) {
	inst := activeInstallation()
	inst.Status = "revoked"
	q := &fakeQueries{inst: inst}
	a := &fakeAudit{}
	d := newDispatcher(q, &fakeChat{}, &fakeEnqueuer{}, a)

	res, _ := d.Handle(context.Background(), dmMessage())
	if res.DropReason != DropReasonRevokedInstallation {
		t.Errorf("got %q, want revoked_installation", res.DropReason)
	}
}

func TestHandle_DuplicateClaim_Drops(t *testing.T) {
	q := &fakeQueries{inst: activeInstallation(), claimErr: pgx.ErrNoRows}
	a := &fakeAudit{}
	d := newDispatcher(q, &fakeChat{}, &fakeEnqueuer{}, a)

	res, _ := d.Handle(context.Background(), dmMessage())
	if res.DropReason != DropReasonDuplicate {
		t.Errorf("got %q, want duplicate", res.DropReason)
	}
}

func TestHandle_GroupNotAddressed_Drops(t *testing.T) {
	q := &fakeQueries{inst: activeInstallation(), markRows: 1}
	a := &fakeAudit{}
	d := newDispatcher(q, &fakeChat{}, &fakeEnqueuer{}, a)

	msg := dmMessage()
	msg.ChannelType = ChannelGroup
	msg.AddressedToBot = false

	res, _ := d.Handle(context.Background(), msg)
	if res.DropReason != DropReasonNotAddressedInGroup {
		t.Errorf("got %q, want not_addressed_in_group", res.DropReason)
	}
	if !q.marked {
		t.Errorf("expected dedup mark on a durable drop")
	}
}

func TestHandle_UnboundUser_NeedsBinding(t *testing.T) {
	q := &fakeQueries{inst: activeInstallation(), bindingErr: pgx.ErrNoRows, markRows: 1}
	a := &fakeAudit{}
	d := newDispatcher(q, &fakeChat{}, &fakeEnqueuer{}, a)

	res, _ := d.Handle(context.Background(), dmMessage())
	if res.Outcome != OutcomeNeedsBinding {
		t.Errorf("got %q, want needs_binding", res.Outcome)
	}
	if res.SenderUID != "uid_1" {
		t.Errorf("SenderUID = %q, want uid_1", res.SenderUID)
	}
}

func TestHandle_Ingested_EnqueuesTask(t *testing.T) {
	q := &fakeQueries{
		inst:    activeInstallation(),
		binding: boundUser(),
	}
	c := &fakeChat{session: db.ChatSession{ID: validUUID(0x22)}, appendResult: AppendResult{DedupMarked: true}}
	e := &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x33)}}
	d := newDispatcher(q, c, e, &fakeAudit{})

	res, err := d.Handle(context.Background(), dmMessage())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("got %q, want ingested", res.Outcome)
	}
	if !e.called {
		t.Errorf("expected EnqueueChatTask to be called")
	}
	if res.TaskID != validUUID(0x33) {
		t.Errorf("TaskID not propagated: %v", res.TaskID)
	}
	if q.marked || q.released {
		t.Errorf("ingest path should finalize in-tx (no post Mark/Release), got marked=%v released=%v", q.marked, q.released)
	}
	// The DM creator is the bound sender, not the installer.
	if c.ensureParams.Creator != boundUser().MulticaUserID {
		t.Errorf("DM session Creator = %v, want sender %v", c.ensureParams.Creator, boundUser().MulticaUserID)
	}
	// The message body and dedup claim token must reach the chat layer intact.
	if c.appendParams.Body != "hello" {
		t.Errorf("AppendUserMessage Body = %q, want %q", c.appendParams.Body, "hello")
	}
	if c.appendParams.MessageID != "msg_1" {
		t.Errorf("AppendUserMessage MessageID = %q, want msg_1", c.appendParams.MessageID)
	}
	if c.appendParams.ClaimToken != validUUID(1) {
		t.Errorf("AppendUserMessage ClaimToken = %v, want the claim token validUUID(1)", c.appendParams.ClaimToken)
	}
}

func TestHandle_GroupAddressed_CreatorIsInstaller(t *testing.T) {
	q := &fakeQueries{
		inst:    activeInstallation(),
		binding: boundUser(),
	}
	c := &fakeChat{session: db.ChatSession{ID: validUUID(0x22)}, appendResult: AppendResult{DedupMarked: true}}
	e := &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x33)}}
	d := newDispatcher(q, c, e, &fakeAudit{})

	msg := dmMessage()
	msg.ChannelType = ChannelGroup
	msg.AddressedToBot = true

	res, err := d.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("got %q, want ingested", res.Outcome)
	}
	// Group sessions are owned by the installer (stable workspace identity that
	// won't cascade away as members churn), not the churnable sender binding.
	if c.ensureParams.Creator != activeInstallation().InstallerUserID {
		t.Errorf("group session Creator = %v, want installer %v", c.ensureParams.Creator, activeInstallation().InstallerUserID)
	}
}

func TestHandle_AgentOffline(t *testing.T) {
	q := &fakeQueries{
		inst:    activeInstallation(),
		binding: boundUser(),
	}
	c := &fakeChat{session: db.ChatSession{ID: validUUID(0x22)}, appendResult: AppendResult{DedupMarked: true}}
	e := &fakeEnqueuer{err: service.ErrChatTaskAgentNoRuntime}
	d := newDispatcher(q, c, e, &fakeAudit{})

	res, _ := d.Handle(context.Background(), dmMessage())
	if res.Outcome != OutcomeAgentOffline {
		t.Errorf("got %q, want agent_offline", res.Outcome)
	}
}

func TestHandle_AgentArchived(t *testing.T) {
	q := &fakeQueries{
		inst:    activeInstallation(),
		binding: boundUser(),
	}
	c := &fakeChat{session: db.ChatSession{ID: validUUID(0x22)}, appendResult: AppendResult{DedupMarked: true}}
	e := &fakeEnqueuer{err: service.ErrChatTaskAgentArchived}
	d := newDispatcher(q, c, e, &fakeAudit{})

	res, _ := d.Handle(context.Background(), dmMessage())
	if res.Outcome != OutcomeAgentArchived {
		t.Errorf("got %q, want agent_archived", res.Outcome)
	}
}

// TestHandle_EnqueueInfraError_IngestedNoTask verifies that a generic (non-
// sentinel) EnqueueChatTask failure still reports the message as ingested and
// finalizes in-tx — the chat_message is durable, so the claim is never released.
func TestHandle_EnqueueInfraError_IngestedNoTask(t *testing.T) {
	q := &fakeQueries{
		inst:    activeInstallation(),
		binding: boundUser(),
	}
	c := &fakeChat{session: db.ChatSession{ID: validUUID(0x22)}, appendResult: AppendResult{DedupMarked: true}}
	e := &fakeEnqueuer{err: errors.New("db down")}
	d := newDispatcher(q, c, e, &fakeAudit{})

	res, err := d.Handle(context.Background(), dmMessage())
	if err != nil {
		t.Fatalf("infra enqueue error must not propagate, got: %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Errorf("got %q, want ingested (message is durable)", res.Outcome)
	}
	if res.TaskID.Valid {
		t.Errorf("no task should be enqueued on infra failure, got TaskID %v", res.TaskID)
	}
	if q.released {
		t.Errorf("post-durable failure must not Release the claim")
	}
}

func TestHandle_ClaimLost_DropsDuplicate(t *testing.T) {
	q := &fakeQueries{
		inst:    activeInstallation(),
		binding: boundUser(),
	}
	c := &fakeChat{session: db.ChatSession{ID: validUUID(0x22)}, appendErr: ErrClaimLost}
	d := newDispatcher(q, c, &fakeEnqueuer{}, &fakeAudit{})

	res, err := d.Handle(context.Background(), dmMessage())
	if err != nil {
		t.Fatalf("ErrClaimLost should be swallowed as a drop, got err: %v", err)
	}
	if res.DropReason != DropReasonDuplicate {
		t.Errorf("got %q, want duplicate", res.DropReason)
	}
	if q.released {
		t.Errorf("ClaimLost is finalizeNone — must not Release the row")
	}
}

func TestHandle_EnsureSessionError_Releases(t *testing.T) {
	q := &fakeQueries{
		inst:        activeInstallation(),
		binding:     boundUser(),
		releaseRows: 1,
	}
	c := &fakeChat{ensureErr: errors.New("db down")}
	d := newDispatcher(q, c, &fakeEnqueuer{}, &fakeAudit{})

	_, err := d.Handle(context.Background(), dmMessage())
	if err == nil {
		t.Fatalf("expected infra error")
	}
	if !q.released {
		t.Errorf("expected dedup release on pre-durable failure")
	}
}

// TestHandle_AppendError_Releases verifies the append-failure (non-ClaimLost)
// path releases the claim: AppendUserMessage rolled back, nothing durable
// landed, so the claim is freed for retry.
func TestHandle_AppendError_Releases(t *testing.T) {
	q := &fakeQueries{
		inst:        activeInstallation(),
		binding:     boundUser(),
		releaseRows: 1,
	}
	c := &fakeChat{session: db.ChatSession{ID: validUUID(0x22)}, appendErr: errors.New("db down")}
	d := newDispatcher(q, c, &fakeEnqueuer{}, &fakeAudit{})

	_, err := d.Handle(context.Background(), dmMessage())
	if err == nil {
		t.Fatalf("expected infra error from append")
	}
	if !q.released {
		t.Errorf("expected dedup release on append failure (rolled back, nothing durable)")
	}
	if q.marked {
		t.Errorf("append failure must not Mark the claim terminal")
	}
}

// TestHandle_EmptyMessageID_SkipsDedup verifies that a message with no id skips
// the dedup gate entirely (no Claim/Mark/Release) yet still ingests.
func TestHandle_EmptyMessageID_SkipsDedup(t *testing.T) {
	q := &fakeQueries{
		inst:    activeInstallation(),
		binding: boundUser(),
	}
	c := &fakeChat{session: db.ChatSession{ID: validUUID(0x22)}, appendResult: AppendResult{DedupMarked: true}}
	e := &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x33)}}
	d := newDispatcher(q, c, e, &fakeAudit{})

	msg := dmMessage()
	msg.MessageID = ""

	res, err := d.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Errorf("got %q, want ingested", res.Outcome)
	}
	if q.claimCalled {
		t.Errorf("empty MessageID must skip the dedup claim")
	}
	if q.marked || q.released {
		t.Errorf("empty MessageID must not Mark/Release, got marked=%v released=%v", q.marked, q.released)
	}
}

// /new directive: the first-line command must be stripped from the persisted
// chat_message body (the agent never sees the command itself) AND
// EnqueueChatTask must receive forceFreshSession=true so the daemon skips
// prior chat-session resume for this dispatch. Without both, /new is either
// echoed back as user content or silently downgraded to a normal turn.
func TestHandle_NewCommand_StripsAndForcesFreshSession(t *testing.T) {
	q := &fakeQueries{
		inst:    activeInstallation(),
		binding: boundUser(),
	}
	c := &fakeChat{session: db.ChatSession{ID: validUUID(0x22)}, appendResult: AppendResult{DedupMarked: true}}
	e := &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x33)}}
	d := newDispatcher(q, c, e, &fakeAudit{})

	msg := dmMessage()
	msg.Body = "/new restart please"

	res, err := d.Handle(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("got %q, want ingested", res.Outcome)
	}
	if c.appendParams.Body != "restart please" {
		t.Errorf("AppendUserMessage Body = %q, want %q (the /new prefix must be stripped before persistence)", c.appendParams.Body, "restart please")
	}
	if !e.forceFresh {
		t.Errorf("expected EnqueueChatTask to receive forceFreshSession=true after /new")
	}
}

// A normal message (no /new) MUST NOT set forceFreshSession — the regression
// the previous test guards against has a mirror: silently sending fresh on
// every message would invalidate Octo's session-resume product semantics.
func TestHandle_NoNewCommand_KeepsFreshFalse(t *testing.T) {
	q := &fakeQueries{
		inst:    activeInstallation(),
		binding: boundUser(),
	}
	c := &fakeChat{session: db.ChatSession{ID: validUUID(0x22)}, appendResult: AppendResult{DedupMarked: true}}
	e := &fakeEnqueuer{task: db.AgentTaskQueue{ID: validUUID(0x33)}}
	d := newDispatcher(q, c, e, &fakeAudit{})

	res, err := d.Handle(context.Background(), dmMessage())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Outcome != OutcomeIngested {
		t.Fatalf("got %q, want ingested", res.Outcome)
	}
	if e.forceFresh {
		t.Errorf("plain message must not set forceFreshSession")
	}
}

