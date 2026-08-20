package handler

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// revokeAndRemoveMember composes the single execution-cleanup implementation
// with native Multica membership removal. Mirrored VIBES authority deliveries
// call applyMemberExecutionCleanup without this final member-row write.
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
// Either way the agent-unbind + task-cancel + force-offline writes are the
// actual production safety net: even if the daemon races back online with a
// still-valid PAT, it finds no runnable agent on the revoked Runtime and no
// dependent queued task to claim — and the member-row deletion in the same tx means subsequent
// requireWorkspaceMember checks will reject the daemon's PAT-authenticated
// requests with 404.
func (h *Handler) revokeAndRemoveMember(ctx context.Context, workspaceID, userID, memberID pgtype.UUID) (revocationResult, error) {
	var empty revocationResult

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)

	// Taken FIRST, before this tx touches member or issue_subscriber. The
	// delegated auto-subscribe rule takes the same (workspace, user) lock, so
	// a run that is mid-decomposition cannot slip a new subscriber row in
	// between this tx's membership delete and its subscription cleanup below.
	// First also means every holder acquires it in the same order, so these
	// paths cannot deadlock against each other (MUL-5483 review round 7).
	if err := qtx.LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	}); err != nil {
		return empty, err
	}

	result, err := h.applyMemberExecutionCleanup(ctx, qtx, workspaceID, userID)
	if err != nil {
		return empty, err
	}

	// A private quick action is visible and runnable ONLY by its creator
	// (quick_action carries no FK, so nothing removes it implicitly). Once the
	// creator is gone the row is unreachable by every remaining member while
	// still consuming the workspace's active-action limit, so drop those in
	// the same tx. Public actions are workspace furniture and survive.
	if err := qtx.DeletePrivateQuickActionsByCreator(ctx, db.DeletePrivateQuickActionsByCreatorParams{
		WorkspaceID: workspaceID,
		CreatedByID: userID,
	}); err != nil {
		return empty, err
	}

	// Saved views follow the private-quick-action rule above: a departed
	// member's PRIVATE views are invisible to everyone left yet still count
	// against quota, and a re-invite must not resurrect them. Shared views
	// stay — they are workspace furniture other members may rely on. The
	// per-user view-bar preferences are meaningless without the member.
	if err := qtx.DeletePrivateIssueViewsByOwner(ctx, db.DeletePrivateIssueViewsByOwnerParams{
		WorkspaceID: workspaceID,
		OwnerID:     userID,
	}); err != nil {
		return empty, err
	}
	if err := qtx.DeleteIssueViewPreferencesByUser(ctx, db.DeleteIssueViewPreferencesByUserParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	}); err != nil {
		return empty, err
	}

	// issue_subscriber carries no FK either (same MUL-3515 rule as the two
	// prunes above), and MUL-5483 gave agents a path that writes member
	// subscriber rows on their own initiative. Dropping them in this tx is what
	// stops a departed member from accruing inbox rows, and stops a re-invite
	// from silently restoring visibility of everything they used to watch.
	if err := qtx.DeleteSubscriptionsByMember(ctx, db.DeleteSubscriptionsByMemberParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	}); err != nil {
		return empty, err
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

// applyMemberExecutionCleanup is the one deep implementation shared by native
// remove/leave and authenticated VIBES restriction delivery. It revokes only
// personal execution resources. Agents are persistent Workspace objects, so
// they are unbound for recovery rather than archived; only Tasks whose Runtime
// or newly-unbound Agent dependency was revoked are cancelled.
func (h *Handler) applyMemberExecutionCleanup(ctx context.Context, qtx *db.Queries, workspaceID, userID pgtype.UUID) (revocationResult, error) {
	var result revocationResult
	runtimes, err := qtx.ListAgentRuntimesByOwner(ctx, db.ListAgentRuntimesByOwnerParams{
		WorkspaceID: workspaceID,
		OwnerID:     userID,
	})
	if err != nil {
		return result, err
	}
	result.Runtimes = runtimes

	runtimeIDs := make([]pgtype.UUID, len(runtimes))
	daemonIDs := make([]string, 0, len(runtimes))
	for index, runtime := range runtimes {
		runtimeIDs[index] = runtime.ID
		if runtime.DaemonID.Valid && runtime.DaemonID.String != "" {
			daemonIDs = append(daemonIDs, runtime.DaemonID.String)
		}
	}
	// Fence Runtime execution before touching Tasks/tokens. Claim finalization
	// holds a share lock on this same row: if an older claim wins, cleanup waits
	// and then deletes its token; if cleanup wins, no new claim can finalize.
	// This ordering prevents a token from being inserted after the cleanup's
	// delete-by-task statement has already run.
	if len(runtimeIDs) > 0 {
		result.OfflineRuntimeIDs, err = qtx.ForceOfflineRuntimesByIDs(ctx, runtimeIDs)
		if err != nil {
			return revocationResult{}, err
		}
	}
	for _, runtime := range runtimes {
		unbound, err := qtx.UnbindUserAgentsFromRuntime(ctx, runtime.ID)
		if err != nil {
			return revocationResult{}, err
		}
		result.UnboundAgents = append(result.UnboundAgents, unbound...)
	}
	unboundAgentIDs := make([]pgtype.UUID, len(result.UnboundAgents))
	for index, agent := range result.UnboundAgents {
		unboundAgentIDs[index] = agent.ID
	}
	if len(unboundAgentIDs) > 0 {
		result.PausedAutopilots, err = qtx.PauseAutopilotsByUnboundAgents(ctx, unboundAgentIDs)
		if err != nil {
			return revocationResult{}, err
		}
	}

	if len(runtimeIDs) > 0 || len(unboundAgentIDs) > 0 {
		result.CancelledTasks, err = qtx.CancelAgentTasksByRuntimeOrAgent(ctx, db.CancelAgentTasksByRuntimeOrAgentParams{
			RuntimeIds: runtimeIDs,
			AgentIds:   unboundAgentIDs,
		})
		if err != nil {
			return revocationResult{}, err
		}
		if len(result.CancelledTasks) > 0 {
			taskIDs := make([]pgtype.UUID, len(result.CancelledTasks))
			for index, task := range result.CancelledTasks {
				taskIDs[index] = task.ID
				if err := qtx.DeleteTaskTokensByTask(ctx, task.ID); err != nil {
					return revocationResult{}, err
				}
			}
			result.CancelledTasks, err = qtx.RecordMemberExecutionDependencyReason(ctx, db.RecordMemberExecutionDependencyReasonParams{
				RuntimeIds: runtimeIDs,
				TaskIds:    taskIDs,
			})
			if err != nil {
				return revocationResult{}, err
			}
		}

		if len(daemonIDs) > 0 {
			result.RevokedTokenHashes, err = qtx.DeleteDaemonTokensByWorkspaceAndDaemons(ctx, db.DeleteDaemonTokensByWorkspaceAndDaemonsParams{
				WorkspaceID: workspaceID,
				DaemonIds:   daemonIDs,
			})
			if err != nil {
				return revocationResult{}, err
			}
			// Invalidating before commit is safe: a rollback merely forces a DB
			// lookup, while a commit can never leave a stale cached token alive.
			for _, hash := range result.RevokedTokenHashes {
				h.DaemonTokenCache.Invalidate(ctx, hash)
			}
		}
	}

	if err := qtx.DeleteChannelUserBindingsByWorkspaceMember(ctx, db.DeleteChannelUserBindingsByWorkspaceMemberParams{
		WorkspaceID: workspaceID, MulticaUserID: userID,
	}); err != nil {
		return revocationResult{}, err
	}
	if err := qtx.DeleteAgentInvocationTargetsByMember(ctx, db.DeleteAgentInvocationTargetsByMemberParams{
		WorkspaceID: workspaceID, TargetID: userID,
	}); err != nil {
		return revocationResult{}, err
	}
	return result, nil
}

// revocationResult captures everything revokeMemberRuntimes touched so the
// caller can fan out events and analytics after the transaction commits.
// Publishing inside the transaction would let subscribers observe a state the
// tx might still roll back (see TaskService.BroadcastCancelledTasks docstring).
type revocationResult struct {
	Runtimes           []db.AgentRuntime
	UnboundAgents      []db.Agent
	CancelledTasks     []db.AgentTaskQueue
	PausedAutopilots   []db.Autopilot
	OfflineRuntimeIDs  []db.ForceOfflineRuntimesByIDsRow
	RevokedTokenHashes []string
}

func (r revocationResult) isEmpty() bool {
	return len(r.Runtimes) == 0
}

// publishRevocation runs all post-commit side effects: broadcast task:cancelled
// with per-agent reconciliation, broadcast agent/autopilot updates, and signal
// a runtime-list refresh. Safe to call on an empty
// result — it returns immediately.
func (h *Handler) publishRevocation(ctx context.Context, result revocationResult, workspaceIDStr, actorType, actorIDStr string) {
	if result.isEmpty() {
		return
	}
	runtimeIDs := make([]pgtype.UUID, 0, len(result.Runtimes))
	for _, runtime := range result.Runtimes {
		runtimeIDs = append(runtimeIDs, runtime.ID)
		if h.LivenessStore != nil {
			h.LivenessStore.Forget(ctx, uuidToString(runtime.ID))
		}
	}
	if h.HeartbeatScheduler != nil {
		h.HeartbeatScheduler.Forget(runtimeIDs)
	}

	// Per-task cancellation: TaskService handles status reconciliation and
	// per-task event broadcast. Run this before agent status updates so
	// subscribers see "task cancelled" before its Runtime binding disappears.
	if h.TaskService != nil && len(result.CancelledTasks) > 0 {
		// The workspace is known and is the one being restricted.
		h.TaskService.BroadcastCancelledTasks(ctx, workspaceIDStr, result.CancelledTasks)
	}

	for _, agent := range result.UnboundAgents {
		h.publish(protocol.EventAgentStatus, workspaceIDStr, actorType, actorIDStr, map[string]any{
			"agent": broadcastAgentResponse(h.agentToResponse(agent)),
		})
	}
	for _, autopilot := range result.PausedAutopilots {
		h.publish(protocol.EventAutopilotUpdated, workspaceIDStr, actorType, actorIDStr, map[string]any{
			"autopilot": autopilotToResponse(autopilot, nil),
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
		"agents_unbound", len(result.UnboundAgents),
		"tasks_cancelled", len(result.CancelledTasks),
		"autopilots_paused", len(result.PausedAutopilots),
		"runtimes_taken_offline", len(result.OfflineRuntimeIDs),
		"daemon_tokens_revoked", len(result.RevokedTokenHashes),
	}
	slog.Info("member runtimes revoked", append(base, attrs...)...)
}
