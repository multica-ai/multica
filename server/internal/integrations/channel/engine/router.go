package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
)

// Router is the channel-agnostic inbound pipeline — the generalization of the
// Feishu-only lark.Dispatcher (+ the Hub's handleEvent outbound seam). It is
// the single shared channel.InboundHandler the Supervisor injects into every
// Channel: a Channel translates its platform payload into a
// channel.InboundMessage and calls Handle, which routes by ChannelType to that
// platform's registered resolver set and runs the same ordered pipeline for
// every platform — installation route → two-phase dedup → group @bot filter →
// identity + membership → ensure session → append+mark → /issue → durable
// debounced run trigger + detached media binding — then drives the detached
// outbound replier + typing indicator.
//
// The core contains no platform specifics: everything platform-shaped lives
// behind the resolver interfaces (a feishu ResolverSet is the first
// implementation). Adding a platform is "register a ResolverSet", not "edit
// the Router".
type Router struct {
	mu   sync.RWMutex
	sets map[channel.Type]ResolverSet

	issues IssueCreator
	tasks  TaskEnqueuer
	reader SessionReader

	batcher *pendingBatcher

	replyTimeout time.Duration
	mediaTimeout time.Duration
	mediaCtx     context.Context
	mediaCancel  context.CancelFunc
	mediaSem     chan struct{}
	replyWg      sync.WaitGroup
	mediaWg      sync.WaitGroup

	mediaQueueMu sync.Mutex
	mediaQueues  map[string]*mediaQueueEntry
	stopping     bool

	logger *slog.Logger

	pendingFreshMu sync.Mutex
	pendingFresh   map[string]bool
}

// Config tunes the Router. Zero values default.
type RouterConfig struct {
	// ReplyTimeout caps a single detached OutboundReplier.Reply / typing
	// call. It runs off the connector ACK path, so it must stay strictly
	// under the platform ACK deadline (Lark: 3s). Defaults to 2.5s.
	ReplyTimeout time.Duration
	// MediaTimeout caps detached best-effort media download, upload, and
	// attachment binding for one message. The budget starts at append time
	// (it must match the persisted fire_at fallback), so it also spans any
	// wait behind earlier media in the same session and for a global
	// concurrency slot. Defaults to 45s.
	MediaTimeout time.Duration
	// MediaConcurrency caps concurrent media resolutions across all
	// sessions, bounding burst memory (unknown-length uploads buffer up to
	// the 100 MiB resource cap each) and platform download pressure.
	// Per-session ordering is unaffected. Defaults to 8.
	MediaConcurrency int
	Logger           *slog.Logger
}

// NewRouter builds a Router around the shared (platform-agnostic) services:
// the IssueCreator + TaskEnqueuer that /issue and chat runs go through, and a
// SessionReader for the debounced flush. Register a platform's ResolverSet
// with Register before Handle is called.
func NewRouter(issues IssueCreator, tasks TaskEnqueuer, reader SessionReader, cfg RouterConfig) *Router {
	if cfg.ReplyTimeout == 0 {
		cfg.ReplyTimeout = 2500 * time.Millisecond
	}
	if cfg.MediaTimeout == 0 {
		cfg.MediaTimeout = DefaultMediaTimeout
	}
	if cfg.MediaConcurrency == 0 {
		cfg.MediaConcurrency = 8
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	mediaCtx, mediaCancel := context.WithCancel(context.Background())
	return &Router{
		sets:         make(map[channel.Type]ResolverSet),
		issues:       issues,
		tasks:        tasks,
		reader:       reader,
		replyTimeout: cfg.ReplyTimeout,
		mediaTimeout: cfg.MediaTimeout,
		mediaCtx:     mediaCtx,
		mediaCancel:  mediaCancel,
		mediaSem:     make(chan struct{}, cfg.MediaConcurrency),
		logger:       cfg.Logger,
		pendingFresh: make(map[string]bool),
		mediaQueues:  make(map[string]*mediaQueueEntry),
	}
}

// DefaultMediaTimeout is the default RouterConfig.MediaTimeout. Exported so
// the channel-media settle invariant test can assert the reconciler's settle
// delay dwarfs every pipeline budget.
const DefaultMediaTimeout = 45 * time.Second

type mediaQueueEntry struct {
	tail chan struct{}
}

// Register binds a platform's ResolverSet under t. Call at boot, before Run.
// Registering an empty Type or a set missing a required resolver is ignored.
func (r *Router) Register(t channel.Type, set ResolverSet) {
	if t == "" || set.Installation == nil || set.Identity == nil || set.Dedup == nil || set.Session == nil || set.Audit == nil {
		r.logger.Warn("channel router: ignoring incomplete resolver set", "channel_type", string(t))
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sets[t] = set
}

// EnableRunBatching installs the debouncer in front of the per-session run
// trigger. Call once at boot. A non-positive window uses
// DefaultChatRunBatchWindow. Without it, runs fire inline (used by tests).
func (r *Router) EnableRunBatching(window time.Duration) {
	r.batcher = newPendingBatcher(window)
}

// Drain cancels detached media processing, flushes debounced run triggers, and
// joins media/reply goroutines until ctx ends. It returns whether everything
// completed. Call on shutdown AFTER the Supervisor has stopped delivering
// events; timed-out media retains its durable placeholder fallback.
func (r *Router) Drain(ctx context.Context) bool {
	r.mediaQueueMu.Lock()
	r.stopping = true
	r.mediaCancel()
	r.mediaQueueMu.Unlock()

	done := make(chan struct{})
	go func() {
		if r.batcher != nil {
			r.batcher.FlushAll()
		}
		r.mediaWg.Wait()
		r.replyWg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

// ErrNoResolverSet is returned by Handle when a message arrives for a channel
// type that has no registered ResolverSet — a boot/registration bug. It is an
// infrastructure error so the adapter surfaces it rather than silently
// dropping.
var ErrNoResolverSet = errors.New("channel router: no resolver set for channel type")

// Handle is the shared channel.InboundHandler. It runs the pipeline and then
// drives the detached outbound side; it returns a non-nil error only for
// infrastructure failures (the adapter reconnects). Product outcomes (dropped,
// needs-binding, …) are not errors.
func (r *Router) Handle(ctx context.Context, msg channel.InboundMessage) error {
	r.mu.RLock()
	set, ok := r.sets[msg.Source.ChannelType]
	r.mu.RUnlock()
	if !ok {
		r.logger.Error("channel router: no resolver set", "channel_type", string(msg.Source.ChannelType))
		return ErrNoResolverSet
	}

	res, inst, err := r.dispatch(ctx, set, msg)
	if err != nil {
		r.logger.Error("channel router: dispatch error",
			"channel_type", string(msg.Source.ChannelType),
			"event_id", msg.EventID,
			"error", err,
		)
		return err
	}
	r.logger.Debug("channel router: dispatch outcome",
		"channel_type", string(msg.Source.ChannelType),
		"event_id", msg.EventID,
		"outcome", string(res.Outcome),
		"drop_reason", string(res.DropReason),
	)

	// Typing indicator on ingest, detached so the reaction HTTP call never
	// blocks the connector ACK path.
	if res.Outcome == OutcomeIngested && set.Typing != nil {
		go func() {
			tctx, cancel := context.WithTimeout(context.Background(), r.replyTimeout)
			defer cancel()
			set.Typing.OnIngested(tctx, inst, msg, res.ChatSessionID)
		}()
	}
	r.scheduleReply(set, inst, msg, res)
	return nil
}

// dispatch runs the pipeline and returns the typed result plus the resolved
// installation (needed by the outbound side). Mirrors lark.Dispatcher.Handle.
func (r *Router) dispatch(ctx context.Context, set ResolverSet, msg channel.InboundMessage) (Result, ResolvedInstallation, error) {
	// 1. Route to installation. The adapter maps the platform routing key
	//    (carried on the message) to its installation row. These drop
	//    branches run BEFORE the dedup claim because they have no valid
	//    installation to attach a claim to.
	inst, err := set.Installation.ResolveInstallation(ctx, msg)
	if err != nil {
		if errors.Is(err, ErrInstallationNotFound) {
			_ = set.Audit.RecordDrop(ctx, pgtype.UUID{}, msg, DropReasonInvalidEvent)
			return Result{Outcome: OutcomeDropped, DropReason: DropReasonInvalidEvent}, ResolvedInstallation{}, nil
		}
		return Result{}, ResolvedInstallation{}, fmt.Errorf("resolve installation: %w", err)
	}
	if !inst.Active {
		return r.drop(ctx, set, msg, inst.ID, DropReasonRevokedInstallation), inst, nil
	}

	// 2. Two-phase dedup claim with owner fencing — before group filter and
	//    identity so a reconnect replay cannot re-trigger a binding prompt,
	//    re-write a drop audit, or re-touch the session. Empty MessageID
	//    means there is no key to dedup by; skip the claim.
	var claimToken pgtype.UUID
	claimed := false
	if msg.MessageID != "" {
		token, err := set.Dedup.Claim(ctx, inst.ID, msg.MessageID)
		if err != nil {
			if errors.Is(err, ErrDuplicate) {
				return r.drop(ctx, set, msg, inst.ID, DropReasonDuplicate), inst, nil
			}
			return Result{}, inst, fmt.Errorf("dedup claim: %w", err)
		}
		claimToken = token
		claimed = true
	}

	res, finalize, err := r.processClaimed(ctx, set, msg, inst, claimToken)

	if claimed {
		r.applyFinalize(ctx, set, inst.ID, msg.MessageID, claimToken, finalize)
	}

	// ErrClaimLost: another worker holds the claim. Surface as duplicate.
	if errors.Is(err, ErrClaimLost) {
		return r.drop(ctx, set, msg, inst.ID, DropReasonDuplicate), inst, nil
	}
	return res, inst, err
}

// dedupFinalize tells dispatch how to land the claim row after processClaimed.
type dedupFinalize int

const (
	finalizeNone dedupFinalize = iota
	finalizeMark
	finalizeRelease
)

// processClaimed runs the post-dedup pipeline. Mirrors
// lark.Dispatcher.processClaimed; see its boundary contract per step.
func (r *Router) processClaimed(ctx context.Context, set ResolverSet, msg channel.InboundMessage, inst ResolvedInstallation, claimToken pgtype.UUID) (Result, dedupFinalize, error) {
	// 3. Group-mention filter (group chats only), before identity so an
	//    unbound user's idle group chatter never spams a binding card.
	if msg.Source.ChatType == channel.ChatTypeGroup && !msg.AddressedToBot {
		return r.drop(ctx, set, msg, inst.ID, DropReasonNotAddressedInGroup), finalizeMark, nil
	}

	// 4. Identity check: map the platform sender to a Multica user and
	//    re-verify workspace membership (no binding->member FK; MUL-3515 §4).
	identity, err := set.Identity.ResolveSender(ctx, inst, msg)
	if err != nil {
		switch {
		case errors.Is(err, ErrSenderUnbound):
			_ = set.Audit.RecordDrop(ctx, inst.ID, msg, DropReasonUnboundUser)
			return Result{
				Outcome:        OutcomeNeedsBinding,
				DropReason:     DropReasonUnboundUser,
				InstallationID: inst.ID,
				Sender:         msg.Source.SenderID,
			}, finalizeMark, nil
		case errors.Is(err, ErrSenderNotMember):
			return r.drop(ctx, set, msg, inst.ID, DropReasonNonWorkspaceMember), finalizeMark, nil
		default:
			return Result{}, finalizeRelease, fmt.Errorf("resolve sender: %w", err)
		}
	}

	// 5. Resolve the chat_session. Group sessions are created by the INSTALLER
	//    (stable workspace identity that won't churn with group membership);
	//    p2p sessions by the sole human sender.
	sessionCreator := identity.UserID
	if msg.Source.ChatType == channel.ChatTypeGroup {
		sessionCreator = inst.InstallerUserID
	}
	sessionID, err := set.Session.EnsureSession(ctx, EnsureSessionParams{
		Installation: inst,
		Sender:       sessionCreator,
		Message:      msg,
	})
	if err != nil {
		// Single tx; an error rolled it back, nothing landed. Release.
		return Result{}, finalizeRelease, fmt.Errorf("ensure chat session: %w", err)
	}
	// 6. Append message + in-tx dedup Mark — the durable transition point.
	// The media budget is persisted only when the message actually carries
	// media: a plain text message must never wait behind the media semaphore
	// or fall back to the 45s deadline after a crash. It is a relative
	// duration — the append transaction anchors it to the DB clock, the same
	// clock every now()-based reader uses.
	mediaPendingSeconds := 0.0
	resolveMedia := set.Media != nil && set.Media.HasMedia(msg)
	// The local monotonic budget starts BEFORE the append: the DB anchors its
	// fallback at now() during the insert, so a post-commit local start would
	// end Δ(append latency) AFTER the durable deadline — a window where the
	// task is already claimable while the resolver still runs, handing the
	// agent a placeholder that binds moments later. Starting here keeps the
	// ordering local-gives-up ≤ durable-fallback-fires.
	localMediaDeadline := time.Now().Add(r.mediaTimeout)
	if resolveMedia {
		mediaPendingSeconds = r.mediaTimeout.Seconds()
	}
	appendRes, err := set.Session.AppendMessage(ctx, AppendParams{
		SessionID:           sessionID,
		Sender:              identity.UserID,
		InstallationID:      inst.ID,
		Message:             msg,
		ClaimToken:          claimToken,
		MediaPendingSeconds: mediaPendingSeconds,
	})
	if err != nil {
		if errors.Is(err, ErrClaimLost) {
			return Result{}, finalizeNone, err
		}
		return Result{}, finalizeRelease, fmt.Errorf("append user message: %w", err)
	}

	// Post-append paths must NOT Release (chat_message + Mark already
	// committed). Mark-again is a no-op, so finalizeNone — unless the binder
	// did not Mark in-tx (defensive), then fall back to a post-pipeline Mark.
	postAppendFinalize := finalizeNone
	if !appendRes.DedupMarked {
		postAppendFinalize = finalizeMark
	}

	res := Result{
		Outcome:        OutcomeIngested,
		InstallationID: inst.ID,
		ChatSessionID:  sessionID,
		Sender:         msg.Source.SenderID,
	}

	// 7. /issue command, if present. chat_message is already durable; all
	//    error returns from here signal finalizeNone (or the defensive Mark).
	if appendRes.IssueCommand != nil {
		issueRes, err := r.createIssue(ctx, inst, set.OriginType, identity.UserID, sessionID, *appendRes.IssueCommand)
		if err != nil {
			return Result{}, postAppendFinalize, fmt.Errorf("create issue from command: %w", err)
		}
		res.IssueID = issueRes.Issue.ID
		res.IssueNumber = issueRes.Issue.Number
		res.IssueTitle = issueRes.Issue.Title
		if ws, werr := r.reader.GetWorkspace(ctx, inst.WorkspaceID); werr == nil && ws.IssuePrefix != "" {
			res.IssueIdentifier = fmt.Sprintf("%s-%d", ws.IssuePrefix, issueRes.Issue.Number)
		} else {
			res.IssueIdentifier = fmt.Sprintf("#%d", issueRes.Issue.Number)
		}
	}

	// 8. Debounce the run trigger. The synchronous outcome is OutcomeIngested
	//    with no TaskID — the task row is created at flush. identity.UserID is
	//    THIS message's sender (the task initiator), deliberately not the
	//    session creator (group sessions are creator=installer). Latest sender
	//    in a window wins (MUL-2645).
	r.scheduleRun(set, inst, msg, sessionID, identity.UserID)
	if resolveMedia {
		r.enqueueMedia(set, inst, identity, appendRes.MessageID, msg, sessionID, localMediaDeadline)
	}
	return res, postAppendFinalize, nil
}

// enqueueMedia detaches remote media I/O from Handle while preserving message
// order within a chat session. Run scheduling is independent and durable: the
// task service defers a task to the persisted media deadline, then media
// completion promotes it early.
func (r *Router) enqueueMedia(set ResolverSet, inst ResolvedInstallation, identity ResolvedIdentity, chatMessageID pgtype.UUID, msg channel.InboundMessage, sessionID pgtype.UUID, deadline time.Time) {
	key := keyForSession(sessionID)
	done := make(chan struct{})

	r.mediaQueueMu.Lock()
	if r.stopping {
		r.mediaQueueMu.Unlock()
		return
	}
	entry, ok := r.mediaQueues[key]
	var previous <-chan struct{}
	if !ok {
		entry = &mediaQueueEntry{}
		r.mediaQueues[key] = entry
	} else {
		previous = entry.tail
	}
	entry.tail = done
	r.mediaWg.Add(1)
	r.mediaQueueMu.Unlock()

	go func() {
		defer r.mediaWg.Done()
		defer close(done)
		defer r.finishMediaQueue(key, done)
		// Both queue waits are bounded by the message's own deadline, not
		// just global shutdown: in a media burst an already-expired job must
		// not keep holding its goroutine and payload until it reaches the
		// front — it skips straight to the empty finalize (marker clear +
		// promotion), which also unblocks the session's later messages.
		expiry := time.NewTimer(time.Until(deadline))
		defer expiry.Stop()
		expired := false
		if previous != nil {
			select {
			case <-previous:
			case <-r.mediaCtx.Done():
			case <-expiry.C:
				expired = true
			}
		}
		if !expired {
			select {
			case r.mediaSem <- struct{}{}:
				defer func() { <-r.mediaSem }()
			case <-r.mediaCtx.Done():
				// Cancelled while queued for a slot: proceed without one.
				// ResolveMedia is skipped on the dead context and only the
				// bounded DB finalize runs, preserving prompt marker
				// clearing on shutdown.
			case <-expiry.C:
				// Expired while queued: no slot needed — resolveAndBindMedia
				// sees the dead deadline and runs only the empty finalize.
			}
		}
		r.resolveAndBindMedia(set, inst, identity, chatMessageID, msg, sessionID, deadline)
	}()
}

const mediaFinalizeTimeout = 5 * time.Second

func (r *Router) resolveAndBindMedia(set ResolverSet, inst ResolvedInstallation, identity ResolvedIdentity, chatMessageID pgtype.UUID, msg channel.InboundMessage, sessionID pgtype.UUID, deadline time.Time) {
	ctx, cancel := context.WithDeadline(r.mediaCtx, deadline)
	defer cancel()

	resolved := msg
	if ctx.Err() == nil {
		// Skipped entirely when the budget expired while queued (or on
		// shutdown): resolving on a dead context would only churn through
		// intent writes that immediately fail.
		resolved = set.Media.ResolveMedia(ctx, inst, identity, sessionID, chatMessageID, msg)
	}
	finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), mediaFinalizeTimeout)
	defer finalizeCancel()
	if err := ctx.Err(); err != nil {
		// Refs resolved before the deadline already sit in object storage but
		// will not gain an attachment row. Nothing is deleted here — their
		// intent-ledger rows were written before the uploads, and the
		// reconciler reclaims unreferenced objects after the settle delay.
		resolved.MediaRefs = nil
		r.logger.Warn("channel router: media resolution incomplete; using placeholder",
			"channel_type", string(msg.Source.ChannelType),
			"event_id", msg.EventID,
			"message_id", msg.MessageID,
			"error", err)
	}
	if err := set.Session.BindMedia(finalizeCtx, BindMediaParams{
		MessageID:   chatMessageID,
		SessionID:   sessionID,
		WorkspaceID: inst.WorkspaceID,
		Sender:      identity.UserID,
		MediaRefs:   resolved.MediaRefs,
	}); err != nil {
		// Never delete inline: the attachments may or may not have landed
		// (an ambiguous commit), but the intent rows are deleted in the SAME
		// transaction, so the ledger already reflects whichever outcome is
		// durable and the reconciler settles the objects.
		r.logger.Warn("channel router: media attachment binding failed",
			"channel_type", string(msg.Source.ChannelType),
			"event_id", msg.EventID,
			"message_id", msg.MessageID,
			"err", err)
	}
	if err := r.tasks.PromoteChannelChatTasksIfMediaReady(finalizeCtx, sessionID); err != nil {
		r.logger.Warn("channel router: media-ready task promotion failed",
			"channel_type", string(msg.Source.ChannelType),
			"event_id", msg.EventID,
			"message_id", msg.MessageID,
			"err", err)
	}
}

func (r *Router) finishMediaQueue(key string, done chan struct{}) {
	r.mediaQueueMu.Lock()
	defer r.mediaQueueMu.Unlock()
	entry, ok := r.mediaQueues[key]
	if !ok || entry.tail != done {
		return
	}
	delete(r.mediaQueues, key)
}

// scheduleRun hands the per-session run trigger to the debouncer (or fires it
// inline when batching is disabled).
func (r *Router) scheduleRun(set ResolverSet, inst ResolvedInstallation, msg channel.InboundMessage, sessionID, initiatorUserID pgtype.UUID) {
	key := keyForSession(sessionID)
	fresh := msg.ForceFresh
	if r.batcher == nil {
		r.flushChatRun(set, inst, msg, sessionID, initiatorUserID, fresh)
		return
	}
	if fresh {
		r.markPendingFresh(key)
	}
	flush := func() {
		r.flushChatRun(set, inst, msg, sessionID, initiatorUserID, r.takePendingFresh(key, fresh))
	}
	r.batcher.Schedule(key, flush)
}

// chatRunFlushTimeout bounds the detached flush (session reload + enqueue +
// notice), which runs on its own fresh context.
const chatRunFlushTimeout = 10 * time.Second

// flushChatRun is the debounced run-trigger: reload session, enqueue exactly
// one chat task for the window, and emit the offline/archived notice (only
// known here now) via the replier. Errors are logged, not returned.
func (r *Router) flushChatRun(set ResolverSet, inst ResolvedInstallation, msg channel.InboundMessage, sessionID, initiatorUserID pgtype.UUID, forceFresh bool) {
	ctx, cancel := context.WithTimeout(context.Background(), chatRunFlushTimeout)
	defer cancel()

	session, err := r.reader.GetChatSession(ctx, sessionID)
	if err != nil {
		r.logger.Error("channel router: flush reload chat session failed",
			"chat_session_id", uuidString(sessionID), "err", err.Error())
		r.clearTyping(ctx, set, sessionID)
		return
	}
	if _, err := r.tasks.EnqueueChatTask(ctx, session, initiatorUserID, forceFresh); err != nil {
		// No task was enqueued, so no task lifecycle event will ever publish and
		// the platform's bus-driven typing clear can never fire. Clear the
		// indicator here (before any notice) so the "processing" reaction does
		// not stick on the user's message.
		r.clearTyping(ctx, set, sessionID)
		switch {
		case errors.Is(err, service.ErrChatTaskAgentNoRuntime):
			r.emitFlushReply(ctx, set, inst, msg, sessionID, OutcomeAgentOffline)
		case errors.Is(err, service.ErrChatTaskAgentArchived):
			r.emitFlushReply(ctx, set, inst, msg, sessionID, OutcomeAgentArchived)
		default:
			r.logger.Error("channel router: flush enqueue chat task failed",
				"chat_session_id", uuidString(sessionID), "err", err.Error())
		}
	}
}

// clearTyping asks the platform to drop the "processing" indicator for a session
// whose flush produced no task run. A nil TypingNotifier (platform without the
// feature) is a no-op.
func (r *Router) clearTyping(ctx context.Context, set ResolverSet, sessionID pgtype.UUID) {
	if set.Typing != nil {
		set.Typing.OnSettled(ctx, sessionID)
	}
}

func (r *Router) markPendingFresh(key string) {
	r.pendingFreshMu.Lock()
	defer r.pendingFreshMu.Unlock()
	r.pendingFresh[key] = true
}

func (r *Router) takePendingFresh(key string, fallback bool) bool {
	r.pendingFreshMu.Lock()
	defer r.pendingFreshMu.Unlock()
	fresh := fallback || r.pendingFresh[key]
	delete(r.pendingFresh, key)
	return fresh
}

// emitFlushReply delivers an offline/archived notice for a flushed run.
func (r *Router) emitFlushReply(ctx context.Context, set ResolverSet, inst ResolvedInstallation, msg channel.InboundMessage, sessionID pgtype.UUID, outcome Outcome) {
	if set.Replier == nil {
		return
	}
	set.Replier.Reply(ctx, inst, msg, Result{
		Outcome:        outcome,
		InstallationID: inst.ID,
		ChatSessionID:  sessionID,
		Sender:         msg.Source.SenderID,
	})
}

// scheduleReply detaches the OutboundReplier from the ACK critical path. The
// reply goroutine uses a fresh context with a ReplyTimeout deadline so it is
// independent of the inbound emit ctx (which the adapter cancels when its
// receive loop exits). A nil replier short-circuits — no goroutine.
func (r *Router) scheduleReply(set ResolverSet, inst ResolvedInstallation, msg channel.InboundMessage, res Result) {
	if set.Replier == nil {
		return
	}
	r.replyWg.Add(1)
	go func() {
		defer r.replyWg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), r.replyTimeout)
		defer cancel()
		set.Replier.Reply(ctx, inst, msg, res)
		if ctx.Err() == context.DeadlineExceeded {
			r.logger.Warn("channel router: outbound reply timed out",
				"event_id", msg.EventID, "outcome", string(res.Outcome),
				"timeout", r.replyTimeout.String())
		}
	}()
}

// keyForSession is the batcher key. chat_session_id is globally unique.
func keyForSession(sessionID pgtype.UUID) string {
	return string(sessionID.Bytes[:])
}

// applyFinalize flips the in-flight claim row to its terminal state,
// token-fenced. Best-effort: a transport failure cannot abort the outcome.
func (r *Router) applyFinalize(ctx context.Context, set ResolverSet, instID pgtype.UUID, messageID string, claimToken pgtype.UUID, action dedupFinalize) {
	switch action {
	case finalizeMark:
		_ = set.Dedup.Mark(ctx, instID, messageID, claimToken)
	case finalizeRelease:
		_ = set.Dedup.Release(ctx, instID, messageID, claimToken)
	case finalizeNone:
	}
}

func (r *Router) drop(ctx context.Context, set ResolverSet, msg channel.InboundMessage, instID pgtype.UUID, reason DropReason) Result {
	_ = set.Audit.RecordDrop(ctx, instID, msg, reason)
	return Result{Outcome: OutcomeDropped, DropReason: reason, InstallationID: instID}
}

func (r *Router) createIssue(ctx context.Context, inst ResolvedInstallation, originType string, creatorUserID, sessionID pgtype.UUID, cmd IssueCommand) (service.IssueCreateResult, error) {
	if cmd.Title == "" {
		return service.IssueCreateResult{}, ErrEmptyIssueTitle
	}
	params := service.IssueCreateParams{
		WorkspaceID:  inst.WorkspaceID,
		Title:        cmd.Title,
		Description:  pgtype.Text{String: cmd.Description, Valid: cmd.Description != ""},
		Status:       "todo",
		Priority:     "none",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   inst.AgentID,
		CreatorType:  "member",
		CreatorID:    creatorUserID,
		OriginType:   pgtype.Text{String: originType, Valid: originType != ""},
		OriginID:     sessionID,
	}
	return r.issues.Create(ctx, params, service.IssueCreateOpts{})
}

// ErrEmptyIssueTitle is returned by createIssue when /issue has no title and
// the binder's previous-message fallback found nothing usable.
var ErrEmptyIssueTitle = errors.New("issue title is empty")

var _ channel.InboundHandler = (*Router)(nil).Handle
