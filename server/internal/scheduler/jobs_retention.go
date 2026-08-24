package scheduler

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// JobNameRetentionSweep is the canonical audit-row name. Stable across
// releases — do not rename without a migration.
const JobNameRetentionSweep = "retention_sweep"

const (
	// retentionBatchSize bounds rows deleted per statement so a first run
	// against a large backlog cannot hold locks or WAL for long stretches.
	retentionBatchSize = 1000
	// retentionMaxLoops caps batches per table per run (1000 * 50 = 50k
	// rows/table/run worst case); the rest waits for tomorrow's plan.
	retentionMaxLoops = 50
)

// Env knobs (all optional):
//
//	MULTICA_RETENTION_DAYS                     shared default age threshold. 0 (unset) = job inert.
//	MULTICA_RETENTION_CRON_EXECUTIONS_DAYS     per-table override for sys_cron_executions.
//	MULTICA_RETENTION_WEBHOOK_DELIVERY_DAYS    per-table override for webhook_delivery.
//	MULTICA_RETENTION_INBOX_ITEM_DAYS          per-table override for inbox_item.
//
// A per-table variable set to 0 disables that table even when the shared
// default is on. Deletes are terminal-state-only and batched; nothing
// referenced by live rows is touched.
type retentionConfig struct {
	CronExecutionsDays int
	WebhookDeliveryDay int
	InboxItemDays      int
}

func retentionConfigFromEnv(getenv func(string) string) retentionConfig {
	shared := retentionDaysFromEnv(getenv, "MULTICA_RETENTION_DAYS", 0)
	return retentionConfig{
		CronExecutionsDays: retentionDaysFromEnv(getenv, "MULTICA_RETENTION_CRON_EXECUTIONS_DAYS", shared),
		WebhookDeliveryDay: retentionDaysFromEnv(getenv, "MULTICA_RETENTION_WEBHOOK_DELIVERY_DAYS", shared),
		InboxItemDays:      retentionDaysFromEnv(getenv, "MULTICA_RETENTION_INBOX_ITEM_DAYS", shared),
	}
}

func retentionDaysFromEnv(getenv func(string) string, name string, def int) int {
	raw := getenv(name)
	if raw == "" {
		return def
	}
	days, err := strconv.Atoi(raw)
	if err != nil || days < 0 {
		slog.Warn("retention sweep: invalid value, using default", "env", name, "value", raw, "default", def)
		return def
	}
	return days
}

// retentionTable is one delete stage: bounded DELETE of rows past the
// age threshold that are in a terminal state.
type retentionTable struct {
	name string
	days int
	sql  string
}

func retentionTables(cfg retentionConfig) []retentionTable {
	tables := []retentionTable{
		{
			name: "sys_cron_executions",
			days: cfg.CronExecutionsDays,
			// Terminal executions only (SUCCESS/FAILED); RUNNING rows are
			// owned by the stale policy. Uses idx_sys_cron_exec_finished.
			sql: `DELETE FROM sys_cron_executions t
				 USING (SELECT id FROM sys_cron_executions
				 	WHERE status IN ('SUCCESS', 'FAILED')
				 	  AND finished_at < now() - make_interval(days => $1)
				 LIMIT $2) s
				WHERE t.id = s.id`,
		},
		{
			name: "webhook_delivery",
			days: cfg.WebhookDeliveryDay,
			// Non-'queued' rows are ingress-terminal (dispatched/rejected/
			// ignored/failed); raw bodies are the bulk of the bloat.
			sql: `DELETE FROM webhook_delivery t
				 USING (SELECT id FROM webhook_delivery
				 	WHERE status <> 'queued'
				 	  AND created_at < now() - make_interval(days => $1)
				 LIMIT $2) s
				WHERE t.id = s.id`,
		},
		{
			name: "inbox_item",
			days: cfg.InboxItemDays,
			// Read notifications only; unread items are kept forever.
			sql: `DELETE FROM inbox_item t
				 USING (SELECT id FROM inbox_item
				 	WHERE read
				 	  AND created_at < now() - make_interval(days => $1)
				 LIMIT $2) s
				WHERE t.id = s.id`,
		},
	}
	out := tables[:0]
	for _, t := range tables {
		if t.days > 0 {
			out = append(out, t)
		}
	}
	return out
}

// RetentionSweepJob returns the JobSpec that prunes append-only history
// tables (GAP-9): terminal sys_cron_executions rows, delivered
// webhook_delivery rows (raw bodies!), read inbox_items. Opt-in via
// MULTICA_RETENTION_DAYS; unset means every stage is disabled and the
// job is an inert no-op. Deletions are batched per run.
func RetentionSweepJob(pool *pgxpool.Pool) JobSpec {
	cfg := retentionConfigFromEnv(os.Getenv)
	return JobSpec{
		Name:              JobNameRetentionSweep,
		Cadence:           24 * time.Hour,
		ScheduleDelay:     time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     7 * 24 * time.Hour,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      15 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true, // deletes are idempotent
		MaxAttempts:       3,
		RetryBackoff: []time.Duration{
			5 * time.Minute,
			30 * time.Minute,
		},
		Scopes: StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			result := map[string]any{}
			var total int64
			for _, t := range retentionTables(cfg) {
				var deleted int64
				for i := 0; i < retentionMaxLoops; i++ {
					tag, err := pool.Exec(ctx, t.sql, t.days, retentionBatchSize)
					if err != nil {
						return HandlerResult{}, &tableError{table: t.name, err: err}
					}
					deleted += tag.RowsAffected()
					if tag.RowsAffected() < retentionBatchSize {
						break
					}
				}
				result[t.name+"_deleted"] = deleted
				total += deleted
			}
			if len(result) == 0 {
				result["disabled"] = true // MULTICA_RETENTION_DAYS unset/0
			}
			return HandlerResult{RowsAffected: total, Result: result}, nil
		},
	}
}

type tableError struct {
	table string
	err   error
}

func (e *tableError) Error() string { return "retention sweep: " + e.table + ": " + e.err.Error() }
func (e *tableError) Unwrap() error { return e.err }
