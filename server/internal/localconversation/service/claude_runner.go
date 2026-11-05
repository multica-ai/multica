package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
	"github.com/multica-ai/multica/server/pkg/agent"
)

var (
	// ErrTurnRunning rejects a send while the conversation already runs a turn.
	ErrTurnRunning = errors.New("local conversation: a turn is already running")
	// ErrOutcomeUnknown means the prompt may or may not have reached Claude;
	// the command must not be replayed automatically (plan §8.1).
	ErrOutcomeUnknown = errors.New("local conversation: provider outcome unknown")

	errQueryEnded = errors.New("Claude Code ended the session while it was starting")
)

// ClaudeAdapter is the running adapter process the runner talks to,
// implemented by *claude.Process.
type ClaudeAdapter interface {
	Request(ctx context.Context, typ string, fields map[string]any) (json.RawMessage, error)
	Close(ctx context.Context) error
	Done() <-chan struct{}
	Err() error
}

// AttachmentFiles writes attachment files, implemented by *store.Store.
type AttachmentFiles interface {
	WriteAttachmentFile(scopeID int64, conversationID, mediaType string, data []byte, now time.Time) (store.NewAttachment, error)
}

// StartClaudeAdapter launches an adapter delivering output to handler.
type StartClaudeAdapter func(ctx context.Context, handler claude.Handler) (ClaudeAdapter, error)

// ClaudeSession is how the conversation's adapter is started: the fields of
// the adapter start request, without resume, which the runner adds.
type ClaudeSession struct {
	Fields            map[string]any
	ProviderSessionID string
}

// SendRequest is one user turn.
type SendRequest struct {
	TurnID    string
	RequestID string
	Origin    TurnOrigin
	// Fingerprint identifies the request content for idempotent retries.
	Fingerprint string
	MessageID   string
	// Text is the message as the user wrote it.
	Text string
	// Prompt is what the provider receives: Text plus attachment metadata.
	Prompt        string
	AttachmentIDs []string
	References    []Reference
	// Title, when set, becomes the conversation title with the message.
	Title *string
	// CreateTask is set when the message carries the New task mention.
	CreateTask bool
	// Hidden turns have no user message: the prompt comes from Multica,
	// such as a background result delivery.
	Hidden bool
	// Compact invokes the provider's native context compaction.
	Compact bool
	// Trace records the turn's timing; nil records nothing.
	Trace *timing.Trace
}

func (r SendRequest) prompt() string {
	if r.Prompt != "" {
		return r.Prompt
	}
	return r.Text
}

const (
	interruptDeadline = 10 * time.Second
	// settingsPushDeadline bounds one push of settings to the adapter.
	settingsPushDeadline = 10 * time.Second
	previewRunes         = 200
	// sendDeadline bounds a send's preparation and dispatch. It is generous
	// because a Claude start includes the project's SessionStart hooks.
	sendDeadline = 5 * time.Minute
)

// detachSend is the context of a send's durable part: the caller's values
// without its cancellation. Once the input is stored the turn exists and a
// retry replays it, so a caller going away must not fail the turn.
func detachSend(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), sendDeadline)
}

// ClaudeRunner runs one Claude conversation: it starts the adapter on
// demand, records each turn through the batcher, normalizes adapter output
// into events, and completes the turn when Claude reports its result or the
// adapter goes away. Output handling never blocks the adapter reader.
type ClaudeRunner struct {
	rec            turnRecorder
	conversationID string
	start          StartClaudeAdapter
	session        ClaudeSession
	batcher        *Batcher
	limiter        *Limiter
	now            func() time.Time
	bindContext    ContextBinder
	// claimSpare takes a parked process for a conversation with no session yet.
	claimSpare func(ctx context.Context, provider string) *parkedSpare
	onTurnEnd  func(turnID string)
	// onBackgroundEnded runs when background tasks end with no turn
	// running and no native reply started.
	onBackgroundEnded func(results []BackgroundResult)
	// nativeTrace starts the timing of a turn Claude began on its own.
	nativeTrace func(turnID string) *timing.Trace
	// processTrace records what happens to the process between turns.
	processTrace func() *timing.Trace
	// transcriptSize measures the session file a resume reloads.
	transcriptSize func(id string) (int64, bool)
	// taskTurns maps a background task item to the turn that started it;
	// later events of the task are recorded under that turn.
	taskTurns map[string]string
	// ended are background results waiting for delivery; a native reply
	// supersedes them.
	ended []BackgroundResult
	// stopped are tasks the user stopped; their end needs no follow-up.
	stopped map[string]bool
	// releaseContext ends the context route of the running adapter.
	releaseContext func()

	mu sync.Mutex
	// normMu serializes the normalizer between the adapter reader and
	// finish, which may run from the exit or interrupt path.
	normMu       sync.Mutex
	interrupting bool
	adapter      ClaudeAdapter
	norm         *claude.Normalizer
	turn         *claudeTurn
	contextName  string
	gate         prepareGate
	// prompted is set once the running adapter has been given input. Before
	// that, a Query's own output (such as from a preheat) is no reply to anyone.
	prompted bool
	// endedNorm is the normalizer of an adapter whose Query ended before install,
	// when its error frame had no installed adapter to act on.
	endedNorm *claude.Normalizer
	// settingsVersion counts ApplySettings calls and settings is the latest, so
	// a start that read older fields can apply them once its Query is live.
	settingsVersion int64
	settings        *store.ConversationSettings
	// adapterVersion is the settingsVersion the running adapter has applied.
	adapterVersion int64
	// restartAfterTurn is set when settings could not reach the adapter while a
	// turn ran on it: the turn keeps what it has, nothing after it does.
	restartAfterTurn bool
	// pushMu keeps settings pushes to one at a time, so an older one cannot land
	// after a newer one. Never taken while holding mu.
	pushMu sync.Mutex
}

type claudeTurn struct {
	id              string
	compacting      bool
	compacted       bool
	compactError    string
	cancelRequested bool
	// storageFailed is set when the batcher rejected output; the rest of
	// the turn's output is dropped and the turn ends as failed.
	storageFailed bool
	// lastMessageID is the uuid of the turn's last transcript message, where
	// a fork through this turn ends.
	lastMessageID string
	references    []Reference
	createTask    bool
	// unlimited marks a native turn that started over the concurrency
	// limit; it holds no limiter slot.
	unlimited bool
	trace     *timing.Trace
	stop      *stopSignal
	// inputSent is set once the prompt went to the adapter, or from the start for
	// a turn Claude began itself; earlier messages are no reply. Guarded by
	// ClaudeRunner.mu.
	inputSent bool
}

// BackgroundResult is a background task that ended after its turn.
type BackgroundResult struct {
	SourceTurnID string
	// State is the task's final item state.
	State map[string]any
}

// ClaudeRunnerOptions configures a runner.
type ClaudeRunnerOptions struct {
	ConversationID string
	ScopeID        int64
	Files          AttachmentFiles
	Start          StartClaudeAdapter
	Session        ClaudeSession
	Batcher        *Batcher
	Limiter        *Limiter
	ContextMCPName string
	Now            func() time.Time
	// OnTurnEnd, when set, runs after a turn's completion is recorded.
	OnTurnEnd func(turnID string)
	// OnBackgroundEnded, when set, receives background tasks that ended
	// while no turn ran.
	OnBackgroundEnded func(results []BackgroundResult)
	// Context, when set, gives the adapter Multica's context tools.
	Context ContextBinder
	// NativeTrace, when set, starts the timing of a turn Claude began itself.
	NativeTrace func(turnID string) *timing.Trace
	// ProcessTrace, when set, records what happens to the process between turns.
	ProcessTrace func() *timing.Trace
	// TranscriptSize, when set, measures the session file a resume reloads.
	TranscriptSize func(id string) (int64, bool)
	// ClaimSpare, when set, offers a parked process for a new conversation.
	ClaimSpare func(ctx context.Context, provider string) *parkedSpare
}

// NewClaudeRunner returns a runner for one conversation.
func NewClaudeRunner(opts ClaudeRunnerOptions) *ClaudeRunner {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &ClaudeRunner{
		rec:            turnRecorder{conversationID: opts.ConversationID, scopeID: opts.ScopeID, files: opts.Files, now: opts.Now},
		conversationID: opts.ConversationID, start: opts.Start, session: opts.Session,
		batcher: opts.Batcher, limiter: opts.Limiter, now: opts.Now, contextName: opts.ContextMCPName, bindContext: opts.Context,
		claimSpare: opts.ClaimSpare, onTurnEnd: opts.OnTurnEnd, onBackgroundEnded: opts.OnBackgroundEnded, nativeTrace: opts.NativeTrace, processTrace: opts.ProcessTrace, transcriptSize: opts.TranscriptSize,
		taskTurns: map[string]string{}, stopped: map[string]bool{}, gate: newPrepareGate(),
	}
}

// Send starts a turn. It returns once the user input, turn start and command
// receipt are durable and the prompt was handed to the adapter; output and
// completion follow as events.
func (r *ClaudeRunner) Send(ctx context.Context, req SendRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	admissionContext := ctx
	ctx, cancel := detachSend(ctx)
	defer cancel()
	r.mu.Lock()
	if err := admissionContext.Err(); err != nil {
		r.mu.Unlock()
		return err
	}

	if r.gate.closed {
		r.mu.Unlock()
		return ErrClosing
	}
	if r.turn != nil || r.interrupting {
		r.mu.Unlock()
		return ErrTurnRunning
	}
	trace := req.Trace
	if err := r.limiter.TryAcquire(); err != nil {
		r.mu.Unlock()
		trace.MarkFailed(timing.StageRunnerLimiter, timing.ReasonConcurrencyLimit)
		return err
	}
	turn := &claudeTurn{id: req.TurnID, references: req.References, createTask: req.CreateTask, compacting: req.Compact, trace: trace, stop: newStopSignal()}
	r.turn = turn
	r.gate.sendReady(trace, r.adapter != nil)
	r.mu.Unlock()
	trace.Mark(timing.StageRunnerLimiter)

	persisted := trace.Begin(timing.StageTurnPersist)
	_, err := r.batcher.AppendWait(ctx, r.rec.startBatch(req))
	persisted(err)
	if err != nil {
		r.mu.Lock()
		r.turn = nil
		r.mu.Unlock()
		r.limiter.Release()
		return err
	}

	prepared := trace.Begin(timing.StagePrepareWait)
	adapter, err := r.awaitDispatch(ctx, turn, trace)
	prepared(err)
	if err != nil {
		r.finish(turn, "failed", &turnError{kind: "provider_unavailable", message: err.Error()})
		return err
	}
	if adapter == nil {
		return nil
	}
	content := []map[string]any{{"type": "text", "text": req.prompt()}}
	queued := trace.Begin(timing.StageProviderInputQueued)
	_, err = adapter.Request(ctx, "send", map[string]any{"request_id": req.RequestID, "content": content})
	queued(err)
	if err != nil {
		var adapterErr *claude.AdapterError
		if errors.As(err, &adapterErr) && (adapterErr.Dispatch == "not_dispatched" || adapterErr.Dispatch == "rejected") {
			r.mu.Lock()
			norm := r.norm
			current := r.adapter == adapter
			r.mu.Unlock()
			r.finish(turn, "failed", &turnError{kind: "provider_error", message: err.Error()})
			// The process lives on with no Query, and would refuse every send.
			if current && adapterErr.Code == "not_started" {
				r.retireAdapter(norm)
			}
			return err
		}
		// Anything else may have reached Claude: transport errors included.
		r.finish(turn, "failed", &turnError{kind: "outcome_unknown", message: err.Error()})
		return fmt.Errorf("%w: %w", ErrOutcomeUnknown, err)
	}
	return nil
}

// awaitDispatch returns the adapter the prompt goes to, with the turn marked as
// sent; nil when the turn was stopped or ended. A prompt must not run under settings
// the user already replaced, so any push waits to finish first, and one that failed
// has retired the adapter, which the next round replaces.
func (r *ClaudeRunner) awaitDispatch(ctx context.Context, turn *claudeTurn, trace *timing.Trace) (ClaudeAdapter, error) {
	for {
		adapter, err := r.awaitReady(ctx, turn, trace)
		if err != nil {
			return nil, err
		}
		if r.stoppedBeforeDispatch(turn) || adapter == nil {
			return nil, nil
		}
		_ = r.syncSettings(adapter)
		r.mu.Lock()
		if r.turn == turn && !turn.cancelRequested && r.adapter == adapter && r.adapterVersion == r.settingsVersion {
			turn.inputSent = true
			r.prompted = true
			r.mu.Unlock()
			return adapter, nil
		}
		r.mu.Unlock()
	}
}

// awaitReady returns the adapter once it is usable, starting or joining the one
// prepare task; nil when the turn was stopped or ended meanwhile.
func (r *ClaudeRunner) awaitReady(ctx context.Context, turn *claudeTurn, trace *timing.Trace) (ClaudeAdapter, error) {
	for {
		r.mu.Lock()
		if r.turn != turn {
			r.mu.Unlock()
			return nil, nil
		}
		task := r.gate.task
		if task == nil {
			if r.adapter != nil {
				adapter := r.adapter
				r.mu.Unlock()
				return adapter, nil
			}
			task = r.startPrepareLocked(trace, false)
		}
		r.mu.Unlock()
		stopped, err := waitPrepare(ctx, task, turn.stop.wait())
		if stopped {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		// A failed preheat was only ever an optimization: the send starts its own.
		if task.err != nil && !task.preheat {
			return nil, task.err
		}
	}
}

// startPrepareLocked starts the prepare task; r.mu is held.
func (r *ClaudeRunner) startPrepareLocked(trace *timing.Trace, preheat bool) *prepareTask {
	task, ctx, cancel := r.gate.begin(preheat)
	if preheat {
		trace.SetPrepare(timing.PreparePreheated, task.id)
	} else {
		trace.SetPrepare(timing.PrepareCold, task.id)
	}
	go func() {
		defer cancel()
		err := r.ensureAdapter(ctx, trace)
		r.mu.Lock()
		task.err = err
		r.gate.finish(task, r.now())
		r.mu.Unlock()
		close(task.done)
	}()
	return task
}

// Prepare starts an existing conversation's Query ahead of its next send,
// without taking a turn slot or sending anything. It does nothing when a turn
// runs or the adapter is ready, and a conversation without a native session is
// never prepared. ctx only bounds the wait; the task continues for whoever else
// waits on it.
func (r *ClaudeRunner) Prepare(ctx context.Context, trace *timing.Trace) error {
	r.mu.Lock()
	if r.gate.closed {
		r.mu.Unlock()
		return ErrClosing
	}
	if r.turn != nil || r.session.ProviderSessionID == "" {
		r.mu.Unlock()
		return nil
	}
	task := r.gate.task
	if task == nil {
		if r.adapter != nil {
			r.mu.Unlock()
			return nil
		}
		task = r.startPrepareLocked(trace, true)
	} else {
		trace.SetPrepare(timing.PreparePreheated, task.id)
	}
	r.mu.Unlock()
	if _, err := waitPrepare(ctx, task, nil); err != nil {
		return err
	}
	return task.err
}

// Evict closes an idle adapter to free its resources. The runner and session
// stay; the next send resumes on a new adapter. The adapter leaves the runner
// under the lock, so a send can never pick up one that is exiting. Background
// tasks live in the adapter's process and may outlast any turn, so an adapter
// with open tasks or undelivered results stays.
func (r *ClaudeRunner) Evict(ctx context.Context) bool {
	r.mu.Lock()
	if r.turn != nil || r.gate.task != nil || r.adapter == nil || !r.evictableLocked() {
		r.mu.Unlock()
		return false
	}
	adapter, release := r.adapter, r.releaseContext
	r.adapter, r.norm, r.releaseContext = nil, nil, nil
	r.gate.preheated = false
	r.mu.Unlock()
	if release != nil {
		release()
	}
	_ = adapter.Close(ctx)
	return true
}

func (r *ClaudeRunner) evictableLocked() bool { return len(r.taskTurns) == 0 && len(r.ended) == 0 }

// IdleProcess reports the runner's process when it is doing nothing.
func (r *ClaudeRunner) IdleProcess() (idleProcess, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.adapter == nil || r.turn != nil || r.gate.task != nil {
		return idleProcess{}, false
	}
	return idleProcess{provider: "claude", scopeID: r.rec.scopeID, since: r.gate.idleSince, preheated: r.gate.preheated, evictable: r.evictableLocked()}, true
}

// stoppedBeforeDispatch ends a turn cancelled during preparation and reports
// whether the turn is over, so its prompt must not be sent.
func (r *ClaudeRunner) stoppedBeforeDispatch(turn *claudeTurn) bool {
	r.mu.Lock()
	ended, cancelled := r.turn != turn, turn.cancelRequested
	r.mu.Unlock()
	if cancelled {
		r.finish(turn, "cancelled", nil)
	}
	return ended || cancelled
}

// StopTasks asks Claude to stop background tasks. Without a running
// adapter there is nothing left to stop.
func (r *ClaudeRunner) StopTasks(ctx context.Context, taskIDs []string) error {
	r.mu.Lock()
	adapter := r.adapter
	r.mu.Unlock()
	if adapter == nil {
		return nil
	}
	r.mu.Lock()
	for _, id := range taskIDs {
		r.stopped[id] = true
	}
	r.mu.Unlock()
	var errs []error
	for _, id := range taskIDs {
		if _, err := adapter.Request(ctx, "stop_task", map[string]any{"task_id": id}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Interrupt asks Claude to stop the running turn. The turn completes as
// cancelled when the adapter confirms or the Query ends.
func (r *ClaudeRunner) Interrupt(ctx context.Context, expectedTurnID ...string) error {
	r.mu.Lock()
	turn, adapter := r.turn, r.adapter
	if len(expectedTurnID) > 0 && (turn == nil || turn.id != expectedTurnID[0]) {
		r.mu.Unlock()
		return nil
	}
	if r.interrupting {
		r.mu.Unlock()
		return nil
	}
	if turn != nil {
		r.interrupting = true
		turn.cancelRequested = true
		if !turn.inputSent {
			turn.stop.fire()
		}
	}
	r.mu.Unlock()
	defer func() { r.mu.Lock(); r.interrupting = false; r.mu.Unlock() }()
	if turn == nil || adapter == nil {
		return nil
	}
	data, err := adapter.Request(ctx, "interrupt", map[string]any{"deadline_ms": interruptDeadline.Milliseconds()})
	if err != nil {
		return err
	}
	var result struct {
		Outcome string `json:"outcome"`
	}
	_ = json.Unmarshal(data, &result)
	if result.Outcome == "closed" {
		// The Query transport was closed; no result will follow.
		r.finish(turn, "cancelled", nil)
	}
	return nil
}

// Close stops the adapter, failing a running turn.
func (r *ClaudeRunner) Close(ctx context.Context) error {
	r.mu.Lock()
	r.gate.closed = true
	r.gate.cancel()
	task := r.gate.task
	adapter, turn, release := r.adapter, r.turn, r.releaseContext
	r.adapter, r.releaseContext = nil, nil
	r.mu.Unlock()
	if turn != nil {
		r.finish(turn, "failed", &turnError{kind: "provider_exited", message: "the conversation was closed"})
	}
	if release != nil {
		release()
	}
	var err error
	if adapter != nil {
		err = adapter.Close(ctx)
	}
	waitClosed(ctx, task)
	return err
}

// ensureAdapter starts the adapter. It runs only inside the prepare task, which
// is what keeps two callers from starting two processes.
func (r *ClaudeRunner) ensureAdapter(ctx context.Context, trace *timing.Trace) error {
	r.mu.Lock()
	fresh := r.session.ProviderSessionID == ""
	model, _ := r.session.Fields["model"].(string)
	r.mu.Unlock()
	// Claude resolves its own default inside the Query; asking first would add a
	// process round trip for an answer nothing here needs.
	if model == "" || model == agent.ClaudeDefaultModelValue {
		trace.MarkReason(timing.StageModelResolve, timing.ReasonNativeDefault)
	} else {
		trace.MarkReason(timing.StageModelResolve, timing.ReasonExplicit)
	}
	if fresh && r.claimSpare != nil {
		if spare := r.claimSpare(ctx, "claude"); spare != nil {
			err := r.adoptSpare(ctx, spare, trace)
			if err == nil || errors.Is(err, ErrClosing) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
	norm := claude.NewNormalizer(r.contextName)
	began := trace.Now()
	adapter, err := r.start(ctx, func(out claude.Output) { r.handle(norm, out) })
	markProcessStart(trace, timing.StageAdapterSpawn, timing.StageAdapterReady, began, adapter, err)
	if err != nil {
		return err
	}
	fields := map[string]any{}
	r.mu.Lock()
	for k, v := range r.session.Fields {
		fields[k] = v
	}
	resume := r.session.ProviderSessionID
	if resume != "" {
		fields["resume"] = resume
	}
	version := r.settingsVersion
	r.mu.Unlock()
	if resume != "" && r.transcriptSize != nil {
		// Finding the file walks the projects folder, which must not delay a start.
		go func() {
			if size, ok := r.transcriptSize(resume); ok {
				trace.MarkBytes(timing.StageThreadPrepare, timing.ReasonResume, size)
			}
		}()
	}
	release := func() {}
	if r.bindContext != nil {
		bound := trace.Begin(timing.StageContextBind)
		binding, rel, err := r.bindContext(context.Background(), "", r.currentContext)
		bound(err)
		if err != nil {
			_ = adapter.Close(context.Background())
			return err
		}
		release = rel
		fields["mcp_servers"] = contextServerEntry(contextName(r.contextName), binding.Binding)
		if binding.SystemPrompt != "" {
			fields["append_system_prompt"] = binding.SystemPrompt
		}
		fields["allowed_tools"] = []string{"mcp__" + contextName(r.contextName) + "__*"}
	}
	starting := trace.Now()
	started, err := adapter.Request(ctx, "start", fields)
	if err != nil {
		release()
		_ = adapter.Close(context.Background())
		return err
	}
	markAdapterStart(trace, starting, started)
	return r.install(adapter, norm, release, version)
}

// adoptSpare binds a parked process to this conversation: claim, then MCP, and
// only then is it ready for the first message, so every failure here happens
// before anything reached the model and the caller can start normally with a new
// adapter and normalizer. Output stays held until the bind succeeds, which also
// keeps its session id from being remembered for a resume with no transcript.
func (r *ClaudeRunner) adoptSpare(ctx context.Context, spare *parkedSpare, trace *timing.Trace) error {
	spare.claudeFrames.hold()
	r.mu.Lock()
	session := r.session.Fields
	version := r.settingsVersion
	r.mu.Unlock()
	fields := map[string]any{"cwd": session["cwd"]}
	if model, ok := session["model"]; ok {
		fields["model"] = model
	}
	if mode, ok := session["permission_mode"]; ok {
		fields["permission_mode"] = mode
	}
	if effort, ok := session["reasoning_effort"]; ok {
		fields["settings"] = map[string]any{"effortLevel": effort}
	}
	release := func() {}
	var servers map[string]any
	if r.bindContext != nil {
		bound := trace.Begin(timing.StageContextBind)
		binding, rel, err := r.bindContext(context.Background(), "", r.currentContext)
		bound(err)
		if err != nil {
			spare.reject(timing.ReasonBindFailed)
			return err
		}
		release = rel
		servers = contextServerEntry(contextName(r.contextName), binding.Binding)
		if binding.SystemPrompt != "" {
			fields["append_system_prompt"] = binding.SystemPrompt
		}
	}
	claiming := trace.Begin(timing.StageSpareClaim)
	_, err := spare.adapter.Request(ctx, "claim", fields)
	claiming(err)
	if err != nil {
		release()
		spare.reject(claimRejection(err))
		return err
	}
	if servers != nil {
		binding := trace.Begin(timing.StageMCPSet)
		raw, err := spare.adapter.Request(ctx, "set_mcp_servers", map[string]any{"servers": servers})
		if err == nil && claude.MCPSetFailed(raw) {
			err = errors.New("Claude Code could not bind the Multica tools")
		}
		binding(err)
		if err != nil {
			release()
			spare.reject(timing.ReasonMCPFailed)
			return err
		}
	}
	norm := claude.NewNormalizer(r.contextName)
	spare.claudeFrames.adopt(func(out claude.Output) { r.handle(norm, out) })
	if err := r.install(spare.adapter, norm, release, version); err != nil {
		return err
	}
	trace.SetPrepare(timing.PrepareSpare, "")
	trace.Mark(timing.StageSpareClaimed)
	return nil
}

// claimRejection is the closed-enum form of why a claim did not bind.
func claimRejection(err error) string {
	switch claude.ClaimReason(err) {
	case "option_not_applied":
		return timing.ReasonOptionNotApplied
	case "settings_not_applied":
		return timing.ReasonSettingsNotApplied
	}
	return timing.ReasonClaimRefused
}

// install makes a started adapter the runner's, unless Close came first, and
// brings it up to date with settings that changed while it was starting.
func (r *ClaudeRunner) install(adapter ClaudeAdapter, norm *claude.Normalizer, release func(), version int64) error {
	r.mu.Lock()
	if r.gate.closed {
		r.mu.Unlock()
		release()
		_ = adapter.Close(context.Background())
		return ErrClosing
	}
	if r.endedNorm == norm {
		r.endedNorm = nil
		r.mu.Unlock()
		release()
		_ = adapter.Close(context.Background())
		return errQueryEnded
	}
	r.adapter, r.norm, r.releaseContext = adapter, norm, release
	r.prompted, r.restartAfterTurn, r.adapterVersion = false, false, version
	r.mu.Unlock()
	go r.watch(adapter)
	// A prompt must not run under settings the user already replaced, such as a
	// permission downgrade, so failing here fails the start.
	return r.syncSettings(adapter)
}

// watch fails the running turn when the adapter process goes away.
func (r *ClaudeRunner) watch(adapter ClaudeAdapter) {
	<-adapter.Done()
	r.mu.Lock()
	if r.adapter != adapter {
		r.mu.Unlock()
		markProcessExit(r.processTrace, nil, timing.ReasonStopped)
		return
	}
	r.adapter = nil
	turn, release := r.turn, r.releaseContext
	r.releaseContext = nil
	r.mu.Unlock()
	var running *timing.Trace
	if turn != nil {
		running = turn.trace
	}
	markProcessExit(r.processTrace, running, timing.ReasonExited)
	if release != nil {
		release()
	}
	if turn != nil {
		message := "Claude adapter exited"
		if err := adapter.Err(); err != nil {
			message = err.Error()
		}
		r.finish(turn, "failed", &turnError{kind: "provider_exited", message: message})
	}
}

// handle runs on the adapter's reader goroutine and must not block.
func (r *ClaudeRunner) handle(norm *claude.Normalizer, out claude.Output) {
	r.mu.Lock()
	turn := r.turn
	cancelRequested := turn != nil && turn.cancelRequested
	inputSent := turn != nil && turn.inputSent
	// awaitingPrompt is a send's turn before its prompt goes out, whose completion
	// cannot come from anything Claude says now.
	awaitingPrompt := turn != nil && !inputSent
	// Until a Query has had input its output is no reply to anyone, including a
	// send already waiting on it: a SessionStart hook can make Claude answer, and
	// its session id has no transcript to resume yet.
	unprompted := !inputSent && !r.prompted
	r.mu.Unlock()
	if unprompted && out.Type == "sdk_message" {
		return
	}
	switch out.Type {
	case "sdk_message":
		if inputSent {
			turn.trace.Once(timing.StageProviderFirstMessage)
		}
		if turn != nil && turn.compacting {
			var status struct {
				Type          string `json:"type"`
				Subtype       string `json:"subtype"`
				CompactResult string `json:"compact_result"`
				CompactError  string `json:"compact_error"`
			}
			_ = json.Unmarshal(out.Message, &status)
			if status.Type == "system" && status.Subtype == "compact_boundary" {
				turn.compacted = true
			}
			if status.CompactResult == "failed" {
				turn.compactError = status.CompactError
			}
		}
		r.normMu.Lock()
		events := norm.Accept(out.Message)
		sessionID := norm.SessionID()
		result := norm.TakeResult()
		r.normMu.Unlock()
		if sessionID != "" {
			r.rememberSession(sessionID)
			if turn != nil {
				turn.trace.Once(timing.StageProviderSessionReady)
			}
		}
		events = r.routeTaskEvents(turn, events)
		if turn == nil && len(events) > 0 {
			// Claude replied on its own, e.g. to a task notification. A
			// bare result after a finished turn opens nothing.
			turn = r.startNativeTurn()
		}
		if turn != nil {
			if id := transcriptMessageID(out.Message); id != "" {
				r.mu.Lock()
				turn.lastMessageID = id
				r.mu.Unlock()
			}
			r.appendOutput(turn, events)
		}
		if result != nil && turn != nil && !awaitingPrompt {
			switch {
			case turn.storageFailed:
				r.finish(turn, "failed", &turnError{kind: "storage_limit", message: ErrStorageBacklog.Error()})
			case cancelRequested:
				r.finish(turn, "cancelled", nil)
			case result.IsError:
				r.finish(turn, "failed", &turnError{kind: "provider_error", message: result.Error})
			case turn.compacting && (!turn.compacted || turn.compactError != ""):
				message := turn.compactError
				if message == "" {
					message = result.Output
				}
				if message == "" {
					message = "The provider did not compact this conversation; there may not be enough history"
				}
				r.finish(turn, "failed", &turnError{kind: "compaction_failed", message: message})
			default:
				r.finish(turn, "completed", nil)
			}
		}
	case "error":
		ended := out.Error.Code == "query_ended" || out.Error.Code == "sdk_error"
		r.mu.Lock()
		current := r.adapter != nil && r.norm == norm
		if !current && ended {
			r.endedNorm = norm
		}
		r.mu.Unlock()
		if !current {
			return // a retired adapter must not touch its successor's turn
		}
		if turn != nil {
			status := "failed"
			if cancelRequested {
				status = "cancelled"
			}
			r.finish(turn, status, &turnError{kind: "provider_exited", message: out.Error.Message})
		}
		if ended {
			r.retireAdapter(norm)
		}
	}
}

// retireAdapter drops an adapter whose Query ended: its process lives on but
// can no longer serve a turn, so the next send has to start a new one. It
// runs on the adapter's reader, so closing happens in the background.
func (r *ClaudeRunner) retireAdapter(norm *claude.Normalizer) {
	r.mu.Lock()
	closeRetired := r.detachAdapterLocked(norm)
	r.mu.Unlock()
	if closeRetired != nil {
		closeRetired()
	}
}

// detachAdapterLocked takes norm's adapter off the runner and returns what closes
// it in the background, or nil if it is no longer the runner's; r.mu is held.
func (r *ClaudeRunner) detachAdapterLocked(norm *claude.Normalizer) func() {
	if r.adapter == nil || r.norm != norm {
		return nil
	}
	adapter, release := r.adapter, r.releaseContext
	r.adapter, r.releaseContext = nil, nil
	return func() {
		go func() {
			if release != nil {
				release()
			}
			ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
			defer cancel()
			_ = adapter.Close(ctx)
		}()
	}
}

func (r *ClaudeRunner) appendOutput(turn *claudeTurn, events []claude.Event) {
	if turn.storageFailed || len(events) == 0 {
		return
	}
	out := make([]outputEvent, len(events))
	for i, e := range events {
		out[i] = claudeOutput(e)
	}
	markProviderOutput(turn.trace, out)
	batch := r.rec.outputBatch(turn.id, out)
	if err := r.batcher.Append(batch, false); err != nil {
		r.mu.Lock()
		turn.storageFailed = true
		adapter := r.adapter
		r.mu.Unlock()
		if adapter != nil {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), interruptDeadline)
				defer cancel()
				_, _ = adapter.Request(ctx, "interrupt", map[string]any{"deadline_ms": interruptDeadline.Milliseconds()})
			}()
		}
	}
}

// finish completes turn once: open items get final events, then
// turn.completed and the turn row are committed together.
func (r *ClaudeRunner) finish(turn *claudeTurn, status string, failure *turnError) {
	r.mu.Lock()
	if r.turn != turn {
		r.mu.Unlock()
		return
	}
	r.turn = nil
	r.gate.idleSince = r.now()
	norm := r.norm
	// Off the runner before anyone can see the turn ended, so the next send cannot
	// pick up an adapter on old settings.
	var closeRetired func()
	if r.restartAfterTurn {
		closeRetired = r.detachAdapterLocked(norm)
	}
	r.mu.Unlock()
	if closeRetired != nil {
		closeRetired()
	}
	if !turn.unlimited {
		defer r.limiter.Release()
	}

	itemStatus := status
	if itemStatus == "completed" {
		// A completed turn still closes items Claude never finished.
		itemStatus = "cancelled"
	}
	var finals []outputEvent
	if norm != nil {
		r.normMu.Lock()
		for _, e := range norm.Close(itemStatus) {
			finals = append(finals, claudeOutput(e))
		}
		r.normMu.Unlock()
	}
	// Critical: committed with the output before it, never rejected by the
	// pending budget.
	r.mu.Lock()
	lastMessageID := turn.lastMessageID
	r.mu.Unlock()
	_ = r.batcher.Append(r.rec.completionBatch(turn.id, lastMessageID, status, failure, finals), true)
	finishTrace(turn.trace, status, failure)
	if r.onTurnEnd != nil {
		r.onTurnEnd(turn.id)
	}
}

func (r *ClaudeRunner) rememberSession(id string) {
	r.mu.Lock()
	changed := r.session.ProviderSessionID != id
	r.session.ProviderSessionID = id
	r.mu.Unlock()
	if changed {
		_ = r.batcher.Append(r.rec.sessionBatch(id), true)
	}
}

// ApplySettings changes the provider settings. A live adapter is updated in place;
// later adapter starts use the new values. The settings are stored by now, so a push
// that fails is no error for the caller: the adapter goes away instead of staying on
// the old ones, and the next start uses them.
func (r *ClaudeRunner) ApplySettings(_ context.Context, st store.ConversationSettings) error {
	r.mu.Lock()
	fields := map[string]any{}
	for k, v := range r.session.Fields {
		fields[k] = v
	}
	delete(fields, "model")
	delete(fields, "reasoning_effort")
	if st.Model != nil {
		fields["model"] = *st.Model
	}
	if st.ReasoningEffort != nil {
		fields["reasoning_effort"] = *st.ReasoningEffort
	}
	fields["permission_mode"] = st.PermissionMode
	r.session.Fields = fields
	r.settingsVersion++
	r.settings = &st
	adapter := r.adapter
	r.mu.Unlock()
	if adapter != nil {
		_ = r.syncSettings(adapter)
	}
	return nil
}

// syncSettings brings adapter up to the newest settings. A push does not depend on
// the context of whoever asked, since stopping half way would leave the adapter on
// settings nobody chose. A failure leaves it on unknown ones, so it is retired.
func (r *ClaudeRunner) syncSettings(adapter ClaudeAdapter) error {
	r.pushMu.Lock()
	defer r.pushMu.Unlock()
	for {
		r.mu.Lock()
		if r.adapter != adapter || r.adapterVersion == r.settingsVersion {
			r.mu.Unlock()
			return nil
		}
		version, st := r.settingsVersion, *r.settings
		r.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), settingsPushDeadline)
		err := r.pushSettings(ctx, adapter, st)
		cancel()
		if err != nil {
			r.settingsNotApplied(adapter)
			return err
		}
		r.mu.Lock()
		if r.adapter == adapter {
			r.adapterVersion = version
		}
		r.mu.Unlock()
	}
}

// settingsNotApplied retires an adapter that could not take new settings, or marks
// it for restart when a turn is running on it. A turn whose prompt has not gone out
// is not running on it yet: that send starts a new adapter.
func (r *ClaudeRunner) settingsNotApplied(adapter ClaudeAdapter) {
	r.mu.Lock()
	if r.adapter != adapter {
		r.mu.Unlock()
		return
	}
	if r.turn != nil && r.turn.inputSent {
		r.restartAfterTurn = true
		r.mu.Unlock()
		return
	}
	norm := r.norm
	r.mu.Unlock()
	r.retireAdapter(norm)
}

// pushSettings sends permission first: it is the setting that must not stay behind
// if a later request fails.
func (r *ClaudeRunner) pushSettings(ctx context.Context, adapter ClaudeAdapter, st store.ConversationSettings) error {
	if _, err := adapter.Request(ctx, "set_permission_mode", map[string]any{"mode": st.PermissionMode}); err != nil {
		return err
	}
	var model any
	if st.Model != nil {
		model = *st.Model
	}
	if _, err := adapter.Request(ctx, "set_model", map[string]any{"model": model}); err != nil {
		return err
	}
	var effort any
	if st.ReasoningEffort != nil {
		effort = *st.ReasoningEffort
	}
	_, err := adapter.Request(ctx, "apply_flag_settings", map[string]any{"settings": map[string]any{"effortLevel": effort}})
	return err
}

func claudeOutput(e claude.Event) outputEvent {
	out := outputEvent{ItemID: e.ItemID, Type: e.Type, Final: e.Final, Payload: e.Payload}
	if e.Image != nil {
		out.ImageMime, out.ImageData = e.Image.MimeType, e.Image.Data
	}
	return out
}

// currentContext is what current_context reports while the adapter runs.
func (r *ClaudeRunner) currentContext() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := map[string]any{"conversation_id": r.rec.conversationID, "references": []Reference{}}
	if r.turn != nil {
		current["turn_id"], current["references"] = r.turn.id, r.turn.references
		current["create_task_requested"] = r.turn.createTask
	}
	return current
}

// contextName is the context MCP server name, defaulting to Multica's.
func contextName(name string) string {
	if name == "" {
		return contextmcp.Name
	}
	return name
}

// transcriptMessageID is the uuid of an SDK message that Claude keeps in
// its session transcript: user and assistant messages.
func transcriptMessageID(raw json.RawMessage) string {
	var m struct {
		Type            string `json:"type"`
		UUID            string `json:"uuid"`
		ParentToolUseID string `json:"parent_tool_use_id"`
	}
	if json.Unmarshal(raw, &m) != nil || m.ParentToolUseID != "" || (m.Type != "user" && m.Type != "assistant") {
		return ""
	}
	return m.UUID
}

// routeTaskEvents records events of background tasks started by another
// turn under that turn and returns the rest. A task's start in the
// current turn is remembered; a task's final event outside any turn is
// queued for delivery.
func (r *ClaudeRunner) routeTaskEvents(turn *claudeTurn, events []claude.Event) []claude.Event {
	var rest []claude.Event
	var ended []BackgroundResult
	r.mu.Lock()
	for _, e := range events {
		if parent, ok := e.Payload["parent_tool_use_id"].(string); ok && parent != "" {
			if owner := r.taskTurns["tool:"+parent]; owner != "" {
				_ = r.batcher.Append(r.rec.outputBatch(owner, []outputEvent{claudeOutput(e)}), e.Final)
			}
			continue
		}
		if e.Payload["kind"] != "background_task" {
			if turn != nil && e.Type == "item.started" && (e.Payload["kind"] == "tool_call" || e.Payload["kind"] == "command") {
				r.taskTurns["tool:"+e.ItemID] = turn.id
			}
			rest = append(rest, e)
			continue
		}
		owner, known := r.taskTurns[e.ItemID]
		if !known {
			if turn == nil {
				continue // a task this adapter never saw start
			}
			r.taskTurns[e.ItemID] = turn.id
			if parent, ok := e.Payload["tool_use_id"].(string); ok && parent != "" {
				r.taskTurns["tool:"+parent] = turn.id
			}
			rest = append(rest, e)
			continue
		}
		if e.Final {
			delete(r.taskTurns, e.ItemID)
		}
		if turn != nil && owner == turn.id {
			rest = append(rest, e)
			continue
		}
		_ = r.batcher.Append(r.rec.outputBatch(owner, []outputEvent{claudeOutput(e)}), e.Final)
		if e.Final && turn == nil {
			taskID, _ := e.Payload["task_id"].(string)
			if !r.stopped[taskID] {
				ended = append(ended, BackgroundResult{SourceTurnID: owner, State: e.Payload})
			}
		}
	}
	r.ended = append(r.ended, ended...)
	pending := r.ended
	r.mu.Unlock()
	if len(ended) > 0 && r.onBackgroundEnded != nil {
		r.onBackgroundEnded(pending)
	}
	return rest
}

// TakeBackgroundResults returns and clears the results waiting for
// delivery.
func (r *ClaudeRunner) TakeBackgroundResults() []BackgroundResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.ended
	r.ended = nil
	return out
}

// startNativeTurn records a turn Claude started without a prompt from
// Multica. It answers any background results waiting for delivery.
func (r *ClaudeRunner) startNativeTurn() *claudeTurn {
	turn := &claudeTurn{id: "turn-native-" + newID(), inputSent: true}
	if r.nativeTrace != nil {
		turn.trace = r.nativeTrace(turn.id)
	}
	turn.unlimited = r.limiter.TryAcquire() != nil
	r.mu.Lock()
	if r.turn != nil {
		existing := r.turn
		r.mu.Unlock()
		if !turn.unlimited {
			r.limiter.Release()
		}
		return existing
	}
	r.turn = turn
	r.ended = nil
	r.mu.Unlock()
	_ = r.batcher.Append(r.rec.startBatch(SendRequest{TurnID: turn.id, RequestID: turn.id, Hidden: true}), true)
	return turn
}

// Running reports whether a turn is in progress.
func (r *ClaudeRunner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.turn != nil || r.interrupting
}
