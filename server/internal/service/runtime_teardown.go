package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

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

// TeardownRuntime performs the shared mutation phase used immediately before
// deleting an agent_runtime row. It takes the runtime and bound-user-agent row
// locks itself; callers that already locked them for a confirmation check can
// safely acquire the same locks again in their transaction.
//
// User agents are persistent business objects and survive unbound. System
// agents are runtime-local infrastructure and are deleted only after their
// non-FK dependencies are pruned; their runtime-local builder drafts and chat
// draft restores are deleted with them. Task history is detached before runtime
// deletion so the legacy ON DELETE CASCADE cannot erase it.
func TeardownRuntime(ctx context.Context, qtx *db.Queries, runtimeID pgtype.UUID, opts RuntimeTeardownOptions) (RuntimeTeardownResult, error) {
	var out RuntimeTeardownResult

	runtime, err := qtx.LockAgentRuntime(ctx, runtimeID)
	if err != nil {
		return out, fmt.Errorf("load runtime: %w", err)
	}
	lockedAgents, err := qtx.ListUserAgentsByRuntimeForUpdate(ctx, runtimeID)
	if err != nil {
		return out, fmt.Errorf("lock runtime agents: %w", err)
	}
	if err := ValidateRuntimeAgentWorkspaces(runtime, lockedAgents); err != nil {
		return out, err
	}

	lockedAgentIDs := make([]pgtype.UUID, len(lockedAgents))
	for i, agent := range lockedAgents {
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
	if _, err := q.LockChatSessionsBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return err
	}
	if err := q.DeleteChatDraftRestoresBySystemRuntimeAgents(ctx, runtimeID); err != nil {
		return err
	}
	return q.DeleteAgentBuilderDraftsBySystemRuntimeAgents(ctx, runtimeID)
}
