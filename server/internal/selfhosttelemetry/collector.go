package selfhosttelemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const collectionSQL = `
WITH terminal_tasks AS (
    SELECT count(*) FILTER (WHERE status = 'completed') AS completed,
           count(*) FILTER (WHERE status = 'failed') AS failed,
           count(*) FILTER (WHERE status = 'cancelled') AS cancelled
      FROM agent_task_queue
     WHERE status IN ('completed', 'failed', 'cancelled')
       AND completed_at >= $1
       AND completed_at < $2
)
SELECT
    (SELECT count(*) FROM workspace),
    (SELECT count(DISTINCT user_id) FROM member),
    (SELECT count(*) FROM agent WHERE archived_at IS NULL),
    (SELECT count(DISTINCT daemon_id)
       FROM agent_runtime
      WHERE daemon_id IS NOT NULL
        AND last_seen_at >= $1
        AND last_seen_at < $2),
    (SELECT count(*)
       FROM agent_task_queue
      WHERE started_at >= $1
        AND started_at < $2),
    terminal_tasks.completed,
    terminal_tasks.failed,
    terminal_tasks.cancelled
FROM terminal_tasks`

const collectionStatementTimeout = 2 * time.Second

type eventCollector interface {
	Collect(context.Context, *pgx.Conn, time.Time) (Counts, error)
}

type SQLCollector struct{}

// Collect executes all aggregates in one SQL statement, so PostgreSQL uses one
// statement snapshot and every field shares exactly the same [T-24h, T)
// boundary. SET LOCAL guarantees the two-second ceiling cannot leak onto the
// pooled connection after the transaction ends.
func (SQLCollector) Collect(ctx context.Context, conn *pgx.Conn, at time.Time) (counts Counts, err error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return Counts{}, fmt.Errorf("begin telemetry collection: %w", err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if rollbackErr := tx.Rollback(cleanupCtx); rollbackErr != nil && rollbackErr != pgx.ErrTxClosed && err == nil {
			err = fmt.Errorf("rollback telemetry collection: %w", rollbackErr)
		}
	}()

	statementTimeoutSQL := fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", collectionStatementTimeout.Milliseconds())
	if _, err = tx.Exec(ctx, statementTimeoutSQL); err != nil {
		return Counts{}, fmt.Errorf("set telemetry statement timeout: %w", err)
	}
	from := at.UTC().Add(-24 * time.Hour)
	to := at.UTC()
	err = tx.QueryRow(ctx, collectionSQL, from, to).Scan(
		&counts.Workspaces,
		&counts.HumanMembers,
		&counts.Agents,
		&counts.ActiveDaemons,
		&counts.TasksStarted,
		&counts.TasksCompleted,
		&counts.TasksFailed,
		&counts.TasksCancelled,
	)
	if err != nil {
		return Counts{}, fmt.Errorf("collect telemetry snapshot: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Counts{}, fmt.Errorf("commit telemetry collection: %w", err)
	}
	return counts, nil
}
