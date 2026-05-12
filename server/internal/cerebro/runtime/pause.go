package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Cerebro-only WS event types for the pause/unpause lifecycle. They are
// defined here (not in upstream's pkg/protocol/events.go) because the
// pause feature is cerebro-only — adding upstream constants for a
// downstream-only event would require a CEREBRO-PATCH on every upstream
// merge.
const (
	EventRuntimePaused   = "runtime:paused"
	EventRuntimeUnpaused = "runtime:unpaused"
)

// Service exposes the cerebro pause/unpause business logic. It owns the
// cerebrodb queries that touch the upstream agent_runtime / agent_task_queue
// tables (with cerebro-added pause columns) and delegates downstream side
// effects — agent reconciliation, issue reset, retry — to the upstream
// TaskService via the already-exported HandleFailedTasks entry point.
type Service struct {
	Cerebro *cerebrodb.Queries
	TaskSvc *service.TaskService
	Bus     *events.Bus
}

// New constructs the cerebro runtime pause Service. Called from the router
// during server bootstrap with the already-constructed cerebrodb queries,
// upstream TaskService, and event bus.
func New(cerebro *cerebrodb.Queries, taskSvc *service.TaskService, bus *events.Bus) *Service {
	return &Service{Cerebro: cerebro, TaskSvc: taskSvc, Bus: bus}
}

// PauseOptions is the cerebro-pkg alias for handler.RuntimePauseOptions.
// Re-exported so internal pkg callers don't have to import the handler pkg
// just to construct an option struct.
type PauseOptions = handler.RuntimePauseOptions

// PauseRuntime marks a runtime paused, suspends in-flight work, and
// broadcasts the state change. Idempotent — re-pausing an already-paused
// runtime updates UnpauseAt/Reason without resetting paused_at.
//
// In-flight tasks (status dispatched/running) are marked failed with
// failure_reason='runtime_paused' so the unpause path can identify and
// resume them. Queued tasks are left alone — they'll simply not be claimed
// while the runtime is paused (see the dispatch-side gate in daemon.go).
func (s *Service) PauseRuntime(ctx context.Context, runtimeID pgtype.UUID, opts PauseOptions) (handler.RuntimePauseState, error) {
	var unpauseAt pgtype.Timestamptz
	if !opts.UnpauseAt.IsZero() {
		unpauseAt = pgtype.Timestamptz{Time: opts.UnpauseAt.UTC(), Valid: true}
	}
	var reason pgtype.Text
	if opts.Reason != "" {
		reason = pgtype.Text{String: opts.Reason, Valid: true}
	}

	rt, err := s.Cerebro.PauseAgentRuntime(ctx, cerebrodb.PauseAgentRuntimeParams{
		ID:          runtimeID,
		UnpauseAt:   unpauseAt,
		PauseReason: reason,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return handler.RuntimePauseState{}, fmt.Errorf("pause runtime: runtime not found")
		}
		return handler.RuntimePauseState{}, fmt.Errorf("pause runtime: %w", err)
	}

	suspended, err := s.Cerebro.SuspendActiveTasksForRuntime(ctx, runtimeID)
	if err != nil {
		// Runtime row is already paused; surface the partial-failure but
		// don't roll back. The sweeper-driven HandleFailedTasks reconciler
		// will retry agent status reconciliation on the next tick anyway.
		slog.Warn("pause runtime: failed to suspend active tasks",
			"runtime_id", util.UUIDToString(runtimeID),
			"error", err,
		)
	} else if len(suspended) > 0 {
		// HandleFailedTasks broadcasts task:failed for each row, resets
		// stuck issues, and reconciles agent status. It deliberately does
		// NOT auto-retry: 'runtime_paused' is excluded from retryableReasons
		// so MaybeRetryFailedTask is a no-op here, and the unpause path
		// owns resumption.
		s.TaskSvc.HandleFailedTasks(ctx, toUpstreamTasks(suspended))
		slog.Info("pause runtime: suspended in-flight tasks",
			"runtime_id", util.UUIDToString(runtimeID),
			"count", len(suspended),
		)
	}

	s.publishRuntimePaused(rt)
	return toState(rt), nil
}

// UnpauseRuntime clears pause state and resumes any work that was suspended
// by the pause. Resumption keys on failure_reason='runtime_paused' (set by
// PauseRuntime) plus the 1-3 transient-error tasks that triggered the
// pause (failed in the 10 minutes immediately before paused_at).
//
// Older transient failures on the same runtime are deliberately excluded —
// they belong to earlier, already-resolved pause episodes (or were never
// pause-related at all) and re-queueing them would surface as the "pause
// resumed 20+ tasks I didn't expect" bug.
//
// Idempotent — calling on a runtime that isn't paused is a cheap no-op
// (paused_at is NULL → category-2 transient branch is skipped, and any
// category-1 'runtime_paused' leaves still hanging around from a prior
// crashed unpause are picked up — which is the desired recovery behaviour).
func (s *Service) UnpauseRuntime(ctx context.Context, runtimeID pgtype.UUID) (handler.RuntimePauseState, error) {
	// Snapshot paused_at *before* clearing it — the resume-task selection
	// keys on when the pause started, not when it ended. Skipped on
	// not-found / errors: ListResumableTasksForRuntime handles a NULL
	// snapshot by falling back to category-1 only.
	pausedAt := pgtype.Timestamptz{}
	if snap, err := s.Cerebro.GetAgentRuntimePauseSnapshot(ctx, runtimeID); err == nil {
		pausedAt = snap.PausedAt
	} else if !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("unpause runtime: failed to snapshot paused_at",
			"runtime_id", util.UUIDToString(runtimeID),
			"error", err,
		)
	}

	rt, err := s.Cerebro.UnpauseAgentRuntime(ctx, runtimeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return handler.RuntimePauseState{}, fmt.Errorf("unpause runtime: runtime not found")
		}
		return handler.RuntimePauseState{}, fmt.Errorf("unpause runtime: %w", err)
	}

	resumable, err := s.Cerebro.ListResumableTasksForRuntime(ctx, cerebrodb.ListResumableTasksForRuntimeParams{
		RuntimeID: runtimeID,
		PausedAt:  pausedAt,
	})
	if err != nil {
		slog.Warn("unpause runtime: failed to list resumable tasks",
			"runtime_id", util.UUIDToString(runtimeID),
			"error", err,
		)
	}

	resumed := 0
	for _, parent := range resumable {
		// Skip tasks that were superseded mid-pause by some other path
		// (manual rerun, autopilot tick). The leaf-detection in
		// ListResumableTasksForRuntime catches the common case but races
		// are possible — re-check here as a cheap guard.
		if parent.AutopilotRunID.Valid {
			// Autopilot owns its own re-run cadence; never double-fire.
			continue
		}
		if !parent.IssueID.Valid && !parent.ChatSessionID.Valid {
			continue
		}

		child, err := s.Cerebro.CreateResumeFromPauseTask(ctx, parent.ID)
		if err != nil {
			slog.Warn("unpause runtime: resume task creation failed",
				"runtime_id", util.UUIDToString(runtimeID),
				"parent_task_id", util.UUIDToString(parent.ID),
				"error", err,
			)
			continue
		}
		s.publishTaskQueued(ctx, child)
		resumed++
	}

	if resumed > 0 {
		slog.Info("unpause runtime: resumed tasks",
			"runtime_id", util.UUIDToString(runtimeID),
			"count", resumed,
		)
	}

	s.publishRuntimeUnpaused(rt, resumed)
	return toState(rt), nil
}

func toState(rt cerebrodb.AgentRuntime) handler.RuntimePauseState {
	return handler.RuntimePauseState{
		WorkspaceID: rt.WorkspaceID,
		PausedAt:    rt.PausedAt,
		UnpauseAt:   rt.UnpauseAt,
		PauseReason: rt.PauseReason,
	}
}

// RunUnpauseSweeper drives SweepUnpauseDue on a 30s ticker until ctx is
// cancelled. Run from main.go in its own goroutine alongside the upstream
// sweepers — the cadence matches runRuntimeSweeper so a runtime whose
// unpause_at falls inside a tick window is unpaused within at most ~30s.
func (s *Service) RunUnpauseSweeper(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n := s.SweepUnpauseDue(ctx); n > 0 {
				slog.Info("unpause sweeper: unpaused runtimes", "count", n)
			}
		}
	}
}

// SweepUnpauseDue runs one pass of the unpause sweeper: any runtime whose
// scheduled unpause_at has passed is unpaused. Returns the count of
// runtimes that were unpaused so the caller can log it. Errors on
// individual runtimes are logged and swallowed — a single broken row
// shouldn't stall the sweeper.
func (s *Service) SweepUnpauseDue(ctx context.Context) int {
	due, err := s.Cerebro.ListRuntimesDueForUnpause(ctx)
	if err != nil {
		slog.Warn("unpause sweeper: list due failed", "error", err)
		return 0
	}
	if len(due) == 0 {
		return 0
	}
	count := 0
	for _, rt := range due {
		if _, err := s.UnpauseRuntime(ctx, rt.ID); err != nil {
			slog.Warn("unpause sweeper: unpause failed",
				"runtime_id", util.UUIDToString(rt.ID),
				"error", err,
			)
			continue
		}
		count++
	}
	return count
}

func (s *Service) publishRuntimePaused(rt cerebrodb.AgentRuntime) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        EventRuntimePaused,
		WorkspaceID: util.UUIDToString(rt.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"runtime_id":   util.UUIDToString(rt.ID),
			"paused_at":    timestamptzToISO(rt.PausedAt),
			"unpause_at":   timestamptzToISO(rt.UnpauseAt),
			"pause_reason": textOrEmpty(rt.PauseReason),
		},
	})
}

func (s *Service) publishRuntimeUnpaused(rt cerebrodb.AgentRuntime, resumedCount int) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        EventRuntimeUnpaused,
		WorkspaceID: util.UUIDToString(rt.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"runtime_id":    util.UUIDToString(rt.ID),
			"resumed_tasks": resumedCount,
		},
	})
}

// publishTaskQueued mirrors the upstream broadcastTaskEvent for the
// task:queued event. We resolve workspace_id via the issue / chat_session
// link directly rather than reaching into TaskService internals — pause-
// resume runs at most a handful of times an hour, so the small extra
// lookup is well within the budget.
func (s *Service) publishTaskQueued(ctx context.Context, task cerebrodb.AgentTaskQueue) {
	workspaceID := s.TaskSvc.ResolveTaskWorkspaceID(ctx, toUpstreamTask(task))
	if workspaceID == "" {
		return
	}
	payload := map[string]any{
		"task_id":  util.UUIDToString(task.ID),
		"agent_id": util.UUIDToString(task.AgentID),
		"issue_id": util.UUIDToString(task.IssueID),
		"status":   task.Status,
	}
	if task.ChatSessionID.Valid {
		payload["chat_session_id"] = util.UUIDToString(task.ChatSessionID)
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventTaskQueued,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload:     payload,
	})
}

func timestamptzToISO(t pgtype.Timestamptz) string {
	if !t.Valid {
		return ""
	}
	return t.Time.UTC().Format(time.RFC3339)
}

func textOrEmpty(t pgtype.Text) string {
	if !t.Valid {
		return ""
	}
	return t.String
}

// toUpstreamTask converts a cerebrodb.AgentTaskQueue (sqlc-generated against
// the same physical table) to upstream's db.AgentTaskQueue. The two structs
// have identical fields by construction — sqlc generates them from the same
// table schema — so this is a field-by-field copy.
func toUpstreamTask(t cerebrodb.AgentTaskQueue) db.AgentTaskQueue {
	return db.AgentTaskQueue{
		ID:                t.ID,
		AgentID:           t.AgentID,
		IssueID:           t.IssueID,
		Status:            t.Status,
		Priority:          t.Priority,
		DispatchedAt:      t.DispatchedAt,
		StartedAt:         t.StartedAt,
		CompletedAt:       t.CompletedAt,
		Result:            t.Result,
		Error:             t.Error,
		CreatedAt:         t.CreatedAt,
		Context:           t.Context,
		RuntimeID:         t.RuntimeID,
		SessionID:         t.SessionID,
		WorkDir:           t.WorkDir,
		TriggerCommentID:  t.TriggerCommentID,
		ChatSessionID:     t.ChatSessionID,
		AutopilotRunID:    t.AutopilotRunID,
		Attempt:           t.Attempt,
		MaxAttempts:       t.MaxAttempts,
		ParentTaskID:      t.ParentTaskID,
		FailureReason:     t.FailureReason,
		TriggerSummary:    t.TriggerSummary,
		ForceFreshSession: t.ForceFreshSession,
		Title:             t.Title,
	}
}

func toUpstreamTasks(in []cerebrodb.AgentTaskQueue) []db.AgentTaskQueue {
	out := make([]db.AgentTaskQueue, len(in))
	for i, t := range in {
		out[i] = toUpstreamTask(t)
	}
	return out
}
