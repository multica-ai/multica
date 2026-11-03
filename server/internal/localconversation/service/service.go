package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/catalog"
	"github.com/multica-ai/multica/server/internal/localconversation/computer"
	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
	"github.com/multica-ai/multica/server/internal/localconversation/settings"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
	"github.com/multica-ai/multica/server/internal/localconversation/timing"
	"github.com/multica-ai/multica/server/internal/localconversation/workspace"
)

var (
	// ErrHostUnavailable means no Desktop of the scope is connected.
	ErrHostUnavailable = errors.New("local conversation: Multica Desktop is not connected")
	// ErrLocalHostUnavailable means no Desktop runs on this device; requests
	// about the device itself cannot go to another device's Desktop.
	ErrLocalHostUnavailable = errors.New("local conversation: open Multica Desktop on the device running this conversation; its diagnostics are not available from another device")
	// ErrClosing rejects new work while the service shuts down.
	ErrClosing = errors.New("local conversation: the service is shutting down")
	// ErrDraining rejects new turns while the daemon waits to restart.
	ErrDraining = errors.New("local conversation: the service is updating; send again shortly")
	// ErrRequestConflict means a request id was reused for different content.
	ErrRequestConflict = errors.New("local conversation: request id reused with different content")
	// ErrUnsupportedProvider means no runner serves the conversation's provider.
	ErrUnsupportedProvider = errors.New("local conversation: provider not supported by this daemon")
	// ErrInvalidSettings rejects an unknown setting value.
	ErrInvalidSettings = errors.New("local conversation: invalid settings")
)

const (
	unreferencedFileAge = time.Hour
	abandonedUploadAge  = 7 * 24 * time.Hour
	uploadSweepInterval = 6 * time.Hour
	closeTimeout        = 10 * time.Second
	snapshotTurns       = 20
)

// ClaudeLauncher prepares how a Claude conversation's adapter is started:
// its start fields (executable, cwd, MCP, Claude settings) and the process
// launcher. The daemon resolves executables and credentials here; the
// service adds the conversation's stored model, effort and permission mode.
type ClaudeLauncher func(ctx context.Context, conv sqlitedb.Conversation) (ClaudeSession, StartClaudeAdapter, error)

// CodexLauncher prepares how a Codex conversation's app-server is started:
// the executable, working directory and environment. The service adds the
// stored thread, model, effort and permission mode.
type CodexLauncher func(ctx context.Context, conv sqlitedb.Conversation) (CodexSession, StartCodexServer, error)

// runner runs one conversation's turns on its provider.
type runner interface {
	Send(ctx context.Context, req SendRequest) error
	Interrupt(ctx context.Context) error
	ApplySettings(ctx context.Context, st store.ConversationSettings) error
	// Running reports whether a turn is in progress.
	Running() bool
	// StopTasks stops the provider's background tasks with these ids.
	StopTasks(ctx context.Context, taskIDs []string) error
	// Prepare loads a conversation that already has a native session ahead of
	// its next send; it takes no turn slot and sends no prompt.
	Prepare(ctx context.Context, trace *timing.Trace) error
	// IdleProcess reports the runner's process when it is doing nothing.
	IdleProcess() (idleProcess, bool)
	// Evict stops an idle process and reports whether it did; the next send
	// starts a new one.
	Evict(ctx context.Context) bool
	Close(ctx context.Context) error
}

// Options configures Open.
type Options struct {
	Path     string
	Computer computer.Command
	Claude   ClaudeLauncher
	Codex    CodexLauncher
	// Cloud, when set, gives the context tools official Multica data for a
	// scope. dir is private to one provider process and removed after it.
	Cloud func(scope store.Scope, dir string) contextmcp.Cloud
	// Catalog answers provider, model and usage questions; nil reports
	// every provider as unavailable.
	Catalog *catalog.Catalog
	Logger  *slog.Logger
	Now     func() time.Time
	// TimingPath is the daemon's timing file; empty records no timing.
	TimingPath string
	// CodexSpare and ClaudeSpare, when set, keep a spare process of the provider
	// ready for new conversations.
	CodexSpare  *CodexSpareLauncher
	ClaudeSpare *ClaudeSpareLauncher
	// PreheatOldConversations reports whether opening a conversation with a
	// native session may start its process, which runs its SessionStart hooks.
	// nil allows it.
	PreheatOldConversations func() bool
	// maintenanceEvery replaces maintenanceInterval in tests.
	maintenanceEvery time.Duration
}

// Service is the Go-owned local conversation service: one store, one
// writer, one batcher, one subscription hub, and one runner per active
// conversation.
type Service struct {
	store     *store.Store
	writer    *store.Writer
	batcher   *Batcher
	hub       *Hub
	limiter   *Limiter
	context   *contextmcp.Server
	opts      Options
	computer  *computer.Manager
	computers computerUse

	// metaMu serializes metadata read-modify-write updates.
	metaMu     sync.Mutex
	mu         sync.Mutex
	closing    bool
	draining   bool
	forking    map[string]bool
	runners    map[string]runner
	questions  interactions
	queues     queues
	titles     titleRuns
	delivery   deliveries
	host       Host
	localHost  Host
	mobileLogs MobileLogs
	preview    PreviewServices
	// turnEnd learns that a conversation's turn ended; see OnTurnEnd.
	turnEnd func(conversationID string)
	side    sideWorkspaces
	imports imports
	traces  *turnTraces
	focus   *focusTracker
	spares  *sparePool
	// closed is closed when Close begins.
	closed chan struct{}
	// maintenanceKick asks for an early maintenance pass.
	maintenanceKick chan struct{}
	// uploadsSwept is only touched by the maintenance goroutine.
	uploadsSwept time.Time
	// background is the goroutines Close waits for before closing the store.
	background sync.WaitGroup
}

// Open opens the store, closes turns a previous daemon left running, removes
// unreferenced attachment files, and starts the write path.
func Open(ctx context.Context, opts Options) (*Service, error) {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	st, err := store.Open(ctx, opts.Path)
	if err != nil {
		return nil, err
	}
	writer := st.NewWriter(store.WriterOptions{})
	closed, err := ReconcileInterruptedTurns(ctx, st, writer, opts.Now())
	if err != nil {
		writer.Close()
		_ = st.Close()
		return nil, fmt.Errorf("reconcile interrupted turns: %w", err)
	}
	if closed > 0 {
		opts.Logger.Info("local conversations: closed turns interrupted by restart", "turns", closed)
	}
	if tasks, err := CloseOrphanedBackgroundTasks(ctx, st, writer, opts.Now()); err != nil {
		opts.Logger.Warn("local conversations: could not close background tasks left by restart", "error", err)
	} else if tasks > 0 {
		opts.Logger.Info("local conversations: closed background tasks left by restart", "tasks", tasks)
	}
	if removed, err := st.RemoveUnreferencedFiles(ctx, unreferencedFileAge, opts.Now()); err != nil {
		opts.Logger.Warn("local conversations: attachment cleanup failed", "error", err)
	} else if removed > 0 {
		opts.Logger.Info("local conversations: removed unreferenced attachment files", "files", removed)
	}
	contextServer, err := contextmcp.Start()
	if err != nil {
		writer.Close()
		_ = st.Close()
		return nil, err
	}
	s := &Service{store: st, writer: writer, hub: NewHub(st, HubOptions{}), limiter: NewLimiter(MaxRunningTurns), context: contextServer, opts: opts, runners: map[string]runner{}, forking: map[string]bool{},
		questions: interactions{waiting: map[string]*waitingQuestion{}},
		queues:    queues{locks: map[string]*sync.Mutex{}, dispatching: map[string]string{}},
		titles:    titleRuns{cancel: map[string]context.CancelFunc{}},
		delivery:  deliveries{timers: map[string]*time.Timer{}},
		side:      sideWorkspaces{entries: map[string]SideWorkspace{}},
		imports:   imports{busy: map[int64]context.CancelFunc{}},
		traces:    newTurnTraces(timing.Open(opts.TimingPath, timing.Options{}), opts.Now),
		focus:     newFocusTracker(),
		closed:    make(chan struct{}), maintenanceKick: make(chan struct{}, 1)}
	s.spares = newSparePool(s, opts)
	s.computer = &computer.Manager{Command: opts.Computer}
	s.computers.active = map[string]*computerExecution{}
	s.batcher = NewBatcher(writer, BatcherOptions{
		Published: func(id string, committed store.Committed) {
			s.traces.reached(committed.Events, timing.StageBatcherFirstOutput)
			s.traces.observe(committed.Events)
			s.hub.Publish(id, committed)
			s.traces.reached(committed.Events, timing.StageHubFirstOutputPublish)
		},
		Failed: func(id string, err error) {
			if !errors.Is(err, store.ErrDuplicateRequest) {
				opts.Logger.Error("local conversations: commit failed", "conversation_id", id, "error", err)
			}
		},
	})
	// Queued messages wait for the user after a restart (plan §8.2).
	queued, err := st.QueuedConversations(ctx)
	if err != nil {
		opts.Logger.Warn("local conversations: could not list queued conversations", "error", err)
	}
	for _, id := range queued {
		if err := s.pauseQueueAfterRestart(ctx, id); err != nil {
			opts.Logger.Warn("local conversations: could not pause a queue after restart", "conversation_id", id, "error", err)
		}
	}
	s.resumeRemote(ctx)
	s.goBackground(s.maintain)
	return s, nil
}

// Close stops every runner, commits pending output and closes the store.
func (s *Service) Close(ctx context.Context) error {
	s.mu.Lock()
	if !s.closing {
		close(s.closed)
	}
	s.closing = true
	runners := s.runners
	s.titles.mu.Lock()
	for _, cancel := range s.titles.cancel {
		cancel()
	}
	s.titles.mu.Unlock()
	s.runners = map[string]runner{}
	s.mu.Unlock()
	s.computers.mu.Lock()
	ids := make([]string, 0, len(s.computers.active))
	for id := range s.computers.active {
		ids = append(ids, id)
	}
	s.computers.mu.Unlock()
	for _, id := range ids {
		s.endComputer(id)
	}
	s.computer.Close()
	s.spares.close()
	var errs []error
	for _, r := range runners {
		closeCtx, cancel := context.WithTimeout(ctx, closeTimeout)
		errs = append(errs, r.Close(closeCtx))
		cancel()
	}
	s.background.Wait()
	errs = append(errs, s.context.Close())
	if preview := s.previewServices(); preview != nil {
		preview.Revoke()
	}
	s.batcher.Close()
	s.writer.Close()
	errs = append(errs, s.store.Close())
	s.traces.log.Close()
	return errors.Join(errs...)
}

// Drain refuses new turns so the daemon can restart once running ones end,
// and reports how many are still running. Queued messages wait.
func (s *Service) Drain(ctx context.Context) (int64, error) {
	s.mu.Lock()
	s.draining = true
	s.mu.Unlock()
	return s.Busy(ctx)
}

// Undrain accepts turns again after a restart was called off and resumes
// the queues that waited.
func (s *Service) Undrain(ctx context.Context) error {
	s.mu.Lock()
	was := s.draining
	s.draining = false
	s.mu.Unlock()
	if !was {
		return nil
	}
	queued, err := s.store.QueuedConversations(ctx)
	for _, id := range queued {
		go s.dispatchQueue(id)
	}
	s.mu.Lock()
	for id, r := range s.runners {
		if _, ok := r.(*ClaudeRunner); ok {
			go s.deliverBackground(id)
		}
	}
	s.mu.Unlock()
	s.preheatFocused()
	s.kickMaintenance()
	return err
}

// StopProviders drains the service and closes every provider process; their
// running turns end interrupted. Desktop calls it before an update replaces
// the executable the processes run.
func (s *Service) StopProviders(ctx context.Context) error {
	s.mu.Lock()
	s.draining = true
	runners := s.runners
	s.runners = map[string]runner{}
	s.mu.Unlock()
	s.spares.stopAll()
	var errs []error
	for _, r := range runners {
		closeCtx, cancel := context.WithTimeout(ctx, closeTimeout)
		errs = append(errs, r.Close(closeCtx))
		cancel()
	}
	return errors.Join(errs...)
}

// Busy counts running turns, local and remote.
func (s *Service) Busy(ctx context.Context) (int64, error) {
	turns, err := s.store.RunningTurns(ctx)
	return int64(len(turns)), err
}

// acceptTurn reports why a new turn cannot start now.
func (s *Service) acceptTurn() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.acceptTurnLocked()
}

func (s *Service) acceptTurnLocked() error {
	switch {
	case s.closing:
		return ErrClosing
	case s.draining:
		return ErrDraining
	}
	return nil
}

// Store exposes reads for the HTTP layer.
func (s *Service) Store() *store.Store { return s.store }

// Hub exposes subscriptions for the WS layer.
func (s *Service) Hub() *Hub { return s.hub }

// Generation identifies the database for client cursors.
func (s *Service) Generation() string { return s.store.Generation() }

// SendCommand is one user message from Desktop.
type SendCommand struct {
	ScopeID        int64
	ConversationID string
	RequestID      string
	Text           string
	// AttachmentIDs are uploads of this scope sent with the message.
	AttachmentIDs []string
	// Prompt, when set, is what the provider receives instead of Text,
	// such as Text with resolved context.
	Prompt string
	// References are the message's @ references, reported by
	// current_context while its turn runs.
	References []Reference
	// Origin is the device asking; it is not part of the request's identity, so a
	// retry from another device is still the same request.
	Origin TurnOrigin
}

// Reference is an @ reference: a local conversation, task or agent.
type Reference struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// createTaskMention is the "New task" mention: an action request rather
// than a reference to read.
const createTaskMention = "multica:create-task"

const maxReferences = 5

func validReferences(refs []Reference) error {
	if len(refs) > maxReferences {
		return fmt.Errorf("%w: reference up to %d items per message", ErrInvalidRequest, maxReferences)
	}
	for _, r := range refs {
		if (r.Kind != "session" && r.Kind != "issue" && r.Kind != "agent") || r.ID == "" || len(r.ID) > 200 {
			return fmt.Errorf("%w: invalid context reference", ErrInvalidRequest)
		}
	}
	return nil
}

// readableReferences drops the New task mention.
func readableReferences(refs []Reference) []Reference {
	out := []Reference{}
	for _, r := range refs {
		if !(r.Kind == "issue" && r.ID == createTaskMention) {
			out = append(out, r)
		}
	}
	return out
}

// SendResult reports the accepted turn. Replayed is set when the request id
// was already accepted and this call returned the original result.
type SendResult struct {
	TurnID   string
	Replayed bool
}

// Send accepts a user message idempotently: a retry with the same request
// id and content returns the original turn; different content is rejected.
func (s *Service) Send(ctx context.Context, cmd SendCommand) (SendResult, error) {
	conv, err := s.scopedConversation(ctx, cmd.ScopeID, cmd.ConversationID)
	if err != nil {
		return SendResult{}, err
	}
	fingerprint := sendFingerprint(cmd)
	if result, done, err := s.replay(ctx, cmd.ScopeID, cmd.RequestID, fingerprint); done || err != nil {
		return result, err
	}
	if err := validReferences(cmd.References); err != nil {
		return SendResult{}, err
	}
	prompt, err := s.promptWithAttachments(ctx, cmd)
	if err != nil {
		return SendResult{}, err
	}
	if side := sideOf(conv); side != nil && conv.ProviderSessionID == "" {
		prompt = sidePrompt(side, prompt)
	}
	turns, err := s.store.ConversationTurns(ctx, conv.ID)
	if err != nil {
		return SendResult{}, err
	}
	for _, t := range turns {
		if t.Status == "running" && t.Remote != "" {
			return SendResult{}, ErrTurnRunning
		}
	}
	// Agents' replies are not in the provider's own session.
	if d, _, err := s.dialogueFor(ctx, conv, turns, "", nil); err == nil {
		prompt = withDialogue(d, prompt)
	}
	if ambient := s.sideAmbient(ctx, cmd.ScopeID, conv.ID); ambient != "" {
		prompt += "\n\n" + ambient
	}
	if err := s.acceptTurn(); err != nil {
		return SendResult{}, err
	}
	turnID := "turn-" + cmd.RequestID
	trace := s.traces.turn(conv, cmd.RequestID, turnID, timing.TurnUser)
	created := trace.Begin(timing.StageRunnerCreate)
	runner, err := s.runner(ctx, conv)
	created(err)
	if err != nil {
		return SendResult{}, err
	}
	err = runner.Send(ctx, SendRequest{TurnID: turnID, RequestID: cmd.RequestID, Origin: cmd.Origin, Fingerprint: fingerprint, MessageID: "msg-" + cmd.RequestID, Text: cmd.Text, Prompt: prompt, AttachmentIDs: cmd.AttachmentIDs, References: readableReferences(cmd.References), CreateTask: len(readableReferences(cmd.References)) < len(cmd.References),
		Title: firstTitle(conv.TurnCount, conv.TitleMode, conv.Title, cmd.Text), Trace: trace})
	switch {
	case errors.Is(err, store.ErrDuplicateRequest):
		// A concurrent retry committed first.
		if result, done, err := s.replay(ctx, cmd.ScopeID, cmd.RequestID, fingerprint); done || err != nil {
			return result, err
		}
		return SendResult{}, err
	case errors.Is(err, ErrOutcomeUnknown):
		if markErr := s.store.MarkUncertain(context.WithoutCancel(ctx), cmd.ScopeID, cmd.RequestID); markErr != nil {
			err = errors.Join(err, markErr)
		}
		return SendResult{TurnID: turnID}, err
	case err != nil:
		return SendResult{}, err
	}
	return SendResult{TurnID: turnID}, nil
}

// Interrupt stops the conversation's running turn, if any.
func (s *Service) Interrupt(ctx context.Context, scopeID int64, conversationID string) error {
	if _, err := s.scopedConversation(ctx, scopeID, conversationID); err != nil {
		return err
	}
	if found, err := s.cancelRemote(ctx, scopeID, conversationID); found || err != nil {
		return err
	}
	s.mu.Lock()
	r := s.runners[conversationID]
	s.mu.Unlock()
	if r == nil {
		return nil
	}
	s.endComputer(conversationID)
	s.computer.Reset(fmt.Sprintf("%d/%s", scopeID, conversationID))
	return r.Interrupt(ctx)
}

// Snapshot returns the conversation's first screen.
func (s *Service) Snapshot(ctx context.Context, scopeID int64, conversationID string) (store.Snapshot, error) {
	if _, err := s.scopedConversation(ctx, scopeID, conversationID); err != nil {
		return store.Snapshot{}, err
	}
	return s.store.Snapshot(ctx, conversationID, snapshotTurns)
}

// History returns settled turns before an exclusive turn index.
func (s *Service) History(ctx context.Context, scopeID int64, conversationID string, before int64) ([]store.TurnView, error) {
	conv, err := s.scopedConversation(ctx, scopeID, conversationID)
	if err != nil {
		return nil, err
	}
	if before < 0 || before > conv.TurnCount {
		return nil, fmt.Errorf("%w: invalid history cursor", ErrInvalidRequest)
	}
	return s.store.TurnsBefore(ctx, conversationID, before, snapshotTurns)
}

// Subscribe starts a catch-up subscription to a conversation in scope.
func (s *Service) Subscribe(ctx context.Context, scopeID int64, conversationID string, afterSeq int64) (*Subscription, int64, error) {
	if _, err := s.scopedConversation(ctx, scopeID, conversationID); err != nil {
		return nil, 0, err
	}
	return s.hub.Subscribe(ctx, conversationID, afterSeq)
}

func (s *Service) replay(ctx context.Context, scopeID int64, requestID, fingerprint string) (SendResult, bool, error) {
	r, err := s.store.Receipt(ctx, scopeID, requestID)
	if errors.Is(err, store.ErrNotFound) {
		return SendResult{}, false, nil
	}
	if err != nil {
		return SendResult{}, true, err
	}
	if r.Fingerprint != fingerprint {
		return SendResult{}, true, ErrRequestConflict
	}
	return SendResult{TurnID: r.Result, Replayed: true}, true, nil
}

// scopedConversation loads a conversation and checks it belongs to the
// caller's scope; another scope's conversation is reported as missing.
func (s *Service) scopedConversation(ctx context.Context, scopeID int64, conversationID string) (sqlitedb.Conversation, error) {
	conv, err := s.store.Conversation(ctx, conversationID)
	if err == nil && conv.ScopeID != scopeID {
		return sqlitedb.Conversation{}, fmt.Errorf("%w: conversation %s", store.ErrNotFound, conversationID)
	}
	return conv, err
}

func (s *Service) runner(ctx context.Context, conv sqlitedb.Conversation) (runner, error) {
	s.mu.Lock()
	r, closing := s.runners[conv.ID], s.closing
	s.mu.Unlock()
	if closing {
		return nil, ErrClosing
	}
	if r != nil {
		return r, nil
	}
	switch {
	case conv.Provider == "codex" && s.opts.Codex != nil:
		r, err := s.codexRunner(ctx, conv)
		if err != nil {
			return nil, err
		}
		return s.keepRunner(ctx, conv, r)
	case conv.Provider != "claude" || s.opts.Claude == nil:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, conv.Provider)
	}
	session, start, err := s.opts.Claude(ctx, conv)
	if err != nil {
		return nil, err
	}
	if session.ProviderSessionID == "" {
		session.ProviderSessionID = conv.ProviderSessionID
	}
	// Stored settings are the source of truth; the launcher only supplies
	// how to run the provider.
	fields := map[string]any{}
	for k, v := range session.Fields {
		fields[k] = v
	}
	if conv.Model.Valid {
		fields["model"] = conv.Model.String
	}
	if conv.ReasoningEffort.Valid {
		fields["reasoning_effort"] = conv.ReasoningEffort.String
	}
	if conv.PermissionMode != "" {
		fields["permission_mode"] = conv.PermissionMode
	}
	session.Fields = fields
	return s.keepRunner(ctx, conv, NewClaudeRunner(ClaudeRunnerOptions{
		ConversationID: conv.ID, ScopeID: conv.ScopeID, Files: s.store, Start: start, Session: session,
		Batcher: s.batcher, Limiter: s.limiter, ContextMCPName: contextmcp.Name, Now: s.opts.Now, Context: s.contextBinder(conv), OnTurnEnd: func(turnID string) { s.turnEnded(conv.ID, turnID) },
		OnBackgroundEnded: func([]BackgroundResult) { s.scheduleDelivery(conv.ID) },
		NativeTrace:       func(turnID string) *timing.Trace { return s.traces.turn(conv, turnID, turnID, timing.TurnNative) },
		ProcessTrace:      func() *timing.Trace { return s.traces.process(conv.ID, conv.Provider) },
		TranscriptSize:    s.claudeTranscriptSize,
		ClaimSpare:        s.spares.claim,
	}))
}

func (s *Service) codexRunner(ctx context.Context, conv sqlitedb.Conversation) (runner, error) {
	session, start, err := s.opts.Codex(ctx, conv)
	if err != nil {
		return nil, err
	}
	if session.Cwd == "" {
		session.Cwd = conv.Cwd
	}
	session.ThreadID = conv.ProviderSessionID
	if conv.Model.Valid {
		session.Model = &conv.Model.String
	}
	if conv.ReasoningEffort.Valid {
		session.ReasoningEffort = &conv.ReasoningEffort.String
	}
	session.PermissionMode = conv.PermissionMode
	return NewCodexRunner(CodexRunnerOptions{
		ConversationID: conv.ID, ScopeID: conv.ScopeID, Files: s.store, Start: start, Session: session,
		Batcher: s.batcher, Limiter: s.limiter, ContextMCPName: contextmcp.Name, Now: s.opts.Now, Context: s.contextBinder(conv), OnTurnEnd: func(turnID string) { s.turnEnded(conv.ID, turnID) },
		ClaimSpare: s.spares.claim, NoteDefault: s.noteCodexDefault,
		ProcessTrace: func() *timing.Trace { return s.traces.process(conv.ID, conv.Provider) },
	}), nil
}

func (s *Service) claudeTranscriptSize(id string) (int64, bool) {
	if s.opts.Catalog == nil {
		return 0, false
	}
	return s.opts.Catalog.ClaudeTranscriptSize(id)
}

func (s *Service) noteCodexDefault(model string) {
	if s.opts.Catalog != nil {
		s.opts.Catalog.NoteCodexDefault(model)
	}
}

// keepRunner registers a new runner, or returns the one another caller
// registered first. A runner that arrives after Close or a drain began is closed:
// its process would run the executable an update is about to replace.
func (s *Service) keepRunner(ctx context.Context, conv sqlitedb.Conversation, r runner) (runner, error) {
	s.mu.Lock()
	if err := s.acceptTurnLocked(); err != nil {
		s.mu.Unlock()
		_ = r.Close(context.Background())
		return nil, err
	}
	if existing := s.runners[conv.ID]; existing != nil {
		s.mu.Unlock()
		return existing, nil
	}
	s.runners[conv.ID] = r
	s.mu.Unlock()
	if err := s.catchUpSettings(context.WithoutCancel(ctx), conv, r); err != nil {
		s.mu.Lock()
		if s.runners[conv.ID] == r {
			delete(s.runners, conv.ID)
		}
		s.mu.Unlock()
		_ = r.Close(context.Background())
		return nil, err
	}
	return r, nil
}

// catchUpSettings applies settings committed after conv was read: UpdateSettings
// found no runner then, and the new runner was built from conv. Reading after
// registering leaves no gap, since a later commit does find the runner. Reading
// again after applying keeps this apply from overwriting that later one.
func (s *Service) catchUpSettings(ctx context.Context, conv sqlitedb.Conversation, r runner) error {
	applied := conversationSettings(conv)
	for {
		latest, err := s.store.Conversation(ctx, conv.ID)
		if err != nil {
			return err
		}
		settings := conversationSettings(latest)
		if sameSettings(applied, settings) {
			return nil
		}
		if err := r.ApplySettings(ctx, settings); err != nil {
			s.opts.Logger.Warn("local conversations: settings not applied to a new runner", "conversation_id", conv.ID, "error", err)
		}
		applied = settings
	}
}

func conversationSettings(conv sqlitedb.Conversation) store.ConversationSettings {
	st := store.ConversationSettings{PermissionMode: conv.PermissionMode}
	if conv.Model.Valid {
		st.Model = &conv.Model.String
	}
	if conv.ReasoningEffort.Valid {
		st.ReasoningEffort = &conv.ReasoningEffort.String
	}
	return st
}

func sameSettings(a, b store.ConversationSettings) bool {
	return a.PermissionMode == b.PermissionMode && sameModel(a.Model, b.Model) && sameModel(a.ReasoningEffort, b.ReasoningEffort)
}

func sendFingerprint(cmd SendCommand) string {
	data, _ := json.Marshal(struct {
		ConversationID string   `json:"conversation_id"`
		Text           string   `json:"text"`
		AttachmentIDs  []string `json:"attachment_ids"`
	}{cmd.ConversationID, cmd.Text, cmd.AttachmentIDs})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SettingsUpdate changes provider settings. A nil field keeps its value;
// ClearModel or ClearReasoningEffort return that setting to the provider
// default.
type SettingsUpdate struct {
	Model                *string
	ClearModel           bool
	ReasoningEffort      *string
	ClearReasoningEffort bool
	PermissionMode       *string
}

var validPermissionModes = map[string]bool{"default": true, "auto": true, "full-access": true}

// UpdateSettings stores new provider settings, records them as a
// conversation.updated event, and applies them to a running adapter.
func (s *Service) UpdateSettings(ctx context.Context, scopeID int64, conversationID string, u SettingsUpdate) error {
	conv, err := s.scopedConversation(ctx, scopeID, conversationID)
	if err != nil {
		return err
	}
	st := conversationSettings(conv)
	switch {
	case u.ClearModel:
		st.Model = nil
	case u.Model != nil:
		st.Model = u.Model
	}
	switch {
	case u.ClearReasoningEffort:
		st.ReasoningEffort = nil
	case u.ReasoningEffort != nil:
		st.ReasoningEffort = u.ReasoningEffort
	}
	if u.PermissionMode != nil {
		if !validPermissionModes[*u.PermissionMode] {
			return fmt.Errorf("%w: permission mode %q", ErrInvalidSettings, *u.PermissionMode)
		}
		st.PermissionMode = *u.PermissionMode
	}
	payload := map[string]any{"model": nil, "reasoning_effort": nil, "permission_mode": st.PermissionMode}
	if st.Model != nil {
		payload["model"] = *st.Model
	}
	if st.ReasoningEffort != nil {
		payload["reasoning_effort"] = *st.ReasoningEffort
	}
	data, _ := json.Marshal(payload)
	if _, err := s.batcher.AppendWait(ctx, store.Batch{ConversationID: conversationID, Settings: &st,
		Events: []store.NewEvent{{Type: "conversation.updated", Payload: data, CreatedAt: s.opts.Now()}}}); err != nil {
		return err
	}
	s.mu.Lock()
	r := s.runners[conversationID]
	s.mu.Unlock()
	if r == nil {
		return nil
	}
	return r.ApplySettings(ctx, st)
}

// contextBinder binds the read-only context tools of conv's scope to one
// provider process. current_context also reports the selected workspace.
func (s *Service) contextBinder(conv sqlitedb.Conversation) ContextBinder {
	return func(ctx context.Context, token string, current func() map[string]any) (BoundContext, func(), error) {
		scope, err := s.store.ScopeByID(ctx, conv.ScopeID)
		if err != nil {
			return BoundContext{}, nil, err
		}
		withWorkspace := func() map[string]any {
			c := current()
			c["workspace_id"] = scope.WorkspaceID
			if origin := s.currentOrigin(context.Background(), conv.ID, current); origin != (TurnOrigin{}) {
				c["origin_peer_id"], c["origin_client_kind"] = origin.PeerID, origin.ClientKind
			}
			return c
		}
		runs := filepath.Join(filepath.Dir(s.opts.Path), "runs")
		if err := os.MkdirAll(runs, 0o700); err != nil {
			return BoundContext{}, nil, err
		}
		dir, err := os.MkdirTemp(runs, "run-")
		if err != nil {
			return BoundContext{}, nil, err
		}
		var cloud contextmcp.Cloud
		if s.opts.Cloud != nil {
			cloud = s.opts.Cloud(scope, dir)
		}
		tools := append(contextmcp.ContextTools(s.store, cloud, conv.ScopeID, withWorkspace), s.questionToolFor(conv, current))
		var readContext func(context.Context, json.RawMessage) (contextmcp.Result, error)
		for _, tool := range tools {
			if tool.Name == "read_context" {
				readContext = tool.Call
			}
		}
		tools = append(tools, s.sideToolsFor(conv, current, readContext)...)
		tools = append(tools, s.developerLogTool(conv))
		s.mu.Lock()
		mobileLogs := s.mobileLogs
		s.mu.Unlock()
		if mobileLogs != nil {
			tools = append(tools, s.mobileLogTool(conv.ID, current, mobileLogs))
		}
		tools = append(tools, s.computerToolsFor(conv, current)...)
		if agents, ok := cloud.(cloudAgents); ok && agents != nil {
			tools = append(tools, s.avatarTool(conv, agents))
		}
		requester, _ := cloud.(workspace.Requester)
		tools = append(tools, workspace.Tools(workspace.Options{
			WorkspaceID: scope.WorkspaceID, UserID: scope.UserID, ConversationID: conv.ID,
			Request: requester, Local: localActions{s: s, scopeID: conv.ScopeID},
		})...)
		settingsOpts := settings.Options{
			Scope:    settings.Scope{WorkspaceID: scope.WorkspaceID, UserID: scope.UserID},
			Receipts: settingsReceipts{store: s.store, scopeID: conv.ScopeID, now: s.opts.Now},
			Host: func(ctx context.Context, kind string, payload any) (json.RawMessage, error) {
				return s.askHost(ctx, conv.ScopeID, conv.ID, kind, payload)
			},
		}
		if requester != nil {
			settingsOpts.Request = requester
		}
		tools = append(tools, settings.Tools(settingsOpts)...)
		binding, err := s.context.Bind(context.Background(), contextmcp.Execution{
			Instructions: contextmcp.ContextInstructions, Token: token, Tools: tools,
		})
		if err != nil {
			_ = os.RemoveAll(dir)
			return BoundContext{}, nil, err
		}
		return BoundContext{Binding: binding, SystemPrompt: systemPrompt(scope.WorkspaceID, cloud != nil)}, func() {
			s.endComputer(conv.ID)
			s.computer.Reset(fmt.Sprintf("%d/%s", conv.ScopeID, conv.ID))
			s.context.Release(binding)
			_ = os.RemoveAll(dir)
		}, nil
	}
}

// Host asks a connected Desktop of the scope to act for a conversation and
// returns its result; the transport prefers the Desktop that last acted on
// the conversation.
type Host func(ctx context.Context, scopeID int64, conversationID, kind string, payload any) (json.RawMessage, error)

// HostError is Desktop's refusal or failure of a host request.
type HostError struct {
	Kind    string
	Message string
}

func (e *HostError) Error() string { return e.Message }

// OnTurnEnd calls fn after each turn of a conversation ends.
func (s *Service) OnTurnEnd(fn func(conversationID string)) {
	s.mu.Lock()
	s.turnEnd = fn
	s.mu.Unlock()
}

// SetHost connects the service to the Desktop transport.
func (s *Service) SetHost(h Host) {
	s.mu.Lock()
	s.host = h
	s.mu.Unlock()
}

// SetLocalHost connects the service to the Desktop of the executing device.
func (s *Service) SetLocalHost(h Host) {
	s.mu.Lock()
	s.localHost = h
	s.mu.Unlock()
}

// askLocalHost sends a host request that only this device's Desktop may answer.
func (s *Service) askLocalHost(ctx context.Context, scopeID int64, conversationID, kind string, payload any) (json.RawMessage, error) {
	s.mu.Lock()
	h := s.localHost
	s.mu.Unlock()
	if h == nil {
		return nil, ErrLocalHostUnavailable
	}
	return h(ctx, scopeID, conversationID, kind, payload)
}

// askHost sends a host request, or fails when no transport is set.
func (s *Service) askHost(ctx context.Context, scopeID int64, conversationID, kind string, payload any) (json.RawMessage, error) {
	s.mu.Lock()
	h := s.host
	s.mu.Unlock()
	if h == nil {
		return nil, ErrHostUnavailable
	}
	return h(ctx, scopeID, conversationID, kind, payload)
}
