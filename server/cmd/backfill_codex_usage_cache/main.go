// Backfill_codex_usage_cache corrects historical Codex task_usage rows
// written before Codex cached input was normalized at ingestion time.
//
// This is a hosted-data repair tool, not a general migration:
//   - dry-run is the default; pass --execute to mutate data
//   - only provider='codex' rows with persisted cache_read_tokens are touched
//   - --cutoff must be the actual hosted deployment time of the ingestion fix
//   - rows updated after the cutoff are left alone to avoid double-subtracting
//   - the hourly rollup is rebuilt explicitly from the updated_at window
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/logger"
)

const rollupAdvisoryLockID = 4246

type config struct {
	cutoff              time.Time
	cutoffRaw           string
	workspaceID         string
	batchSize           int
	sleepBetweenBatches time.Duration
	execute             bool
	rebuildRollup       bool
}

type summaryRow struct {
	WorkspaceID   string
	DateUTC       string
	Rows          int64
	InputBefore   int64
	InputAfter    int64
	Overcount     int64
	ClampedRows   int64
	MinCreatedUTC time.Time
	MaxCreatedUTC time.Time
}

type totals struct {
	Rows        int64
	InputBefore int64
	InputAfter  int64
	Overcount   int64
	ClampedRows int64
}

func main() {
	logger.Init()
	if err := run(); err != nil {
		slog.Error("codex usage cache backfill failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config{}
	flag.StringVar(&cfg.cutoffRaw, "cutoff", "", "required RFC3339 hosted deployment time of the Codex usage normalization fix")
	flag.StringVar(&cfg.workspaceID, "workspace-id", "", "optional workspace UUID to limit the backfill")
	flag.IntVar(&cfg.batchSize, "batch-size", 1000, "number of task_usage rows to update per batch when --execute is set")
	flag.DurationVar(&cfg.sleepBetweenBatches, "sleep-between-batches", 0, "pause between update batches to throttle write pressure")
	flag.BoolVar(&cfg.execute, "execute", false, "mutate task_usage rows; without this flag the command only prints a dry-run summary")
	flag.BoolVar(&cfg.rebuildRollup, "rebuild-rollup", true, "after --execute, call rollup_task_usage_hourly_window for the update window")
	flag.Parse()

	now := time.Now().UTC()
	if err := cfg.parseAndValidate(now); err != nil {
		return err
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	rows, total, err := loadDryRunSummary(ctx, pool, cfg)
	if err != nil {
		return err
	}
	logSummary(cfg, rows, total)
	if total.Rows == 0 {
		slog.Info("no eligible Codex task_usage rows found")
		return nil
	}
	if !cfg.execute {
		slog.Info("dry-run complete; review the summary, then re-run with --execute to apply the backfill")
		return nil
	}

	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire advisory-lock connection: %w", err)
	}
	defer lockConn.Release()
	if _, err := lockConn.Exec(ctx, `SELECT pg_advisory_lock($1)`, rollupAdvisoryLockID); err != nil {
		return fmt.Errorf("acquire advisory lock %d: %w", rollupAdvisoryLockID, err)
	}
	defer func() {
		_, _ = lockConn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, rollupAdvisoryLockID)
	}()

	updateStartedAt, err := databaseClock(ctx, pool)
	if err != nil {
		return err
	}

	updatedRows, removedTokens, err := executeBackfill(ctx, pool, cfg)
	if err != nil {
		return err
	}
	slog.Info("task_usage rows updated", "rows", updatedRows, "input_tokens_removed", removedTokens)
	if updatedRows == 0 || !cfg.rebuildRollup {
		return nil
	}

	updateFinishedAt, err := databaseClock(ctx, pool)
	if err != nil {
		return err
	}
	rollupFrom, rollupTo := rollupWindow(updateStartedAt, updateFinishedAt)
	var rollupRows int64
	if err := pool.QueryRow(ctx,
		`SELECT rollup_task_usage_hourly_window($1::timestamptz, $2::timestamptz)`,
		rollupFrom, rollupTo,
	).Scan(&rollupRows); err != nil {
		return fmt.Errorf("rebuild hourly rollup for update window %s..%s: %w",
			rollupFrom.Format(time.RFC3339), rollupTo.Format(time.RFC3339), err)
	}
	slog.Info("hourly rollup rebuilt",
		"from", rollupFrom.Format(time.RFC3339),
		"to", rollupTo.Format(time.RFC3339),
		"rows_touched", rollupRows)
	return nil
}

func (cfg *config) parseAndValidate(now time.Time) error {
	if cfg.cutoffRaw == "" {
		return fmt.Errorf("--cutoff is required; use the hosted deployment time of the Codex usage normalization fix")
	}
	cutoff, err := time.Parse(time.RFC3339, cfg.cutoffRaw)
	if err != nil {
		return fmt.Errorf("parse --cutoff as RFC3339: %w", err)
	}
	cutoff = cutoff.UTC()
	if !cutoff.Before(now) {
		return fmt.Errorf("--cutoff must be before now; refusing a future cutoff because it could double-subtract already-normalized rows")
	}
	if cfg.batchSize <= 0 {
		return fmt.Errorf("--batch-size must be positive")
	}
	cfg.cutoff = cutoff
	return nil
}

func loadDryRunSummary(ctx context.Context, pool *pgxpool.Pool, cfg config) ([]summaryRow, totals, error) {
	const query = `
SELECT
    a.workspace_id::text AS workspace_id,
    (tu.created_at AT TIME ZONE 'UTC')::date::text AS date_utc,
    COUNT(*)::bigint AS rows,
    COALESCE(SUM(tu.input_tokens), 0)::bigint AS input_before,
    COALESCE(SUM(GREATEST(tu.input_tokens - tu.cache_read_tokens, 0)), 0)::bigint AS input_after,
    COALESCE(SUM(tu.input_tokens - GREATEST(tu.input_tokens - tu.cache_read_tokens, 0)), 0)::bigint AS overcount,
    COUNT(*) FILTER (WHERE tu.input_tokens < tu.cache_read_tokens)::bigint AS clamped_rows,
    MIN(tu.created_at) AS min_created_at,
    MAX(tu.created_at) AS max_created_at
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
JOIN agent a ON a.id = atq.agent_id
WHERE tu.provider = 'codex'
  AND tu.cache_read_tokens > 0
  AND tu.input_tokens > 0
  AND COALESCE(tu.updated_at, tu.created_at) < $1
  AND (NULLIF($2, '')::uuid IS NULL OR a.workspace_id = NULLIF($2, '')::uuid)
GROUP BY a.workspace_id, (tu.created_at AT TIME ZONE 'UTC')::date
ORDER BY a.workspace_id, date_utc`
	rows, err := pool.Query(ctx, query, cfg.cutoff, cfg.workspaceID)
	if err != nil {
		return nil, totals{}, fmt.Errorf("load dry-run summary: %w", err)
	}
	defer rows.Close()

	var summaries []summaryRow
	var total totals
	for rows.Next() {
		var row summaryRow
		if err := rows.Scan(
			&row.WorkspaceID,
			&row.DateUTC,
			&row.Rows,
			&row.InputBefore,
			&row.InputAfter,
			&row.Overcount,
			&row.ClampedRows,
			&row.MinCreatedUTC,
			&row.MaxCreatedUTC,
		); err != nil {
			return nil, totals{}, fmt.Errorf("scan dry-run summary: %w", err)
		}
		summaries = append(summaries, row)
		total.Rows += row.Rows
		total.InputBefore += row.InputBefore
		total.InputAfter += row.InputAfter
		total.Overcount += row.Overcount
		total.ClampedRows += row.ClampedRows
	}
	if err := rows.Err(); err != nil {
		return nil, totals{}, fmt.Errorf("iterate dry-run summary: %w", err)
	}
	return summaries, total, nil
}

func logSummary(cfg config, rows []summaryRow, total totals) {
	slog.Info("Codex usage cache backfill candidate total",
		"execute", cfg.execute,
		"cutoff", cfg.cutoff.Format(time.RFC3339),
		"workspace_id", cfg.workspaceID,
		"rows", total.Rows,
		"input_before", total.InputBefore,
		"input_after", total.InputAfter,
		"input_tokens_removed", total.Overcount,
		"clamped_rows", total.ClampedRows)
	for _, row := range rows {
		slog.Info("candidate summary",
			"workspace_id", row.WorkspaceID,
			"date_utc", row.DateUTC,
			"rows", row.Rows,
			"input_before", row.InputBefore,
			"input_after", row.InputAfter,
			"input_tokens_removed", row.Overcount,
			"clamped_rows", row.ClampedRows,
			"min_created_at", row.MinCreatedUTC.UTC().Format(time.RFC3339),
			"max_created_at", row.MaxCreatedUTC.UTC().Format(time.RFC3339))
	}
}

func executeBackfill(ctx context.Context, pool *pgxpool.Pool, cfg config) (int64, int64, error) {
	const query = `
WITH candidates AS (
    SELECT
        tu.id,
        (tu.input_tokens - GREATEST(tu.input_tokens - tu.cache_read_tokens, 0))::bigint AS removed_tokens
      FROM task_usage tu
      JOIN agent_task_queue atq ON atq.id = tu.task_id
      JOIN agent a ON a.id = atq.agent_id
     WHERE tu.provider = 'codex'
       AND tu.cache_read_tokens > 0
       AND tu.input_tokens > 0
       AND COALESCE(tu.updated_at, tu.created_at) < $1
       AND (NULLIF($2, '')::uuid IS NULL OR a.workspace_id = NULLIF($2, '')::uuid)
     ORDER BY COALESCE(tu.updated_at, tu.created_at), tu.id
     LIMIT $3
     FOR UPDATE OF tu SKIP LOCKED
),
updated AS (
    UPDATE task_usage tu
       SET input_tokens = GREATEST(tu.input_tokens - tu.cache_read_tokens, 0),
           updated_at = now()
      FROM candidates c
     WHERE tu.id = c.id
     RETURNING c.removed_tokens
)
SELECT COUNT(*)::bigint, COALESCE(SUM(removed_tokens), 0)::bigint FROM updated`

	var totalRows, totalRemoved int64
	for {
		var rows, removed int64
		if err := pool.QueryRow(ctx, query, cfg.cutoff, cfg.workspaceID, cfg.batchSize).Scan(&rows, &removed); err != nil {
			return totalRows, totalRemoved, fmt.Errorf("update Codex task_usage batch: %w", err)
		}
		if rows == 0 {
			break
		}
		totalRows += rows
		totalRemoved += removed
		slog.Info("updated Codex task_usage batch", "rows", rows, "input_tokens_removed", removed, "total_rows", totalRows)
		if cfg.sleepBetweenBatches > 0 {
			select {
			case <-time.After(cfg.sleepBetweenBatches):
			case <-ctx.Done():
				return totalRows, totalRemoved, ctx.Err()
			}
		}
	}
	return totalRows, totalRemoved, nil
}

func databaseClock(ctx context.Context, pool *pgxpool.Pool) (time.Time, error) {
	var ts time.Time
	if err := pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&ts); err != nil {
		return time.Time{}, fmt.Errorf("read database clock: %w", err)
	}
	return ts.UTC(), nil
}

func rollupWindow(startedAt, finishedAt time.Time) (time.Time, time.Time) {
	return startedAt.UTC().Add(-time.Second), finishedAt.UTC().Add(time.Second)
}

func correctedInputTokens(inputTokens, cacheReadTokens int64) int64 {
	corrected := inputTokens - cacheReadTokens
	if corrected < 0 {
		return 0
	}
	return corrected
}
