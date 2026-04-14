package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
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
)

// runRuntimeSweeper periodically marks runtimes as offline if their
// last_seen_at exceeds the stale threshold, and fails orphaned tasks.
// This handles cases where the daemon crashes, is killed without calling
// the deregister endpoint, or leaves tasks in a non-terminal state.
func runRuntimeSweeper(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepStaleRuntimes(ctx, queries, bus)
			sweepStaleTasks(ctx, queries, bus)
			gcRuntimes(ctx, queries, bus)
		}
	}
}

// sweepStaleRuntimes marks runtimes offline if they haven't heartbeated,
// then fails any tasks belonging to those offline runtimes.
func sweepStaleRuntimes(ctx context.Context, queries *db.Queries, bus *events.Bus) {
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
		broadcastFailedTasks(ctx, queries, bus, failedTasks)
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
func sweepStaleTasks(ctx context.Context, queries *db.Queries, bus *events.Bus) {
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
	broadcastFailedTasks(ctx, queries, bus, failedTasks)
}

// failedTask is a common interface for both sweeper result types.
type failedTask struct {
	ID      pgtype.UUID
	AgentID pgtype.UUID
	IssueID pgtype.UUID
}

// broadcastFailedTasks publishes task:failed events with the correct WorkspaceID
// and reconciles agent status for all affected agents.
func broadcastFailedTasks(ctx context.Context, queries *db.Queries, bus *events.Bus, tasks any) {
	var items []failedTask
	switch ts := tasks.(type) {
	case []db.FailStaleTasksRow:
		for _, t := range ts {
			items = append(items, failedTask{ID: t.ID, AgentID: t.AgentID, IssueID: t.IssueID})
		}
	case []db.FailTasksForOfflineRuntimesRow:
		for _, t := range ts {
			items = append(items, failedTask{ID: t.ID, AgentID: t.AgentID, IssueID: t.IssueID})
		}
	}

	affectedAgents := make(map[string]pgtype.UUID)
	processedIssues := make(map[string]bool)

	for _, ft := range items {
		// Look up workspace ID from the issue so the event reaches the right WS room.
		workspaceID := ""
		if issue, err := queries.GetIssue(ctx, ft.IssueID); err == nil {
			workspaceID = util.UUIDToString(issue.WorkspaceID)
			// If the issue is still in_progress and no other active tasks remain,
			// reset it back to todo so the daemon can pick it up again.
			issueKey := util.UUIDToString(ft.IssueID)
			if issue.Status == "in_progress" && !processedIssues[issueKey] {
				processedIssues[issueKey] = true
				hasActive, checkErr := queries.HasActiveTaskForIssue(ctx, ft.IssueID)
				if checkErr != nil {
					slog.Warn("runtime sweeper: failed to check active tasks for issue",
						"issue_id", issueKey,
						"error", checkErr,
					)
				} else if !hasActive {
					if _, updateErr := queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
						ID:     ft.IssueID,
						Status: "todo",
					}); updateErr != nil {
						slog.Warn("runtime sweeper: failed to reset stuck issue to todo",
							"issue_id", issueKey,
							"error", updateErr,
						)
					}
				}
			}
		}

		bus.Publish(events.Event{
			Type:        protocol.EventTaskFailed,
			WorkspaceID: workspaceID,
			ActorType:   "system",
			Payload: map[string]any{
				"task_id":  util.UUIDToString(ft.ID),
				"agent_id": util.UUIDToString(ft.AgentID),
				"issue_id": util.UUIDToString(ft.IssueID),
				"status":   "failed",
			},
		})

		agentKey := util.UUIDToString(ft.AgentID)
		affectedAgents[agentKey] = ft.AgentID
	}

	// Reconcile status for each affected agent.
	for _, agentID := range affectedAgents {
		reconcileAgentStatus(ctx, queries, bus, agentID)
	}
}

// reconcileAgentStatus checks running task count and updates agent status.
func reconcileAgentStatus(ctx context.Context, queries *db.Queries, bus *events.Bus, agentID pgtype.UUID) {
	running, err := queries.CountRunningTasks(ctx, agentID)
	if err != nil {
		return
	}
	newStatus := "idle"
	if running > 0 {
		newStatus = "working"
	}
	agent, err := queries.UpdateAgentStatus(ctx, db.UpdateAgentStatusParams{
		ID:     agentID,
		Status: newStatus,
	})
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
