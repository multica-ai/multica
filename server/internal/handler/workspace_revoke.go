package handler

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// revokeAndRemoveMember converges all server-side state that should follow a
// member leaving a workspace: every runtime they own becomes unusable, every
// agent pinned to one of those runtimes is archived, every in-flight task on
// those runtimes is cancelled (cancelled rather than failed so the daemon's
// per-task status poller interrupts the running agent gracefully), the
// daemon_token rows for those runtimes are deleted, and finally the member row
// itself is removed.
//
// All DB writes run inside a single transaction so a partial revocation never
// leaves the workspace half-converged — e.g. a member who is "gone" but whose
// runtime row is still active. Once the transaction commits, daemon_token
// cache entries are invalidated and events are published (see
// publishRevocation) so connected clients and other workspace members observe
// the new state immediately.
//
// Note on scope: this revokes every runtime whose owner_id matches userID,
// regardless of how the daemon authenticates. Today most daemons fall back to
// PAT/JWT and `daemon_token` rows are unused in production; deleting them is
// a no-op for those daemons but takes effect once the mdt_ flow is live.
// Either way the agent-archive + task-cancel + force-offline writes are the
// actual production safety net: even if the daemon races back online with a
// still-valid PAT, it finds no agent it can run for, no queued task to claim,
// and the dispatcher (which gates on agent.archived_at IS NULL) won't hand it
// new work — and the member-row deletion in the same tx means subsequent
// requireWorkspaceMember checks will reject the daemon's PAT-authenticated
// requests with 404.
//
// archivedBy is the actor who triggered the revocation. For DeleteMember it's
// the requester (the admin doing the kick); for LeaveWorkspace it's the leaver
// themselves.
func (h *Handler) revokeAndRemoveMember(ctx context.Context, workspaceID, userID, memberID, archivedBy pgtype.UUID) (revocationResult, error) {
	var empty revocationResult

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)

	runtimes, err := qtx.ListAgentRuntimesByOwner(ctx, db.ListAgentRuntimesByOwnerParams{
		WorkspaceID: workspaceID,
		OwnerID:     userID,
	})
	if err != nil {
		return empty, err
	}

	result := revocationResult{Runtimes: runtimes}

	if len(runtimes) > 0 {
		runtimeIDs := make([]pgtype.UUID, len(runtimes))
		daemonIDs := make([]string, 0, len(runtimes))
		for i, rt := range runtimes {
			runtimeIDs[i] = rt.ID
			if rt.DaemonID.Valid && rt.DaemonID.String != "" {
				daemonIDs = append(daemonIDs, rt.DaemonID.String)
			}
		}

		result.ArchivedAgents, err = qtx.ArchiveAgentsByRuntime(ctx, db.ArchiveAgentsByRuntimeParams{
			ArchivedBy: archivedBy,
			RuntimeIds: runtimeIDs,
		})
		if err != nil {
			return empty, err
		}

		// Cancel by runtime AND by archived agent. agent.runtime_id can be
		// reassigned via UpdateAgent without rewriting the runtime_id on
		// historical agent_task_queue rows, so an archived agent may still
		// have queued/running tasks pinned to a different runtime — and
		// ClaimAgentTask does not gate on agent.archived_at, so those tasks
		// would otherwise stay claimable after the agent is gone.
		archivedAgentIDs := make([]pgtype.UUID, len(result.ArchivedAgents))
		for i, a := range result.ArchivedAgents {
			archivedAgentIDs[i] = a.ID
		}
		result.CancelledTasks, err = qtx.CancelAgentTasksByRuntimeOrAgent(ctx, db.CancelAgentTasksByRuntimeOrAgentParams{
			RuntimeIds: runtimeIDs,
			AgentIds:   archivedAgentIDs,
		})
		if err != nil {
			return empty, err
		}

		result.OfflineRuntimeIDs, err = qtx.ForceOfflineRuntimesByIDs(ctx, runtimeIDs)
		if err != nil {
			return empty, err
		}

		if len(daemonIDs) > 0 {
			result.RevokedTokenHashes, err = qtx.DeleteDaemonTokensByWorkspaceAndDaemons(ctx, db.DeleteDaemonTokensByWorkspaceAndDaemonsParams{
				WorkspaceID: workspaceID,
				DaemonIds:   daemonIDs,
			})
			if err != nil {
				return empty, err
			}
		}
	}

	// Member row deletion lives inside the same tx so a successful revoke is
	// never followed by a failed member-delete (which would leave the user
	// still a member with a dead runtime), and a failed revoke never leaves
	// the user out of the workspace with a still-online runtime.
	if err := qtx.DeleteMember(ctx, memberID); err != nil {
		return empty, err
	}

	if err := tx.Commit(ctx); err != nil {
		return empty, err
	}

	return result, nil
}

// revocationResult captures everything revokeMemberRuntimes touched so the
// caller can fan out events and analytics after the transaction commits.
// Publishing inside the transaction would let subscribers observe a state the
// tx might still roll back (see TaskService.BroadcastCancelledTasks docstring).
type revocationResult struct {
	Runtimes           []db.AgentRuntime
	ArchivedAgents     []db.Agent
	CancelledTasks     []db.AgentTaskQueue
	OfflineRuntimeIDs  []db.ForceOfflineRuntimesByIDsRow
	RevokedTokenHashes []string
}

func (r revocationResult) isEmpty() bool {
	return len(r.Runtimes) == 0
}

// publishRevocation runs all post-commit side effects: invalidate daemon token
// cache, broadcast task:cancelled with per-agent reconciliation, broadcast
// agent:archived, and signal a runtime-list refresh. Safe to call on an empty
// result — it returns immediately.
func (h *Handler) publishRevocation(ctx context.Context, result revocationResult, workspaceIDStr, actorType, actorIDStr string) {
	if result.isEmpty() {
		return
	}

	for _, hash := range result.RevokedTokenHashes {
		h.DaemonTokenCache.Invalidate(ctx, hash)
	}

	// Per-task cancellation: TaskService handles status reconciliation and
	// per-task event broadcast. Run this before the agent:archived burst so
	// subscribers see "task cancelled" before the parent agent disappears
	// from active lists, matching the order ArchiveAgent uses.
	if h.TaskService != nil && len(result.CancelledTasks) > 0 {
		h.TaskService.BroadcastCancelledTasks(ctx, result.CancelledTasks)
	}

	for _, agent := range result.ArchivedAgents {
		h.publish(protocol.EventAgentArchived, workspaceIDStr, actorType, actorIDStr, map[string]any{
			"agent": agentToResponse(agent),
		})
	}

	// Tell connected clients to refresh the runtime list. We piggyback on
	// EventDaemonRegister with a "revoke" action — same channel the runtime
	// delete handler uses — so the frontend invalidates its cached list
	// without us having to introduce a new event type the desktop app would
	// need a build to learn about.
	if len(result.OfflineRuntimeIDs) > 0 {
		h.publish(protocol.EventDaemonRegister, workspaceIDStr, actorType, actorIDStr, map[string]any{
			"action": "revoke",
		})
	}
}

// logRevocation emits a structured info line summarising the revocation. Kept
// separate from publish so the log is identical whether or not the bus is wired.
func logRevocation(result revocationResult, workspaceID, userID string, attrs ...any) {
	if result.isEmpty() {
		return
	}
	base := []any{
		"workspace_id", workspaceID,
		"user_id", userID,
		"runtimes_revoked", len(result.Runtimes),
		"agents_archived", len(result.ArchivedAgents),
		"tasks_cancelled", len(result.CancelledTasks),
		"runtimes_taken_offline", len(result.OfflineRuntimeIDs),
		"daemon_tokens_revoked", len(result.RevokedTokenHashes),
	}
	slog.Info("member runtimes revoked", append(base, attrs...)...)
}
