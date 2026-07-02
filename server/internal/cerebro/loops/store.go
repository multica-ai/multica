package loops

// store.go is the persistence half of the loop check transport. The pure brain
// (Reconcile / EvaluateGate) decides a delivery gate from a []CheckOutcome; the
// Store is the seam that holds those outcomes in cerebro_loop_check_run between
// the engine enqueuing a check and the runtime reporting its exit code, scoped
// per (issue, gate, round).
//
// It uses pgx directly against the pool rather than the sqlc-generated
// cerebrodb queries: the canonical query definitions live in
// internal/cerebro/queries/loop_check_run.sql and will generate once the
// pre-existing sqlc breakage in the cerebrodb package (an unrelated ambiguous
// column in project_grant.sql) is fixed. Inline SQL against the pool is the
// same pattern several cerebro stores already use (sessions, toolpolicy,
// connections), so the transport is not blocked on that fix.
//
// The flow the engine drives:
//
//	Reconcile -> GateEnqueue  =>  Store.Enqueue(argvs)         // pending rows
//	runtime reports exit code  =>  Store.Report(argv, code)     // fills one row
//	Store.Outcomes(...) -> Reconcile/EvaluateGate              // decide again
//
// Enqueue is idempotent (a re-fire never resets a check that already ran) and
// Outcomes returns the rows in the CheckOutcome shape the gate logic consumes,
// so the persisted state plugs straight into the deterministic core.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store reads and writes the per-round check outcomes for a delivery gate.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore constructs a Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Enqueue registers each check as pending (ran = false) for one gate round.
// Idempotent: re-enqueuing the same argv leaves the existing row — and any
// result already reported on it — untouched, so a duplicate evaluation never
// clears a check that has run.
func (s *Store) Enqueue(ctx context.Context, issueID pgtype.UUID, gate string, round int32, argvs [][]string) error {
	for _, argv := range argvs {
		if _, err := s.pool.Exec(ctx,
			`INSERT INTO cerebro_loop_check_run (issue_id, gate, round, argv)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (issue_id, gate, round, argv) DO NOTHING`,
			issueID, gate, round, argv); err != nil {
			return fmt.Errorf("enqueue loop check run: %w", err)
		}
	}
	return nil
}

// PendingDispatch returns the argv of every check that is enqueued for this
// gate round but not yet dispatched to a runtime (dispatched_at IS NULL) and
// not yet reported (ran = false), oldest first. The engine dispatches exactly
// these and stamps each with MarkDispatched, so a gate re-evaluation never
// re-sends a check that is already in flight.
func (s *Store) PendingDispatch(ctx context.Context, issueID pgtype.UUID, gate string, round int32) ([][]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT argv
		 FROM cerebro_loop_check_run
		 WHERE issue_id = $1 AND gate = $2 AND round = $3
		   AND ran = false AND dispatched_at IS NULL
		 ORDER BY created_at ASC, id ASC`,
		issueID, gate, round)
	if err != nil {
		return nil, fmt.Errorf("list pending dispatch loop checks: %w", err)
	}
	defer rows.Close()

	var argvs [][]string
	for rows.Next() {
		var argv []string
		if err := rows.Scan(&argv); err != nil {
			return nil, fmt.Errorf("scan pending dispatch loop check: %w", err)
		}
		argvs = append(argvs, argv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending dispatch loop checks: %w", err)
	}
	return argvs, nil
}

// MarkDispatched stamps one enqueued check as sent to a runtime. The
// dispatched_at IS NULL guard makes it idempotent: a retry after a partial
// dispatch never moves an already-set timestamp.
func (s *Store) MarkDispatched(ctx context.Context, issueID pgtype.UUID, gate string, round int32, argv []string) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE cerebro_loop_check_run
		 SET dispatched_at = now()
		 WHERE issue_id = $1 AND gate = $2 AND round = $3 AND argv = $4
		   AND dispatched_at IS NULL`,
		issueID, gate, round, argv); err != nil {
		return fmt.Errorf("mark loop check dispatched: %w", err)
	}
	return nil
}

// Report records the runtime's reported exit code for one enqueued check.
func (s *Store) Report(ctx context.Context, issueID pgtype.UUID, gate string, round int32, argv []string, exitCode int32) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE cerebro_loop_check_run
		 SET ran = true, exit_code = $5, updated_at = now()
		 WHERE issue_id = $1 AND gate = $2 AND round = $3 AND argv = $4`,
		issueID, gate, round, argv, exitCode); err != nil {
		return fmt.Errorf("report loop check run: %w", err)
	}
	return nil
}

// GateRoundState is the caps-tracking state for one delivery gate, persisted in
// cerebro_loop_gate_state. It is what turns Spec.Caps from a parsed-but-unused
// field into an enforced termination guard: LoadGateState tells the evaluator
// which round to evaluate against, and RecordRevision is where a GateRevise
// decision gets checked against MaxIterations / MaxRevisions /
// NoProgressStalls and frozen (Stopped) the moment one trips.
type GateRoundState struct {
	Round                int32
	Revisions            int32
	ConsecutiveStalls    int32
	LastOutcomeSignature string
	Stopped              bool
	StopReason           string
}

// LoadGateState returns the current caps-tracking state for a gate, creating
// a fresh row (round 1, not stopped) on first access. Idempotent: calling it
// repeatedly for the same gate never resets an existing row.
func (s *Store) LoadGateState(ctx context.Context, issueID pgtype.UUID, gate string) (GateRoundState, error) {
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO cerebro_loop_gate_state (issue_id, gate) VALUES ($1, $2)
		 ON CONFLICT (issue_id, gate) DO NOTHING`,
		issueID, gate); err != nil {
		return GateRoundState{}, fmt.Errorf("init loop gate state: %w", err)
	}
	var st GateRoundState
	if err := s.pool.QueryRow(ctx,
		`SELECT round, revisions, consecutive_stalls, last_outcome_signature, stopped, stop_reason
		 FROM cerebro_loop_gate_state WHERE issue_id = $1 AND gate = $2`,
		issueID, gate,
	).Scan(&st.Round, &st.Revisions, &st.ConsecutiveStalls, &st.LastOutcomeSignature, &st.Stopped, &st.StopReason); err != nil {
		return GateRoundState{}, fmt.Errorf("load loop gate state: %w", err)
	}
	return st, nil
}

// RecordRevision advances a gate to a new round after a GateRevise decision
// and applies the spec's termination guards. signature is
// OutcomeSignature(outcomes) for the round that just failed: an unchanged
// signature across two consecutive revisions means the worker made no
// progress, and increments ConsecutiveStalls instead of resetting it.
//
// A zero Caps field means that guard is not configured (the raw
// CheckGateConfig tests in this package do not set Caps) and is never
// checked — in production Compile always populates Caps from a validated
// Spec, whose caps are required to be positive. Once any guard trips the gate
// is Stopped for good: a cap trip is a one-way door, so a later call is a
// no-op that returns the already-stopped state unchanged.
func (s *Store) RecordRevision(ctx context.Context, issueID pgtype.UUID, gate string, signature string, caps Caps) (GateRoundState, error) {
	cur, err := s.LoadGateState(ctx, issueID, gate)
	if err != nil {
		return GateRoundState{}, err
	}
	if cur.Stopped {
		return cur, nil
	}

	next := cur
	next.Round = cur.Round + 1
	next.Revisions = cur.Revisions + 1
	if signature != "" && signature == cur.LastOutcomeSignature {
		next.ConsecutiveStalls = cur.ConsecutiveStalls + 1
	} else {
		next.ConsecutiveStalls = 0
	}
	next.LastOutcomeSignature = signature

	switch {
	case caps.MaxRevisions > 0 && next.Revisions >= int32(caps.MaxRevisions):
		next.Stopped = true
		next.StopReason = fmt.Sprintf("max_revisions reached (%d)", caps.MaxRevisions)
	case caps.MaxIterations > 0 && next.Round > int32(caps.MaxIterations):
		next.Stopped = true
		next.StopReason = fmt.Sprintf("max_iterations reached (%d)", caps.MaxIterations)
	case caps.NoProgressStalls > 0 && next.ConsecutiveStalls >= int32(caps.NoProgressStalls):
		next.Stopped = true
		next.StopReason = fmt.Sprintf("no_progress_stalls reached (%d)", caps.NoProgressStalls)
	}

	if _, err := s.pool.Exec(ctx,
		`UPDATE cerebro_loop_gate_state
		 SET round = $3, revisions = $4, consecutive_stalls = $5,
		     last_outcome_signature = $6, stopped = $7, stop_reason = $8, updated_at = now()
		 WHERE issue_id = $1 AND gate = $2`,
		issueID, gate, next.Round, next.Revisions, next.ConsecutiveStalls,
		next.LastOutcomeSignature, next.Stopped, next.StopReason); err != nil {
		return GateRoundState{}, fmt.Errorf("record loop gate revision: %w", err)
	}
	return next, nil
}

// Outcomes returns every check (pending and reported) for a gate round, oldest
// first, folded into the CheckOutcome shape Reconcile / EvaluateGate consume.
func (s *Store) Outcomes(ctx context.Context, issueID pgtype.UUID, gate string, round int32) ([]CheckOutcome, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT argv, ran, exit_code
		 FROM cerebro_loop_check_run
		 WHERE issue_id = $1 AND gate = $2 AND round = $3
		 ORDER BY created_at ASC, id ASC`,
		issueID, gate, round)
	if err != nil {
		return nil, fmt.Errorf("list loop check runs: %w", err)
	}
	defer rows.Close()

	outcomes := []CheckOutcome{}
	for rows.Next() {
		var o CheckOutcome
		if err := rows.Scan(&o.Argv, &o.Ran, &o.ExitCode); err != nil {
			return nil, fmt.Errorf("scan loop check run: %w", err)
		}
		outcomes = append(outcomes, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate loop check runs: %w", err)
	}
	return outcomes, nil
}
