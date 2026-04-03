package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	// sweepInterval is how often we check for stale runtimes and tasks.
	sweepInterval = 30 * time.Second
	// staleThresholdSeconds marks runtimes offline if no heartbeat for this long.
	// The daemon heartbeat interval is 15s, so 45s = 3 missed heartbeats.
	staleThresholdSeconds = 45.0
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
		}
	}
}

// sweepStaleRuntimes marks runtimes offline if they haven't heartbeated,
// then fails any tasks belonging to those offline runtimes.
func sweepStaleRuntimes(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	staleRows, err := queries.MarkStaleRuntimesOffline(ctx, fmt.Sprintf("%g", staleThresholdSeconds))
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
		workspaces[row.WorkspaceID] = true
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

// sweepStaleTasks fails tasks stuck in dispatched/running for too long,
// even when the runtime is still online. This handles cases where:
// - The agent process hangs and the daemon is still heartbeating
// - The daemon failed to report task completion/failure
// - A server restart left tasks in a non-terminal state
func sweepStaleTasks(ctx context.Context, queries *db.Queries, bus *events.Bus) {
	failedTasks, err := queries.FailStaleTasks(ctx, db.FailStaleTasksParams{
		DispatchTimeoutSecs: fmt.Sprintf("%g", dispatchTimeoutSeconds),
		RunningTimeoutSecs:  fmt.Sprintf("%g", runningTimeoutSeconds),
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
	ID      string
	AgentID string
	IssueID string
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

	affectedAgents := make(map[string]string)

	for _, ft := range items {
		// Look up workspace ID from the issue so the event reaches the right WS room.
		workspaceID := ""
		if issue, err := queries.GetIssue(ctx, ft.IssueID); err == nil {
			workspaceID = issue.WorkspaceID
		}

		bus.Publish(events.Event{
			Type:        protocol.EventTaskFailed,
			WorkspaceID: workspaceID,
			ActorType:   "system",
			Payload: map[string]any{
				"task_id":  ft.ID,
				"agent_id": ft.AgentID,
				"issue_id": ft.IssueID,
				"status":   "failed",
			},
		})

		affectedAgents[ft.AgentID] = ft.AgentID
	}

	// Reconcile status for each affected agent.
	for _, agentID := range affectedAgents {
		reconcileAgentStatus(ctx, queries, bus, agentID)
	}
}

// reconcileAgentStatus checks running task count and updates agent status.
func reconcileAgentStatus(ctx context.Context, queries *db.Queries, bus *events.Bus, agentID string) {
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
		WorkspaceID: agent.WorkspaceID,
		ActorType:   "system",
		Payload:     map[string]any{"agent_id": agent.ID, "status": agent.Status},
	})
}
