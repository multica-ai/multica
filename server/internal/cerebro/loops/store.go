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
