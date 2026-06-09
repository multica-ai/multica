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

// ResumeRuntime clears the hold on a runtime, allowing tasks to be
// dispatched to it again.
func (s *TaskService) ResumeRuntime(ctx context.Context, runtimeID pgtype.UUID) error {
	rt, err := s.Queries.ClearRuntimeHold(ctx, runtimeID)
	if err != nil {
		return err
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
	return nil
}
