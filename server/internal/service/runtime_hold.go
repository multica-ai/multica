package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// HoldRuntime places a runtime on hold until the given reset time. While on
// hold, the claim path skips the runtime so no new tasks are dispatched to
// it. The hold_reason is stored for observability (e.g. "session_limit").
func (s *TaskService) HoldRuntime(ctx context.Context, runtimeID pgtype.UUID, reason string, resetTime time.Time) error {
	rt, err := s.Queries.SetRuntimeHold(ctx, db.SetRuntimeHoldParams{
		ID: runtimeID,
		HoldUntil: pgtype.Timestamptz{
			Time:  resetTime,
			Valid: true,
		},
		HoldReason: pgtype.Text{
			String: reason,
			Valid:  reason != "",
		},
	})
	if err != nil {
		return err
	}

	slog.Info("runtime placed on hold",
		"runtime_id", util.UUIDToString(runtimeID),
		"reason", reason,
		"hold_until", resetTime.Format(time.RFC3339),
	)

	s.Bus.Publish(events.Event{
		Type:        protocol.EventRuntimeHeld,
		WorkspaceID: util.UUIDToString(rt.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"runtime_id": util.UUIDToString(runtimeID),
			"reason":     reason,
			"hold_until": resetTime.Format(time.RFC3339),
		},
	})
	return nil
}

// HoldRuntimeIfSessionLimit classifies the error message, and when it
// indicates a session limit with a parseable reset time, places the runtime
// on hold until that time. Bundles the Classify + ParseSessionLimitResetTime
// + HoldRuntime chain so callers cannot accidentally hold a runtime without
// first verifying the reset time is parseable (which would leave the runtime
// stuck on hold indefinitely).
//
// Returns true when the runtime was placed on hold, false otherwise.
func (s *TaskService) HoldRuntimeIfSessionLimit(ctx context.Context, runtimeID pgtype.UUID, errMsg string) bool {
	if taskfailure.Classify(errMsg) != taskfailure.ReasonSessionLimit {
		return false
	}
	resetTime, ok := ParseSessionLimitResetTime(errMsg)
	if !ok {
		return false
	}
	if err := s.HoldRuntime(ctx, runtimeID, "session_limit", resetTime); err != nil {
		slog.Warn("failed to hold runtime after session limit",
			"runtime_id", util.UUIDToString(runtimeID),
			"error", err,
		)
		return false
	}
	return true
}

// ResumeRuntime clears the hold on a runtime, allowing tasks to be
// dispatched to it again. It returns the updated runtime row produced by the
// ClearRuntimeHold query so callers can avoid a redundant re-fetch.
func (s *TaskService) ResumeRuntime(ctx context.Context, runtimeID pgtype.UUID) (db.AgentRuntime, error) {
	rt, err := s.Queries.ClearRuntimeHold(ctx, runtimeID)
	if err != nil {
		return db.AgentRuntime{}, err
	}

	slog.Info("runtime hold cleared",
		"runtime_id", util.UUIDToString(runtimeID),
	)

	s.Bus.Publish(events.Event{
		Type:        protocol.EventRuntimeResumed,
		WorkspaceID: util.UUIDToString(rt.WorkspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"runtime_id": util.UUIDToString(runtimeID),
		},
	})
	return rt, nil
}
