package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/codex"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
	"github.com/multica-ai/multica/server/pkg/codexappserver"
)

// CodexServer is a running app-server, implemented by *codex.Server.
type CodexServer interface {
	Request(ctx context.Context, method string, params any) (json.RawMessage, error)
	Respond(id int, result any)
	RespondError(id int, code int, message string)
	Stop()
	Done() <-chan struct{}
	Err() error
}

// StartCodexServer launches app-server delivering notifications and server
// requests to handler, with env added to its environment.
type StartCodexServer func(ctx context.Context, handler codex.Handler, env []string) (CodexServer, error)

// ContextTokenEnv carries the context MCP route token to app-server, which
// sends it as the bearer token; Codex configuration never holds the value.
const ContextTokenEnv = "MULTICA_CONTEXT_MCP_TOKEN"

// CodexSession is how the conversation runs Codex.
type CodexSession struct {
	Cwd string
	// ThreadID is the native thread to resume; empty starts a new one.
	ThreadID        string
	Model           *string
	ReasoningEffort *string
	PermissionMode  string
	// DeveloperInstructions are appended to Codex's own instructions.
	DeveloperInstructions string
}

// ErrNoRunningTurn rejects steering when no turn runs.
var ErrNoRunningTurn = errors.New("local conversation: no running turn")

const (
	codexInterruptRetries    = 20
	codexInterruptRetryDelay = 100 * time.Millisecond
	threadSetupTimeout       = 60 * time.Second
	codexInterruptWait       = 15 * time.Second
	maxBufferedTurnFrame     = 1000
)

// CodexRunner runs one Codex conversation on its own app-server process:
// it starts or resumes the native thread, records each turn, normalizes the
// active turn's notifications, and completes the turn on turn/completed or
// when the process goes away. Like the TypeScript host it has no approval
// surface: approval requests are declined, never treated as approved.
type CodexRunner struct {
	// catalogDefault is what catalogServer's model/list answered.
	catalogServer  CodexServer
	catalogDefault string
	noteDefault    func(string)
	processTrace   func() *timing.Trace

	rec     turnRecorder
	start   StartCodexServer
	batcher *Batcher
	limiter *Limiter
	context string
	bind    ContextBinder
	// claimSpare takes a running process for a conversation with no thread yet.
	claimSpare func(ctx context.Context, provider string) *parkedSpare
	// onTurnEnd runs after a turn's completion is recorded.
	onTurnEnd func(turnID string)

	mu      sync.Mutex
	session CodexSession
	server  CodexServer
	native  *codexNative
	turn    *codexTurn
	gate    prepareGate
	// restart is set when the running server's loaded thread has settings
	// that no longer apply: thread/resume ignores new model and permission
	// values on a loaded thread, so only a new process applies them.
	restart bool
	// contextURL is the running server's context route; empty without one.
	contextURL string
	// contextToken is the route's bearer token, handed to each process that
	// uses the route.
	contextToken string
	// systemPrompt is the bound context's prompt, added to Codex's
	// developer instructions.
	systemPrompt   string
	releaseContext func()
}

type codexTurn struct {
	id           string
	compacting   bool
	compacted    bool
	threadID     string
	nativeTurnID string
	// normMu serializes the normalizer between the app-server reader and
	// finish, which may run from the exit or interrupt path.
	normMu          sync.Mutex
	norm            *codex.Normalizer
	buffered        []codexappserver.Frame
	cancelRequested bool
	// active is set by the turn's first frame: Codex rejects turn/interrupt
	// until the turn has started, so an earlier interrupt waits for it.
	active        bool
	interrupted   bool
	storageFailed bool
	references    []Reference
	createTask    bool
	trace         *timing.Trace
	stop          *stopSignal
}

type codexNative struct {
	DeveloperInstructions string         `json:"developer_instructions"`
	Model                 string         `json:"model"`
	ReasoningEffort       string         `json:"model_reasoning_effort"`
	MCPServers            map[string]any `json:"mcp_servers"`
	ApprovalPolicy        any            `json:"approval_policy"`
	ApprovalsReviewer     string         `json:"approvals_reviewer"`
	SandboxMode           string         `json:"sandbox_mode"`
}

// CodexRunnerOptions configures a runner.
type CodexRunnerOptions struct {
	ConversationID string
	ScopeID        int64
	Files          AttachmentFiles
	Start          StartCodexServer
	Session        CodexSession
	Batcher        *Batcher
	Limiter        *Limiter
	ContextMCPName string
	Now            func() time.Time
	// OnTurnEnd, when set, runs after a turn's completion is recorded.
	OnTurnEnd func(turnID string)
	// Context, when set, gives app-server Multica's context tools.
	Context ContextBinder
	// ClaimSpare, when set, offers a running process for a new conversation.
	ClaimSpare func(ctx context.Context, provider string) *parkedSpare
	// NoteDefault, when set, hears the default model a running Codex reported.
	NoteDefault func(model string)
	// ProcessTrace, when set, records what happens to the process between turns.
	ProcessTrace func() *timing.Trace
}

// NewCodexRunner returns a runner for one conversation.
func NewCodexRunner(opts CodexRunnerOptions) *CodexRunner {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &CodexRunner{
		rec:   turnRecorder{conversationID: opts.ConversationID, scopeID: opts.ScopeID, files: opts.Files, now: opts.Now},
		start: opts.Start, batcher: opts.Batcher, limiter: opts.Limiter, context: contextName(opts.ContextMCPName), session: opts.Session,
		bind: opts.Context, claimSpare: opts.ClaimSpare, onTurnEnd: opts.OnTurnEnd, noteDefault: opts.NoteDefault, processTrace: opts.ProcessTrace, gate: newPrepareGate(),
	}
}

// Send starts a turn; it returns once the input is durable and Codex
// accepted turn/start.
func (r *CodexRunner) Send(ctx context.Context, req SendRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := detachSend(ctx)
	defer cancel()
	r.mu.Lock()
	if r.gate.closed {
		r.mu.Unlock()
		return ErrClosing
	}
	if r.turn != nil {
		r.mu.Unlock()
		return ErrTurnRunning
	}
	trace := req.Trace
	if err := r.limiter.TryAcquire(); err != nil {
		r.mu.Unlock()
		trace.MarkFailed(timing.StageRunnerLimiter, timing.ReasonConcurrencyLimit)
		return err
	}
	turn := &codexTurn{id: req.TurnID, norm: codex.NewNormalizer(r.context), references: req.References, createTask: req.CreateTask, compacting: req.Compact, trace: trace, stop: newStopSignal()}
	r.turn = turn
	r.gate.sendReady(trace, r.server != nil && !r.restart)
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
	ready, failKind, err := r.awaitReady(ctx, turn, trace)
	if err == nil && ready.server != nil && ready.threadID == "" {
		failKind = "provider_error"
		ready.threadID, err = r.thread(ctx, ready.server, ready.native, trace)
	}
	prepared(err)
	if err != nil {
		r.finish(turn, "failed", &turnError{kind: failKind, message: err.Error()})
		return err
	}
	if ready.server == nil {
		r.stoppedBeforeDispatch(turn)
		return nil
	}
	server, threadID := ready.server, ready.threadID
	r.mu.Lock()
	turn.threadID = threadID
	effort := ready.native.ReasoningEffort
	if r.session.ReasoningEffort != nil {
		effort = *r.session.ReasoningEffort
	}
	r.mu.Unlock()
	if r.stoppedBeforeDispatch(turn) {
		return nil
	}

	submitted := trace.Begin(timing.StageProviderTurnStart)
	if req.Compact {
		_, err := server.Request(ctx, "thread/compact/start", map[string]any{"threadId": threadID})
		submitted(err)
		if err != nil {
			if codexOutcomeUnknown(err) {
				r.finish(turn, "failed", &turnError{kind: "outcome_unknown", message: err.Error()})
				return fmt.Errorf("%w: %w", ErrOutcomeUnknown, err)
			}
			r.finish(turn, "failed", &turnError{kind: "compaction_failed", message: err.Error()})
		}
		return err
	}

	var effortParam any
	if effort != "" {
		effortParam = effort
	}
	raw, err := server.Request(ctx, "turn/start", map[string]any{
		"threadId": threadID, "effort": effortParam,
		"input": []map[string]any{{"type": "text", "text": req.prompt(), "text_elements": []any{}}},
	})
	submitted(err)
	if err != nil {
		if codexOutcomeUnknown(err) {
			r.finish(turn, "failed", &turnError{kind: "outcome_unknown", message: err.Error()})
			return fmt.Errorf("%w: %w", ErrOutcomeUnknown, err)
		}
		r.finish(turn, "failed", &turnError{kind: "provider_error", message: err.Error()})
		return err
	}
	var started struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(raw, &started) != nil || started.Turn.ID == "" {
		r.finish(turn, "failed", &turnError{kind: "provider_error", message: "Codex returned no turn ID"})
		return errors.New("codex returned no turn id")
	}
	// Only the turn/start response establishes the turn; replay frames that
	// arrived before it are filtered by turn id now.
	r.mu.Lock()
	turn.nativeTurnID = started.Turn.ID
	buffered := turn.buffered
	turn.buffered = nil
	r.mu.Unlock()
	for _, frame := range buffered {
		r.handle(frame)
	}
	return nil
}

// codexReady is a server a turn can use; an empty threadID still needs thread.
type codexReady struct {
	server   CodexServer
	native   *codexNative
	threadID string
}

// awaitReady returns once the runner has a usable server, starting or joining
// the one prepare task. A turn that was stopped or ended meanwhile gets a zero
// codexReady. A settings change during a task sets restart, which loops here so
// the prompt never reaches a thread still loaded with the old settings.
func (r *CodexRunner) awaitReady(ctx context.Context, turn *codexTurn, trace *timing.Trace) (codexReady, string, error) {
	var last *prepareTask
	for {
		r.mu.Lock()
		if r.turn != turn {
			r.mu.Unlock()
			return codexReady{}, "", nil
		}
		task := r.gate.task
		if task == nil {
			if r.server != nil && !r.restart {
				ready := codexReady{server: r.server, native: r.native}
				if last != nil {
					ready.threadID = last.threadID
				}
				r.mu.Unlock()
				return ready, "", nil
			}
			task = r.startPrepareLocked(trace, false)
		}
		r.mu.Unlock()
		stopped, err := waitPrepare(ctx, task, turn.stop.wait())
		if stopped {
			return codexReady{}, "", nil
		}
		if err != nil {
			return codexReady{}, "provider_unavailable", err
		}
		// A failed preheat was only ever an optimization: the send starts its own.
		if task.err != nil && !task.preheat {
			return codexReady{}, task.failKind, task.err
		}
		last = task
		if task.err != nil {
			last = nil
		}
	}
}

// startPrepareLocked starts the prepare task; r.mu is held.
func (r *CodexRunner) startPrepareLocked(trace *timing.Trace, preheat bool) *prepareTask {
	task, ctx, cancel := r.gate.begin(preheat)
	if preheat {
		trace.SetPrepare(timing.PreparePreheated, task.id)
	} else {
		trace.SetPrepare(timing.PrepareCold, task.id)
	}
	go func() {
		defer cancel()
		threadID, failKind, err := r.prepare(ctx, trace, preheat)
		r.mu.Lock()
		task.threadID, task.failKind, task.err = threadID, failKind, err
		r.gate.finish(task, r.rec.now())
		r.mu.Unlock()
		close(task.done)
	}()
	return task
}

func (r *CodexRunner) prepare(ctx context.Context, trace *timing.Trace, preheat bool) (threadID, failKind string, err error) {
	server, native, spared, err := r.ensureServer(ctx, trace, true)
	if err != nil {
		return "", "provider_unavailable", err
	}
	threadID, err = r.thread(ctx, server, native, trace)
	if err != nil && spared && ctx.Err() == nil {
		// No message was submitted, so the normal start can simply take over.
		r.dropServer(server)
		trace.MarkReason(timing.StageSpareRejected, timing.ReasonBindFailed)
		trace.SetPrepare(timing.PrepareCold, "")
		if server, native, _, err = r.ensureServer(ctx, trace, false); err != nil {
			return "", "provider_unavailable", err
		}
		threadID, err = r.thread(ctx, server, native, trace)
	}
	if err != nil {
		// Nothing depends on a preheat's process, and one that could not load
		// its thread may be what a later send would hang on.
		if preheat {
			r.dropServer(server)
		}
		return "", "provider_error", err
	}
	return threadID, "", nil
}

// dropServer stops server if it is still the runner's.
func (r *CodexRunner) dropServer(server CodexServer) {
	r.mu.Lock()
	if r.server != server {
		r.mu.Unlock()
		return
	}
	release := r.detachServerLocked()
	r.mu.Unlock()
	if release != nil {
		release()
	}
	server.Stop()
}

// detachServerLocked forgets the process and what was bound to it; r.mu is held.
func (r *CodexRunner) detachServerLocked() (release func()) {
	release = r.releaseContext
	r.server, r.native, r.restart, r.contextURL, r.contextToken, r.releaseContext, r.systemPrompt = nil, nil, false, "", "", nil, ""
	return release
}

// Prepare gets an existing conversation's thread loaded ahead of its next send,
// without taking a turn slot or sending anything. It does nothing when a turn
// runs or the process is ready, and a conversation without a native thread is
// never prepared: starting one here would leave an empty thread behind. ctx only
// bounds the wait; the task continues for whoever else waits on it.
func (r *CodexRunner) Prepare(ctx context.Context, trace *timing.Trace) error {
	r.mu.Lock()
	if r.gate.closed {
		r.mu.Unlock()
		return ErrClosing
	}
	if r.turn != nil || r.session.ThreadID == "" {
		r.mu.Unlock()
		return nil
	}
	task := r.gate.task
	if task == nil {
		if r.server != nil && !r.restart {
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

// Evict stops an idle process to free its resources. The runner and thread stay;
// the next send resumes on a new process. The process leaves the runner under
// the lock, so a send can never pick up one that is exiting. A settings restart
// that failed left its context binding with no process; that is evicted the same way.
func (r *CodexRunner) Evict(context.Context) bool {
	r.mu.Lock()
	if r.turn != nil || r.gate.task != nil || !r.holdsResourcesLocked() {
		r.mu.Unlock()
		return false
	}
	server := r.server
	release := r.detachServerLocked()
	r.gate.preheated = false
	r.mu.Unlock()
	if release != nil {
		release()
	}
	if server != nil {
		server.Stop()
	}
	return true
}

func (r *CodexRunner) holdsResourcesLocked() bool {
	return r.server != nil || r.releaseContext != nil
}

// IdleProcess reports the runner's process when it is doing nothing.
func (r *CodexRunner) IdleProcess() (idleProcess, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.holdsResourcesLocked() || r.turn != nil || r.gate.task != nil {
		return idleProcess{}, false
	}
	return idleProcess{provider: "codex", scopeID: r.rec.scopeID, since: r.gate.idleSince, preheated: r.gate.preheated, evictable: true}, true
}

// stoppedBeforeDispatch ends a turn cancelled during preparation and reports
// whether the turn is over, so its prompt must not be sent.
func (r *CodexRunner) stoppedBeforeDispatch(turn *codexTurn) bool {
	r.mu.Lock()
	ended, cancelled := r.turn != turn, turn.cancelRequested
	r.mu.Unlock()
	if cancelled {
		r.finish(turn, "cancelled", nil)
	}
	return ended || cancelled
}

// codexOutcomeUnknown reports whether a dispatch ended without an answer from
// Codex, so the request may or may not have taken effect.
func codexOutcomeUnknown(err error) bool {
	return codex.IsClosed(err) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// Interrupt asks Codex to stop the running turn. A turn Codex has not
// started yet is interrupted as soon as it starts. If Codex does not
// confirm in time, the process is stopped and the turn ends as cancelled.
func (r *CodexRunner) Interrupt(ctx context.Context) error {
	r.mu.Lock()
	turn, server := r.turn, r.server
	if turn == nil || turn.cancelRequested {
		r.mu.Unlock()
		return nil
	}
	turn.cancelRequested = true
	active := turn.active
	if active {
		turn.interrupted = true
	} else {
		turn.stop.fire()
	}
	r.mu.Unlock()
	if active {
		if err := r.interruptNative(ctx, turn, server); err != nil {
			r.mu.Lock()
			turn.cancelRequested, turn.interrupted = false, false
			r.mu.Unlock()
			return err
		}
	}
	if server != nil {
		go r.confirmInterrupt(turn, server)
	}
	return nil
}

func (r *CodexRunner) interruptNative(ctx context.Context, turn *codexTurn, server CodexServer) error {
	r.mu.Lock()
	threadID, nativeTurnID := turn.threadID, turn.nativeTurnID
	r.mu.Unlock()
	// Codex announces turn/started slightly before the turn can be interrupted.
	for attempt := 0; ; attempt++ {
		_, err := server.Request(ctx, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": nativeTurnID})
		if err == nil || !strings.Contains(err.Error(), "no active turn") || attempt == codexInterruptRetries {
			return err
		}
		select {
		case <-time.After(codexInterruptRetryDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
		r.mu.Lock()
		ended := r.turn != turn
		r.mu.Unlock()
		if ended {
			return nil
		}
	}
}

// confirmInterrupt stops the app-server when an interrupted turn is still
// running after codexInterruptWait.
func (r *CodexRunner) confirmInterrupt(turn *codexTurn, server CodexServer) {
	select {
	case <-time.After(codexInterruptWait):
		r.mu.Lock()
		stillRunning := r.turn == turn
		r.mu.Unlock()
		if stillRunning {
			server.Stop()
			r.finish(turn, "cancelled", &turnError{kind: "interrupt_unconfirmed", message: "Codex did not confirm interruption"})
		}
	case <-server.Done():
	}
}

// Steer adds input to the running turn.
func (r *CodexRunner) Steer(ctx context.Context, text string) error {
	r.mu.Lock()
	turn, server := r.turn, r.server
	var threadID, nativeTurnID string
	if turn != nil {
		threadID, nativeTurnID = turn.threadID, turn.nativeTurnID
	}
	r.mu.Unlock()
	if turn == nil || server == nil || nativeTurnID == "" {
		return ErrNoRunningTurn
	}
	_, err := server.Request(ctx, "turn/steer", map[string]any{"threadId": threadID, "expectedTurnId": nativeTurnID,
		"input": []map[string]any{{"type": "text", "text": text, "text_elements": []any{}}}})
	return err
}

// ApplySettings changes settings for the next turn; a running turn keeps
// the settings it started with. Reasoning effort travels with turn/start, so
// only a model or permission change restarts the process before the next turn.
func (r *CodexRunner) ApplySettings(_ context.Context, st store.ConversationSettings) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.server != nil && (!sameModel(r.session.Model, st.Model) || r.session.PermissionMode != st.PermissionMode) {
		r.restart = true
	}
	r.session.Model, r.session.ReasoningEffort, r.session.PermissionMode = st.Model, st.ReasoningEffort, st.PermissionMode
	return nil
}

// sameModel compares two optional settings values.
func sameModel(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Close stops app-server, failing a running turn.
func (r *CodexRunner) Close(ctx context.Context) error {
	r.mu.Lock()
	r.gate.closed = true
	r.gate.cancel()
	task := r.gate.task
	server, turn, release := r.server, r.turn, r.releaseContext
	r.server, r.releaseContext, r.contextToken = nil, nil, ""
	r.mu.Unlock()
	if turn != nil {
		r.finish(turn, "failed", &turnError{kind: "provider_exited", message: "the conversation was closed"})
	}
	if server != nil {
		server.Stop()
	}
	if release != nil {
		release()
	}
	waitClosed(ctx, task)
	return nil
}

// ensureServer runs only inside the prepare task, which is what keeps two
// callers from starting two processes.
func (r *CodexRunner) ensureServer(ctx context.Context, trace *timing.Trace, allowSpare bool) (CodexServer, *codexNative, bool, error) {
	r.mu.Lock()
	if r.server != nil && !r.restart {
		server, native := r.server, r.native
		r.mu.Unlock()
		return server, native, false, nil
	}
	stale := r.server
	r.server, r.native, r.restart = nil, nil, false
	cwd := r.session.Cwd
	// A settings restart keeps the context binding: releasing it would also
	// reset the conversation's Computer Use runtime.
	bound, token := r.releaseContext != nil, r.contextToken
	fresh := r.session.ThreadID == ""
	r.mu.Unlock()
	if stale != nil {
		stale.Stop()
	}
	if allowSpare && fresh && !bound && stale == nil && r.claimSpare != nil {
		if spare := r.claimSpare(ctx, "codex"); spare != nil {
			server, native, err := r.adoptSpare(ctx, spare, cwd, trace)
			if err == nil {
				return server, native, true, nil
			}
			if errors.Is(err, ErrClosing) {
				return nil, nil, false, err
			}
		}
	}

	var env []string
	if r.bind != nil {
		if !bound {
			var err error
			if token, err = contextmcp.NewToken(); err != nil {
				return nil, nil, false, err
			}
		}
		env = []string{ContextTokenEnv + "=" + token}
	}
	began := trace.Now()
	server, err := r.start(ctx, r.handle, env)
	markProcessStart(trace, timing.StageProviderSpawn, timing.StageProviderHandshake, began, server, err)
	if err != nil {
		return nil, nil, false, err
	}
	native, err := r.readConfig(ctx, server, cwd, trace)
	if err != nil {
		server.Stop()
		return nil, nil, false, err
	}
	contextURL, release, systemPrompt := "", func() {}, ""
	if r.bind != nil && !bound {
		bound := trace.Begin(timing.StageContextBind)
		binding, rel, err := r.bind(context.Background(), token, r.currentContext)
		bound(err)
		if err != nil {
			server.Stop()
			return nil, nil, false, err
		}
		contextURL, release, systemPrompt = binding.URL, rel, binding.SystemPrompt
	}
	r.mu.Lock()
	if r.gate.closed {
		r.mu.Unlock()
		if !bound {
			release()
		}
		server.Stop()
		return nil, nil, false, ErrClosing
	}
	r.server, r.native = server, native
	if !bound {
		r.contextURL, r.contextToken, r.releaseContext, r.systemPrompt = contextURL, token, release, systemPrompt
	}
	r.mu.Unlock()
	go r.watch(server)
	return server, native, false, nil
}

// readConfig reads the native configuration for the conversation's directory.
// The directory comes from the request, never from where the process started:
// a spare runs elsewhere, and what it reports must still be this project's.
func (r *CodexRunner) readConfig(ctx context.Context, server CodexServer, cwd string, trace *timing.Trace) (*codexNative, error) {
	configured := trace.Begin(timing.StageProviderConfig)
	raw, err := server.Request(ctx, "config/read", map[string]any{"cwd": cwd, "includeLayers": false})
	configured(err)
	if err != nil {
		return nil, err
	}
	var result struct {
		Config codexNative `json:"config"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, errors.New("invalid Codex configuration response")
	}
	if _, reserved := result.Config.MCPServers[r.context]; reserved {
		return nil, fmt.Errorf("native Codex settings use the reserved Multica MCP name %s", r.context)
	}
	return &result.Config, nil
}

// adoptSpare binds a spare to this conversation. On failure the spare is gone
// and nothing was submitted, so the caller starts a process the normal way.
func (r *CodexRunner) adoptSpare(ctx context.Context, spare *parkedSpare, cwd string, trace *timing.Trace) (CodexServer, *codexNative, error) {
	claiming := trace.Begin(timing.StageSpareClaim)
	server := spare.server
	native, err := r.readConfig(ctx, server, cwd, trace)
	contextURL, release, systemPrompt := "", func() {}, ""
	if err == nil && r.bind != nil {
		binding := trace.Begin(timing.StageContextBind)
		var bound BoundContext
		bound, release, err = r.bind(context.Background(), spare.token, r.currentContext)
		binding(err)
		if err == nil {
			contextURL, systemPrompt = bound.URL, bound.SystemPrompt
		}
	}
	claiming(err)
	if err != nil {
		spare.reject(timing.ReasonBindFailed)
		return nil, nil, err
	}
	spare.codexFrames.adopt(r.handle)
	r.mu.Lock()
	if r.gate.closed {
		r.mu.Unlock()
		release()
		server.Stop()
		return nil, nil, ErrClosing
	}
	r.server, r.native = server, native
	r.contextURL, r.contextToken, r.releaseContext, r.systemPrompt = contextURL, spare.token, release, systemPrompt
	r.mu.Unlock()
	go r.watch(server)
	trace.SetPrepare(timing.PrepareSpare, "")
	trace.Mark(timing.StageSpareClaimed)
	return server, native, nil
}

// thread starts the conversation's native thread or resumes it.
func (r *CodexRunner) thread(ctx context.Context, server CodexServer, native *codexNative, trace *timing.Trace) (string, error) {
	r.mu.Lock()
	session := r.session
	r.mu.Unlock()

	model, err := r.resolveModel(ctx, server, session, native, trace)
	if err != nil {
		return "", err
	}
	// Native MCP servers stay disabled in local conversations.
	mcp := map[string]any{}
	for name := range native.MCPServers {
		mcp[name] = map[string]any{"enabled": false}
	}
	r.mu.Lock()
	contextURL := r.contextURL
	r.mu.Unlock()
	if contextURL != "" {
		mcp[r.context] = map[string]any{"url": contextURL, "bearer_token_env_var": ContextTokenEnv, "enabled": true, "required": true,
			"default_tools_approval_mode": "approve", "tool_timeout_sec": 3600}
	}
	config := map[string]any{"mcp_servers": mcp}
	if session.ReasoningEffort != nil {
		config["model_reasoning_effort"] = *session.ReasoningEffort
	}
	params := map[string]any{"cwd": session.Cwd, "config": config}
	if model != "" {
		params["model"] = model
	} else {
		params["model"] = nil
	}
	instructions := native.DeveloperInstructions
	r.mu.Lock()
	extra := []string{session.DeveloperInstructions, r.systemPrompt}
	r.mu.Unlock()
	for _, part := range extra {
		if part == "" {
			continue
		}
		if instructions != "" {
			instructions += "\n\n"
		}
		instructions += part
	}
	if instructions != "" {
		params["developerInstructions"] = instructions
	} else {
		params["developerInstructions"] = nil
	}
	switch session.PermissionMode {
	case "auto":
		params["approvalPolicy"], params["approvalsReviewer"], params["sandbox"] = "on-request", "auto_review", "workspace-write"
	case "full-access":
		params["approvalPolicy"], params["sandbox"] = "never", "danger-full-access"
	default:
		// thread/resume without these restores what the thread stored, so
		// returning from full access would silently keep it.
		if session.ThreadID != "" {
			params["approvalPolicy"], params["approvalsReviewer"], params["sandbox"] = nativePermissions(native)
		}
	}
	method, reason := "thread/start", timing.ReasonStart
	if session.ThreadID != "" {
		method, reason = "thread/resume", timing.ReasonResume
		params["threadId"], params["excludeTurns"] = session.ThreadID, true
	}
	setupCtx, cancel := context.WithTimeout(ctx, threadSetupTimeout)
	defer cancel()
	threadReady := trace.BeginReason(timing.StageThreadPrepare, reason)
	raw, err := server.Request(setupCtx, method, params)
	threadReady(err)
	if err != nil {
		return "", err
	}
	var result struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Thread.ID == "" {
		return "", errors.New("codex returned no thread id")
	}
	if result.Thread.ID != session.ThreadID {
		r.mu.Lock()
		r.session.ThreadID = result.Thread.ID
		r.mu.Unlock()
		_ = r.batcher.Append(r.rec.sessionBatch(result.Thread.ID), true)
	}
	return result.Thread.ID, nil
}

// nativePermissions are the user's own Codex permissions, with Codex's
// defaults for a trusted project where the configuration sets none.
func nativePermissions(native *codexNative) (approval any, reviewer, sandbox string) {
	approval, reviewer, sandbox = native.ApprovalPolicy, native.ApprovalsReviewer, native.SandboxMode
	if approval == nil {
		approval = "on-request"
	}
	if reviewer == "" {
		reviewer = "user"
	}
	if sandbox == "" {
		sandbox = "workspace-write"
	}
	return approval, reviewer, sandbox
}

// resolveModel applies explicit, then runtime config, then runtime catalog. A new
// thread stops short of the catalog: Codex applies its own default anyway, so
// asking would only add a blocking query. The result is never stored as a setting,
// and the catalog's answer is kept for as long as the process that gave it runs.
func (r *CodexRunner) resolveModel(ctx context.Context, server CodexServer, session CodexSession, native *codexNative, trace *timing.Trace) (string, error) {
	if session.Model != nil && *session.Model != "" {
		trace.MarkReason(timing.StageModelResolve, timing.ReasonExplicit)
		return *session.Model, nil
	}
	if native.Model != "" {
		trace.MarkReason(timing.StageModelResolve, timing.ReasonConfig)
		return native.Model, nil
	}
	if session.ThreadID == "" {
		trace.MarkReason(timing.StageModelResolve, timing.ReasonNativeDefault)
		return "", nil
	}
	r.mu.Lock()
	cached := r.catalogDefault
	if r.catalogServer != server {
		cached = ""
	}
	r.mu.Unlock()
	if cached != "" {
		trace.MarkReason(timing.StageModelResolve, timing.ReasonCatalogCached)
		return cached, nil
	}
	// An explicit model keeps resume from restoring a previous effort.
	resolved := trace.BeginReason(timing.StageModelResolve, timing.ReasonCatalog)
	model, err := codexappserver.CatalogDefaultModel(ctx, server.Request)
	resolved(err)
	if err != nil {
		return "", fmt.Errorf("could not resolve the default Codex model; select a model explicitly and retry: %w", err)
	}
	r.mu.Lock()
	r.catalogServer, r.catalogDefault = server, model
	r.mu.Unlock()
	if r.noteDefault != nil {
		r.noteDefault(model)
	}
	return model, nil
}

// handle runs on the app-server reader and must not block.
func (r *CodexRunner) handle(frame codexappserver.Frame) {
	var p struct {
		ThreadID string `json:"threadId"`
		TurnID   string `json:"turnId"`
		Turn     struct {
			ID     string `json:"id"`
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(frame.Params, &p)

	r.mu.Lock()
	turn, server := r.turn, r.server
	if turn == nil || p.ThreadID == "" || p.ThreadID != turn.threadID {
		r.mu.Unlock()
		if frame.Kind == codexappserver.FrameServerRequest && server != nil {
			declineCodexRequest(server, frame)
		}
		return
	}
	turn.trace.Once(timing.StageProviderFirstFrame)
	if turn.nativeTurnID == "" {
		if turn.compacting && frame.Method == "turn/started" && p.Turn.ID != "" {
			turn.nativeTurnID = p.Turn.ID
			buffered := turn.buffered
			turn.buffered = nil
			r.mu.Unlock()
			for _, previous := range buffered {
				r.handle(previous)
			}
			r.handle(frame)
			return
		}
		if len(turn.buffered) < maxBufferedTurnFrame {
			turn.buffered = append(turn.buffered, frame)
		}
		r.mu.Unlock()
		return
	}
	cancelRequested, storageFailed, nativeTurnID := turn.cancelRequested, turn.storageFailed, turn.nativeTurnID
	if (p.TurnID != "" && p.TurnID != nativeTurnID) || (p.Turn.ID != "" && p.Turn.ID != nativeTurnID) {
		r.mu.Unlock()
		if frame.Kind == codexappserver.FrameServerRequest && server != nil {
			declineCodexRequest(server, frame)
		}
		return
	}
	turn.active = true
	interrupt := cancelRequested && !turn.interrupted && frame.Method != "turn/completed"
	if interrupt {
		turn.interrupted = true
	}
	r.mu.Unlock()
	if interrupt {
		// handle must not block the reader; confirmInterrupt, started by
		// Interrupt, ends the turn if this request fails.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), codexInterruptWait)
			defer cancel()
			_ = r.interruptNative(ctx, turn, server)
		}()
	}

	if frame.Kind == codexappserver.FrameServerRequest {
		declineCodexRequest(server, frame)
		r.appendOutput(turn, []outputEvent{{ItemID: fmt.Sprintf("declined:%d", frame.ID), Type: "item.completed", Final: true, Payload: map[string]any{
			"kind": "error", "status": "failed", "error": map[string]any{"kind": "interaction_unsupported", "message": "Codex requested interactive input; this request was declined"},
		}}})
		return
	}
	if frame.Method == "turn/completed" {
		switch {
		case storageFailed:
			r.finish(turn, "failed", &turnError{kind: "storage_limit", message: ErrStorageBacklog.Error()})
		case p.Turn.Status == "completed" && turn.compacting && !turn.compacted:
			r.finish(turn, "failed", &turnError{kind: "compaction_failed", message: "The provider did not confirm context compaction"})
		case p.Turn.Status == "completed":
			r.finish(turn, "completed", nil)
		case p.Turn.Status == "interrupted" || cancelRequested:
			r.finish(turn, "cancelled", nil)
		default:
			message := "Codex turn failed"
			if p.Turn.Error != nil && p.Turn.Error.Message != "" {
				message = p.Turn.Error.Message
			}
			r.finish(turn, "failed", &turnError{kind: "provider_error", message: message})
		}
		return
	}
	if turn.compacting && frame.Method == "item/completed" {
		var item struct {
			Item struct {
				Type string `json:"type"`
			} `json:"item"`
		}
		if json.Unmarshal(frame.Params, &item) == nil && item.Item.Type == "contextCompaction" {
			turn.compacted = true
		}
	}
	turn.normMu.Lock()
	events := turn.norm.Accept(frame.Method, frame.Params)
	turn.normMu.Unlock()
	out := make([]outputEvent, len(events))
	for i, e := range events {
		out[i] = codexOutput(e)
	}
	r.appendOutput(turn, out)
}

func (r *CodexRunner) appendOutput(turn *codexTurn, events []outputEvent) {
	markProviderOutput(turn.trace, events)
	r.mu.Lock()
	failed := turn.storageFailed
	r.mu.Unlock()
	if failed || len(events) == 0 {
		return
	}
	if err := r.batcher.Append(r.rec.outputBatch(turn.id, events), false); err != nil {
		r.mu.Lock()
		turn.storageFailed = true
		server, threadID, nativeTurnID := r.server, turn.threadID, turn.nativeTurnID
		r.mu.Unlock()
		if server != nil && nativeTurnID != "" {
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), interruptDeadline)
				defer cancel()
				_, _ = server.Request(ctx, "turn/interrupt", map[string]any{"threadId": threadID, "turnId": nativeTurnID})
			}()
		}
	}
}

func (r *CodexRunner) watch(server CodexServer) {
	<-server.Done()
	r.mu.Lock()
	if r.server != server {
		r.mu.Unlock()
		markProcessExit(r.processTrace, nil, timing.ReasonStopped)
		return
	}
	r.server, r.native, r.contextURL, r.contextToken = nil, nil, "", ""
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
		message := "Codex app-server exited"
		if err := server.Err(); err != nil {
			message = err.Error()
		}
		r.finish(turn, "failed", &turnError{kind: "provider_exited", message: message})
	}
}

// finish completes turn once, with finals for items it left open.
func (r *CodexRunner) finish(turn *codexTurn, status string, failure *turnError) {
	r.mu.Lock()
	if r.turn != turn {
		r.mu.Unlock()
		return
	}
	r.turn = nil
	r.gate.idleSince = r.rec.now()
	r.mu.Unlock()
	defer r.limiter.Release()
	itemStatus := status
	if itemStatus == "completed" {
		itemStatus = "cancelled"
	}
	var finals []outputEvent
	turn.normMu.Lock()
	for _, e := range turn.norm.Close(itemStatus) {
		finals = append(finals, codexOutput(e))
	}
	turn.normMu.Unlock()
	_ = r.batcher.Append(r.rec.completionBatch(turn.id, turn.nativeTurnID, status, failure, finals), true)
	finishTrace(turn.trace, status, failure)
	if r.onTurnEnd != nil {
		r.onTurnEnd(turn.id)
	}
}

// declineCodexRequest answers a server request without approving it.
func declineCodexRequest(server CodexServer, frame codexappserver.Frame) {
	switch frame.Method {
	case "item/permissions/requestApproval":
		server.Respond(frame.ID, map[string]any{"permissions": map[string]any{}, "scope": "turn"})
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		server.Respond(frame.ID, map[string]any{"decision": "cancel"})
	case "mcpServer/elicitation/request":
		server.Respond(frame.ID, map[string]any{"action": "cancel", "content": nil})
	default:
		server.RespondError(frame.ID, -32601, "unsupported request: "+frame.Method)
	}
}

func codexOutput(e codex.Event) outputEvent {
	out := outputEvent{ItemID: e.ItemID, Type: e.Type, Final: e.Final, Payload: e.Payload}
	if e.Image != nil {
		out.ImageMime, out.ImageData = e.Image.MimeType, e.Image.Data
	}
	return out
}

// currentContext is what current_context reports while app-server runs.
func (r *CodexRunner) currentContext() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := map[string]any{"conversation_id": r.rec.conversationID, "references": []Reference{}}
	if r.turn != nil {
		current["turn_id"], current["references"] = r.turn.id, r.turn.references
		current["create_task_requested"] = r.turn.createTask
	}
	return current
}

// StopTasks is a no-op: Codex reports no background tasks.
func (r *CodexRunner) StopTasks(context.Context, []string) error { return nil }

// Running reports whether a turn is in progress.
func (r *CodexRunner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.turn != nil
}
