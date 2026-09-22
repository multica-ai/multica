package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// SE-37711 / SE-37664: wire the pure circuit decision core (runtime_circuit.go)
// into the terminal task callbacks. A terminal provider quota/auth failure opens
// the failing runtime's provider circuit; a terminal success closes it.
//
// The open (F7) is durable: it runs inside the FailAgentTask transaction via
// openRuntimeProviderCircuitTx, so the circuit write commits atomically with the
// terminal status. A write failure aborts the whole transaction (fail-closed) —
// the task is never marked failed while its circuit hold is lost, which would
// otherwise read as a closed circuit and let the autopilot dispatch straight
// back into the exhausted provider. The close-on-success stays a best-effort,
// post-commit side effect: a lost close only delays recovery until the next
// successful run, and must never turn a committed success into an error.
// Idempotency is owned by the queries — a duplicate or late callback for an
// older task is a no-op (zero rows), and a stale success cannot close a fresher
// open circuit.

// openRuntimeProviderCircuitTx opens the failing runtime's provider circuit for
// a terminal quota/auth failure (invariant I7), using the caller's queries
// handle. When that handle is a transaction (the FailAgentTask path), the write
// commits atomically with the terminal status and any write failure propagates
// so the transaction rolls back fail-closed. It only reaches the DB for the two
// circuit-bearing failure classes; every other reason resolves to "no circuit"
// in the pure classifier and returns before any I/O. A vanished runtime or a
// duplicate/late generation is a no-op (opened=false, err=nil).
func openRuntimeProviderCircuitTx(ctx context.Context, q *db.Queries, task db.AgentTaskQueue, failureReason, errMsg string) (bool, error) {
	if !task.RuntimeID.Valid {
		return false, nil
	}
	now := time.Now().UTC()
	failedAt := now
	if task.CompletedAt.Valid {
		failedAt = task.CompletedAt.Time.UTC()
	}
	decision := classifyCircuitFailure(failureReason, errMsg, failedAt, now)
	if !decision.Open {
		return false, nil
	}
	runtime, err := q.GetAgentRuntime(ctx, task.RuntimeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The runtime was torn down mid-flight; there is nothing left to hold,
			// and blocking the terminal status on a runtime that no longer exists
			// would strand the task in 'running'.
			return false, nil
		}
		return false, fmt.Errorf("load runtime for circuit open: %w", err)
	}
	rows, err := q.OpenRuntimeProviderCircuit(ctx, db.OpenRuntimeProviderCircuitParams{
		WorkspaceID:        runtime.WorkspaceID,
		RuntimeID:          task.RuntimeID,
		Provider:           runtime.Provider,
		Reason:             pgtype.Text{String: decision.FailureClass, Valid: true},
		ResetAt:            pgtype.Timestamptz{Time: decision.HoldUntil, Valid: true},
		FailureCompletedAt: pgtype.Timestamptz{Time: failedAt, Valid: true},
		FailureTaskID:      task.ID,
		ResetSource:        pgtype.Text{String: decision.ResetSource, Valid: decision.ResetSource != ""},
	})
	if err != nil {
		return false, fmt.Errorf("open runtime provider circuit: %w", err)
	}
	if len(rows) == 0 {
		// An equal-or-newer failure already owns the current generation: this is
		// a duplicate/late callback for an older task. Nothing changed.
		return false, nil
	}
	slog.Info("runtime provider circuit opened",
		"task_id", util.UUIDToString(task.ID),
		"runtime_id", util.UUIDToString(task.RuntimeID),
		"provider", runtime.Provider,
		"failure_class", decision.FailureClass,
		"reset_source", decision.ResetSource,
		"reset_at", decision.HoldUntil,
		"generation", rows[0].Generation)
	return true, nil
}

// syncRuntimeCircuitOnSuccess closes the runtime's provider circuit after a
// terminal success. This is what promotes an open/half_open circuit back to
// closed once a probe (or any run) on the runtime succeeds. It is a no-op when
// the circuit is already closed or the success is stale relative to the failure
// that opened the current generation.
func (s *TaskService) syncRuntimeCircuitOnSuccess(ctx context.Context, task db.AgentTaskQueue) {
	if !task.RuntimeID.Valid {
		return
	}
	runtime, err := s.Queries.GetAgentRuntime(ctx, task.RuntimeID)
	if err != nil {
		slog.Warn("runtime circuit: load runtime for close failed",
			"task_id", util.UUIDToString(task.ID),
			"runtime_id", util.UUIDToString(task.RuntimeID),
			"error", err)
		return
	}
	completedAt := time.Now().UTC()
	if task.CompletedAt.Valid {
		completedAt = task.CompletedAt.Time.UTC()
	}
	rows, err := s.Queries.CloseRuntimeProviderCircuitOnSuccess(ctx, db.CloseRuntimeProviderCircuitOnSuccessParams{
		RuntimeID:          task.RuntimeID,
		Provider:           runtime.Provider,
		SuccessCompletedAt: pgtype.Timestamptz{Time: completedAt, Valid: true},
		SuccessTaskID:      task.ID,
	})
	if err != nil {
		slog.Warn("runtime circuit: close on success failed",
			"task_id", util.UUIDToString(task.ID),
			"runtime_id", util.UUIDToString(task.RuntimeID),
			"provider", runtime.Provider,
			"error", err)
		return
	}
	if len(rows) == 0 {
		// Already closed, or this success predates the current failure epoch.
		return
	}
	slog.Info("runtime provider circuit closed on success",
		"task_id", util.UUIDToString(task.ID),
		"runtime_id", util.UUIDToString(task.RuntimeID),
		"provider", runtime.Provider,
		"generation", rows[0].Generation)
}
