package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	// sweepInterval is how often we check for stale runtimes and tasks.
	sweepInterval = 30 * time.Second
	// staleThresholdSeconds marks runtimes offline if no heartbeat for this long.
	// The daemon heartbeat interval is 15s, so 45s = 3 missed heartbeats.
	staleThresholdSeconds = 45.0
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
	// autopilotRunTimeoutSeconds fails autopilot_run rows stuck in
	// 'issue_created' or 'running' beyond this. 6 hours covers the longest
	// realistic agent run with generous headroom.
	autopilotRunTimeoutSeconds = 6 * 3600.0
)

// runRuntimeSweeper periodically marks runtimes as offline if their
// last_seen_at exceeds the stale threshold, and fails orphaned tasks.
// This handles cases where the daemon crashes, is killed without calling
// the deregister endpoint, or leaves tasks in a non-terminal state.
func runRuntimeSweeper(ctx context.Context, queries *db.Queries, bus *events.Bus, taskSvc *service.TaskService) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepStaleRuntimes(ctx, queries, bus, taskSvc)
			sweepStaleTasks(ctx, queries, bus, taskSvc)
			sweepStaleAutopilotRuns(ctx, queries, bus)
			gcRuntimes(ctx, queries, bus)
		}
	}
}

// sweepStaleRuntimes marks runtimes offline if they haven't heartbeated,
// then fails any tasks belonging to those offline runtimes.
func sweepStaleRuntimes(ctx context.Context, queries *db.Queries, bus *events.Bus, taskSvc *service.TaskService) {
	staleRows, err := queries.MarkStaleRuntimesOffline(ctx, staleThresholdSeconds)
	if err != nil {
		slog.Warn("runtime sweeper: failed to mark stale runtimes offline", "error", err)
		return
	}
	if len(staleRows) == 0 {
		return
	}

	// Collect unique workspace IDs to notify.
	workspaces := make(map[string]bool)
	for _, row := range staleRows {
		wsID := util.UUIDToString(row.WorkspaceID)
		workspaces[wsID] = true
	}

	slog.Info("runtime sweeper: marked stale runtimes offline", "count", len(staleRows), "workspaces", len(workspaces))

	// Fail orphaned tasks (dispatched/running) whose runtimes just went offline.
	failedTasks, err := queries.FailTasksForOfflineRuntimes(ctx)
	if err != nil {
		slog.Warn("runtime sweeper: failed to clean up stale tasks", "error", err)
	} else if len(failedTasks) > 0 {
		slog.Info("runtime sweeper: failed orphaned tasks", "count", len(failedTasks))
		handleSweptFailures(ctx, queries, taskSvc, failedTasks, "runtime went offline")
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
func sweepStaleTasks(ctx context.Context, queries *db.Queries, bus *events.Bus, taskSvc *service.TaskService) {
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
	handleSweptFailures(ctx, queries, taskSvc, failedTasks, "task timed out")
}

// handleSweptFailures hands every freshly-failed task off to
// TaskService.HandleTaskFailure, which broadcasts task:failed, attempts a
// retry per the failure class, resets the issue when no retry is
// scheduled, and reconciles agent status. defaultErrMsg seeds the
// classifier when the row's free-form error column doesn't pin down the
// reason on its own.
func handleSweptFailures(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, tasks any, defaultErrMsg string) {
	var ids []pgtype.UUID
	switch ts := tasks.(type) {
	case []db.FailStaleTasksRow:
		for _, t := range ts {
			ids = append(ids, t.ID)
		}
	case []db.FailTasksForOfflineRuntimesRow:
		for _, t := range ts {
			ids = append(ids, t.ID)
		}
	}

	for _, id := range ids {
		task, err := queries.GetAgentTask(ctx, id)
		if err != nil {
			slog.Warn("sweeper: failed to load just-failed task", "task_id", util.UUIDToString(id), "error", err)
			continue
		}
		errMsg := defaultErrMsg
		if task.Error.Valid && task.Error.String != "" {
			errMsg = task.Error.String
		}
		taskSvc.HandleTaskFailure(ctx, task, errMsg)
	}
}

// sweepStaleAutopilotRuns fails autopilot runs stuck in 'issue_created' or
// 'running' beyond autopilotRunTimeoutSeconds. This covers two cases:
// - create_issue runs whose linked issue never reached a terminal status
// - run_only runs whose task completion was never recorded (e.g. server restart)
func sweepStaleAutopilotRuns(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	rows, err := queries.FailStaleAutopilotRuns(ctx, autopilotRunTimeoutSeconds)
	if err != nil {
		slog.Warn("autopilot run sweeper: failed to sweep stale runs", "error", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	slog.Info("autopilot run sweeper: failed stale runs", "count", len(rows))

	// Publish one event per run so clients can update each row individually.
	for _, row := range rows {
		ap, err := queries.GetAutopilot(ctx, row.AutopilotID)
		if err != nil {
			slog.Warn("autopilot run sweeper: failed to load autopilot", "autopilot_id", util.UUIDToString(row.AutopilotID), "error", err)
			continue
		}
		bus.Publish(events.Event{
			Type:        protocol.EventAutopilotRunDone,
			WorkspaceID: util.UUIDToString(ap.WorkspaceID),
			ActorType:   "system",
			Payload: map[string]any{
				"run_id":       util.UUIDToString(row.ID),
				"autopilot_id": util.UUIDToString(row.AutopilotID),
				"status":       "failed",
			},
		})
	}
}

