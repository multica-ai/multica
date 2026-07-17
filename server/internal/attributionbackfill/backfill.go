// Package attributionbackfill reconciles agent_task_queue rows to the
// Human Attribution strict invariant BEFORE migration 198 validates it.
//
// Background (GH #5544, MUL-4302, MUL-4897). Migration 197 installs the
// strict cross-column CHECK
//
//	originator_user_id IS NULL
//	  OR (accountable_user_id IS NOT NULL AND accountable_user_id = originator_user_id)
//
// as agent_task_queue_accountable_matches_originator_strict (NOT VALID),
// and migration 198 runs VALIDATE CONSTRAINT on it. Migration 197's
// comment assumed the violating legacy rows had "been backfilled out of
// band" — which was true only on Multica's own cloud, where the backfill
// was run manually. Self-hosted deployments never got that step, so their
// legacy rows (originator_user_id set from before migration 185, or written
// by an older rolling-deploy backend, with accountable_user_id NULL) make
// migration 198 fail closed with SQLSTATE 23514 and the backend refuses to
// start.
//
// A new higher-numbered migration cannot fix this: a stuck instance sits
// with 197 applied and 198 failing, and never reaches a migration numbered
// above 198. The fix must run at-or-before 198. cmd/migrate already exposes
// a preMigrationHook mechanism (used for migration 103, MUL-2957) that runs
// idempotent work before a specific migration's SQL; this package is that
// hook for 198.
//
// Scope. The strict constraint can ONLY be violated by rows where
// originator_user_id IS NOT NULL, and for those the design invariant is
// unambiguous: a resolved originator IS the accountable human, so
// accountable_user_id MUST equal originator_user_id (MUL-4302 §1/§11).
// This hook therefore mirrors originator_user_id into accountable_user_id
// for exactly those rows and stamps originator_source='backfill' where it
// was NULL, which is precisely and only what is required to make VALIDATE
// pass with zero manual operator steps.
//
// Rows with originator_user_id IS NULL (autopilot rule_owner / owner_fallback,
// or fully unattributed history) already satisfy the strict constraint and
// are left untouched — enriching their accountable_user_id from the trigger
// chat/agent-owner waterfall is audit-completeness work that does not block
// startup and is deliberately kept out of this fail-closed path.
package attributionbackfill

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultBatchSize bounds how many rows each UPDATE statement rewrites, so
// the reconciliation never takes one long transaction / lock on a large
// agent_task_queue. Each batch autocommits on the pool; partial progress is
// simply resumed on the next batch (or the next migrate run), because a
// reconciled row no longer matches the selection predicate.
const DefaultBatchSize = 5000

// Result reports what a single Hook run did. Exposed so cmd/migrate and
// tests can log or assert on it.
type Result struct {
	// RowsBackfilled is the total number of rows whose accountable_user_id
	// was mirrored from originator_user_id across all batches.
	RowsBackfilled int64
	// Batches counts how many UPDATE batches ran (0 when nothing needed
	// fixing).
	Batches int
	// MismatchNormalized counts rows that had BOTH originator_user_id and
	// accountable_user_id set to DIFFERENT non-NULL users before the run.
	// Per the design these should not exist (migration 190's transitional
	// constraint has rejected them on every source-tagged write since it
	// shipped); the count is surfaced as a warning so a genuinely
	// mis-attributed row is visible even though the invariant forces
	// accountable to follow originator.
	MismatchNormalized int64
}

// HookOptions controls Hook behaviour. Zero values are correct for
// production; tests override fields as needed.
type HookOptions struct {
	// Logger receives slog records about the walk. nil = slog.Default().
	Logger *slog.Logger
	// BatchSize overrides DefaultBatchSize. <= 0 = DefaultBatchSize.
	BatchSize int
	// Table overrides the target table name. Empty = "agent_task_queue".
	// Tests point this at a throwaway fixture table; production leaves it
	// empty. The value is interpolated into SQL, so callers must pass a
	// trusted identifier (never user input) — production passes a
	// constant and tests pass their own schema-qualified fixture name.
	Table string
}

// countMismatchSQL counts rows where both attribution users are set but
// disagree — the "real mis-attribution" shape worth a warning.
const countMismatchSQL = `
SELECT count(*)
FROM %s
WHERE originator_user_id IS NOT NULL
  AND accountable_user_id IS NOT NULL
  AND accountable_user_id <> originator_user_id`

// backfillBatchSQL mirrors originator_user_id into accountable_user_id for a
// bounded batch of violating rows and stamps originator_source='backfill'
// only where it was NULL (COALESCE preserves an existing precise source such
// as 'direct_human'). Because originator_user_id IS NOT NULL for every
// selected row, `accountable_user_id IS DISTINCT FROM originator_user_id`
// captures both the NULL-accountable legacy rows and the both-set-but-differ
// rows. Reconciled rows stop matching the predicate, so the loop terminates
// and re-running is a no-op.
//
// Concurrency safety (defense in depth — the migrate loop normally runs with
// the server down, but the hook executes on the pool and a rolling deploy or
// an operator-run migrate could overlap a live writer). Two guards close the
// stale-selection race where a row is selected as violating but a concurrent
// writer flips it to a legitimate originator-NULL fork before it is rewritten:
//
//   - FOR UPDATE in the CTE locks each candidate row and, under READ
//     COMMITTED, re-evaluates the predicate against the latest committed
//     version once the lock is granted — a row that became originator-NULL is
//     dropped from the batch instead of being blindly overwritten.
//   - The outer UPDATE repeats the same predicate on q, so it never writes a
//     row that no longer needs reconciliation even if it changed underneath
//     the selection.
//
// Without these, `SET accountable_user_id = q.originator_user_id` on a row a
// writer just turned into (originator NULL, accountable set) would clobber the
// writer's accountable value with NULL.
const backfillBatchSQL = `
WITH batch AS (
    SELECT id
    FROM %s
    WHERE originator_user_id IS NOT NULL
      AND accountable_user_id IS DISTINCT FROM originator_user_id
    LIMIT $1
    FOR UPDATE
)
UPDATE %s q
SET accountable_user_id = q.originator_user_id,
    originator_source   = COALESCE(q.originator_source, 'backfill')
FROM batch
WHERE q.id = batch.id
  AND q.originator_user_id IS NOT NULL
  AND q.accountable_user_id IS DISTINCT FROM q.originator_user_id`

// Hook is the migration-time entrypoint invoked before migration 198's
// VALIDATE. It is idempotent and safe to retry: it reconciles every row
// that would violate agent_task_queue_accountable_matches_originator_strict
// and returns nil so the migration loop proceeds to VALIDATE.
//
// It returns an error only when a SQL statement itself fails (e.g. the
// database is unreachable mid-walk); that aborts the migrate run before
// schema_migrations records 198, so the same version — hook included —
// retries cleanly on the next invocation.
//
// The hook runs on the pool (autocommit per batch), NOT on the migrate
// loop's pinned lock connection, matching the taskusagebackfill hook
// contract. The loop already holds migrationAdvisoryLockKey, and migrate
// runs with the server down, so no concurrent writer competes for these
// rows.
func Hook(ctx context.Context, pool *pgxpool.Pool, opts HookOptions) (Result, error) {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	table := opts.Table
	if table == "" {
		table = "agent_task_queue"
	}

	var res Result

	// Surface any genuinely mis-attributed rows (both users set but
	// different) up front. The invariant still forces accountable to follow
	// originator below, but the operator/audit trail should show the count.
	if err := pool.QueryRow(ctx, fmt.Sprintf(countMismatchSQL, table)).Scan(&res.MismatchNormalized); err != nil {
		return res, fmt.Errorf("count attribution mismatches: %w", err)
	}
	if res.MismatchNormalized > 0 {
		log.Warn("attribution backfill: normalizing rows where accountable_user_id disagreed with a non-NULL originator_user_id; originator is authoritative but these are worth auditing",
			"mismatch_rows", res.MismatchNormalized)
	}

	updateSQL := fmt.Sprintf(backfillBatchSQL, table, table)
	for {
		tag, err := pool.Exec(ctx, updateSQL, batchSize)
		if err != nil {
			return res, fmt.Errorf("backfill accountable_user_id batch: %w", err)
		}
		n := tag.RowsAffected()
		if n == 0 {
			break
		}
		res.RowsBackfilled += n
		res.Batches++
		log.Info("attribution backfill: batch reconciled",
			"rows", n,
			"total", res.RowsBackfilled)
	}

	if res.RowsBackfilled == 0 {
		log.Info("attribution backfill: no rows needed reconciliation before migration 198")
	} else {
		log.Info("attribution backfill: complete",
			"rows_backfilled", res.RowsBackfilled,
			"batches", res.Batches,
			"mismatch_normalized", res.MismatchNormalized)
	}
	return res, nil
}
