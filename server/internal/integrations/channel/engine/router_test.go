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
	mu           sync.Mutex
	ensureID     pgtype.UUID
	ensureErr    error
	appendResult AppendResult
	appendErr    error
	appendDelay  time.Duration
	bindErr      error
	lastEnsure   EnsureSessionParams
	lastAppend   AppendParams
	lastBind     BindMediaParams
}

func (f *fakeBinder) EnsureSession(_ context.Context, p EnsureSessionParams) (pgtype.UUID, error) {
	f.lastEnsure = p
	return f.ensureID, f.ensureErr
}
func (f *fakeBinder) AppendMessage(_ context.Context, p AppendParams) (AppendResult, error) {
	f.mu.Lock()
	delay := f.appendDelay
	f.lastAppend = p
	res, err := f.appendResult, f.appendErr
	f.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	return res, err
}
func (f *fakeBinder) BindMedia(_ context.Context, p BindMediaParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastBind = p
	return f.bindErr
}
func (f *fakeBinder) boundMedia() BindMediaParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastBind
}
func (f *fakeBinder) appendedParams() AppendParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastAppend
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

type fakeMedia struct {
	mu            sync.Mutex
	count         int
	noMedia       bool
	waitForCancel bool
	started       chan struct{}
	release       <-chan struct{}
	resolve       func(context.Context, channel.InboundMessage) channel.InboundMessage
	lastMessageID pgtype.UUID
}

func (f *fakeMedia) HasMedia(_ channel.InboundMessage) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.noMedia
}

func (f *fakeMedia) resolvedMessageID() pgtype.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastMessageID
}

func (f *fakeMedia) ResolveMedia(ctx context.Context, _ ResolvedInstallation, _ ResolvedIdentity, _ pgtype.UUID, chatMessageID pgtype.UUID, msg channel.InboundMessage) channel.InboundMessage {
	f.mu.Lock()
	f.count++
	f.lastMessageID = chatMessageID
	waitForCancel := f.waitForCancel
	started := f.started
	release := f.release
	resolve := f.resolve
	f.mu.Unlock()
	if resolve != nil {
		return resolve(ctx, msg)
	}
	if started != nil {
		close(started)
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return msg
		}
	}
	if waitForCancel {
		<-ctx.Done()
		return msg
	}
	msg.MediaRefs = append(msg.MediaRefs, channel.MediaRef{
		Type:       channel.MsgTypeImage,
		StorageKey: "workspaces/ws/lark/image",
		StorageURL: "https://cdn.example.test/image",
		Filename:   "image.png",
		MimeType:   "image/png",
		SizeBytes:  3,
	})
	return msg
}

func (f *fakeMedia) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count
}

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
	callCount  int
	promotions int
	forceFresh bool
	initiator  pgtype.UUID
	err        error
}

func (f *fakeTasks) PromoteChannelChatTasksIfMediaReady(_ context.Context, _ pgtype.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.promotions++
	return nil
}

func (f *fakeTasks) EnqueueChatTask(_ context.Context, _ db.ChatSession, initiator pgtype.UUID, forceFresh bool) (db.AgentTaskQueue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.callCount++
	f.forceFresh = forceFresh
	f.initiator = initiator
	return db.AgentTaskQueue{}, f.err
}
func (f *fakeTasks) wasCalled() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.called }
func (f *fakeTasks) freshArg() bool  { f.mu.Lock(); defer f.mu.Unlock(); return f.forceFresh }
func (f *fakeTasks) calls() int      { f.mu.Lock(); defer f.mu.Unlock(); return f.callCount }
func (f *fakeTasks) promotionCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.promotions
}
func (f *fakeTasks) initiatorArg() pgtype.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.initiator
}

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
	media   *fakeMedia
	issues  *fakeIssues
	tasks   *fakeTasks
	reader  *fakeReader
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		inst:  &fakeInstaller{inst: activeResolved(t)},
		ident: &fakeIdentity{id: ResolvedIdentity{UserID: uuidFromString(t, "44444444-4444-4444-4444-444444444444")}},
		dedup: &fakeDedup{token: uuidFromString(t, "55555555-5555-5555-5555-555555555555")},
		binder: &fakeBinder{
			ensureID: uuidFromString(t, "66666666-6666-6666-6666-666666666666"),
			appendResult: AppendResult{
				MessageID:   uuidFromString(t, "99999999-9999-4999-8999-999999999999"),
				DedupMarked: true,
			},
		},
		audit:   &fakeAuditor{},
		replier: &fakeReplier{},
		typing:  &fakeTyping{},
		media:   &fakeMedia{},
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
		Media:        h.media,
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
	if h.media.calls() != 0 {
		t.Fatal("revoked installation must not resolve media")
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
	if h.media.calls() != 0 {
		t.Fatal("duplicate message must not resolve media")
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
	if h.media.calls() != 0 {
		t.Fatal("unaddressed group message must not resolve media")
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
	if h.media.calls() != 0 {
		t.Fatal("unbound sender must not resolve media")
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
	if h.media.calls() != 0 {
		t.Fatal("non-member sender must not resolve media")
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

func TestRouter_AppendErrorReleasesClaimAndAllowsMediaRetry(t *testing.T) {
	h := newHarness(t)
	h.binder.appendErr = errors.New("db down")
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err == nil {
		t.Fatal("append error must surface to the caller")
	}
	if h.dedup.releases() != 1 || h.media.calls() != 0 {
		t.Fatalf("failed attempt: releases=%d media_calls=%d, want release=1 media_calls=0", h.dedup.releases(), h.media.calls())
	}

	h.binder.appendErr = nil
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !waitFor(time.Second, func() bool { return h.media.calls() == 1 }) {
		t.Fatalf("retry media calls = %d, want 1 total", h.media.calls())
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
	if !waitFor(time.Second, h.tasks.wasCalled) {
		t.Fatalf("ingest must trigger a chat run (inline, no batcher)")
	}
	if !waitFor(time.Second, func() bool { return h.typing.calls() == 1 }) {
		t.Fatalf("ingest must show the typing indicator")
	}
	if h.media.calls() != 1 {
		t.Fatalf("ingested message resolved media %d times, want 1", h.media.calls())
	}
	if refs := h.binder.boundMedia().MediaRefs; len(refs) != 1 {
		t.Fatalf("resolved media not bound after append: %+v", refs)
	}
}

func TestRouter_NoMediaMessageSkipsMediaPipeline(t *testing.T) {
	h := newHarness(t)
	h.media.noMedia = true

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !waitFor(time.Second, h.tasks.wasCalled) {
		t.Fatal("no-media message must still trigger a chat run")
	}
	if got := h.binder.appendedParams().MediaPendingSeconds; got != 0 {
		t.Fatalf("no-media message persisted a media budget: %v", got)
	}
	if h.media.calls() != 0 {
		t.Fatalf("no-media message ran ResolveMedia %d times, want 0", h.media.calls())
	}
	if h.tasks.promotionCalls() != 0 {
		t.Fatalf("no-media message triggered %d promotions, want 0", h.tasks.promotionCalls())
	}
}

func TestRouter_MediaResolverTimeoutAppendsOriginalMessage(t *testing.T) {
	h := newHarness(t)
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{MediaTimeout: 10 * time.Millisecond, Logger: discardLogger()})
	h.media.waitForCancel = true
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

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !waitFor(time.Second, func() bool { return h.media.calls() == 1 }) {
		t.Fatalf("media resolver calls = %d, want 1", h.media.calls())
	}
	if refs := h.binder.boundMedia().MediaRefs; len(refs) != 0 {
		t.Fatalf("timed-out media refs must not attach: %+v", refs)
	}
	if !waitFor(time.Second, h.tasks.wasCalled) {
		t.Fatalf("message should still be ingested and trigger a chat run")
	}
	if h.dedup.releases() != 0 {
		t.Fatalf("media timeout must not release a durably appended message, got %d", h.dedup.releases())
	}
}

func TestRouter_MediaBindFailureStillChecksPlaceholderPromotion(t *testing.T) {
	h := newHarness(t)
	h.binder.bindErr = errors.New("attachment write failed")

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !waitFor(time.Second, func() bool { return h.tasks.promotionCalls() == 1 }) {
		t.Fatal("binding failure did not check whether the cleared placeholder task could be promoted")
	}
	// No inline deletion on bind failure: the attachments may or may not
	// have landed (ambiguous commit), and the intent ledger — cleared inside
	// the same transaction — already reflects whichever outcome is durable.
	// The reconciler settles the objects.
}

func TestRouter_MediaDeadlineDropsRefsWithoutBinding(t *testing.T) {
	h := newHarness(t)
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{MediaTimeout: 10 * time.Millisecond, Logger: discardLogger()})
	// A rich post where an early upload succeeded before the deadline killed
	// the rest of the resolution: the resolver returns the partial refs. The
	// router must not bind them — and must not delete anything inline: the
	// intent-ledger rows written before the uploads are the reclaim path.
	h.media.resolve = func(ctx context.Context, msg channel.InboundMessage) channel.InboundMessage {
		msg.MediaRefs = append(msg.MediaRefs, channel.MediaRef{
			Type:       channel.MsgTypeImage,
			StorageKey: "workspaces/ws/lark/uploaded-before-deadline",
		})
		<-ctx.Done()
		return msg
	}
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

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !waitFor(time.Second, func() bool { return h.tasks.promotionCalls() == 1 }) {
		t.Fatal("deadline expiry must still clear the marker and check promotion")
	}
	if refs := h.binder.boundMedia().MediaRefs; len(refs) != 0 {
		t.Fatalf("timed-out refs must not bind: %+v", refs)
	}
}

func TestRouter_MediaResolutionDoesNotBlockInboundHandle(t *testing.T) {
	h := newHarness(t)
	h.reader.session = db.ChatSession{}
	started := make(chan struct{})
	release := make(chan struct{})
	h.media.started = started
	h.media.release = release
	msg := p2pMessage(t)

	handled := make(chan error, 1)
	go func() {
		handled <- h.router.Handle(context.Background(), msg)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("media resolver did not start")
	}
	select {
	case err := <-handled:
		if err != nil {
			t.Fatalf("Handle: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Handle waited for media resolution on the connector ACK path")
	}
	if !h.tasks.wasCalled() {
		t.Fatal("durable run trigger was not scheduled while media resolution was pending")
	}

	close(release)
	if !waitFor(time.Second, func() bool { return h.tasks.promotionCalls() == 1 }) {
		t.Fatal("media completion did not promote the durable deferred run")
	}
}

func TestRouter_MediaQueuePreservesSessionOrderWithoutCancellingRunBoundary(t *testing.T) {
	h := newHarness(t)
	timers := &fakeTimerFactory{}
	h.router.batcher = newTestBatcher(timers)
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	h.media.resolve = func(ctx context.Context, msg channel.InboundMessage) channel.InboundMessage {
		if msg.MessageID == "m2" {
			close(secondStarted)
			select {
			case <-releaseSecond:
			case <-ctx.Done():
			}
		}
		return msg
	}

	first := p2pMessage(t)
	first.MessageID = "m1"
	if err := h.router.Handle(context.Background(), first); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	if got := h.router.batcher.pendingCount(); got != 1 {
		t.Fatalf("first message did not arm a run, pending=%d", got)
	}

	second := p2pMessage(t)
	second.MessageID = "m2"
	if err := h.router.Handle(context.Background(), second); err != nil {
		t.Fatalf("second Handle: %v", err)
	}
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second media job did not start")
	}
	if got := h.router.batcher.pendingCount(); got != 1 {
		t.Fatalf("new media must keep one fenced run boundary, pending=%d", got)
	}
	timers.fireArmed()
	if !waitFor(time.Second, func() bool { return h.tasks.calls() == 1 }) {
		t.Fatal("durable run trigger did not flush while media was pending")
	}

	close(releaseSecond)
	if !waitFor(time.Second, func() bool { return h.tasks.promotionCalls() == 2 }) {
		t.Fatalf("media completion promotions = %d, want 2", h.tasks.promotionCalls())
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
	if !waitFor(time.Second, h.tasks.wasCalled) || h.tasks.initiatorArg() != h.ident.id.UserID {
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
	if !waitFor(time.Second, func() bool {
		for _, r := range h.replier.calls() {
			if r.Outcome == OutcomeAgentOffline {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("agent-no-runtime must emit an AgentOffline reply")
	}
	// The reaction was added on ingest but no task will run, so the bus-driven
	// clear never fires — the flush must clear the typing indicator itself.
	if !waitFor(time.Second, func() bool { return h.typing.settledCalls() == 1 }) {
		t.Fatalf("offline flush must clear the typing indicator, got %d OnSettled calls", h.typing.settledCalls())
	}
}

func TestRouter_FlushArchived_ClearsTyping(t *testing.T) {
	h := newHarness(t)
	h.tasks.err = service.ErrChatTaskAgentArchived
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !waitFor(time.Second, func() bool { return h.typing.settledCalls() == 1 }) {
		t.Fatalf("archived flush must clear the typing indicator, got %d OnSettled calls", h.typing.settledCalls())
	}
}

func TestRouter_FlushSuccess_DoesNotClearTyping(t *testing.T) {
	h := newHarness(t)
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !waitFor(time.Second, h.tasks.wasCalled) {
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
	if !waitFor(time.Second, h.tasks.wasCalled) || !h.tasks.freshArg() {
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
	go func() { h.router.Drain(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Drain did not join the reply goroutine")
	}
	if len(h.replier.calls()) != 1 {
		t.Fatalf("expected exactly one reply after drain, got %d", len(h.replier.calls()))
	}
}

func TestRouter_MediaConcurrencyCapAppliesAcrossSessions(t *testing.T) {
	h := newHarness(t)
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{MediaConcurrency: 1, Logger: discardLogger()})
	release := make(chan struct{})
	h.media.resolve = func(ctx context.Context, msg channel.InboundMessage) channel.InboundMessage {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return msg
	}
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

	first := p2pMessage(t)
	first.MessageID = "m1"
	if err := h.router.Handle(context.Background(), first); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	if !waitFor(time.Second, func() bool { return h.media.calls() == 1 }) {
		t.Fatalf("first media job did not start, calls=%d", h.media.calls())
	}

	// A different session gets its own queue; only the global cap gates it.
	h.binder.ensureID = uuidFromString(t, "77777777-7777-4777-8777-777777777777")
	second := p2pMessage(t)
	second.MessageID = "m2"
	second.Source.ChatID = "oc_chat_b"
	if err := h.router.Handle(context.Background(), second); err != nil {
		t.Fatalf("second Handle: %v", err)
	}
	if waitFor(150*time.Millisecond, func() bool { return h.media.calls() == 2 }) {
		t.Fatal("second media job ran while the only concurrency slot was held")
	}

	close(release)
	if !waitFor(time.Second, func() bool { return h.media.calls() == 2 }) {
		t.Fatalf("second media job never ran after the slot freed, calls=%d", h.media.calls())
	}
}

func TestRouter_DrainHonorsDeadlineWhenMediaResolverIgnoresCancellation(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	started := make(chan struct{})
	h.media.resolve = func(_ context.Context, msg channel.InboundMessage) channel.InboundMessage {
		close(started)
		<-release
		return msg
	}

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("media resolver did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if h.router.Drain(ctx) {
		t.Fatal("Drain reported completion for a wedged media resolver")
	}
	close(release)
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
	if !waitFor(time.Second, h.tasks.wasCalled) {
		t.Fatalf("message must still ingest without a dedup key")
	}
}

// A media job whose budget expires while it is still QUEUED (waiting for the
// global slot) must not wait for the front of the line: it finalizes the
// placeholder (marker clear + promotion) immediately, without ever invoking
// the resolver, while the slot holder keeps running.
func TestRouter_MediaQueueWaitExpiryFinalizesWithoutResolving(t *testing.T) {
	h := newHarness(t)
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{MediaConcurrency: 1, MediaTimeout: 150 * time.Millisecond, Logger: discardLogger()})
	release := make(chan struct{})
	firstStarted := make(chan struct{})
	h.media.resolve = func(_ context.Context, msg channel.InboundMessage) channel.InboundMessage {
		if msg.MessageID == "m1" {
			close(firstStarted)
			// Deliberately ignores ctx: the slot must stay held past m2's
			// expiry so m2 deterministically expires while QUEUED.
			<-release
		}
		return msg
	}
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

	first := p2pMessage(t)
	first.MessageID = "m1"
	if err := h.router.Handle(context.Background(), first); err != nil {
		t.Fatalf("first Handle: %v", err)
	}
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first media job did not start")
	}

	// Second job (another session) queues behind the only slot and expires
	// there; it must finalize while the slot is still held.
	h.binder.ensureID = uuidFromString(t, "77777777-7777-4777-8777-777777777777")
	second := p2pMessage(t)
	second.MessageID = "m2"
	second.Source.ChatID = "oc_chat_b"
	if err := h.router.Handle(context.Background(), second); err != nil {
		t.Fatalf("second Handle: %v", err)
	}
	if !waitFor(2*time.Second, func() bool { return h.tasks.promotionCalls() >= 1 }) {
		t.Fatal("expired queued job did not finalize while the slot was held")
	}
	if h.media.calls() != 1 {
		t.Fatalf("expired job must not invoke the resolver, calls=%d", h.media.calls())
	}
	close(release)
	if !waitFor(2*time.Second, func() bool { return h.tasks.promotionCalls() == 2 }) {
		t.Fatalf("slot holder promotion missing, promotions=%d", h.tasks.promotionCalls())
	}
}

// The local resolve budget must start BEFORE the append transaction: the DB
// anchors the durable fallback at insert-time now(), so a budget started
// post-commit would outlive the fallback by the append latency and let the
// task fire while the resolver still runs. With a slow append, the resolver's
// context deadline must still be measured from the pre-append instant.
func TestRouter_MediaDeadlineStartsBeforeAppend(t *testing.T) {
	h := newHarness(t)
	const timeout = 300 * time.Millisecond
	const appendLatency = 150 * time.Millisecond
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{MediaTimeout: timeout, Logger: discardLogger()})
	h.binder.appendDelay = appendLatency
	deadlines := make(chan time.Time, 1)
	h.media.resolve = func(ctx context.Context, msg channel.InboundMessage) channel.InboundMessage {
		if d, ok := ctx.Deadline(); ok {
			deadlines <- d
		}
		return msg
	}
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

	start := time.Now()
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	select {
	case d := <-deadlines:
		// Started pre-append: deadline ≈ start+timeout. Started post-append it
		// would be ≥ start+appendLatency+timeout; the midpoint separates them.
		if limit := start.Add(timeout + appendLatency/2); d.After(limit) {
			t.Fatalf("resolve deadline %v exceeds %v — local budget started after the append", d.Sub(start), timeout+appendLatency/2)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resolver did not run")
	}
}
