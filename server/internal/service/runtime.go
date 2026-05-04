package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// PauseOptions configures a runtime-pause request.
//
// UnpauseAt is optional — when zero the runtime stays paused until manual
// unpause. The auto-pause-on-rate-limit path sets it from
// ParseRateLimitReset; the manual handler sets it from the request body
// (or leaves it zero for an indefinite pause).
//
// Reason is a short slug ('rate_limit', 'manual', 'maintenance', ...) used
// for telemetry and shown in the UI alongside the pause indicator.
type PauseOptions struct {
	UnpauseAt time.Time
	Reason    string
}

// PauseRuntime marks a runtime paused, suspends in-flight work, and
// broadcasts the state change. Idempotent — re-pausing an already-paused
// runtime updates UnpauseAt/Reason without resetting paused_at.
//
// In-flight tasks (status dispatched/running) are marked failed with
// failure_reason='runtime_paused' so the unpause path can identify and
// resume them. Queued tasks are left alone — they'll simply not be claimed
// while the runtime is paused (see the claim-side gate).
func (s *TaskService) PauseRuntime(ctx context.Context, runtimeID pgtype.UUID, opts PauseOptions) (db.AgentRuntime, error) {
	var unpauseAt pgtype.Timestamptz
	if !opts.UnpauseAt.IsZero() {
		unpauseAt = pgtype.Timestamptz{Time: opts.UnpauseAt.UTC(), Valid: true}
	}
	var reason pgtype.Text
	if opts.Reason != "" {
		reason = pgtype.Text{String: opts.Reason, Valid: true}
	}

	rt, err := s.Queries.PauseAgentRuntime(ctx, db.PauseAgentRuntimeParams{
		ID:          runtimeID,
		UnpauseAt:   unpauseAt,
		PauseReason: reason,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.AgentRuntime{}, fmt.Errorf("pause runtime: runtime not found")
		}
		return db.AgentRuntime{}, fmt.Errorf("pause runtime: %w", err)
	}

	suspended, err := s.Queries.SuspendActiveTasksForRuntime(ctx, runtimeID)
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
		s.HandleFailedTasks(ctx, suspended)
		slog.Info("pause runtime: suspended in-flight tasks",
			"runtime_id", util.UUIDToString(runtimeID),
			"count", len(suspended),
		)
	}

	s.publishRuntimePaused(rt)
	return rt, nil
}

// UnpauseRuntime clears pause state and resumes any work that was suspended
// by the pause. Resumption keys on failure_reason='runtime_paused' (set by
// PauseRuntime) plus other transient-error leaves within a 24h window.
//
// Idempotent — calling on a runtime that isn't paused is a cheap no-op
// (the SQL UPDATE just sets already-NULL columns to NULL, no resume
// candidates qualify).
func (s *TaskService) UnpauseRuntime(ctx context.Context, runtimeID pgtype.UUID) (db.AgentRuntime, error) {
	rt, err := s.Queries.UnpauseAgentRuntime(ctx, runtimeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.AgentRuntime{}, fmt.Errorf("unpause runtime: runtime not found")
		}
		return db.AgentRuntime{}, fmt.Errorf("unpause runtime: %w", err)
	}

	resumable, err := s.Queries.ListResumableTasksForRuntime(ctx, runtimeID)
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

		child, err := s.Queries.CreateResumeFromPauseTask(ctx, parent.ID)
		if err != nil {
			slog.Warn("unpause runtime: resume task creation failed",
				"runtime_id", util.UUIDToString(runtimeID),
				"parent_task_id", util.UUIDToString(parent.ID),
				"error", err,
			)
			continue
		}
		s.broadcastTaskEvent(ctx, protocol.EventTaskQueued, child)
		s.notifyTaskAvailable(child)
		resumed++
	}

	if resumed > 0 {
		slog.Info("unpause runtime: resumed tasks",
			"runtime_id", util.UUIDToString(runtimeID),
			"count", resumed,
		)
	}

	s.publishRuntimeUnpaused(rt, resumed)
	return rt, nil
}

// SweepUnpauseDue runs one pass of the unpause sweeper: any runtime whose
// scheduled unpause_at has passed is unpaused. Returns the count of
// runtimes that were unpaused so the caller can log it. Errors on
// individual runtimes are logged and swallowed — a single broken row
// shouldn't stall the sweeper.
func (s *TaskService) SweepUnpauseDue(ctx context.Context) int {
	due, err := s.Queries.ListRuntimesDueForUnpause(ctx)
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

func (s *TaskService) publishRuntimePaused(rt db.AgentRuntime) {
	if s.Bus == nil {
		return
	}
	payload := map[string]any{
		"runtime_id":   util.UUIDToString(rt.ID),
		"paused_at":    timestamptzToISO(rt.PausedAt),
		"unpause_at":   timestamptzToISO(rt.UnpauseAt),
		"pause_reason": textOrEmpty(rt.PauseReason),
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventRuntimePaused,
		WorkspaceID: util.UUIDToString(rt.WorkspaceID),
		ActorType:   "system",
		Payload:     payload,
	})
}

func (s *TaskService) publishRuntimeUnpaused(rt db.AgentRuntime, resumedCount int) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        protocol.EventRuntimeUnpaused,
		WorkspaceID: util.UUIDToString(rt.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"runtime_id":     util.UUIDToString(rt.ID),
			"resumed_tasks":  resumedCount,
		},
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
