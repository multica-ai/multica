package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// OfflineRuntimeTTLSeconds is how long an offline runtime is kept before
// retention GC deletes it, once no active agent and no non-terminal task
// remain on it. GC applies this to every runtime row, including the
// profile-backed ones the interactive delete endpoints refuse — so those
// refusal messages quote this same number to tell the user when the row they
// cannot delete will disappear on its own. Keeping one constant is what stops
// that promise from drifting away from the sweeper that has to honour it.
const OfflineRuntimeTTLSeconds = 7 * 24 * 3600.0

// OfflineRuntimeTTLDays renders OfflineRuntimeTTLSeconds for user-facing copy.
func OfflineRuntimeTTLDays() int {
	return int(OfflineRuntimeTTLSeconds / 86400)
}

var (
	// ErrRuntimeNotDrained means a runtime or one of its bound user agents
	// still owns a non-terminal task. Callers must abort the transaction rather
	// than deleting the runtime and relying on database cascades.
	ErrRuntimeNotDrained = errors.New("runtime still has non-terminal tasks")
	// ErrRuntimeWorkspaceMismatch protects tenant isolation if legacy or
	// corrupted data binds an agent to a runtime from another workspace.
	ErrRuntimeWorkspaceMismatch = errors.New("runtime agent workspace mismatch")
)

// RuntimeTeardownOptions controls the only intentional semantic difference
// between user-confirmed deletion and retention GC. Manual deletion cancels
// active work; automatic GC must fail closed and leave it untouched.
type RuntimeTeardownOptions struct {
	CancelNonTerminalTasks bool
}

// RuntimeTeardownResult reports committed business-object changes so callers
// can publish events after their transaction commits.
type RuntimeTeardownResult struct {
	UnboundAgents    []db.Agent
	CancelledTasks   []db.AgentTaskQueue
	PausedAutopilots []db.Autopilot
}

// ValidateRuntimeAgentWorkspaces refuses to mutate cross-workspace bindings.
// agent.runtime_id is not backed by a composite workspace foreign key, so this
// application-layer guard is required before a runtime teardown changes data.
func ValidateRuntimeAgentWorkspaces(runtime db.AgentRuntime, agents []db.Agent) error {
	for _, agent := range agents {
		if agent.WorkspaceID != runtime.WorkspaceID {
			return fmt.Errorf("%w: agent %x runtime %x", ErrRuntimeWorkspaceMismatch, agent.ID.Bytes, runtime.ID.Bytes)
		}
	}
	return nil
}

// RuntimeTeardown contains the runtime and user agents locked for deletion
// in the caller's transaction.
type RuntimeTeardown struct {
	Runtime    db.AgentRuntime
	UserAgents []db.Agent
}

func LockRuntimeTeardown(ctx context.Context, qtx *db.Queries, runtimeID pgtype.UUID) (RuntimeTeardown, error) {
	targets, err := LockRuntimeTeardowns(ctx, qtx, []pgtype.UUID{runtimeID})
	if err != nil {
		return RuntimeTeardown{}, err
	}
	return targets[0], nil
}

// LockRuntimeTeardowns prepares a batch for validation and deletion. The
// runtime IDs must be in ascending order. Keep these locks through commit.
func LockRuntimeTeardowns(ctx context.Context, qtx *db.Queries, runtimeIDs []pgtype.UUID) ([]RuntimeTeardown, error) {
	if len(runtimeIDs) == 0 {
		return nil, nil
	}
	// Serialize binding, claim delivery and other runtime writers. An enqueue
	// already holding a session or agent can still take runtime KEY SHARE and
	// finish. Upgrade only when we've locked the dependencies for the whole
	// batch, then re-read them without waiting on any references we missed.
	for _, id := range runtimeIDs {
		if _, err := qtx.LockAgentRuntimeForAccessChange(ctx, id); err != nil {
			return nil, fmt.Errorf("lock runtime for teardown: %w", err)
		}
	}
	if err := qtx.LockRuntimeTeardownChatSessions(ctx, runtimeIDs); err != nil {
		return nil, fmt.Errorf("lock runtime chat sessions: %w", err)
	}
	if err := qtx.LockRuntimeTeardownAgents(ctx, runtimeIDs); err != nil {
		return nil, fmt.Errorf("lock runtime agents: %w", err)
	}
	targets := make([]RuntimeTeardown, len(runtimeIDs))
	targetByID := make(map[pgtype.UUID]*RuntimeTeardown, len(runtimeIDs))
	for i, id := range runtimeIDs {
		runtime, err := qtx.LockAgentRuntime(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("fence runtime references: %w", err)
		}
		targets[i].Runtime = runtime
		targetByID[id] = &targets[i]
	}
	agents, err := qtx.ListLockedRuntimeTeardownAgents(ctx, runtimeIDs)
	if err != nil {
		return nil, fmt.Errorf("recheck locked runtime agents: %w", err)
	}
	if err := qtx.LockLateRuntimeTeardownChatSessions(ctx, runtimeIDs); err != nil {
		return nil, fmt.Errorf("recheck locked runtime chat sessions: %w", err)
	}
	for _, agent := range agents {
		if agent.Kind == "user" {
			target := targetByID[agent.RuntimeID]
			target.UserAgents = append(target.UserAgents, agent)
		}
	}
	return targets, nil
}

// TeardownRuntime performs the shared mutation phase used immediately before
// deleting an agent_runtime row. The target must have been locked in the
// same transaction.
//
// User agents are persistent business objects and survive unbound. System
// agents are runtime-local infrastructure and are deleted only after their
// non-FK dependencies are pruned; their runtime-local builder drafts and chat
// draft restores are deleted with them. Task history is detached before runtime
// deletion so the legacy ON DELETE CASCADE cannot erase it.
func TeardownRuntime(ctx context.Context, qtx *db.Queries, target RuntimeTeardown, opts RuntimeTeardownOptions) (RuntimeTeardownResult, error) {
	var out RuntimeTeardownResult

	runtimeID := target.Runtime.ID
	if err := ValidateRuntimeAgentWorkspaces(target.Runtime, target.UserAgents); err != nil {
		return out, err
	}

	lockedAgentIDs := make([]pgtype.UUID, len(target.UserAgents))
	for i, agent := range target.UserAgents {
		lockedAgentIDs[i] = agent.ID
	}

	if opts.CancelNonTerminalTasks {
		cancelled, err := qtx.CancelAgentTasksByRuntimeOrAgent(ctx, db.CancelAgentTasksByRuntimeOrAgentParams{
			RuntimeIds: []pgtype.UUID{runtimeID},
			AgentIds:   lockedAgentIDs,
		})
		if err != nil {
			return out, fmt.Errorf("cancel tasks: %w", err)
		}
		// Task locks precede receipt locks, as in delivery acknowledgement.
		// Removing the runtime withdraws guidance rather than starting new work.
		if err := qtx.WithdrawChatTaskSupplementsByRuntime(ctx, runtimeID); err != nil {
			return out, err
		}
		if err := SettleTerminalTaskState(ctx, qtx, cancelled...); err != nil {
			return out, err
		}
		out.CancelledTasks = cancelled
	}

	undrained, err := qtx.CountUndrainedTasksByRuntimeOrAgent(ctx, db.CountUndrainedTasksByRuntimeOrAgentParams{
		RuntimeIds: []pgtype.UUID{runtimeID},
		AgentIds:   lockedAgentIDs,
	})
	if err != nil {
		return out, fmt.Errorf("count undrained tasks: %w", err)
	}
	if undrained > 0 {
		return out, fmt.Errorf("%w: %d", ErrRuntimeNotDrained, undrained)
	}

	unbound, err := qtx.UnbindUserAgentsFromRuntime(ctx, runtimeID)
	if err != nil {
		return out, fmt.Errorf("unbind agents: %w", err)
	}
	out.UnboundAgents = unbound

	unboundIDs := make([]pgtype.UUID, len(unbound))
	for i, agent := range unbound {
		unboundIDs[i] = agent.ID
	}
	paused, err := qtx.PauseAutopilotsByUnboundAgents(ctx, unboundIDs)
	if err != nil {
		return out, fmt.Errorf("pause autopilots: %w", err)
	}
	out.PausedAutopilots = paused

	if _, err := qtx.UnbindTasksFromRuntime(ctx, runtimeID); err != nil {
		return out, fmt.Errorf("unbind task history: %w", err)
	}
	remaining, err := qtx.CountTasksByRuntime(ctx, runtimeID)
	if err != nil {
		return out, fmt.Errorf("confirm task history detached: %w", err)
	}
	if remaining != 0 {
		return out, fmt.Errorf("task history still references runtime after detach: %d", remaining)
	}

	// These tables intentionally lack agent foreign keys, so they must be
	// pruned before the system-agent rows disappear.
	if err := qtx.DeleteAgentInvocationTargetsBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return out, fmt.Errorf("clean up agent invocation targets: %w", err)
	}
	if err := qtx.DeleteChannelInstallationsBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return out, fmt.Errorf("clean up channel installations: %w", err)
	}
	if err := qtx.DeleteChatPinnedAgentsBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return out, fmt.Errorf("clean up chat pins: %w", err)
	}
	if err := qtx.DeleteAgentLabelAssignmentsBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return out, fmt.Errorf("clean up agent label assignments: %w", err)
	}
	if err := pruneRuntimeSystemAgentChatDraftRestores(ctx, qtx, runtimeID); err != nil {
		return out, fmt.Errorf("clean up chat draft restores: %w", err)
	}
	if err := qtx.DeleteSystemAgentsByRuntime(ctx, runtimeID); err != nil {
		return out, fmt.Errorf("clean up system agents: %w", err)
	}
	return out, nil
}

// pruneRuntimeSystemAgentChatDraftRestores removes rows without a database FK
// before the system agents and their chat sessions cascade away.
func pruneRuntimeSystemAgentChatDraftRestores(ctx context.Context, q *db.Queries, runtimeID pgtype.UUID) error {
	if err := q.DeleteChatSubmissionReceiptsBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return err
	}
	if err := q.DeleteChatDraftRestoresBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return err
	}
	if err := q.DeleteChatWakeupsBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return err
	}
	return q.DeleteAgentBuilderDraftsBySystemRuntimeAgents(ctx, runtimeID)
}
