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

// holdExpiryMargin keeps a runtime held for a grace period past its hold_until
// before it is considered released. The provider quota is often not lifted on
// the very first run after the stated reset time, and there is some clock skew
// between when we parsed the reset time and when the limit actually clears, so
// we wait this much longer rather than dispatch into a still-throttled
// provider. Must stay in sync with the 300s margin in the ClearExpiredHolds,
// ClaimAgentTask, and ListQueuedClaimCandidatesByRuntime SQL queries.
const holdExpiryMargin = 5 * time.Minute

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

// HoldRuntimeIfSessionLimit places the runtime on hold until the session
// limit's reset time. The caller must have already classified errMsg as a
// session-limit failure; the helper only extracts the reset time and applies
// the hold, so it cannot accidentally hold a runtime without a bounded
// hold_until (which would leave the runtime stuck on hold indefinitely).
//
// Returns true when the runtime was placed on hold, false otherwise. A false
// return means no bounded hold could be applied — currently only when the
// reset time is unparseable — and the caller must NOT auto-retry the task in
// that case, otherwise the retry is claimed immediately and hot-loops until
// max_attempts.
func (s *TaskService) HoldRuntimeIfSessionLimit(ctx context.Context, runtimeID, taskID pgtype.UUID, errMsg string) bool {
	resetTime, ok := ParseSessionLimitResetTime(errMsg)
	if !ok {
		// Classified as session_limit but the reset time is unparseable, so we
		// cannot bound the hold. Surface it instead of degrading silently: the
		// task is left un-retried rather than hot-looping, and an unparseable
		// reset time means the upstream message format likely drifted.
		slog.Warn("session limit detected but reset time unparseable; runtime not held",
			"runtime_id", util.UUIDToString(runtimeID),
			"task_id", util.UUIDToString(taskID),
		)
		return false
	}
	if err := s.HoldRuntime(ctx, runtimeID, "session_limit", resetTime); err != nil {
		slog.Warn("failed to hold runtime after session limit",
			"runtime_id", util.UUIDToString(runtimeID),
			"task_id", util.UUIDToString(taskID),
			"error", err,
		)
		return false
	}
	return true
}

// runtimeOnHold reports whether the runtime currently has an active hold
// (hold_until set and not yet past by more than holdExpiryMargin). It is the
// gate for auto-retrying session_limit failures: the retry is only safe once
// the runtime is held, otherwise the requeued task is claimed immediately and
// hot-loops. A missing runtime or read error is treated as "not held" so the
// caller fails closed (no retry) rather than risking a hot-loop.
func (s *TaskService) runtimeOnHold(ctx context.Context, runtimeID pgtype.UUID) bool {
	if !runtimeID.Valid {
		return false
	}
	rt, err := s.Queries.GetAgentRuntime(ctx, runtimeID)
	if err != nil {
		slog.Warn("failed to read runtime hold state for retry gating",
			"runtime_id", util.UUIDToString(runtimeID),
			"error", err,
		)
		return false
	}
	return rt.HoldUntil.Valid && rt.HoldUntil.Time.Add(holdExpiryMargin).After(time.Now())
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
