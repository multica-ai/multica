package cascade

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// reconPool is the minimal pgx surface the Reconciler needs.
// *pgxpool.Pool satisfies it. Local to reconcile.go so PR8 ships
// independently from PR4's matching pgConn interface; when both
// merge, they coexist without a duplicate-type-declaration error.
type reconPool interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// StuckThreshold defines how long a cascade can sit with no events
// before the reconciliation cron flags it as stuck and nudges. Plan
// pins it at 24h.
const StuckThreshold = 24 * time.Hour

// RetriggerTTL is how long cascade_retrigger rows survive after
// being processed before the cron cleans them up. P2 amendment: 30
// days lets us keep loop-guard history visible for the typical
// incident-investigation horizon without unbounded growth.
const RetriggerTTL = 30 * 24 * time.Hour

// StuckCascadeReport is one nudge target the cron found. Renders
// into a comment on the issue and (when push is wired) a Slack /
// Telegram message via notify.Bridge.
type StuckCascadeReport struct {
	IssueID       string
	IssueNumber   int32
	LastEventAt   time.Time
	StalenessHours float64
}

// Reconciler runs the daily housekeeping pass: nudges stuck
// cascades, drops old retrigger rows. Spawned as a goroutine from
// cmd/server, fires on a time.Ticker at 03:00 UTC. Tests drive
// RunOnce directly.
type Reconciler struct {
	pool    reconPool
	notify  func(ctx context.Context, r StuckCascadeReport)
	logger  *slog.Logger
}

// NewReconciler constructs the housekeeper. notify is called once
// per stuck cascade — production wiring posts a multica comment +
// fires the notify.Bridge for off-platform push. Pass a no-op
// closure to disable (e.g. dev boxes).
func NewReconciler(pool reconPool, notify func(ctx context.Context, r StuckCascadeReport), logger *slog.Logger) *Reconciler {
	if logger == nil {
		logger = slog.Default()
	}
	if notify == nil {
		notify = func(context.Context, StuckCascadeReport) {}
	}
	return &Reconciler{pool: pool, notify: notify, logger: logger}
}

// Run starts the cron loop. Fires once on startup (catches up state
// after a deploy), then at every 24h boundary. Cancel ctx to stop.
func (r *Reconciler) Run(ctx context.Context) {
	r.logger.Info("cascade.reconciler.started")
	r.RunOnce(ctx)
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			r.logger.Info("cascade.reconciler.stopped")
			return
		case <-t.C:
			r.RunOnce(ctx)
		}
	}
}

// RunOnce executes one reconciliation pass. Exposed for cron-driven
// invocation (a k8s CronJob can call it via a one-shot binary) and
// for tests.
func (r *Reconciler) RunOnce(ctx context.Context) {
	r.nudgeStuckCascades(ctx)
	r.cleanupOldRetriggers(ctx)
}

// nudgeStuckCascades finds approved cascades whose last event is >
// StuckThreshold old, calls notify for each. Idempotency: we update
// cascade_last_event_at to now() inside the same UPDATE that runs
// the SELECT so the same row does not nudge twice within 24h.
//
// Single SQL statement uses RETURNING so we get the row identity
// without a second query. The cron is single-instance by design
// (k8s CronJob with concurrencyPolicy=Forbid), so we do not need
// row-level locking here.
func (r *Reconciler) nudgeStuckCascades(ctx context.Context) {
	const sql = `
WITH stuck AS (
    SELECT id, number, cascade_last_event_at
    FROM issue
    WHERE cascade_state = 'approved'
      AND status NOT IN ('done', 'deployed', 'cancelled')
      AND cascade_last_event_at IS NOT NULL
      AND cascade_last_event_at < now() - $1::interval
    FOR UPDATE
)
UPDATE issue
SET cascade_last_event_at = now()
FROM stuck
WHERE issue.id = stuck.id
RETURNING issue.id, issue.number, stuck.cascade_last_event_at`

	rows, err := r.pool.Query(ctx, sql, formatInterval(StuckThreshold))
	if err != nil {
		r.logger.Warn("cascade.reconciler.stuck_query_failed", "error", err)
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var (
			id     pgtype.UUID
			number int32
			last   pgtype.Timestamptz
		)
		if err := rows.Scan(&id, &number, &last); err != nil {
			r.logger.Warn("cascade.reconciler.stuck_scan_failed", "error", err)
			continue
		}
		report := StuckCascadeReport{
			IssueNumber: number,
		}
		if id.Valid {
			report.IssueID = uuidString(id)
		}
		if last.Valid {
			report.LastEventAt = last.Time
			report.StalenessHours = time.Since(last.Time).Hours()
		}
		r.notify(ctx, report)
		count++
	}
	if count > 0 {
		r.logger.Info("cascade.reconciler.nudged_stuck", "count", count)
	}
}

// cleanupOldRetriggers drops cascade_retrigger rows older than
// RetriggerTTL. The unique index on event_id is the
// deduplication-by-replay contract; once a row is older than the
// TTL, neither the worker (which only reads processed_at IS NULL)
// nor the loop-guard query (6h window) needs it.
func (r *Reconciler) cleanupOldRetriggers(ctx context.Context) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM cascade_retrigger WHERE fired_at < now() - $1::interval`,
		formatInterval(RetriggerTTL))
	if err != nil {
		r.logger.Warn("cascade.reconciler.cleanup_failed", "error", err)
		return
	}
	if n := tag.RowsAffected(); n > 0 {
		r.logger.Info("cascade.reconciler.cleaned_retriggers", "rows_deleted", n)
	}
}

// formatInterval renders a Go duration into a Postgres interval
// literal Postgres can parse. We use seconds because they're the
// most precise representation that any duration boils down to —
// avoids accidental rounding into integer-minute / integer-hour
// buckets.
func formatInterval(d time.Duration) string {
	return fmt.Sprintf("%d seconds", int(d.Seconds()))
}

// uuidString stringifies a pgtype.UUID. Local helper so the
// reconciler doesn't take a dep on the google/uuid package just for
// the .String() formatter.
func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
