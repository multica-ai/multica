package cascade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// LoopGuardWindow defines the time window over which we count
// distinct head_sha retriggers. Plan-pinned at 6h. Three or more
// distinct shas in this window trips the guard and stops further
// spawns for the affected PR until the next head_sha or manual
// intervention.
const LoopGuardWindow = 6 * time.Hour

// LoopGuardThreshold is the number of distinct head_shas in
// LoopGuardWindow that trips the guard. Below this, retriggers
// proceed; at or above, the cascade flips to 'loop_guarded'.
const LoopGuardThreshold = 3

// PollInterval is how often the worker polls cascade_retrigger for
// new pending events. Short enough that webhook → spawn latency
// hits the 30s target; long enough that an idle deployment doesn't
// burn cycles. PR8 may swap this for Postgres LISTEN/NOTIFY for
// sub-second wake-up.
const PollInterval = 2 * time.Second

// pollBatchSize bounds the events processed per tick so a burst
// doesn't stall the loop on per-event work.
const pollBatchSize = 5

// pgConn is the minimal pgx surface the worker needs. *pgxpool.Pool
// satisfies it; tests substitute a fake. Same shape as the cascade
// package-level pgPool — duplicated here to keep this file
// importable without taking a runtime dep on cascade.Store.
type pgConn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Spawner enqueues a multica task for an issue when the worker
// decides an event warrants a run. Implemented by TaskService in
// production; tests supply a fake. The TriggerContext records why
// the run was woken so the agent can branch its behavior (fix CI
// vs continue next PR vs re-read review comments).
type Spawner interface {
	Spawn(ctx context.Context, issueID uuid.UUID, trigger TriggerContext) error
	// HasActiveRun reports whether a run is currently active for
	// the issue. Used for the G2 concurrency check.
	HasActiveRun(ctx context.Context, issueID uuid.UUID) (bool, error)
}

// IssueLoader resolves a PUL-N style identifier to a multica issue
// UUID. The worker uses it after parsing the identifier out of the
// PR title or branch name; resolution is workspace-scoped because
// issue.number is workspace-scoped (PUL-102 in workspace A is a
// different issue from PUL-102 in workspace B). Returns ErrNotFound
// when the identifier doesn't match — the worker marks
// scope_filter_skip in that case.
//
// Implemented by cmd/server wiring against db.Queries; tests supply
// a fake.
type IssueLoader interface {
	LookupByIdentifier(ctx context.Context, identifier string) (uuid.UUID, error)
}

// ErrIssueNotFound signals the IssueLoader could not find a row.
// Distinct from a generic error so the worker can differentiate
// "scope skip, normal" from "DB problem, retry later".
var ErrIssueNotFound = errors.New("cascade: issue not found")

// TriggerContext records why a run was spawned. Persisted into the
// cascade_pending_event JSONB column when the event is queued; read
// back by the drain hook so the next run still knows what woke it.
type TriggerContext struct {
	EventID   uuid.UUID `json:"event_id"`
	EventType string    `json:"event_type"`
	PRURL     string    `json:"pr_url,omitempty"`
	PRNumber  int32     `json:"pr_number,omitempty"`
	HeadSHA   string    `json:"head_sha,omitempty"`
}

// Worker is the background queue processor. Constructed once at
// server startup; Run in a goroutine; cancel ctx to stop.
type Worker struct {
	pool    pgConn
	spawner Spawner
	loader  IssueLoader
	logger  *slog.Logger
}

// NewWorker constructs the worker. Spawner is required; pool must
// satisfy the cascade_retrigger / cascade_pending_event / issue
// query surface (real *pgxpool.Pool or a test fake). IssueLoader is
// optional: when nil the worker falls back to scope_filter_skip on
// rows with NULL issue_id (the pre-wiring behavior), so an
// incomplete startup still doesn't drop events on the floor.
func NewWorker(pool pgConn, spawner Spawner, loader IssueLoader, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{pool: pool, spawner: spawner, loader: loader, logger: logger}
}

// Run polls cascade_retrigger for pending events until ctx is
// cancelled. Per-event errors are logged but do not stop the loop —
// one bad event must not silence the rest.
func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(PollInterval)
	defer t.Stop()
	w.logger.Info("cascade.worker.started", "poll_interval", PollInterval)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("cascade.worker.stopped")
			return
		case <-t.C:
			w.PollOnce(ctx)
		}
	}
}

// PollOnce processes one batch of pending events. Exposed (not
// pollOnce) so tests can drive the worker deterministically without
// time.Tick races.
func (w *Worker) PollOnce(ctx context.Context) {
	const sql = `
SELECT id, event_id, issue_id, pr_url, pr_number, COALESCE(pr_title, ''), head_sha,
       COALESCE(branch, ''), event_type
FROM cascade_retrigger
WHERE processed_at IS NULL
ORDER BY fired_at ASC
LIMIT $1
FOR UPDATE SKIP LOCKED`
	rows, err := w.pool.Query(ctx, sql, pollBatchSize)
	if err != nil {
		w.logger.Warn("cascade.worker.query_failed", "error", err)
		return
	}

	type pending struct {
		id        int64
		eventID   uuid.UUID
		issueID   uuid.UUID
		hasIssue  bool
		prURL     string
		prNumber  int32
		prTitle   string
		headSHA   string
		branch    string
		eventType string
	}
	var batch []pending
	for rows.Next() {
		var (
			p   pending
			eid pgtype.UUID
			iid pgtype.UUID
		)
		if err := rows.Scan(&p.id, &eid, &iid, &p.prURL, &p.prNumber, &p.prTitle, &p.headSHA, &p.branch, &p.eventType); err != nil {
			w.logger.Warn("cascade.worker.scan_failed", "error", err)
			continue
		}
		p.eventID = uuid.UUID(eid.Bytes)
		if iid.Valid {
			p.issueID = uuid.UUID(iid.Bytes)
			p.hasIssue = true
		}
		batch = append(batch, p)
	}
	rows.Close()

	for _, p := range batch {
		if !p.hasIssue {
			resolved, ok := w.resolveIssue(ctx, p.id, p.eventID, p.prTitle, p.branch)
			if !ok {
				continue
			}
			p.issueID = resolved
			p.hasIssue = true
		}
		w.processOne(ctx, p.id, p.eventID, p.issueID, p.prURL, p.prNumber, p.headSHA, p.eventType)
	}
}

// resolveIssue runs LookupIssueIdentifier against pr_title + branch
// and asks the IssueLoader to map the identifier to a UUID. On
// success, persists issue_id back to the row so a future iteration
// (or the dashboard query) sees the resolved value. On any failure
// branch the row is marked scope_filter_skip with the specific
// reason and the function returns ok=false.
//
// When IssueLoader is nil (pre-wiring deployments), every NULL
// issue_id row scope-skips — same as the pre-FU1 behavior, so a
// partial rollout never silently drops or mis-routes events.
func (w *Worker) resolveIssue(ctx context.Context, rowID int64, eventID uuid.UUID, prTitle, branch string) (uuid.UUID, bool) {
	if w.loader == nil {
		w.markRow(ctx, rowID, "scope_filter_skip")
		w.logger.Info("cascade.worker.no_loader_configured",
			"event_id", eventID, "row_id", rowID)
		return uuid.Nil, false
	}
	identifier := LookupIssueIdentifier(prTitle, branch)
	if identifier == "" {
		w.markRow(ctx, rowID, "scope_filter_skip")
		w.logger.Info("cascade.worker.no_identifier",
			"event_id", eventID, "row_id", rowID,
			"pr_title", prTitle, "branch", branch)
		return uuid.Nil, false
	}
	issueID, err := w.loader.LookupByIdentifier(ctx, identifier)
	if err != nil {
		if errors.Is(err, ErrIssueNotFound) {
			w.markRow(ctx, rowID, "scope_filter_skip")
			w.logger.Info("cascade.worker.issue_not_found",
				"event_id", eventID, "identifier", identifier)
			return uuid.Nil, false
		}
		w.logger.Warn("cascade.worker.lookup_failed",
			"event_id", eventID, "identifier", identifier, "error", err)
		// Don't mark — next tick retries. Loader returning a real
		// error (DB down, etc.) is recoverable; we keep the row
		// unprocessed.
		return uuid.Nil, false
	}
	if _, err := w.pool.Exec(ctx,
		`UPDATE cascade_retrigger SET issue_id = $1 WHERE id = $2`,
		pgtype.UUID{Bytes: issueID, Valid: true}, rowID); err != nil {
		w.logger.Warn("cascade.worker.set_issue_failed",
			"row_id", rowID, "error", err)
		return uuid.Nil, false
	}
	return issueID, true
}

// processOne runs the per-event pipeline for an already-resolved
// issue: loop guard → concurrency → spawn.
func (w *Worker) processOne(ctx context.Context, rowID int64, eventID, issueID uuid.UUID, prURL string, prNumber int32, headSHA, eventType string) {
	// Loop guard: count distinct head_sha within the 6h window.
	if tripped, err := w.checkLoopGuard(ctx, prURL); err != nil {
		w.logger.Warn("cascade.worker.loop_guard_query_failed", "error", err)
	} else if tripped {
		w.flipLoopGuarded(ctx, issueID)
		w.markRow(ctx, rowID, "loop_guard_skip")
		w.logger.Warn("cascade.worker.loop_guard_tripped",
			"issue_id", issueID, "pr_url", prURL)
		return
	}

	// Concurrency: active run → queue pending.
	active, err := w.spawner.HasActiveRun(ctx, issueID)
	if err != nil {
		w.logger.Warn("cascade.worker.active_run_query_failed",
			"issue_id", issueID, "error", err)
		// Conservative: do not mark; next tick retries.
		return
	}
	tc := TriggerContext{
		EventID:   eventID,
		EventType: eventType,
		PRURL:     prURL,
		PRNumber:  prNumber,
		HeadSHA:   headSHA,
	}
	if active {
		if err := w.queuePending(ctx, issueID, eventID, tc); err != nil {
			w.logger.Warn("cascade.worker.queue_pending_failed",
				"issue_id", issueID, "error", err)
		}
		w.markRow(ctx, rowID, "queued_pending")
		return
	}

	// Spawn.
	if err := w.spawner.Spawn(ctx, issueID, tc); err != nil {
		w.logger.Warn("cascade.worker.spawn_failed",
			"issue_id", issueID, "error", err)
		// Leave processed_at NULL so the next tick retries.
		return
	}
	w.markRow(ctx, rowID, "spawn")
	w.logger.Info("cascade.worker.spawned",
		"issue_id", issueID,
		"event_id", eventID,
		"event_type", eventType,
		"pr_number", prNumber,
	)

	// Touch the issue for the dashboard.
	_, _ = w.pool.Exec(ctx,
		`UPDATE issue SET cascade_last_event_at = now() WHERE id = $1`,
		pgtype.UUID{Bytes: issueID, Valid: true})
}

func (w *Worker) markRow(ctx context.Context, rowID int64, action string) {
	if _, err := w.pool.Exec(ctx,
		`UPDATE cascade_retrigger SET processed_at = now(), action = $1 WHERE id = $2`,
		action, rowID); err != nil {
		w.logger.Warn("cascade.worker.mark_failed", "row_id", rowID, "error", err)
	}
}

// checkLoopGuard reports whether the per-PR distinct-head_sha count
// in LoopGuardWindow meets LoopGuardThreshold. Hits the
// idx_cascade_retrigger_loop_guard index from migration 072.
func (w *Worker) checkLoopGuard(ctx context.Context, prURL string) (bool, error) {
	const sql = `
SELECT COUNT(DISTINCT head_sha)
FROM cascade_retrigger
WHERE pr_url = $1
  AND action = 'spawn'
  AND fired_at > now() - $2::interval`
	var n int
	if err := w.pool.QueryRow(ctx, sql, prURL,
		fmt.Sprintf("%d seconds", int(LoopGuardWindow.Seconds()))).Scan(&n); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return n >= LoopGuardThreshold, nil
}

// flipLoopGuarded transitions the issue's cascade_state to
// 'loop_guarded'. Idempotent — guarded by WHERE state <> 'loop_guarded'.
func (w *Worker) flipLoopGuarded(ctx context.Context, issueID uuid.UUID) {
	if _, err := w.pool.Exec(ctx,
		`UPDATE issue SET cascade_state = 'loop_guarded', cascade_last_event_at = now()
		 WHERE id = $1 AND (cascade_state IS NULL OR cascade_state <> 'loop_guarded')`,
		pgtype.UUID{Bytes: issueID, Valid: true}); err != nil {
		w.logger.Warn("cascade.worker.flip_guard_failed", "issue_id", issueID, "error", err)
	}
}

// queuePending records the event in cascade_pending_event for the
// drain hook (DrainPending) to pick up when the current run
// terminates. Uses ON CONFLICT to enforce queue depth 1.
func (w *Worker) queuePending(ctx context.Context, issueID, eventID uuid.UUID, tc TriggerContext) error {
	payload, err := json.Marshal(tc)
	if err != nil {
		return fmt.Errorf("marshal trigger context: %w", err)
	}
	_, err = w.pool.Exec(ctx, `
INSERT INTO cascade_pending_event (issue_id, event_id, trigger_context, enqueued_at)
VALUES ($1, $2, $3::jsonb, now())
ON CONFLICT (issue_id) DO UPDATE
   SET event_id = EXCLUDED.event_id,
       trigger_context = EXCLUDED.trigger_context,
       enqueued_at = now()`,
		pgtype.UUID{Bytes: issueID, Valid: true},
		pgtype.UUID{Bytes: eventID, Valid: true},
		string(payload),
	)
	return err
}

// DrainPending implements the A2 hook. The TaskService lifecycle
// callbacks (CompleteTask / FailAgentTask / CancelAgentTask) call
// this when a run for the issue ends. If a pending event accumulated
// during the run, this spawns the next run with that event's
// TriggerContext and deletes the pending row.
//
// Errors are logged but not returned — the caller is the task
// completion path and must not be blocked on cascade plumbing.
func (w *Worker) DrainPending(ctx context.Context, issueID uuid.UUID) {
	row := w.pool.QueryRow(ctx, `
DELETE FROM cascade_pending_event
WHERE issue_id = $1
RETURNING event_id, trigger_context`,
		pgtype.UUID{Bytes: issueID, Valid: true})
	var (
		eid pgtype.UUID
		raw []byte
	)
	if err := row.Scan(&eid, &raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return // no pending — normal case
		}
		w.logger.Warn("cascade.worker.drain_query_failed", "issue_id", issueID, "error", err)
		return
	}
	var tc TriggerContext
	if err := json.Unmarshal(raw, &tc); err != nil {
		w.logger.Warn("cascade.worker.drain_unmarshal_failed", "issue_id", issueID, "error", err)
		return
	}
	if err := w.spawner.Spawn(ctx, issueID, tc); err != nil {
		w.logger.Warn("cascade.worker.drain_spawn_failed", "issue_id", issueID, "error", err)
		return
	}
	w.logger.Info("cascade.worker.drained",
		"issue_id", issueID, "event_id", uuid.UUID(eid.Bytes))
}
