package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	// sweepInterval is how often we check for stale runtimes and tasks.
	sweepInterval = 30 * time.Second
	// staleThresholdSeconds marks runtimes offline if no heartbeat for this
	// long. Must be strictly greater than runtimeHeartbeatDBFlushInterval
	// (60s in handler/daemon.go) plus one daemon heartbeat cycle (~15s)
	// plus the BatchedHeartbeatScheduler tick interval (~30s) so the DB
	// stale window never trips on an alive-but-DB-lagging runtime when the
	// sweeper's Redis check errors and we fall back to the DB.
	// 150s leaves a 45s buffer above the 105s worst-case DB age, and keeps
	// detection latency for a genuinely-dead runtime under staleThreshold +
	// sweepInterval = 180s (~3 minutes).
	staleThresholdSeconds = 150.0
	// offlineRuntimeTTLSeconds deletes offline runtimes with no active agents
	// after this duration. 7 days gives users plenty of time to restart daemons.
	offlineRuntimeTTLSeconds = 7 * 24 * 3600.0
	// dispatchTimeoutSeconds fails tasks stuck in 'dispatched' beyond this.
	// The dispatched→running transition should be near-instant, so 5 minutes
	// means something went wrong (e.g. StartTask API call failed silently).
	dispatchTimeoutSeconds = 300.0
	// runningTimeoutSeconds fails tasks stuck in 'running' beyond this.
	// The default agent timeout is 2h, so 2.5h gives a generous buffer.
	runningTimeoutSeconds = 9000.0
	// queuedTTLSeconds expires tasks that have been sitting in 'queued'
	// for longer than this without ever being claimed. This is the cleanup
	// arm of the MUL-1899 backlog fix: even with the dispatch-time
	// admission gate that blocks new enqueues against offline runtimes,
	// tasks already on the queue when a runtime drops off (or that lost
	// the race against a runtime that went offline mid-tick) need a
	// time-bounded exit. 2 hours is conservatively above any reasonable
	// "queued behind a long-running task" window for an online runtime
	// (default agent timeout is 2h, sweeper interval is 30s) so we don't
	// expire legitimately-pending work, while still draining the historical
	// 87k autopilot backlog within ~24h once enabled.
	queuedTTLSeconds = 2 * 3600.0
	// queuedExpireBatchSize caps how many queued rows a single sweeper tick
	// transitions to failed. Keeps the sweep transaction short even when
	// the historical backlog is large (~89k at MUL-1899 baseline). At 30s
	// ticks and 500 rows/tick we drain 60k rows/hour worst case — plenty
	// of headroom for the documented backlog without monopolising DB CPU.
	queuedExpireBatchSize = 500
)

// runRuntimeSweeper periodically marks runtimes as offline if their
// last_seen_at exceeds the stale threshold, and fails orphaned tasks.
// This handles cases where the daemon crashes, is killed without calling
// the deregister endpoint, or leaves tasks in a non-terminal state.
//
// liveness is consulted before flipping any candidate to offline: when the
// LivenessStore is available and reports the runtime as alive, we skip the
// row even though its DB last_seen_at is old (Redis is the authority on the
// hot heartbeat path; the DB is allowed to lag up to runtimeHeartbeatDBFlushInterval).
// When liveness is unavailable or errors, we fall back to trusting the DB
// stale window — that is the original behavior.
func runRuntimeSweeper(ctx context.Context, queries *db.Queries, liveness handler.LivenessStore, taskSvc *service.TaskService, bus *events.Bus) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepStaleRuntimes(ctx, queries, liveness, taskSvc, bus)
			sweepStaleTasks(ctx, queries, taskSvc, bus)
			sweepExpiredClaimLeases(ctx, taskSvc)
			sweepExpiredQueuedTasks(ctx, queries, taskSvc)
			gcRuntimes(ctx, queries, bus)
		}
	}
}

// sweepStaleRuntimes marks runtimes offline if they haven't heartbeated,
// then fails any tasks belonging to those offline runtimes.
func sweepStaleRuntimes(ctx context.Context, queries *db.Queries, liveness handler.LivenessStore, taskSvc *service.TaskService, bus *events.Bus) {
	candidates, err := queries.SelectStaleOnlineRuntimes(ctx, staleThresholdSeconds)
	if err != nil {
		slog.Warn("runtime sweeper: failed to list stale online runtimes", "error", err)
		return
	}
	if len(candidates) == 0 {
		return
	}

	toOffline := filterStaleRuntimesByLiveness(ctx, candidates, liveness)
	if len(toOffline) == 0 {
		return
	}

	staleRows, err := queries.MarkRuntimesOfflineByIDs(ctx, db.MarkRuntimesOfflineByIDsParams{
		Ids:          toOffline,
		StaleSeconds: staleThresholdSeconds,
	})
	if err != nil {
		slog.Warn("runtime sweeper: failed to mark stale runtimes offline", "error", err)
		return
	}
	if len(staleRows) == 0 {
		// All filtered candidates raced into a non-online state between the
		// SELECT and the UPDATE. Nothing to broadcast.
		return
	}
	if taskSvc != nil && taskSvc.Analytics != nil {
		for _, row := range staleRows {
			taskSvc.Analytics.Capture(analytics.RuntimeOffline(
				util.UUIDToString(row.OwnerID),
				util.UUIDToString(row.WorkspaceID),
				util.UUIDToString(row.ID),
				row.DaemonID.String,
				row.Provider,
			))
		}
	}

	// Collect unique workspace IDs to notify.
	workspaces := make(map[string]bool)
	for _, row := range staleRows {
		wsID := util.UUIDToString(row.WorkspaceID)
		workspaces[wsID] = true
	}

	// Drop liveness records for confirmed-offline runtimes so a future
	// MGET sweep doesn't see a stray key keep them "alive". TTLs would
	// reap these eventually, but explicit cleanup is cheap and clearer.
	if liveness.Available() {
		for _, row := range staleRows {
			liveness.Forget(ctx, util.UUIDToString(row.ID))
		}
	}

	slog.Info("runtime sweeper: marked stale runtimes offline", "count", len(staleRows), "workspaces", len(workspaces))

	// Fail orphaned tasks (dispatched/running) whose runtimes just went offline.
	failedTasks, err := queries.FailTasksForOfflineRuntimes(ctx)
	if err != nil {
		slog.Warn("runtime sweeper: failed to clean up stale tasks", "error", err)
	} else if len(failedTasks) > 0 {
		slog.Info("runtime sweeper: failed orphaned tasks", "count", len(failedTasks))
		taskSvc.HandleFailedTasks(ctx, failedTasks)
	}

	// Notify frontend clients so they re-fetch runtime list.
	for wsID := range workspaces {
		bus.Publish(events.Event{
			Type:        protocol.EventDaemonRegister,
			WorkspaceID: wsID,
			ActorType:   "system",
			Payload: map[string]any{
				"action": "stale_sweep",
			},
		})
	}
}

// filterStaleRuntimesByLiveness narrows a SELECT-of-stale-candidates down to
// the set that should actually be flipped offline. When liveness is available
// and reports a candidate as alive, we skip it (DB is just lagging). When the
// store is unavailable or errors, we trust the DB stale window — i.e. every
// candidate flips, matching the legacy MarkStaleRuntimesOffline behavior.
func filterStaleRuntimesByLiveness(ctx context.Context, candidates []db.SelectStaleOnlineRuntimesRow, liveness handler.LivenessStore) []pgtype.UUID {
	ids := make([]pgtype.UUID, 0, len(candidates))
	if !liveness.Available() {
		for _, c := range candidates {
			ids = append(ids, c.ID)
		}
		return ids
	}
	idStrs := make([]string, len(candidates))
	for i, c := range candidates {
		idStrs[i] = util.UUIDToString(c.ID)
	}
	alive, ok := liveness.IsAliveBatch(ctx, idStrs)
	if !ok {
		// Store hiccup: degrade to DB-only behavior for this tick.
		for _, c := range candidates {
			ids = append(ids, c.ID)
		}
		return ids
	}
	for i, c := range candidates {
		if alive[idStrs[i]] {
			continue
		}
		ids = append(ids, c.ID)
	}
	return ids
}

// gcRuntimes deletes offline runtimes that have exceeded the TTL and have
// no active (non-archived) agents. Before deleting, it cleans up any
// archived agents so the FK constraint (ON DELETE RESTRICT) doesn't block.
func gcRuntimes(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	deleted, err := queries.DeleteStaleOfflineRuntimes(ctx, offlineRuntimeTTLSeconds)
	if err != nil {
		slog.Warn("runtime GC: failed to delete stale offline runtimes", "error", err)
		return
	}
	if len(deleted) == 0 {
		return
	}

	gcWorkspaces := make(map[string]bool)
	for _, row := range deleted {
		gcWorkspaces[util.UUIDToString(row.WorkspaceID)] = true
	}

	slog.Info("runtime GC: deleted stale offline runtimes", "count", len(deleted), "workspaces", len(gcWorkspaces))

	for wsID := range gcWorkspaces {
		bus.Publish(events.Event{
			Type:        protocol.EventDaemonRegister,
			WorkspaceID: wsID,
			ActorType:   "system",
			Payload: map[string]any{
				"action": "runtime_gc",
			},
		})
	}
}

// sweepStaleTasks fails tasks stuck in dispatched/running for too long,
// even when the runtime is still online. This handles cases where:
// - The agent process hangs and the daemon is still heartbeating
// - The daemon failed to report task completion/failure
// - A server restart left tasks in a non-terminal state
func sweepStaleTasks(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, bus *events.Bus) {
	failedTasks, err := queries.FailStaleTasks(ctx, db.FailStaleTasksParams{
		DispatchTimeoutSecs: dispatchTimeoutSeconds,
		RunningTimeoutSecs:  runningTimeoutSeconds,
	})
	if err != nil {
		slog.Warn("task sweeper: failed to clean up stale tasks", "error", err)
		return
	}
	if len(failedTasks) == 0 {
		return
	}

	slog.Info("task sweeper: failed stale tasks", "count", len(failedTasks))
	taskSvc.HandleFailedTasks(ctx, failedTasks)
}

// sweepExpiredClaimLeases is the global backstop that requeues dispatched
// tasks whose claim lease has expired. Only requeues tasks whose runtime has
// a fresh heartbeat (last_seen_at within staleThresholdSeconds), preventing
// requeue to a dead runtime in the 90s gap between lease expiry (60s) and
// offline detection (150s). The primary requeue path is the preflight in
// ClaimTaskForRuntime (self-requeue when the runtime actively claims).
func sweepExpiredClaimLeases(ctx context.Context, taskSvc *service.TaskService) {
	if taskSvc == nil {
		return
	}
	taskSvc.RequeueExpiredClaimLeases(ctx, staleThresholdSeconds)
}

// sweepExpiredQueuedTasks fails tasks that have been sitting in 'queued' for
// longer than the TTL. Companion to the dispatch-time admission gate added
// in MUL-1899: that gate prevents new doomed enqueues; this gate drains the
// historical backlog and catches the race where a runtime goes offline AFTER
// a task is already queued. Capped to queuedExpireBatchSize per tick so a
// big backlog can't monopolise the DB.
func sweepExpiredQueuedTasks(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService) {
	failedTasks, err := queries.ExpireStaleQueuedTasks(ctx, db.ExpireStaleQueuedTasksParams{
		TtlSecs:    queuedTTLSeconds,
		MaxPerTick: queuedExpireBatchSize,
	})
	if err != nil {
		slog.Warn("task sweeper: failed to expire stale queued tasks", "error", err)
		return
	}
	if len(failedTasks) == 0 {
		return
	}

	slog.Info("task sweeper: expired stale queued tasks", "count", len(failedTasks))
	taskSvc.HandleFailedTasks(ctx, failedTasks)
}

// broadcastFailedTasks is preserved as a thin shim for the integration tests
// in this package. New call sites should use TaskService.HandleFailedTasks
// directly so the side effects (event broadcast, agent reconcile, issue
// rollback, auto-retry) are guaranteed in one place.
func broadcastFailedTasks(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, bus *events.Bus, tasks []db.AgentTaskQueue) {
	if taskSvc != nil {
		taskSvc.HandleFailedTasks(ctx, tasks)
		return
	}
	// Fallback path used by tests that don't construct a TaskService:
	// publish task:failed events with workspace IDs and reset stuck issues.
	processedIssues := make(map[string]bool)
	affectedAgents := make(map[string]pgtype.UUID)
	for _, t := range tasks {
		failureReason := "agent_error"
		if t.FailureReason.Valid && t.FailureReason.String != "" {
			failureReason = t.FailureReason.String
		}
		workspaceID := ""
		if t.IssueID.Valid {
			if issue, err := queries.GetIssue(ctx, t.IssueID); err == nil {
				workspaceID = util.UUIDToString(issue.WorkspaceID)
				issueKey := util.UUIDToString(t.IssueID)
				if issue.Status == "in_progress" && !processedIssues[issueKey] {
					processedIssues[issueKey] = true
					if hasActive, herr := queries.HasActiveTaskForIssue(ctx, t.IssueID); herr == nil && !hasActive {
						queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: t.IssueID, Status: "todo"})
					}
				}
			}
		}
		bus.Publish(events.Event{
			Type:        protocol.EventTaskFailed,
			WorkspaceID: workspaceID,
			ActorType:   "system",
			Payload: map[string]any{
				"task_id":        util.UUIDToString(t.ID),
				"agent_id":       util.UUIDToString(t.AgentID),
				"issue_id":       util.UUIDToString(t.IssueID),
				"status":         "failed",
				"failure_reason": failureReason,
			},
		})
		affectedAgents[util.UUIDToString(t.AgentID)] = t.AgentID
	}
	for _, agentID := range affectedAgents {
		reconcileAgentStatus(ctx, queries, bus, agentID)
	}
}

// reconcileAgentStatus refreshes agent status from the current active task set.
// Used only by the test-fallback path of broadcastFailedTasks above.
func reconcileAgentStatus(ctx context.Context, queries *db.Queries, bus *events.Bus, agentID pgtype.UUID) {
	agent, err := queries.RefreshAgentStatusFromTasks(ctx, agentID)
	if err != nil {
		return
	}
	bus.Publish(events.Event{
		Type:        protocol.EventAgentStatus,
		WorkspaceID: util.UUIDToString(agent.WorkspaceID),
		ActorType:   "system",
		Payload:     map[string]any{"agent_id": util.UUIDToString(agent.ID), "status": agent.Status},
	})
}
