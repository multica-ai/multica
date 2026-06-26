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
	mu    sync.Mutex
	count int
}

func (f *fakeTyping) OnIngested(_ context.Context, _ ResolvedInstallation, _ channel.InboundMessage, _ pgtype.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
}
func (f *fakeTyping) calls() int { f.mu.Lock(); defer f.mu.Unlock(); return f.count }

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
