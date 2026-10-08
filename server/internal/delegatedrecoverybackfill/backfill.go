// Package delegatedrecoverybackfill provides bounded, resumable batches that
// retire delegated-failure recovery comments already closed by a terminal
// delivery receipt.
//
// migrations 444/445 add comment.recovery_settled_at and the partial index that
// only holds unsettled recovery comments, and TaskService writes the marker
// inside every terminal task transaction. Neither reaches the history written
// before the marker existed: those rows keep matching the index predicate and
// keep being re-proven settled by the sweeper's outbox scan on every tick. This
// walk marks them once.
//
// Run it after every backend in a rolling deployment carries the writing code.
// Running it earlier is not harmful — an in-flight task simply gets marked by
// whichever path reaches its terminal receipt first — but it leaves rows behind
// that a second pass has to pick up.
package delegatedrecoverybackfill

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultBatchSize bounds the candidate comments examined per statement.
//
// Measured on a synthetic table of 500k comments / 200k tasks / 50k unsettled
// recoveries, total walk time is flat across batch size — 50x228ms at 1000,
// 10x1016ms at 5000, 5x2199ms at 10000, all ~11s — because the per-batch
// agent_task_queue read is not the dominant cost. Batch size therefore only
// trades how long a single statement holds row locks on comment, so take the
// smallest of the equal-throughput options. The command's inter-batch delay is
// what actually paces the walk.
const DefaultBatchSize = 1000

// Cursor is the keyset position of a walk. It is (created_at, id) rather than
// id alone so it matches idx_comment_delegated_failure_unsettled — an id-only
// keyset cannot use that index for both the predicate and the ordering, which
// is the same "LIMIT bounds what is returned, not what is examined" mistake
// this whole change set exists to remove.
type Cursor struct {
	CreatedAt time.Time
	ID        string
}

// Options controls one backfill batch. Zero values are production defaults.
type Options struct {
	BatchSize int
	// After is the exclusive keyset watermark. Nil starts from the beginning.
	After *Cursor
	// Schema is a trusted identifier override used only by tests.
	Schema string
}

// BatchResult advances a caller's keyset walk.
//
// Last tracks the SCANNED candidates rather than the settled ones: a recovery
// that is genuinely still pending — or that is currently excluded only by a
// reversible condition such as its issue sitting in 'done' — must never be
// marked, and must not stall the walk either. Last is nil only when the batch
// found no candidates at all, which is how the caller detects the end.
type BatchResult struct {
	Scanned int64
	Settled int64
	Last    *Cursor
}

func qualify(schema, table string) string {
	if schema == "" {
		return table
	}
	return fmt.Sprintf("%q.%s", schema, table)
}

// Batch settles at most BatchSize candidate recovery comments.
//
// A candidate is settled only when some terminal task already recorded it in
// delivered_comment_ids — the one exclusion in
// ListPendingDelegatedFailureRecoveries that cannot be taken back. Every other
// exclusion that query applies is reversible, so a row it currently skips for
// another reason is deliberately left pending here.
//
// The receipt lookup is inverted relative to the runtime query: instead of
// asking "does any terminal task hold THIS comment" once per candidate — which
// re-reads agent_task_queue, the largest table in the database, for every row
// examined — it reads agent_task_queue once per BATCH and hash-joins the
// delivered ids back against the candidates. Both CTEs are MATERIALIZED so that
// stays true regardless of how the planner would otherwise inline them.
func Batch(ctx context.Context, pool *pgxpool.Pool, opts Options) (BatchResult, error) {
	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}
	comment := qualify(opts.Schema, "comment")
	task := qualify(opts.Schema, "agent_task_queue")

	var afterCreatedAt any
	var afterID any
	if opts.After != nil {
		afterCreatedAt = opts.After.CreatedAt
		afterID = opts.After.ID
	}

	query := fmt.Sprintf(`
WITH batch AS MATERIALIZED (
    SELECT id, created_at
    FROM %s
    WHERE author_type = 'system'
      AND type = 'progress_update'
      AND source_task_id IS NOT NULL
      AND recovery_settled_at IS NULL
      AND ($2::timestamptz IS NULL OR (created_at, id) > ($2::timestamptz, $3::uuid))
    ORDER BY created_at, id
    LIMIT $1
), receipts AS MATERIALIZED (
    SELECT DISTINCT batch.id
    FROM %s covering
    CROSS JOIN LATERAL unnest(covering.delivered_comment_ids) AS delivered(id)
    JOIN batch ON batch.id = delivered.id
    WHERE covering.status IN ('completed', 'failed', 'cancelled')
      AND covering.delivered_comment_ids && (SELECT array_agg(id) FROM batch)
), settled AS (
    UPDATE %s recovery
    SET recovery_settled_at = now()
    WHERE recovery.id IN (SELECT id FROM receipts)
      AND recovery.recovery_settled_at IS NULL
    RETURNING recovery.id
)
SELECT (SELECT count(*)::bigint FROM batch),
       (SELECT count(*)::bigint FROM settled),
       (SELECT created_at FROM batch ORDER BY created_at DESC, id DESC LIMIT 1),
       COALESCE((SELECT id::text FROM batch ORDER BY created_at DESC, id DESC LIMIT 1), '')`,
		comment, task, comment)

	var result BatchResult
	var lastCreatedAt *time.Time
	var lastID string
	if err := pool.QueryRow(ctx, query, batchSize, afterCreatedAt, afterID).
		Scan(&result.Scanned, &result.Settled, &lastCreatedAt, &lastID); err != nil {
		return BatchResult{}, fmt.Errorf("backfill delegated failure recovery settled batch: %w", err)
	}
	if lastCreatedAt != nil && lastID != "" {
		result.Last = &Cursor{CreatedAt: *lastCreatedAt, ID: lastID}
	}
	return result, nil
}

// CountRemaining reports how many recovery comments are still unsettled, which
// is exactly the row count of idx_comment_delegated_failure_unsettled. After a
// completed walk this should be the genuinely pending backlog, not history.
func CountRemaining(ctx context.Context, pool *pgxpool.Pool, schema string) (int64, error) {
	var count int64
	if err := pool.QueryRow(ctx, fmt.Sprintf(`
SELECT count(*)
FROM %s
WHERE author_type = 'system'
  AND type = 'progress_update'
  AND source_task_id IS NOT NULL
  AND recovery_settled_at IS NULL`, qualify(schema, "comment"))).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unsettled delegated failure recoveries: %w", err)
	}
	return count, nil
}
