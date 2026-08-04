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
	"github.com/multica-ai/multica/server/internal/util"
)

// Agent-scoped pause events (FIR-4508). Cerebro-only — not in upstream protocol.
const (
	EventAgentPaused   = "agent:paused"
	EventAgentUnpaused = "agent:unpaused"
)

// AgentPauseOptions is the cerebro-pkg alias for handler.AgentPauseOptions.
type AgentPauseOptions = handler.AgentPauseOptions

// PauseAgent marks one agent paused, suspends that agent's in-flight work,
// and leaves sibling agents on the same multi-provider runtime online.
// Idempotent — re-pausing updates UnpauseAt/Reason without resetting paused_at.
func (s *Service) PauseAgent(ctx context.Context, agentID pgtype.UUID, opts AgentPauseOptions) (handler.AgentPauseState, error) {
	var unpauseAt pgtype.Timestamptz
	if !opts.UnpauseAt.IsZero() {
		unpauseAt = pgtype.Timestamptz{Time: opts.UnpauseAt.UTC(), Valid: true}
	}
	var reason pgtype.Text
	if opts.Reason != "" {
		reason = pgtype.Text{String: opts.Reason, Valid: true}
	}

	agent, err := s.Cerebro.PauseAgentRow(ctx, cerebrodb.PauseAgentRowParams{
		ID:          agentID,
		UnpauseAt:   unpauseAt,
		PauseReason: reason,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return handler.AgentPauseState{}, fmt.Errorf("pause agent: agent not found")
		}
		return handler.AgentPauseState{}, fmt.Errorf("pause agent: %w", err)
	}

	suspended, err := s.Cerebro.SuspendActiveTasksForAgent(ctx, agentID)
	if err != nil {
		slog.Warn("pause agent: failed to suspend active tasks",
			"agent_id", util.UUIDToString(agentID),
			"error", err,
		)
	} else if len(suspended) > 0 && s.TaskSvc != nil {
		s.TaskSvc.HandleFailedTasks(ctx, toUpstreamTasks(suspended))
		slog.Info("pause agent: suspended in-flight tasks",
			"agent_id", util.UUIDToString(agentID),
			"count", len(suspended),
		)
	}

	unpauseTime := time.Time{}
	if agent.UnpauseAt.Valid {
		unpauseTime = agent.UnpauseAt.Time
	}
	waitReason := FormatAgentPauseWaitReason(textOrEmpty(agent.PauseReason), unpauseTime, !agent.UnpauseAt.Valid)
	if err := s.Cerebro.StampQueuedTasksAgentPauseWaitReason(ctx, cerebrodb.StampQueuedTasksAgentPauseWaitReasonParams{
		AgentID:    agentID,
		WaitReason: pgtype.Text{String: waitReason, Valid: true},
	}); err != nil {
		slog.Warn("pause agent: stamp queued wait_reason failed",
			"agent_id", util.UUIDToString(agentID),
			"error", err,
		)
	}

	s.publishAgentPaused(agent)
	return agentToHandlerState(agent), nil
}

// UnpauseAgent clears agent pause state and resumes work suspended by PauseAgent.
func (s *Service) UnpauseAgent(ctx context.Context, agentID pgtype.UUID) (handler.AgentPauseState, error) {
	pausedAt := pgtype.Timestamptz{}
	if snap, err := s.Cerebro.GetAgentPauseSnapshot(ctx, agentID); err == nil {
		pausedAt = snap.PausedAt
	} else if !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("unpause agent: failed to snapshot paused_at",
			"agent_id", util.UUIDToString(agentID),
			"error", err,
		)
	}

	if err := s.Cerebro.ClearQueuedTasksAgentPauseWaitReason(ctx, agentID); err != nil {
		slog.Warn("unpause agent: clear queued wait_reason failed",
			"agent_id", util.UUIDToString(agentID),
			"error", err,
		)
	}

	agent, err := s.Cerebro.UnpauseAgentRow(ctx, agentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return handler.AgentPauseState{}, fmt.Errorf("unpause agent: agent not found")
		}
		return handler.AgentPauseState{}, fmt.Errorf("unpause agent: %w", err)
	}

	resumable, err := s.Cerebro.ListResumableTasksForAgent(ctx, cerebrodb.ListResumableTasksForAgentParams{
		AgentID:  agentID,
		PausedAt: pausedAt,
	})
	if err != nil {
		slog.Warn("unpause agent: failed to list resumable tasks",
			"agent_id", util.UUIDToString(agentID),
			"error", err,
		)
	}

	resumed := 0
	for _, parent := range resumable {
		if parent.AutopilotRunID.Valid {
			continue
		}
		if !parent.IssueID.Valid && !parent.ChatSessionID.Valid {
			continue
		}
		child, err := s.Cerebro.CreateResumeFromPauseTask(ctx, parent.ID)
		if err != nil {
			slog.Warn("unpause agent: resume task creation failed",
				"agent_id", util.UUIDToString(agentID),
				"parent_task_id", util.UUIDToString(parent.ID),
				"error", err,
			)
			continue
		}
		if parent.IssueID.Valid {
			if delErr := s.Cerebro.DeleteResumedTimeoutComment(ctx, cerebrodb.DeleteResumedTimeoutCommentParams{
				IssueID:      parent.IssueID,
				AuthorID:     parent.AgentID,
				ParentTaskID: util.UUIDToString(parent.ID),
			}); delErr != nil {
				slog.Warn("unpause agent: stale BLOCKED comment cleanup failed",
					"agent_id", util.UUIDToString(agentID),
					"parent_task_id", util.UUIDToString(parent.ID),
					"error", delErr,
				)
			}
		}
		s.publishTaskQueued(ctx, child)
		resumed++
	}

	if resumed > 0 {
		slog.Info("unpause agent: resumed tasks",
			"agent_id", util.UUIDToString(agentID),
			"count", resumed,
		)
	}

	s.publishAgentUnpaused(agent, resumed)
	return agentToHandlerState(agent), nil
}

// SweepAgentUnpauseDue unpauses agents whose scheduled unpause_at has passed.
func (s *Service) SweepAgentUnpauseDue(ctx context.Context) int {
	due, err := s.Cerebro.ListAgentsDueForUnpause(ctx)
	if err != nil {
		slog.Warn("agent unpause sweeper: list due failed", "error", err)
		return 0
	}
	if len(due) == 0 {
		return 0
	}
	count := 0
	for _, agent := range due {
		if _, err := s.UnpauseAgent(ctx, agent.ID); err != nil {
			slog.Warn("agent unpause sweeper: unpause failed",
				"agent_id", util.UUIDToString(agent.ID),
				"error", err,
			)
			continue
		}
		count++
	}
	return count
}

func agentToHandlerState(a cerebrodb.Agent) handler.AgentPauseState {
	return handler.AgentPauseState{
		WorkspaceID: a.WorkspaceID,
		PausedAt:    a.PausedAt,
		UnpauseAt:   a.UnpauseAt,
		PauseReason: a.PauseReason,
	}
}

func (s *Service) publishAgentPaused(a cerebrodb.Agent) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        EventAgentPaused,
		WorkspaceID: util.UUIDToString(a.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"agent_id":     util.UUIDToString(a.ID),
			"paused_at":    timestamptzToISO(a.PausedAt),
			"unpause_at":   timestamptzToISO(a.UnpauseAt),
			"pause_reason": textOrEmpty(a.PauseReason),
		},
	})
}

func (s *Service) publishAgentUnpaused(a cerebrodb.Agent, resumedCount int) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        EventAgentUnpaused,
		WorkspaceID: util.UUIDToString(a.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"agent_id":      util.UUIDToString(a.ID),
			"resumed_tasks": resumedCount,
		},
	})
}
