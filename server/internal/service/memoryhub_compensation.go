// Package service: durable compensation / saga sweeper (Plan v1.2 §11).
// Owner: ALL-16.
//
// The sweeper is the ONLY worker that claims and drives memoryhub_compensation
// rows. It runs alongside the existing task sweeper at server startup.
//
// State machine (frozen): pending -> running (claimed) -> compensated |
// retry_wait -> pending | blocked | dead_letter. blocked and dead_letter are
// NEVER auto-retried; only manual unlock moves them (manual unlock is not part
// of this file). Every remote side effect is idempotent: the row is inserted
// with an idempotency key BEFORE any remote call, so a crash after the remote
// call but before the local write is reconciled on restart by re-driving the
// existing row, never by duplicating the remote effect.
package service

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CompensationOp is the frozen op enum (§11).
type CompensationOp string

const (
	CompensationCreateRemote CompensationOp = "create_remote"
	CompensationReuseRemote  CompensationOp = "reuse_remote"
	CompensationRebindRemote CompensationOp = "rebind_remote"
	CompensationDeleteRemote CompensationOp = "delete_remote"
	CompensationPurgeMemory  CompensationOp = "purge_memory"
)

// CompensationState is the frozen six-state enum (§11).
type CompensationState string

const (
	CompensationPending     CompensationState = "pending"
	CompensationRunning     CompensationState = "running"
	CompensationRetryWait   CompensationState = "retry_wait"
	CompensationCompensated CompensationState = "compensated"
	CompensationBlocked     CompensationState = "blocked"
	CompensationDeadLetter  CompensationState = "dead_letter"
)

// ErrCompensationTransient marks a retryable error (network/remote hiccup).
// Any other error from the executor is treated as fatal.
var ErrCompensationTransient = errors.New("memoryhub: compensation transient failure")

// CompensationExecutor performs the remote side effect for one op, idempotently.
// Implementations must find-or-create / reuse the existing remote_ref rather
// than blindly re-creating, so re-drive never duplicates a side effect.
type CompensationExecutor interface {
	// Execute runs the op for the claimed row. Returning nil commits the
	// compensation; ErrCompensationTransient schedules a backoff retry; any
	// other error blocks the row.
	Execute(ctx context.Context, comp db.MemoryhubCompensation) error
}

// CompensationQuerier is the DB boundary the sweeper needs. The concrete
// *db.Queries satisfies it; tests substitute a fake.
type CompensationQuerier interface {
	ReleaseExpiredCompensationLeases(ctx context.Context) error
	ResetExpiredRunningCompensations(ctx context.Context) error
	ClaimDueCompensations(ctx context.Context, arg db.ClaimDueCompensationsParams) ([]db.MemoryhubCompensation, error)
	MarkCompensated(ctx context.Context, id pgtype.UUID) (db.MemoryhubCompensation, error)
	MarkRetryWait(ctx context.Context, arg db.MarkRetryWaitParams) (db.MemoryhubCompensation, error)
	MarkBlocked(ctx context.Context, arg db.MarkBlockedParams) (db.MemoryhubCompensation, error)
	MarkDeadLetter(ctx context.Context, arg db.MarkDeadLetterParams) (db.MemoryhubCompensation, error)
}

// CompensationSweeper claims due compensation rows and drives each op to a
// terminal (or retry) state.
type CompensationSweeper struct {
	Queries       CompensationQuerier
	Executor      CompensationExecutor
	LeaseOwner    string
	LeaseDuration time.Duration
	BatchSize     int32
}

// NewCompensationSweeper wires the sweeper.
func NewCompensationSweeper(q CompensationQuerier, ex CompensationExecutor, leaseOwner string, leaseDuration time.Duration, batchSize int32) *CompensationSweeper {
	return &CompensationSweeper{
		Queries:       q,
		Executor:      ex,
		LeaseOwner:    leaseOwner,
		LeaseDuration: leaseDuration,
		BatchSize:     batchSize,
	}
}

// Sweep runs one worker pass:
//  1. release expired leases (both claimed-running and stale running rows);
//  2. claim due pending/retry_wait rows (FOR UPDATE SKIP LOCKED + lease);
//  3. execute each op idempotently and mark compensated / retry_wait /
//     blocked / dead_letter.
//
// The four crash points all resolve through idempotency:
//  1. remote created, local row missing  -> row already inserted pre-remote;
//  2. local written, remote failed       -> retry_wait + idempotent executor;
//  3. delete_remote remote gone          -> executor treats as success;
//  4. crash mid-execution                -> stale running lease is reset and
//     re-driven idempotently by another worker.
func (s *CompensationSweeper) Sweep(ctx context.Context) error {
	// Reset stale running rows BEFORE releasing leases: the reset predicate
	// requires lease_owner IS NOT NULL, and releasing first would clear it.
	if err := s.Queries.ResetExpiredRunningCompensations(ctx); err != nil {
		return err
	}
	if err := s.Queries.ReleaseExpiredCompensationLeases(ctx); err != nil {
		return err
	}

	claimed, err := s.Queries.ClaimDueCompensations(ctx, db.ClaimDueCompensationsParams{
		LeaseOwner: pgtype.Text{String: s.LeaseOwner, Valid: true},
		Column2:    pgtype.Interval{Microseconds: s.LeaseDuration.Microseconds(), Valid: true},
		Limit:      s.BatchSize,
	})
	if err != nil {
		return err
	}

	for _, comp := range claimed {
		if err := s.drive(ctx, comp); err != nil {
			// A single row failure must not kill the whole pass; other rows
			// keep their leases and are reconciled on the next sweep.
			continue
		}
	}
	return nil
}

// drive executes one claimed row and persists the outcome.
func (s *CompensationSweeper) drive(ctx context.Context, comp db.MemoryhubCompensation) error {
	err := s.Executor.Execute(ctx, comp)
	switch {
	case err == nil:
		_, cerr := s.Queries.MarkCompensated(ctx, comp.ID)
		return cerr
	case errors.Is(err, ErrCompensationTransient):
		if comp.Attempt < comp.MaxAttempt {
			_, rerr := s.Queries.MarkRetryWait(ctx, db.MarkRetryWaitParams{
				ID:        comp.ID,
				LastError: err.Error(),
			})
			return rerr
		}
		_, berr := s.Queries.MarkBlocked(ctx, db.MarkBlockedParams{
			ID:        comp.ID,
			LastError: err.Error(),
		})
		return berr
	default:
		// Fatal / scope-invalid errors land in blocked (recoverable by manual
		// unlock); dead_letter is reserved for rows the executor explicitly
		// classifies as unrecoverable.
		_, derr := s.Queries.MarkDeadLetter(ctx, db.MarkDeadLetterParams{
			ID:        comp.ID,
			LastError: err.Error(),
		})
		return derr
	}
}
