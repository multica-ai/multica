package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/service/contextguard"
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
	// runningTimeoutSeconds fails tasks stuck in 'running' beyond this. It is a
	// coarse server-side backstop keyed on started_at (it does NOT look at task
	// activity) — mainly for runs whose daemon died without reporting. The
	// daemon itself decides stuck-vs-long-running by activity (idle/tool
	// watchdog), so this only needs to sit generously above any realistic single
	// run rather than track a per-run wall-clock cap (MUL-3064).
	runningTimeoutSeconds = 9000.0
	// queuedTTLSeconds expires tasks that have been sitting in 'queued'
	// for longer than this without ever being claimed. This is the cleanup
	// arm of the MUL-1899 backlog fix: even with the dispatch-time
	// admission gate that blocks new enqueues against offline runtimes,
	// tasks already on the queue when a runtime drops off (or that lost
	// the race against a runtime that went offline mid-tick) need a
	// time-bounded exit. 2 hours is conservatively above any reasonable
	// "queued behind a long-running task" window for an online runtime, so we
	// don't expire legitimately-pending work, while still draining the historical
	// 87k autopilot backlog within ~24h once enabled.
	queuedTTLSeconds = 2 * 3600.0
	// queuedExpireBatchSize caps how many queued rows a single sweeper tick
	// transitions to failed. Keeps the sweep transaction short even when
	// the historical backlog is large (~89k at MUL-1899 baseline). At 30s
	// ticks and 500 rows/tick we drain 60k rows/hour worst case — plenty
	// of headroom for the documented backlog without monopolising DB CPU.
	queuedExpireBatchSize = 500

	// MUL-4059: inactivity sweeper tunables. activitySweepBatchSize
	// caps the per-tick fan-out so a busy deploy (hundreds of
	// simultaneously-stalled tasks) cannot monopolise the DB. 200 is
	// small enough to keep the transaction sub-second at p99 and large
	// enough that one tick drains any realistic transient backlog.
	activitySweepBatchSize = 200
	// pendingContextRevalidateSecs is how stale a pending_context row
	// must be before sweepPendingContextTasks revalidates it. 60s keeps
	// the UI responsive when the user links a repo: typically the next
	// tick (within 30s) sees the row, revalidates, and requeues. Long
	// enough that the per-row guard cost (1 SELECT workspace, 0..1
	// SELECT project_resources) doesn't dominate the DB even at the
	// historical ~89k task count.
	pendingContextRevalidateSecs = 60.0
	// pendingContextSweepBatchSize caps the per-tick fan-out of the
	// pending_context revalidation sweep. Same rationale as the
	// inactivity sweeper batch size — drain the backlog in O(batches)
	// ticks without monopolising the DB.
	pendingContextSweepBatchSize = 200
	// pendingContextMaxRevalidations caps how many times a single
	// pending_context row can be revalidated before it is cancelled.
	// 3 strikes over ~3 minutes (60s between ticks * 3) is short enough
	// that a misconfigured workspace does not leave tasks parked
	// forever, but long enough that a slow user (e.g. answering a
	// "link a repo" prompt) can still rescue the task. The counter is
	// implemented in the context_guard JSONB envelope so the schema
	// stays unchanged.
	pendingContextMaxRevalidations = 3
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
			sweepExpiredQueuedTasks(ctx, queries, taskSvc)
			// MUL-4059: the two new ticks below sit AFTER the
			// existing sweep loop so a busy tick (e.g. 5k queued
			// rows to expire) doesn't push them off schedule. They
			// are also bounded by their own batch sizes so they
			// cannot monopolise the DB at the tail end of a tick.
			sweepInactiveTasks(ctx, queries, taskSvc)
			sweepPendingContextTasks(ctx, queries, taskSvc)
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
			obsmetrics.RecordEvent(taskSvc.Analytics, taskSvc.Metrics, analytics.RuntimeOffline(
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
	taskSvc.CaptureLeaseExpiredTasks(ctx, failedTasks)
	taskSvc.HandleFailedTasks(ctx, failedTasks)
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
	taskSvc.CaptureQueuedExpiredTasks(ctx, failedTasks)
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
						queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: t.IssueID, Status: "todo", WorkspaceID: issue.WorkspaceID})
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

// ============================================================================
// MUL-4059: max-inactivity sweeper + no-context revalidation
//
// Both functions follow the same shape as sweepStaleTasks /
// sweepExpiredQueuedTasks: read a batch of candidates, fail / requeue
// them in one round-trip, then hand the rows to TaskService so the
// usual HandleFailedTasks / NotifyTaskEnqueued / broadcast pipeline
// runs. The DB writes themselves happen inside the sqlc queries
// (FailInactiveRunningTasks, MarkAgentTaskRequeued,
// MarkAgentTaskPendingContextCancelled) so the sweeper stays
// O(1) DB round-trips per tick regardless of the batch size.
// ============================================================================

// sweepInactiveTasks fails 'running' tasks whose daemon hasn't produced
// any server-visible activity for the per-task inactivity cap stored
// on agent_task_queue.max_inactivity_secs. P0-2 review fix: the cap is
// now read per-row via COALESCE inside FailInactiveRunningTasks, not
// from a single scalar the caller passes in. The task-level / agent-
// level / workspace-level / server-default chain resolved at enqueue
// time is finally respected by the sweeper.
//
// Failures funnel through HandleFailedTasks so the issue rollback /
// agent-status reconcile / auto-retry semantics match the existing
// 'timeout' / 'inactivity_timeout' pipelines. retryableReasons
// (task.go) is the gate that decides whether the auto-retry path
// resurrects the task.
func sweepInactiveTasks(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService) {
	defaultSecs := inactivityDefaultSecs(taskSvc)

	failed, err := queries.FailInactiveRunningTasks(ctx)
	if err != nil {
		slog.Warn("inactivity sweeper: failed to mark inactive tasks", "error", err)
		return
	}
	if len(failed) == 0 {
		return
	}
	slog.Info("inactivity sweeper: failed inactive tasks",
		"count", len(failed), "default_max_inactivity_secs", defaultSecs)
	if taskSvc != nil {
		taskSvc.CaptureLeaseExpiredTasks(ctx, failed)
		taskSvc.HandleFailedTasks(ctx, failed)
	}
}

// inactivityDefaultSecs returns the configured server default for the
// inactivity cap. Reads from the TaskService if set, otherwise returns
// the package-level constant (1200s = 20 min). Used for log messages
// only — the SQL has its own hardcoded fallback (also 1200) so the two
// stay in lockstep.
//
// Kept as a free function (rather than a TaskService method) so the
// sweeper test stub path can call it without constructing a full
// TaskService.
func inactivityDefaultSecs(taskSvc *service.TaskService) int {
	if taskSvc != nil && taskSvc.InactivityDefaultSecs > 0 {
		return taskSvc.InactivityDefaultSecs
	}
	return 1200
}

// sweepPendingContextTasks revalidates tasks stuck in 'pending_context'
// because the no-context guard rejected them. For each row older than
// pendingContextRevalidateSecs, the function consults the context guard
// again:
//
//   - the guard passes -> MarkAgentTaskRequeued -> notifyTaskAvailable
//   - the guard still fails -> check the per-row revalidation count; if
//     below pendingContextMaxRevalidations, refresh the
//     context_guard_checked_at timestamp so the next tick considers it
//     fresh; else MarkAgentTaskPendingContextCancelled -> HandleFailedTasks
//
// The revalidation counter is kept inside the context_guard JSONB envelope
// (revalidations field) so the schema stays untouched. Counter overflow
// is impossible (max 3) but the check defends against a row that was
// hand-edited by an operator.
func sweepPendingContextTasks(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService) {
	pending, err := queries.ListPendingContextTasks(ctx, db.ListPendingContextTasksParams{
		StaleSecs: pendingContextRevalidateSecs,
		RowLimit:  int32(pendingContextSweepBatchSize),
	})
	if err != nil {
		slog.Warn("pending-context sweeper: failed to list pending rows", "error", err)
		return
	}
	if len(pending) == 0 {
		return
	}

	requeued := 0
	cancelled := 0
	for _, task := range pending {
		workspaceID := workspaceIDFromTask(ctx, queries, task)
		if !workspaceID.Valid {
			// P1-6 review fix: the original draft `continue`d on
			// workspace-resolution failure, which means the
			// revalidation counter never advances and the row
			// stays parked forever. Cancel the row instead with
			// a clear failure_reason so the operator / user
			// sees "your workspace was deleted while this task
			// was waiting" rather than the row lingering on the
			// issue card.
			//
			// We cancel as failure (not 'cancelled') so
			// HandleFailedTasks's issue-rollback / agent
			// reconcile path still runs — the auto-retry branch
			// skips it because 'no_context' isn't in
			// retryableReasons, but the issue status flip
			// out of 'blocked' does happen via the broadcast
			// fallthrough.
			cancelledRow, err := queries.MarkAgentTaskPendingContextCancelled(ctx, db.MarkAgentTaskPendingContextCancelledParams{
				ID:    task.ID,
				Error: pgtype.Text{String: "workspace no longer resolvable; pending_context row will not be requeued", Valid: true},
			})
			if err != nil {
				slog.Warn("pending-context sweeper: cancel-on-unresolvable failed",
					"task_id", util.UUIDToString(task.ID), "error", err)
				continue
			}
			cancelled++
			if taskSvc != nil {
				taskSvc.HandleFailedTasks(ctx, []db.AgentTaskQueue{cancelledRow})
			}
			slog.Info("pending-context sweeper: cancelled task (workspace unresolvable)",
				"task_id", util.UUIDToString(task.ID))
			continue
		}

		reason, projectID := guardVerify(ctx, queries, workspaceID, task)
		if reason.OK {
			if _, err := queries.MarkAgentTaskRequeued(ctx, task.ID); err != nil {
				slog.Warn("pending-context sweeper: requeue failed",
					"task_id", util.UUIDToString(task.ID), "error", err)
				continue
			}
			requeued++
			// P1-8 review fix: parkIssueTaskPendingContext flipped
			// the issue to 'blocked'. Now that the guard has
			// passed, flip it back to 'in_progress' so the UI
			// shows the task is actually running, not waiting on
			// the user. The flip is best-effort: a stale
			// 'in_progress' write against an issue the user has
			// since closed is harmless (the next status mutation
			// wins). The companion system comment is the same
			// idea: a one-liner the user sees in the issue
			// timeline explaining "the context guard now passes,
			// resuming".
			if taskSvc != nil && task.IssueID.Valid {
				resumeIssueFromBlocked(ctx, queries, task, workspaceID)
			}
			if taskSvc != nil {
				// Notify the daemon WS the same way Enqueue* does
				// — close the gap between "guard passed" and
				// "claim path picks the row up". Without this the
				// row sits in 'queued' for up to the daemon poll
				// interval (30s default).
				taskSvc.NotifyTaskEnqueued(ctx, task)
			}
			slog.Info("pending-context sweeper: requeued task after guard passed",
				"task_id", util.UUIDToString(task.ID),
				"workspace_id", util.UUIDToString(workspaceID),
				"project_id", util.UUIDToString(projectID))
			continue
		}

		// Guard still failing. Decide whether to retry the check
		// next tick or cancel now.
		count, rawBytes := pendingContextRevalidationCount(task.ContextGuard)
		if count >= pendingContextMaxRevalidations {
			reasonBytes, _ := json.Marshal(reason)
			cancelledRow, err := queries.MarkAgentTaskPendingContextCancelled(ctx, db.MarkAgentTaskPendingContextCancelledParams{
				ID:    task.ID,
				Error: pgtype.Text{String: string(reasonBytes), Valid: true},
			})
			if err != nil {
				slog.Warn("pending-context sweeper: cancel failed",
					"task_id", util.UUIDToString(task.ID), "error", err)
				continue
			}
			cancelled++
			if taskSvc != nil {
				taskSvc.HandleFailedTasks(ctx, []db.AgentTaskQueue{cancelledRow})
			}
			slog.Info("pending-context sweeper: cancelled task after exhausting revalidations",
				"task_id", util.UUIDToString(task.ID),
				"revalidations", count,
				"hint", reason.Hint)
			continue
		}

		// Refresh the JSONB envelope with the new reason and
		// bumped counter so the next tick sees the updated state.
		// We also update context_guard_checked_at via a separate
		// UPDATE because MarkAgentTaskPendingContext is gated on
		// status='queued' and the row is in 'pending_context'.
		updated := pendingContextBumpEnvelope(rawBytes, reason, count+1)
		if err := bumpPendingContextCheckedAt(ctx, queries, task.ID, updated); err != nil {
			slog.Warn("pending-context sweeper: refresh checked_at failed",
				"task_id", util.UUIDToString(task.ID), "error", err)
		}
	}

	if requeued > 0 || cancelled > 0 {
		slog.Info("pending-context sweeper: tick summary",
			"requeued", requeued, "cancelled", cancelled, "scanned", len(pending))
	}
}

// workspaceIDFromTask resolves the workspace ID for a task row.
// Mirrors TaskService.ResolveTaskWorkspaceID but is duplicated here
// to avoid an import cycle (service -> cmd/server is one-way; the
// sweeper is in main package, so it can't reach into the service
// package's exported helper without exposing it).
//
// For issue tasks the workspace comes from the issue. For chat tasks,
// from the chat session. Quick-create tasks have no FK link; we read
// the workspace out of the task context JSONB (workspace_id field).
func workspaceIDFromTask(ctx context.Context, queries *db.Queries, task db.AgentTaskQueue) pgtype.UUID {
	if task.IssueID.Valid {
		if issue, err := queries.GetIssue(ctx, task.IssueID); err == nil {
			return issue.WorkspaceID
		}
	}
	if task.ChatSessionID.Valid {
		if cs, err := queries.GetChatSession(ctx, task.ChatSessionID); err == nil {
			return cs.WorkspaceID
		}
	}
	// Quick-create fallback: workspace is in context JSONB.
	if len(task.Context) > 0 {
		var probe struct {
			WorkspaceID string `json:"workspace_id"`
		}
		if json.Unmarshal(task.Context, &probe) == nil && probe.WorkspaceID != "" {
			return util.MustParseUUID(probe.WorkspaceID)
		}
	}
	return pgtype.UUID{}
}

// guardVerify re-runs the no-context guard for a single task. Returns
// the (possibly-updated) reason and the project ID used for the check
// (used in the audit log so operators can see what was consulted).
//
// Re-implemented here rather than calling the service package helper
// because the service package's helper threads through the daemon
// event-bus side-effects (issue flip + system comment) that we do NOT
// want during a sweep tick — the issue is already blocked from the
// original park, and we only want to verify, not re-notify.
func guardVerify(ctx context.Context, queries *db.Queries, workspaceID pgtype.UUID, task db.AgentTaskQueue) (contextguard.Reason, pgtype.UUID) {
	reason := contextguard.Reason{WorkspaceID: util.UUIDToString(workspaceID)}
	workspace, err := queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		reason.OK = false
		reason.Hint = "workspace not found"
		return reason, pgtype.UUID{}
	}
	// (A) repos check
	var probe []any
	if len(workspace.Repos) > 0 {
		if err := json.Unmarshal(workspace.Repos, &probe); err == nil && len(probe) > 0 {
			reason.HasWorkspaceRepos = true
		}
	}

	// (B) project local_directory check (best-effort project resolution)
	projectID := pendingContextProjectID(ctx, queries, task)
	if projectID.Valid {
		resources, err := queries.ListProjectResources(ctx, projectID)
		if err == nil {
			for _, r := range resources {
				reason.ProjectResources = append(reason.ProjectResources, r.ResourceType)
				if r.ResourceType == "local_directory" {
					reason.HasLocalDirectory = true
				}
			}
		}
	}

	reason.OK = reason.HasWorkspaceRepos || reason.HasLocalDirectory
	if reason.OK {
		reason.Hint = "context guard now passes"
	} else {
		reason.Hint = "context guard still failing"
	}
	return reason, projectID
}

// pendingContextProjectID extracts the project ID for the revalidation
// check. For issue tasks the project lives on the issue row and is
// fetched on demand; for chat / quick-create tasks no project is
// available and the function returns an invalid UUID (the guard
// short-circuits to the (A) repos-only check, which is the right
// behaviour for the (B)-only path because chat / quick-create have
// no project_id by definition).
//
// P1-5 review fix: the original implementation returned an invalid
// UUID unconditionally ("avoid a per-row round-trip"). That silently
// disabled the (B) local_directory branch for issue-driven
// pending_context tasks, so a workspace whose only context is a
// project local_directory (no repos) would never see the guard
// re-evaluate true — the row was eventually cancelled after 3
// revalidation attempts even though the user had supplied valid
// context. The cost of the GetIssue round-trip is acceptable
// because (a) partial index keeps the candidate set tiny and (b)
// the revalidation path already issues a GetWorkspace SELECT per
// row, so one more PK lookup is rounding error.
func pendingContextProjectID(ctx context.Context, queries *db.Queries, task db.AgentTaskQueue) pgtype.UUID {
	if !task.IssueID.Valid {
		return pgtype.UUID{}
	}
	issue, err := queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		// Workspace lookup at the next call site will also fail if
		// the issue is gone; the workspaceIDFromTask helper falls
		// through to a non-blocking skip. The pending_context
		// revalidation count never advances in that case, so
		// the workspace-lookup-failure safety net (P1-6) takes
		// over and cancels the row. Returning an invalid UUID
		// here is therefore the conservative choice — we don't
		// pretend we have a project when we don't.
		return pgtype.UUID{}
	}
	return issue.ProjectID
}

// pendingContextRevalidationCount parses the context_guard JSONB
// envelope to extract the revalidations counter. Returns the raw bytes
// alongside so the sweeper can re-encode without losing unknown fields
// the front-end might have added.
//
// A malformed or absent counter starts at 0 — same as "no previous
// revalidation attempts". This means a hand-edited row is treated as
// fresh, which is fine: the next guard failure will increment to 1
// and the cancellation logic still kicks in at 3.
func pendingContextRevalidationCount(raw []byte) (int, []byte) {
	if len(raw) == 0 {
		return 0, raw
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return 0, raw
	}
	val, ok := probe["revalidations"]
	if !ok {
		return 0, raw
	}
	if n, ok := val.(float64); ok && n >= 0 {
		return int(n), raw
	}
	return 0, raw
}

// pendingContextBumpEnvelope returns a new JSONB payload with the
// revalidations counter incremented and the latest reason embedded.
// Unknown fields are preserved so the audit trail keeps its shape.
func pendingContextBumpEnvelope(raw []byte, reason contextguard.Reason, newCount int) []byte {
	if newCount < 1 {
		newCount = 1
	}
	var probe map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &probe)
	}
	if probe == nil {
		probe = map[string]any{}
	}
	probe["revalidations"] = newCount
	probe["policy"] = string(reason.Policy)
	probe["ok"] = reason.OK
	probe["workspace_id"] = reason.WorkspaceID
	probe["project_id"] = reason.ProjectID
	probe["has_workspace_repos"] = reason.HasWorkspaceRepos
	probe["has_local_directory"] = reason.HasLocalDirectory
	probe["hint"] = reason.Hint
	probe["checked_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	out, err := json.Marshal(probe)
	if err != nil {
		// Fall back to the raw bytes so the column doesn't go NULL;
		// the next sweep tick then has fresh material to work with.
		return raw
	}
	return out
}

// bumpPendingContextCheckedAt writes the new envelope and bumps
// context_guard_checked_at in a single statement via the sqlc-generated
// RefreshPendingContextEnvelope query. Defined as a thin wrapper so
// the call site in sweepPendingContextTasks stays symmetric with the
// other per-task helpers (MarkAgentTaskRequeued,
// MarkAgentTaskPendingContextCancelled).
func bumpPendingContextCheckedAt(ctx context.Context, queries *db.Queries, taskID pgtype.UUID, payload []byte) error {
	if !taskID.Valid {
		return fmt.Errorf("invalid task id")
	}
	return queries.RefreshPendingContextEnvelope(ctx, db.RefreshPendingContextEnvelopeParams{
		ID:           taskID,
		ContextGuard: payload,
	})
}

// resumeIssueFromBlocked is the P1-8 best-effort companion to the
// requeue path. When the no-context guard rejected a task the park
// helper flipped the issue to 'blocked'; now that the guard has
// passed on a revalidate, the issue should flip back to
// 'in_progress' so the UI stops showing the parked card. Best-effort
// because the user may have closed the issue between park and
// revalidate — a stale UPDATE against a closed issue is harmless
// (the next status mutation wins) but a noisy log on every tick
// would be. We also skip the flip if the issue is already in a
// non-blocked state (in_progress / in_review) to avoid pointless
// writes.
//
// The companion system comment uses the same author attribution
// pattern as parkIssueTaskPendingContext (workspace_id, agent_id),
// so a future sweep that needs to surface "we unsuspended you" can
// query by author.
func resumeIssueFromBlocked(ctx context.Context, queries *db.Queries, task db.AgentTaskQueue, workspaceID pgtype.UUID) {
	if !task.IssueID.Valid || !workspaceID.Valid {
		return
	}
	issue, err := queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		slog.Warn("pending-context sweeper: failed to load issue for unblock",
			"issue_id", util.UUIDToString(task.IssueID), "error", err)
		return
	}
	// Don't touch a terminal issue (the user already moved on) and
	// don't touch a row already past 'blocked' (e.g. someone set it
	// to in_review while the task was parked).
	if issue.Status == "done" || issue.Status == "cancelled" {
		return
	}
	if issue.Status != "blocked" {
		return
	}
	if _, err := queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:          task.IssueID,
		Status:      "in_progress",
		WorkspaceID: workspaceID,
	}); err != nil {
		slog.Warn("pending-context sweeper: failed to flip issue back to in_progress",
			"issue_id", util.UUIDToString(task.IssueID), "error", err)
		return
	}
	if _, err := queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     task.IssueID,
		WorkspaceID: workspaceID,
		AuthorType:  "system",
		AuthorID:    task.AgentID,
		Content:     "✅ Context guard now passes; resuming the task.",
		Type:        "comment",
	}); err != nil {
		slog.Warn("pending-context sweeper: failed to post 'resuming' system comment",
			"issue_id", util.UUIDToString(task.IssueID), "error", err)
	}
}
