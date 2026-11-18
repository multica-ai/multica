package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/multica-ai/multica/server/pkg/kagent"
)

// KagentConfig explicitly enrolls one native-harness DEV conversation. This is
// not Pi/workspace parity. The daemon supplies trusted configuration and
// authoritative claim identities; the journal must survive daemon restarts.
type KagentConfig struct {
	Enabled          bool
	Lifecycle        *kagent.Lifecycle
	Journal          *kagent.Journal
	Client           *kagent.A2AClient
	AgentID          string
	WorkspaceID      string
	ConversationID   string
	AttemptID        string
	PollInterval     time.Duration
	ReadinessTimeout time.Duration
}

type kagentBackend struct{ cfg KagentConfig }

var _ Backend = (*kagentBackend)(nil)

// NewKagentBackend is separate from the custom CLI runtime whitelist: Kagent
// cannot execute a custom local command. Callers must explicitly opt in and
// provide stable workspace/conversation/attempt identities.
func NewKagentBackend(c KagentConfig) (Backend, error) {
	if !c.Enabled {
		return nil, fmt.Errorf("kagent: DEV backend is disabled")
	}
	if c.Lifecycle == nil || c.Journal == nil || c.Client == nil || c.WorkspaceID == "" || c.ConversationID == "" || c.AttemptID == "" {
		return nil, fmt.Errorf("kagent: lifecycle, journal, client and durable correlation IDs required")
	}
	if _, err := kagent.ResourceName(c.AgentID); err != nil {
		return nil, err
	}
	c.AgentID = strings.ToLower(c.AgentID)
	if c.PollInterval <= 0 {
		c.PollInterval = 500 * time.Millisecond
	}
	if c.ReadinessTimeout <= 0 {
		c.ReadinessTimeout = 30 * time.Second
	}
	return &kagentBackend{c}, nil
}

type kagentAttempt struct {
	SuppressOutput                           bool
	DeadlineExceeded                         bool
	Outcome                                  *kagentOutcome
	TerminalState                            string
	MessageID, InputHash, TaskID, Generation string
	Settled                                  bool
}

// Only bounded intentional completed output and local, fixed errors are retained.
// Provider diagnostics never enter this replay record.
type kagentOutcome struct {
	Status, Error, Output, SessionID string
}

type kagentConversation struct {
	Version                                                              int
	AgentUID, OwnerToken, Endpoint, SessionID, ActiveAttempt, Generation string
	Attempts                                                             map[string]kagentAttempt
}

func validateKagentOptions(prompt string, o ExecOptions) error {
	if strings.TrimSpace(prompt) == "" || strings.TrimSpace(o.SystemPrompt) == "" {
		return fmt.Errorf("kagent: prompt and explicit inline runtime brief required")
	}
	if o.Cwd != "" || o.TaskToken != "" || o.MulticaServerURL != "" || o.McpConfig != nil || len(o.PiSkillPaths) > 0 || len(o.ExtraArgs) > 0 || len(o.CustomArgs) > 0 || o.Model != "" || o.ThinkingLevel != "" || o.ServiceTier != "" || o.QwenpawWorkspace != "" || o.MaxTurns != 0 || o.EnableTaskSupplement {
		return fmt.Errorf("kagent: unsupported local workspace, credentials, tools or execution options")
	}
	if o.ThreadName != "" || o.OpenclawMode != "" || o.OpenclawModelOverride != "" || o.ClaudeSettingsPath != "" || len(o.CursorHookCommand) > 0 || o.ResumeContinuityNotice != "" ||
		o.SemanticInactivityTimeout != 0 || o.FirstTurnNoProgressTimeout != 0 || o.HandshakeTimeout != 0 || o.TurnInterruptTimeout != 0 || o.ThreadHandshakeTimeout != 0 {
		return fmt.Errorf("kagent: unsupported provider-specific execution options")
	}
	return nil
}
func (b *kagentBackend) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	if err := validateKagentOptions(prompt, opts); err != nil {
		return nil, err
	}
	if opts.MulticaWorkspaceID != "" && opts.MulticaWorkspaceID != b.cfg.WorkspaceID {
		return nil, fmt.Errorf("kagent: workspace mismatch")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	runCtx, cancel := runContext(ctx, opts.Timeout)
	started := false
	defer func() {
		if !started {
			cancel()
		}
	}()
	lease, err := b.acquireReady(runCtx)
	if err != nil {
		return nil, err
	}
	// JSON tuple encoding avoids collisions between user-controlled identifiers.
	key := kagent.ConversationKey(b.cfg.WorkspaceID, b.cfg.AgentID, b.cfg.ConversationID)
	fence := lease.Execution()
	if fence != nil && (fence.Version != 1 || fence.ConversationKey != key || fence.AttemptID != b.cfg.AttemptID || fence.TaskID == "" || fence.SessionID == "" || fence.Endpoint != b.cfg.Client.Identity()) {
		lease.Close()
		return nil, kagent.ErrUnknownOutcome
	}
	rec, err := b.cfg.Journal.Lock(key)
	if err != nil {
		lease.Close()
		return nil, err
	}
	fail := func(err error) (*Session, error) { rec.Close(); lease.Close(); return nil, err }
	var state kagentConversation
	exists, err := rec.Load(&state)
	if err != nil {
		return fail(err)
	}
	if exists && (state.Version != 2 || state.AgentUID != lease.Desired.ExpectedUID || state.OwnerToken != lease.Desired.OwnerToken || state.Endpoint != b.cfg.Client.Identity()) {
		return fail(fmt.Errorf("%w: conversation binding changed or journal version unsupported", kagent.ErrUnknownOutcome))
	}
	if !exists {
		state = kagentConversation{Version: 2, AgentUID: lease.Desired.ExpectedUID, OwnerToken: lease.Desired.OwnerToken, Endpoint: b.cfg.Client.Identity(), Attempts: map[string]kagentAttempt{}}
	}
	if state.Attempts == nil {
		return fail(fmt.Errorf("kagent: invalid conversation journal"))
	}
	for id, a := range state.Attempts {
		t := kagent.RemoteTask{}
		t.Status.State = a.TerminalState
		if (a.Settled && (!t.Terminal() || a.TaskID == "" || id == state.ActiveAttempt)) || (!a.Settled && (id != state.ActiveAttempt || a.TerminalState != "")) {
			return fail(kagent.ErrUnknownOutcome)
		}
	}
	if state.ActiveAttempt != "" {
		active, ok := state.Attempts[state.ActiveAttempt]
		if !ok || (active.TaskID == "" && fence == nil) || state.ActiveAttempt != b.cfg.AttemptID {
			return fail(kagent.ErrUnknownOutcome)
		}
	}
	if (opts.ResumeSessionID != "" && opts.ResumeSessionID != state.SessionID) || (opts.ResumeExpected && opts.ResumeSessionID == "") {
		return fail(fmt.Errorf("kagent: resume requires the durable conversation mapping"))
	}
	input := opts.SystemPrompt + "\n\n" + prompt
	hash := sha256.Sum256([]byte(input))
	inputHash := hex.EncodeToString(hash[:])
	attempt, seen := state.Attempts[b.cfg.AttemptID]
	if seen {
		// An attempt whose send was never confirmed fences the whole attempt
		// identity: recovery is by remote observation only, so any new input
		// under this id is refused regardless of content.
		if attempt.TaskID == "" && fence == nil {
			return fail(kagent.ErrUnknownOutcome)
		}
		if attempt.Generation != state.Generation {
			return fail(fmt.Errorf("kagent: attempt belongs to a retired context"))
		}
		if attempt.InputHash != inputHash {
			return fail(fmt.Errorf("kagent: attempt input changed"))
		}
	} else {
		if fence != nil {
			return fail(kagent.ErrUnknownOutcome)
		}
		// The claim protocol carries no context generation. Only an explicit
		// resume pointer authorizes continuation; never infer it from the journal.
		// Keep prior attempts so a clear cannot erase replay/unknown-outcome fences.
		if opts.ResumeSessionID == "" {
			state.SessionID = ""
			state.Generation = kagent.NewID()
		}
		attempt = kagentAttempt{MessageID: kagent.NewID(), InputHash: inputHash, Generation: state.Generation}
		state.Attempts[b.cfg.AttemptID] = attempt
		state.ActiveAttempt = b.cfg.AttemptID
	}
	if seen && attempt.Settled {
		// A terminal conversation commit may precede clearing the agent fence.
		// Never reinstall a fence or depend on the remote task's retention.
		if fence != nil {
			if fence.MessageID != attempt.MessageID || fence.InputHash != attempt.InputHash || fence.Generation != attempt.Generation || fence.TaskID != attempt.TaskID || fence.SessionID != state.SessionID {
				return fail(kagent.ErrUnknownOutcome)
			}
			if err := lease.SaveExecution(nil); err != nil {
				return fail(err)
			}
		}
		if attempt.Outcome == nil {
			return fail(kagent.ErrUnknownOutcome)
		}
		o := attempt.Outcome
		if len(o.Output) > 32<<10 || o.SessionID != state.SessionID || (o.Status != "completed" && o.Status != "failed" && o.Status != "cancelled" && o.Status != "timeout") || (attempt.SuppressOutput && (o.Output != "" || o.Status != "failed")) {
			return fail(kagent.ErrUnknownOutcome)
		}
		messages := make(chan Message, 1)
		if o.Output != "" {
			messages <- Message{Type: MessageText, Content: o.Output}
		}
		close(messages)
		results := make(chan Result, 1)
		results <- Result{Status: o.Status, Error: o.Error, Output: o.Output, SessionID: o.SessionID}
		close(results)
		rec.Close()
		lease.Close()
		return &Session{CleanupRequired: true, Messages: messages, Result: results, TerminalObserved: func() bool { return true }}, nil
	}
	// Upgrade existing journals under the same agent lock. A prior unresolved
	// conversation must not be hidden by choosing another conversation or clearing.
	if fence == nil {
		recoveryKey, recoveryAttempt := "", ""
		if seen {
			recoveryKey, recoveryAttempt = key, b.cfg.AttemptID
		}
		if err = b.cfg.Journal.CheckConversationFences(state.AgentUID, state.OwnerToken, recoveryKey, recoveryAttempt); err != nil {
			return fail(err)
		}
		fence = &kagent.ExecutionIntent{
			Version: 1, ConversationKey: key, AttemptID: b.cfg.AttemptID,
			MessageID: attempt.MessageID, InputHash: inputHash, Generation: attempt.Generation,
			Endpoint: b.cfg.Client.Identity(), TaskID: attempt.TaskID, SessionID: state.SessionID,
			SuppressOutput: attempt.SuppressOutput, DeadlineExceeded: attempt.DeadlineExceeded,
		}
		// Durable agent-wide intent precedes the conversation commit AND dispatch.
		// Any failure after this commit retains the fence, even if no send occurred.
		if err = lease.SaveExecution(fence); err != nil {
			return fail(err)
		}
	} else {
		if fence.MessageID != attempt.MessageID || fence.InputHash != inputHash || fence.Generation != attempt.Generation {
			return fail(kagent.ErrUnknownOutcome)
		}
		// The agent record is committed first when a send returns an identity. Repair
		// a conversation commit interrupted by a crash using that exact identity.
		if attempt.TaskID != "" && attempt.TaskID != fence.TaskID {
			return fail(kagent.ErrUnknownOutcome)
		}
		attempt.TaskID = fence.TaskID
		attempt.SuppressOutput = attempt.SuppressOutput || fence.SuppressOutput
		attempt.DeadlineExceeded = attempt.DeadlineExceeded || fence.DeadlineExceeded
		state.SessionID = fence.SessionID
		state.Attempts[b.cfg.AttemptID] = attempt
	}
	if err = rec.Save(state); err != nil {
		return fail(err)
	}
	messages := make(chan Message, 16)
	results := make(chan Result, 1)
	var terminal atomic.Bool
	started = true
	session := &Session{CleanupRequired: true, Messages: messages, Result: results, TerminalObserved: terminal.Load}
	go func() {
		start := time.Now()
		result := b.runKagent(runCtx, lease, rec, &state, attempt, seen, input, messages, &terminal)
		result.DurationMs = time.Since(start).Milliseconds()
		cancel()
		close(messages)
		rec.Close()
		lease.Close()
		// cleanup_unconfirmed is the existing daemon fence for remote work whose
		// terminal outcome is unknown; it must never be classified as retry-safe.
		results <- result
		close(results)
	}()
	return session, nil
}

// Each acquisition revalidates enrollment, ownership and UID. Only a successful
// but unready reconcile is retried; permanent reconciliation errors fail at once.
func (b *kagentBackend) acquireReady(ctx context.Context) (*kagent.BindingLease, error) {
	readyCtx, cancel := context.WithTimeout(ctx, b.cfg.ReadinessTimeout)
	defer cancel()
	for {
		if err := readyCtx.Err(); err != nil {
			return nil, fmt.Errorf("kagent: waiting for Agent readiness: %w", err)
		}
		lease, err := b.cfg.Lifecycle.Acquire(readyCtx, b.cfg.AgentID)
		if err != nil {
			if readyCtx.Err() != nil {
				return nil, fmt.Errorf("kagent: waiting for Agent readiness: %w", readyCtx.Err())
			}
			return nil, err
		}
		if lease.Outcome.Ready {
			return lease, nil
		}
		lease.Close()
		timer := time.NewTimer(b.cfg.PollInterval)
		select {
		case <-readyCtx.Done():
			timer.Stop()
			return nil, fmt.Errorf("kagent: waiting for Agent readiness: %w", readyCtx.Err())
		case <-timer.C:
		}
	}
}
