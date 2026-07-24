package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---- fakes ----

type fakeInstaller struct {
	inst ResolvedInstallation
	err  error
}

func (f *fakeInstaller) ResolveInstallation(_ context.Context, _ channel.InboundMessage) (ResolvedInstallation, error) {
	return f.inst, f.err
}

type fakeIdentity struct {
	id  ResolvedIdentity
	err error
}

func (f *fakeIdentity) ResolveSender(_ context.Context, _ ResolvedInstallation, _ channel.InboundMessage) (ResolvedIdentity, error) {
	return f.id, f.err
}

type fakeDedup struct {
	mu         sync.Mutex
	token      pgtype.UUID
	claimErr   error
	markCalls  int
	relCalls   int
	claimCalls int
}

func (f *fakeDedup) Claim(_ context.Context, _ pgtype.UUID, _ string) (pgtype.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimCalls++
	if f.claimErr != nil {
		return pgtype.UUID{}, f.claimErr
	}
	return f.token, nil
}
func (f *fakeDedup) Mark(_ context.Context, _ pgtype.UUID, _ string, _ pgtype.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markCalls++
	return nil
}
func (f *fakeDedup) Release(_ context.Context, _ pgtype.UUID, _ string, _ pgtype.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.relCalls++
	return nil
}
func (f *fakeDedup) marks() int    { f.mu.Lock(); defer f.mu.Unlock(); return f.markCalls }
func (f *fakeDedup) releases() int { f.mu.Lock(); defer f.mu.Unlock(); return f.relCalls }

type fakeBinder struct {
	ensureID     pgtype.UUID
	ensureErr    error
	appendResult AppendResult
	appendErr    error
	lastEnsure   EnsureSessionParams
	lastAppend   AppendParams
}

func (f *fakeBinder) EnsureSession(_ context.Context, p EnsureSessionParams) (pgtype.UUID, error) {
	f.lastEnsure = p
	return f.ensureID, f.ensureErr
}
func (f *fakeBinder) AppendMessage(_ context.Context, p AppendParams) (AppendResult, error) {
	f.lastAppend = p
	return f.appendResult, f.appendErr
}

type fakeAuditor struct {
	mu    sync.Mutex
	drops []DropReason
}

func (f *fakeAuditor) RecordDrop(_ context.Context, _ pgtype.UUID, _ channel.InboundMessage, reason DropReason) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drops = append(f.drops, reason)
	return nil
}
func (f *fakeAuditor) last() (DropReason, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.drops) == 0 {
		return "", false
	}
	return f.drops[len(f.drops)-1], true
}

type fakeReplier struct {
	mu      sync.Mutex
	results []Result
}

func (f *fakeReplier) Reply(_ context.Context, _ ResolvedInstallation, _ channel.InboundMessage, res Result) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.results = append(f.results, res)
}
func (f *fakeReplier) calls() []Result {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Result(nil), f.results...)
}

type fakeTyping struct {
	mu      sync.Mutex
	count   int
	settled int
}

func (f *fakeTyping) OnIngested(_ context.Context, _ ResolvedInstallation, _ channel.InboundMessage, _ pgtype.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
}
func (f *fakeTyping) OnSettled(_ context.Context, _ pgtype.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.settled++
}
func (f *fakeTyping) calls() int        { f.mu.Lock(); defer f.mu.Unlock(); return f.count }
func (f *fakeTyping) settledCalls() int { f.mu.Lock(); defer f.mu.Unlock(); return f.settled }

type fakeIssues struct {
	called bool
	params service.IssueCreateParams
	result service.IssueCreateResult
	err    error
}

func (f *fakeIssues) Create(_ context.Context, p service.IssueCreateParams, _ service.IssueCreateOpts) (service.IssueCreateResult, error) {
	f.called = true
	f.params = p
	return f.result, f.err
}

type fakeTasks struct {
	mu         sync.Mutex
	called     bool
	forceFresh bool
	initiator  pgtype.UUID
	err        error
}

func (f *fakeTasks) EnqueueChatTask(_ context.Context, _ db.ChatSession, initiator pgtype.UUID, forceFresh bool) (db.AgentTaskQueue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.forceFresh = forceFresh
	f.initiator = initiator
	return db.AgentTaskQueue{}, f.err
}
func (f *fakeTasks) wasCalled() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.called }
func (f *fakeTasks) freshArg() bool  { f.mu.Lock(); defer f.mu.Unlock(); return f.forceFresh }

type fakeReader struct {
	session db.ChatSession
	ws      db.Workspace
	sessErr error
}

func (f *fakeReader) GetChatSession(_ context.Context, _ pgtype.UUID) (db.ChatSession, error) {
	return f.session, f.sessErr
}
func (f *fakeReader) GetWorkspace(_ context.Context, _ pgtype.UUID) (db.Workspace, error) {
	return f.ws, nil
}

type fakeQuickCreate struct {
	mu          sync.Mutex
	called      bool
	prompt      string
	ws          pgtype.UUID
	requester   pgtype.UUID
	agent       pgtype.UUID
	session     pgtype.UUID
	attachments []pgtype.UUID
	err         error
}

func (f *fakeQuickCreate) EnqueueQuickCreateChatTask(_ context.Context, workspaceID, requesterID, agentID pgtype.UUID, prompt string, chatSessionID pgtype.UUID, attachmentIDs []pgtype.UUID) (db.AgentTaskQueue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.ws, f.requester, f.agent, f.session, f.prompt = workspaceID, requesterID, agentID, chatSessionID, prompt
	f.attachments = attachmentIDs
	return db.AgentTaskQueue{}, f.err
}
func (f *fakeQuickCreate) wasCalled() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.called }

type fakeMedia struct {
	mu          sync.Mutex
	ingestCalls int
	lastParams  IngestParams
	staged      []StagedMedia
	err         error
	discards    int
}

func (f *fakeMedia) Ingest(_ context.Context, p IngestParams) ([]StagedMedia, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ingestCalls++
	f.lastParams = p
	if f.err != nil {
		return nil, f.err
	}
	return f.staged, nil
}
func (f *fakeMedia) Discard(_ context.Context, _ []StagedMedia) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.discards++
}
func (f *fakeMedia) ingests() int      { f.mu.Lock(); defer f.mu.Unlock(); return f.ingestCalls }
func (f *fakeMedia) discardCalls() int { f.mu.Lock(); defer f.mu.Unlock(); return f.discards }

type fakeMessages struct {
	mu   sync.Mutex
	rows []db.CreateChatMessageParams
}

func (f *fakeMessages) CreateChatMessage(_ context.Context, arg db.CreateChatMessageParams) (db.ChatMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, arg)
	return db.ChatMessage{}, nil
}
func (f *fakeMessages) all() []db.CreateChatMessageParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]db.CreateChatMessageParams(nil), f.rows...)
}

// ---- harness ----

func activeResolved(t *testing.T) ResolvedInstallation {
	return ResolvedInstallation{
		ID:              uuidFromString(t, "11111111-1111-1111-1111-111111111111"),
		WorkspaceID:     uuidFromString(t, "22222222-2222-2222-2222-222222222222"),
		AgentID:         uuidFromString(t, "33333333-3333-3333-3333-333333333333"),
		InstallerUserID: uuidFromString(t, "99999999-9999-9999-9999-999999999999"),
		Active:          true,
	}
}

func p2pMessage(t *testing.T) channel.InboundMessage {
	return channel.InboundMessage{
		EventID:   "evt-1",
		MessageID: "om-1",
		Type:      channel.MsgTypeText,
		Text:      "hello",
		Source: channel.Source{
			ChannelType: channel.TypeFeishu,
			ChatID:      "oc_chat",
			ChatType:    channel.ChatTypeP2P,
			SenderID:    "ou_user_a",
		},
	}
}

type harness struct {
	router  *Router
	inst    *fakeInstaller
	ident   *fakeIdentity
	dedup   *fakeDedup
	binder  *fakeBinder
	audit   *fakeAuditor
	replier *fakeReplier
	typing  *fakeTyping
	issues  *fakeIssues
	tasks   *fakeTasks
	reader  *fakeReader
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		inst:    &fakeInstaller{inst: activeResolved(t)},
		ident:   &fakeIdentity{id: ResolvedIdentity{UserID: uuidFromString(t, "44444444-4444-4444-4444-444444444444")}},
		dedup:   &fakeDedup{token: uuidFromString(t, "55555555-5555-5555-5555-555555555555")},
		binder:  &fakeBinder{ensureID: uuidFromString(t, "66666666-6666-6666-6666-666666666666"), appendResult: AppendResult{DedupMarked: true}},
		audit:   &fakeAuditor{},
		replier: &fakeReplier{},
		typing:  &fakeTyping{},
		issues:  &fakeIssues{},
		tasks:   &fakeTasks{},
		reader:  &fakeReader{ws: db.Workspace{IssuePrefix: "MUL"}},
	}
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{Logger: discardLogger()})
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst,
		Identity:     h.ident,
		Dedup:        h.dedup,
		Session:      h.binder,
		Audit:        h.audit,
		Replier:      h.replier,
		Typing:       h.typing,
		OriginType:   "lark_chat",
	})
	return h
}

func TestRouter_NoResolverSet_ReturnsError(t *testing.T) {
	h := newHarness(t)
	msg := p2pMessage(t)
	msg.Source.ChannelType = channel.Type("slack")
	if err := h.router.Handle(context.Background(), msg); !errors.Is(err, ErrNoResolverSet) {
		t.Fatalf("expected ErrNoResolverSet, got %v", err)
	}
}

func TestRouter_InstallationNotFound_Drops(t *testing.T) {
	h := newHarness(t)
	h.inst.err = ErrInstallationNotFound
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("drop must not be an error: %v", err)
	}
	if r, _ := h.audit.last(); r != DropReasonInvalidEvent {
		t.Fatalf("expected invalid_event audit, got %q", r)
	}
	if h.dedup.claimCalls != 0 {
		t.Fatalf("must not claim dedup before installation routing")
	}
}

func TestRouter_RevokedInstallation_Drops(t *testing.T) {
	h := newHarness(t)
	h.inst.inst.Active = false
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r, _ := h.audit.last(); r != DropReasonRevokedInstallation {
		t.Fatalf("expected revoked_installation, got %q", r)
	}
}

func TestRouter_Duplicate_Drops(t *testing.T) {
	h := newHarness(t)
	h.dedup.claimErr = ErrDuplicate
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r, _ := h.audit.last(); r != DropReasonDuplicate {
		t.Fatalf("expected duplicate, got %q", r)
	}
}

func TestRouter_GroupNotAddressed_Drops(t *testing.T) {
	h := newHarness(t)
	msg := p2pMessage(t)
	msg.Source.ChatType = channel.ChatTypeGroup
	msg.AddressedToBot = false
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r, _ := h.audit.last(); r != DropReasonNotAddressedInGroup {
		t.Fatalf("expected not_addressed_in_group, got %q", r)
	}
	if h.dedup.marks() != 1 {
		t.Fatalf("group-filter drop must finalize Mark (1), got %d", h.dedup.marks())
	}
}

func TestRouter_UnboundSender_NeedsBinding(t *testing.T) {
	h := newHarness(t)
	h.ident.err = ErrSenderUnbound
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r, _ := h.audit.last(); r != DropReasonUnboundUser {
		t.Fatalf("expected unbound_user audit, got %q", r)
	}
	if h.dedup.marks() != 1 {
		t.Fatalf("unbound drop must finalize Mark, got %d", h.dedup.marks())
	}
	if !waitFor(time.Second, func() bool {
		for _, r := range h.replier.calls() {
			if r.Outcome == OutcomeNeedsBinding && r.Sender == "ou_user_a" {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("expected a NeedsBinding reply targeting the sender")
	}
}

func TestRouter_NonMember_Drops(t *testing.T) {
	h := newHarness(t)
	h.ident.err = ErrSenderNotMember
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r, _ := h.audit.last(); r != DropReasonNonWorkspaceMember {
		t.Fatalf("expected non_workspace_member, got %q", r)
	}
}

func TestRouter_EnsureSessionError_Releases(t *testing.T) {
	h := newHarness(t)
	h.binder.ensureErr = errors.New("db down")
	err := h.router.Handle(context.Background(), p2pMessage(t))
	if err == nil {
		t.Fatal("ensure-session infra error must surface to the caller")
	}
	if h.dedup.releases() != 1 {
		t.Fatalf("ensure-session error must Release the claim (1), got %d", h.dedup.releases())
	}
}

func TestRouter_Ingested_InTxMark_FinalizeNone(t *testing.T) {
	h := newHarness(t)
	h.reader.session = db.ChatSession{}
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// AppendMessage marked in-tx (DedupMarked=true) -> no post-pipeline Mark.
	if h.dedup.marks() != 0 {
		t.Fatalf("in-tx Mark must skip post-pipeline finalize Mark, got %d", h.dedup.marks())
	}
	if h.dedup.releases() != 0 {
		t.Fatalf("a durable ingest must not Release, got %d", h.dedup.releases())
	}
	if !h.tasks.wasCalled() {
		t.Fatalf("ingest must trigger a chat run (inline, no batcher)")
	}
	if !waitFor(time.Second, func() bool { return h.typing.calls() == 1 }) {
		t.Fatalf("ingest must show the typing indicator")
	}
}

func TestRouter_ClaimLost_Drops(t *testing.T) {
	h := newHarness(t)
	h.binder.appendErr = ErrClaimLost
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("ErrClaimLost must be a duplicate drop, not an error: %v", err)
	}
	if r, _ := h.audit.last(); r != DropReasonDuplicate {
		t.Fatalf("expected duplicate, got %q", r)
	}
	if h.dedup.releases() != 0 || h.dedup.marks() != 0 {
		t.Fatalf("ErrClaimLost must finalizeNone (no Mark/Release); marks=%d rel=%d", h.dedup.marks(), h.dedup.releases())
	}
}

func TestRouter_IssueCommand_Creates(t *testing.T) {
	h := newHarness(t)
	h.binder.appendResult = AppendResult{DedupMarked: true, IssueCommand: &IssueCommand{Title: "Fix login", Description: "details"}}
	h.issues.result = service.IssueCreateResult{Issue: db.Issue{ID: uuidFromString(t, "77777777-7777-7777-7777-777777777777"), Number: 42, Title: "Fix login"}}
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !h.issues.called {
		t.Fatal("expected issue create")
	}
	if h.issues.params.OriginType.String != "lark_chat" {
		t.Fatalf("origin_type must come from the resolver set, got %q", h.issues.params.OriginType.String)
	}
	if !waitFor(time.Second, func() bool {
		for _, r := range h.replier.calls() {
			if r.IssueIdentifier == "MUL-42" && r.IssueTitle == "Fix login" {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("expected an issue-created reply with the workspace-qualified identifier")
	}
}

func TestRouter_GroupSessionCreatorIsInstaller(t *testing.T) {
	h := newHarness(t)
	msg := p2pMessage(t)
	msg.Source.ChatType = channel.ChatTypeGroup
	msg.AddressedToBot = true
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.binder.lastEnsure.Sender != h.inst.inst.InstallerUserID {
		t.Fatalf("group session creator must be the installer")
	}
	// And the run initiator is the sender, not the installer.
	if h.tasks.initiator != h.ident.id.UserID {
		t.Fatalf("run initiator must be the message sender")
	}
}

func TestRouter_P2PSessionCreatorIsSender(t *testing.T) {
	h := newHarness(t)
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.binder.lastEnsure.Sender != h.ident.id.UserID {
		t.Fatalf("p2p session creator must be the sender")
	}
}

func TestRouter_FlushOffline_RepliesAgentOffline(t *testing.T) {
	h := newHarness(t)
	h.tasks.err = service.ErrChatTaskAgentNoRuntime
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Inline flush (no batcher) emits the offline notice synchronously via replier.
	found := false
	for _, r := range h.replier.calls() {
		if r.Outcome == OutcomeAgentOffline {
			found = true
		}
	}
	if !found {
		t.Fatalf("agent-no-runtime must emit an AgentOffline reply")
	}
	// The reaction was added on ingest but no task will run, so the bus-driven
	// clear never fires — the flush must clear the typing indicator itself.
	if h.typing.settledCalls() != 1 {
		t.Fatalf("offline flush must clear the typing indicator, got %d OnSettled calls", h.typing.settledCalls())
	}
}

func TestRouter_FlushArchived_ClearsTyping(t *testing.T) {
	h := newHarness(t)
	h.tasks.err = service.ErrChatTaskAgentArchived
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.typing.settledCalls() != 1 {
		t.Fatalf("archived flush must clear the typing indicator, got %d OnSettled calls", h.typing.settledCalls())
	}
}

func TestRouter_FlushSuccess_DoesNotClearTyping(t *testing.T) {
	h := newHarness(t)
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !h.tasks.wasCalled() {
		t.Fatalf("a healthy session must enqueue a task")
	}
	// A successfully enqueued task is cleared by the platform's bus-driven
	// handler on chat-done / task-failed, NOT by the flush.
	if h.typing.settledCalls() != 0 {
		t.Fatalf("successful flush must not clear the typing indicator, got %d OnSettled calls", h.typing.settledCalls())
	}
}

func TestRouter_ForceFresh_Propagates(t *testing.T) {
	h := newHarness(t)
	msg := p2pMessage(t)
	msg.ForceFresh = true
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !h.tasks.freshArg() {
		t.Fatalf("ForceFresh must propagate to EnqueueChatTask")
	}
}

func TestRouter_BareFreshReset_NoRun(t *testing.T) {
	h := newHarness(t)
	msg := p2pMessage(t)
	msg.ForceFresh = true
	msg.BareFresh = true // a lone "/new": rotate the session, no prompt to run
	msg.Text = ""
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The session is still rotated (EnsureSession) and the ingest is durable,
	// but with no prompt there is nothing for the agent to act on.
	if h.tasks.wasCalled() {
		t.Fatalf("a bare fresh-session reset must not enqueue an agent run")
	}
	// No prompt to record: the fresh transcript must not begin with a blank
	// user message, so AppendMessage is skipped entirely.
	if h.binder.lastAppend.SessionID.Valid {
		t.Fatalf("a bare fresh-session reset must not append an (empty) user message")
	}
	// No run means nothing would ever clear a typing indicator, so it must not
	// be shown in the first place. The indicator fires in a detached goroutine,
	// so wait a bounded window and fail if it is ever shown.
	if waitFor(200*time.Millisecond, func() bool { return h.typing.calls() > 0 }) {
		t.Fatalf("a bare fresh-session reset must not show the typing indicator")
	}
}

// A "/new hello" (ForceFresh but NOT bare) must still run the agent: the reset
// short-circuit keys on BareFresh, never on Text being empty. This guards the
// regression where an adapter enriches Text (e.g. Lark injects group
// <recent_context>) so a lone "/new" arrives with a non-empty Text — the run
// must be decided by BareFresh, which the adapter sets from the user's own body.
func TestRouter_ForceFreshWithPrompt_Runs(t *testing.T) {
	h := newHarness(t)
	msg := p2pMessage(t)
	msg.ForceFresh = true
	msg.BareFresh = false
	msg.Text = "[Alice]: hello\n<recent_context>…</recent_context>" // enriched, non-empty
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !h.tasks.wasCalled() {
		t.Fatalf("a fresh session with a prompt must still enqueue an agent run")
	}
	if !h.binder.lastAppend.SessionID.Valid {
		t.Fatalf("a fresh session with a prompt must append the user message")
	}
}

func TestRouter_DrainJoinsReplies(t *testing.T) {
	h := newHarness(t)
	h.ident.err = ErrSenderUnbound // triggers a NeedsBinding reply goroutine
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	done := make(chan struct{})
	go func() { h.router.Drain(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Drain did not join the reply goroutine")
	}
	if len(h.replier.calls()) != 1 {
		t.Fatalf("expected exactly one reply after drain, got %d", len(h.replier.calls()))
	}
}

func TestRouter_EmptyMessageID_SkipsDedup(t *testing.T) {
	h := newHarness(t)
	msg := p2pMessage(t)
	msg.MessageID = ""
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h.dedup.claimCalls != 0 {
		t.Fatalf("empty message id must skip the dedup claim, got %d", h.dedup.claimCalls)
	}
	if !h.tasks.wasCalled() {
		t.Fatalf("message must still ingest without a dedup key")
	}
}

func newQuickCreateHarness(t *testing.T) (*harness, *fakeQuickCreate, *fakeMessages) {
	t.Helper()
	h := newHarness(t)
	qc := &fakeQuickCreate{}
	msgs := &fakeMessages{}
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{Logger: discardLogger(), Messages: msgs})
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst,
		Identity:     h.ident,
		Dedup:        h.dedup,
		Session:      h.binder,
		Audit:        h.audit,
		Replier:      h.replier,
		Typing:       h.typing,
		OriginType:   "lark_chat",
		QuickCreate:  qc,
	})
	return h, qc, msgs
}

// ---- inbound media ----

func mediaMessage(t *testing.T) channel.InboundMessage {
	msg := p2pMessage(t)
	msg.Type = channel.MsgTypeImage
	msg.Text = "look at this"
	msg.Segments = []channel.Segment{{Text: "look at this ", MediaIdx: -1}, {MediaIdx: 0}}
	msg.PendingMedia = []channel.PendingMedia{{Kind: channel.MsgTypeImage, Ref: "dl-1", Alt: "pdl-1"}}
	return msg
}

func newMediaHarness(t *testing.T) (*harness, *fakeMedia) {
	t.Helper()
	h := newHarness(t)
	media := &fakeMedia{staged: []StagedMedia{{Filename: "image-1.png", StorageKey: "workspaces/w/x.png"}}}
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst,
		Identity:     h.ident,
		Dedup:        h.dedup,
		Session:      h.binder,
		Audit:        h.audit,
		Replier:      h.replier,
		Typing:       h.typing,
		OriginType:   "lark_chat",
		Media:        media,
	})
	return h, media
}

func TestRouter_MediaIngestAfterIdentity(t *testing.T) {
	h, media := newMediaHarness(t)
	if err := h.router.Handle(context.Background(), mediaMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if media.ingests() != 1 {
		t.Fatalf("ingest calls = %d, want 1", media.ingests())
	}
	inst := activeResolved(t)
	if media.lastParams.WorkspaceID != inst.WorkspaceID {
		t.Fatal("ingest must carry the resolved workspace id")
	}
	if len(h.binder.lastAppend.Staged) != 1 || h.binder.lastAppend.Staged[0].Filename != "image-1.png" {
		t.Fatalf("staged media must reach AppendMessage, got %+v", h.binder.lastAppend.Staged)
	}
	if !h.binder.lastAppend.MediaChatBind {
		t.Fatal("a plain chat turn's media must be chat-bound")
	}
	if h.binder.lastAppend.WorkspaceID != inst.WorkspaceID {
		t.Fatal("AppendParams must carry the workspace id")
	}
	if !h.tasks.wasCalled() {
		t.Fatal("a media chat turn must still schedule a run")
	}
}

func TestRouter_MediaIngest_StrangerNeverFetches(t *testing.T) {
	h, media := newMediaHarness(t)
	h.ident.err = ErrSenderUnbound
	if err := h.router.Handle(context.Background(), mediaMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if media.ingests() != 0 {
		t.Fatalf("an unbound sender's media must never be fetched, got %d ingests", media.ingests())
	}
}

func TestRouter_MediaIngest_DuplicateNeverFetches(t *testing.T) {
	h, media := newMediaHarness(t)
	h.dedup.claimErr = ErrDuplicate
	if err := h.router.Handle(context.Background(), mediaMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if media.ingests() != 0 {
		t.Fatalf("a replayed message's media must never be fetched, got %d ingests", media.ingests())
	}
}

func TestRouter_MediaIngestFailure(t *testing.T) {
	h, media := newMediaHarness(t)
	media.err = errors.New("download failed")
	if err := h.router.Handle(context.Background(), mediaMessage(t)); err != nil {
		t.Fatalf("media failure must be a product drop, not an error: %v", err)
	}
	if r, _ := h.audit.last(); r != DropReasonMediaFetchFailed {
		t.Fatalf("expected media_fetch_failed, got %q", r)
	}
	if h.binder.lastAppend.SessionID.Valid {
		t.Fatal("a failed ingest must not append the turn")
	}
	if h.dedup.releases() != 1 {
		t.Fatalf("a failed ingest must Release the claim, got %d", h.dedup.releases())
	}
	if h.tasks.wasCalled() {
		t.Fatal("no run for a refused turn")
	}
}

func TestRouter_MediaWithoutSeam(t *testing.T) {
	h := newHarness(t) // default harness: no Media seam
	if err := h.router.Handle(context.Background(), mediaMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r, _ := h.audit.last(); r != DropReasonMediaUnsupported {
		t.Fatalf("expected media_unsupported for a seamless channel, got %q", r)
	}
	if h.binder.lastAppend.SessionID.Valid {
		t.Fatal("must not append when media is unsupported")
	}
	// Permanent capability gap: Mark (not Release) so a redelivery does not
	// re-run the pipeline to refuse again.
	if h.dedup.marks() != 1 {
		t.Fatalf("nil-seam media drop must finalize Mark, got %d", h.dedup.marks())
	}
}

func TestRouter_AppendFailureDiscardsStaged(t *testing.T) {
	h, media := newMediaHarness(t)
	h.binder.appendErr = errors.New("db down")
	if err := h.router.Handle(context.Background(), mediaMessage(t)); err == nil {
		t.Fatal("append infra error must surface")
	}
	if media.discardCalls() != 1 {
		t.Fatalf("staged objects must be discarded when the append fails, got %d", media.discardCalls())
	}
}

// registerSet re-registers the Feishu-typed resolver set with overrides applied,
// so a test can flip one capability (e.g. RefuseUnsupportedKinds) without
// rebuilding every fake.
func (h *harness) registerSet(mutate func(*ResolverSet)) {
	set := ResolverSet{
		Installation: h.inst,
		Identity:     h.ident,
		Dedup:        h.dedup,
		Session:      h.binder,
		Audit:        h.audit,
		Replier:      h.replier,
		Typing:       h.typing,
		OriginType:   "lark_chat",
	}
	mutate(&set)
	h.router.Register(channel.TypeFeishu, set)
}

func TestRouter_UnsupportedKindNotice(t *testing.T) {
	h := newHarness(t)
	h.registerSet(func(s *ResolverSet) { s.RefuseUnsupportedKinds = true })
	for _, kind := range []channel.MsgType{channel.MsgTypeAudio, channel.MsgTypeVideo, channel.MsgTypeFile, channel.MsgTypeUnknown} {
		t.Run(string(kind), func(t *testing.T) {
			h.audit.drops = nil
			h.binder.lastEnsure = EnsureSessionParams{}
			msg := p2pMessage(t)
			msg.Type = kind
			msg.Text = ""
			if err := h.router.Handle(context.Background(), msg); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if r, _ := h.audit.last(); r != DropReasonUnsupportedKind {
				t.Fatalf("expected unsupported_message_kind, got %q", r)
			}
			if h.binder.lastEnsure.Installation.ID.Valid {
				t.Fatal("unsupported kinds must not create or touch a session")
			}
		})
	}
}

// A channel that has NOT opted into RefuseUnsupportedKinds (Feishu/Slack) keeps
// ingesting those kinds unchanged — enabling the DingTalk gate must never
// silently drop another channel's audio/file/video turns.
func TestRouter_UnsupportedKind_IngestedWhenOptOut(t *testing.T) {
	h := newHarness(t) // default set: RefuseUnsupportedKinds is false
	msg := p2pMessage(t)
	msg.Type = channel.MsgTypeAudio
	msg.Text = ""
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r, ok := h.audit.last(); ok && r == DropReasonUnsupportedKind {
		t.Fatal("an opt-out channel must not refuse unsupported kinds")
	}
	if !h.binder.lastAppend.SessionID.Valid {
		t.Fatal("an opt-out channel must still append the turn")
	}
	if !h.tasks.wasCalled() {
		t.Fatal("an opt-out channel must still schedule a run")
	}
}

func TestRouter_QuickCreate_EnqueuesAndAcks(t *testing.T) {
	h, qc, msgs := newQuickCreateHarness(t)
	// The prompt is derived from the turn's OWN text, not the binder's parsed
	// command, so drive it from the message body.
	h.binder.appendResult = AppendResult{
		DedupMarked:  true,
		IssueCommand: &IssueCommand{Title: "fix login", Description: "steps to reproduce"},
	}
	msg := p2pMessage(t)
	msg.Text = "/issue fix login\nsteps to reproduce"

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("handle: %v", err)
	}
	h.router.Drain()

	if !qc.wasCalled() {
		t.Fatal("quick-create enqueuer not called")
	}
	if qc.prompt != "fix login\nsteps to reproduce" {
		t.Fatalf("prompt = %q", qc.prompt)
	}
	inst := activeResolved(t)
	if qc.ws != inst.WorkspaceID || qc.agent != inst.AgentID {
		t.Fatal("workspace/agent mismatch")
	}
	if qc.session != h.binder.ensureID {
		t.Fatal("chat session mismatch")
	}
	if h.issues.called {
		t.Fatal("direct-create path must not run when QuickCreate seam is set")
	}
	if h.tasks.wasCalled() {
		t.Fatal("issue-command turn must not schedule a chat run")
	}
	rows := msgs.all()
	if len(rows) != 1 || rows[0].Role != "assistant" || rows[0].Content != IssueQueuedAckText {
		t.Fatalf("transcript ack rows = %+v", rows)
	}
	if rows[0].ChatSessionID != h.binder.ensureID {
		t.Fatal("ack appended to wrong session")
	}
	replies := h.replier.calls()
	if len(replies) != 1 || !replies[0].IssueQueued {
		t.Fatalf("replier results = %+v", replies)
	}
	if h.typing.calls() != 0 {
		t.Fatal("typing ack must not fire for issue-command turns")
	}
}

// A bare "/issue" with no content of its own must NOT create an issue and must
// NOT adopt a previous turn — it asks the user what to file (regression guard
// for the previous-message fallback dragging an earlier image into a new issue).
func TestRouter_QuickCreate_EmptyPrompt_Usage(t *testing.T) {
	h, qc, msgs := newQuickCreateHarness(t)
	h.binder.appendResult = AppendResult{
		DedupMarked:  true,
		IssueCommand: &IssueCommand{},
	}
	msg := p2pMessage(t)
	msg.Text = "/issue"

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("handle: %v", err)
	}
	h.router.Drain()

	if qc.wasCalled() {
		t.Fatal("must not enqueue on empty prompt")
	}
	rows := msgs.all()
	if len(rows) != 1 || rows[0].Role != "assistant" || rows[0].Content != IssueUsageText {
		t.Fatalf("usage note rows = %+v; the transcript must mirror the usage reply", rows)
	}
	if h.tasks.wasCalled() {
		t.Fatal("no chat run expected")
	}
	replies := h.replier.calls()
	if len(replies) != 1 || !replies[0].IssueUsage {
		t.Fatalf("replier results = %+v", replies)
	}
}

// A "/new /issue x" turn rotates the session at EnsureSession (ForceFresh) and
// the quick-create must bind to the rotated session — regression guard for the
// combination the deleted pre-engine divert test used to cover.
func TestRouter_QuickCreate_ForceFreshBindsRotatedSession(t *testing.T) {
	h, qc, msgs := newQuickCreateHarness(t)
	h.binder.appendResult = AppendResult{
		DedupMarked:  true,
		IssueCommand: &IssueCommand{Title: "login broken"},
	}
	msg := p2pMessage(t)
	msg.Text = "/issue login broken"
	msg.ForceFresh = true

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("handle: %v", err)
	}
	h.router.Drain()

	if !h.binder.lastEnsure.Message.ForceFresh {
		t.Fatal("EnsureSession must see ForceFresh so it rotates the session")
	}
	if !qc.wasCalled() {
		t.Fatal("quick-create enqueuer not called")
	}
	if qc.session != h.binder.ensureID {
		t.Fatal("quick-create must bind to the session EnsureSession returned")
	}
	if h.tasks.wasCalled() {
		t.Fatal("issue-command turn must not schedule a chat run")
	}
	rows := msgs.all()
	if len(rows) != 1 || rows[0].Content != IssueQueuedAckText || rows[0].ChatSessionID != h.binder.ensureID {
		t.Fatalf("ack rows = %+v", rows)
	}
}

// An /issue turn with images: the attachments must NOT be chat-bound (they
// ride the quick-create task onto the issue) and their ids must reach the
// enqueuer.
func TestRouter_IssueTurnMediaNotChatBound(t *testing.T) {
	h, qc, _ := newQuickCreateHarness(t)
	media := &fakeMedia{staged: []StagedMedia{{Filename: "image-1.png", StorageKey: "workspaces/w/x.png"}}}
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst,
		Identity:     h.ident,
		Dedup:        h.dedup,
		Session:      h.binder,
		Audit:        h.audit,
		Replier:      h.replier,
		Typing:       h.typing,
		OriginType:   "lark_chat",
		QuickCreate:  qc,
		Media:        media,
	})
	attID := uuidFromString(t, "88888888-8888-8888-8888-888888888888")
	h.binder.appendResult = AppendResult{
		DedupMarked:   true,
		IssueCommand:  &IssueCommand{Title: "broken layout [image: image-1.png]"},
		AttachmentIDs: []pgtype.UUID{attID},
	}
	msg := mediaMessage(t)
	msg.Text = "/issue broken layout"
	msg.Segments = []channel.Segment{{Text: "/issue broken layout ", MediaIdx: -1}, {MediaIdx: 0}}

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("handle: %v", err)
	}
	h.router.Drain()

	if h.binder.lastAppend.MediaChatBind {
		t.Fatal("an /issue turn's media must not be chat-bound")
	}
	if !qc.wasCalled() {
		t.Fatal("quick-create enqueuer not called")
	}
	if len(qc.attachments) != 1 || qc.attachments[0] != attID {
		t.Fatalf("enqueue attachments = %v, want [%v]", qc.attachments, attID)
	}
}

func TestRouter_QuickCreate_EnqueueError_FailureNote(t *testing.T) {
	h, qc, msgs := newQuickCreateHarness(t)
	qc.err = errors.New("boom")
	h.binder.appendResult = AppendResult{
		DedupMarked:  true,
		IssueCommand: &IssueCommand{Title: "fix login"},
	}

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	h.router.Drain()

	rows := msgs.all()
	if len(rows) != 1 || rows[0].Content != IssueQueueFailedText {
		t.Fatalf("transcript rows = %+v", rows)
	}
	if h.tasks.wasCalled() {
		t.Fatal("no chat run expected")
	}
	replies := h.replier.calls()
	if len(replies) != 1 || !replies[0].IssueQueueFailed {
		t.Fatalf("replier results = %+v", replies)
	}
}

// ---- unreadable media (adapter could not download; e.g. over-quota) ----

func TestRouter_MediaUnreadable_BoundMember_RefusesWithFeedback(t *testing.T) {
	h, media := newMediaHarness(t)
	msg := p2pMessage(t)
	msg.Type = channel.MsgTypeImage
	msg.Text = ""
	msg.MediaUnreadable = true

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("unreadable media must be a product drop, not an error: %v", err)
	}
	h.router.Drain()
	if r, _ := h.audit.last(); r != DropReasonMediaFetchFailed {
		t.Fatalf("expected media_fetch_failed, got %q", r)
	}
	if media.ingests() != 0 {
		t.Fatal("there is nothing downloadable — the ingester must never run")
	}
	if h.binder.lastEnsure.Installation.ID.Valid {
		t.Fatal("unreadable media must not create or touch a session")
	}
	if h.tasks.wasCalled() {
		t.Fatal("no run for a refused turn")
	}
	if h.dedup.marks() != 1 {
		t.Fatalf("unreadable-media drop must finalize Mark, got %d", h.dedup.marks())
	}
	replies := h.replier.calls()
	if len(replies) != 1 || replies[0].DropReason != DropReasonMediaFetchFailed {
		t.Fatalf("the sender must get a media-failed reply, got %+v", replies)
	}
}

// An unbound sender's unreadable media is decided by the identity gate FIRST
// (binding prompt), never the media-failed refusal — the MediaUnreadable check
// sits after identity precisely so the two do not collide.
func TestRouter_MediaUnreadable_UnboundSender_NeedsBinding(t *testing.T) {
	h, _ := newMediaHarness(t)
	h.ident.err = ErrSenderUnbound
	msg := p2pMessage(t)
	msg.Type = channel.MsgTypeImage
	msg.MediaUnreadable = true

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r, _ := h.audit.last(); r != DropReasonUnboundUser {
		t.Fatalf("an unbound sender must get the binding path, got %q", r)
	}
}

// ---- /issue media cleanup when no issue is created (#5 enqueue fail, #6 empty prompt) ----

type fakeAttachments struct {
	mu      sync.Mutex
	calls   int
	lastWS  pgtype.UUID
	lastIDs []pgtype.UUID
}

func (f *fakeAttachments) DiscardAttachments(_ context.Context, ws pgtype.UUID, ids []pgtype.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastWS = ws
	f.lastIDs = ids
}
func (f *fakeAttachments) discardCalls() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

func newIssueMediaCleanupHarness(t *testing.T) (*harness, *fakeQuickCreate, *fakeMedia, *fakeAttachments) {
	t.Helper()
	h := newHarness(t)
	qc := &fakeQuickCreate{}
	media := &fakeMedia{staged: []StagedMedia{{Filename: "image-1.png", StorageKey: "workspaces/w/x.png"}}}
	att := &fakeAttachments{}
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{Logger: discardLogger(), Messages: &fakeMessages{}})
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst,
		Identity:     h.ident,
		Dedup:        h.dedup,
		Session:      h.binder,
		Audit:        h.audit,
		Replier:      h.replier,
		Typing:       h.typing,
		OriginType:   "lark_chat",
		QuickCreate:  qc,
		Media:        media,
		Attachments:  att,
	})
	return h, qc, media, att
}

// issueMediaMessage is an /issue turn carrying one image.
func issueMediaMessage(t *testing.T) channel.InboundMessage {
	msg := mediaMessage(t)
	msg.Text = "/issue broken layout"
	msg.Segments = []channel.Segment{{Text: "/issue broken layout ", MediaIdx: -1}, {MediaIdx: 0}}
	return msg
}

// An /issue turn carrying ONLY an image (no text of its own) must still create
// an issue: the image markdown composes a non-empty prompt, so the media is
// filed onto the issue instead of being discarded (regression guard for the
// bare-/issue-with-image path that used to drop the image).
func TestRouter_QuickCreate_MediaOnlyIssue_CreatesWithImage(t *testing.T) {
	h, qc, media, att := newIssueMediaCleanupHarness(t)
	media.staged = []StagedMedia{{Filename: "image-1.png", StorageKey: "workspaces/w/x.png", URL: "https://cdn/x.png"}}
	attID := uuidFromString(t, "88888888-8888-8888-8888-888888888888")
	h.binder.appendResult = AppendResult{
		DedupMarked:   true,
		IssueCommand:  &IssueCommand{}, // no typed title/desc — the image IS the content
		AttachmentIDs: []pgtype.UUID{attID},
	}
	msg := mediaMessage(t)
	msg.Text = "/issue"
	msg.Segments = []channel.Segment{{Text: "/issue", MediaIdx: -1}, {MediaIdx: 0}}

	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("handle: %v", err)
	}
	h.router.Drain()

	if !qc.wasCalled() {
		t.Fatal("an /issue turn carrying an image must create an issue, not discard it")
	}
	if qc.prompt != "![image-1.png](https://cdn/x.png)" {
		t.Fatalf("prompt must keep the image markdown inline, got %q", qc.prompt)
	}
	if len(qc.attachments) != 1 || qc.attachments[0] != attID {
		t.Fatalf("enqueue attachments = %v, want [%v]", qc.attachments, attID)
	}
	if media.discardCalls() != 0 || att.discardCalls() != 0 {
		t.Fatalf("media must ride the issue, not be discarded: storage=%d rows=%d", media.discardCalls(), att.discardCalls())
	}
}

// On an enqueue failure the durable transcript body already embeds the image
// markdown, so the staged storage OBJECTS must be kept (deleting them would
// leave a broken image inline); only the chat-unbound attachment ROWS — which
// would otherwise dangle bound to neither a chat nor an issue — are dropped.
func TestRouter_QuickCreate_EnqueueError_DropsRowsKeepsObjects(t *testing.T) {
	h, qc, media, att := newIssueMediaCleanupHarness(t)
	qc.err = errors.New("queue down")
	attID := uuidFromString(t, "88888888-8888-8888-8888-888888888888")
	h.binder.appendResult = AppendResult{
		DedupMarked:   true,
		IssueCommand:  &IssueCommand{Title: "broken layout"},
		AttachmentIDs: []pgtype.UUID{attID},
	}

	if err := h.router.Handle(context.Background(), issueMediaMessage(t)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	h.router.Drain()

	if !qc.wasCalled() {
		t.Fatal("enqueue was attempted")
	}
	if media.discardCalls() != 0 {
		t.Fatalf("staged storage objects must be KEPT (transcript references them), got %d discards", media.discardCalls())
	}
	if att.discardCalls() != 1 {
		t.Fatalf("chat-unbound attachment rows must be deleted on enqueue failure, got %d", att.discardCalls())
	}
	if len(att.lastIDs) != 1 || att.lastIDs[0] != attID || att.lastWS != activeResolved(t).WorkspaceID {
		t.Fatalf("discard targeted wrong rows/workspace: ids=%v ws=%v", att.lastIDs, att.lastWS)
	}
}

// A successful /issue turn must NOT discard its media — the quick-create task
// carries it onto the issue.
func TestRouter_QuickCreate_Success_KeepsMedia(t *testing.T) {
	h, qc, media, att := newIssueMediaCleanupHarness(t)
	h.binder.appendResult = AppendResult{
		DedupMarked:   true,
		IssueCommand:  &IssueCommand{Title: "broken layout"},
		AttachmentIDs: []pgtype.UUID{uuidFromString(t, "88888888-8888-8888-8888-888888888888")},
	}

	if err := h.router.Handle(context.Background(), issueMediaMessage(t)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	h.router.Drain()

	if !qc.wasCalled() {
		t.Fatal("quick-create must be enqueued")
	}
	if media.discardCalls() != 0 || att.discardCalls() != 0 {
		t.Fatalf("a successful issue turn must keep its media: storage=%d rows=%d", media.discardCalls(), att.discardCalls())
	}
}
