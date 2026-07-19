package evals

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"
)

// scheduleCronParser accepts standard 5-field cron expressions and the
// descriptor shortcuts (@hourly, @daily, …). Same configuration the workflow
// cron sweeper uses (workflows/cron.go); duplicated here to avoid exporting an
// internal parser across package boundaries.
var scheduleCronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// defaultScheduleTimezone is the IANA tz used when a schedule leaves timezone
// empty — Firtal office hours.
const defaultScheduleTimezone = "Europe/Copenhagen"

// EvalSchedule is one recurring-run schedule for an eval.
type EvalSchedule struct {
	ID           uuid.UUID  `json:"id"`
	WorkspaceID  uuid.UUID  `json:"workspace_id"`
	EvalID       uuid.UUID  `json:"eval_id"`
	ScheduleExpr string     `json:"schedule_expr"`
	Timezone     string     `json:"timezone"`
	Enabled      bool       `json:"enabled"`
	NextRunAt    *time.Time `json:"next_run_at,omitempty"`
	LastRunAt    *time.Time `json:"last_run_at,omitempty"`
	CreatedByID  uuid.UUID  `json:"created_by_id"`
	CreatedAt    time.Time  `json:"created_at"`
}

type EvalScheduleInput struct {
	ScheduleExpr string `json:"schedule_expr"`
	Timezone     string `json:"timezone"`
	Enabled      bool   `json:"enabled"`
}

const evalScheduleColumns = `id, workspace_id, eval_id, schedule_expr, timezone,
 enabled, next_run_at, last_run_at, created_by_id, created_at`

func scanEvalSchedule(row pgx.Row) (EvalSchedule, error) {
	var value EvalSchedule
	var next, last pgtype.Timestamptz
	if err := row.Scan(&value.ID, &value.WorkspaceID, &value.EvalID, &value.ScheduleExpr,
		&value.Timezone, &value.Enabled, &next, &last, &value.CreatedByID, &value.CreatedAt); err != nil {
		return EvalSchedule{}, err
	}
	if next.Valid {
		t := next.Time
		value.NextRunAt = &t
	}
	if last.Valid {
		t := last.Time
		value.LastRunAt = &t
	}
	return value, nil
}

// nextScheduleRun parses expr in tz and returns the first fire instant strictly
// after `from`. An empty timezone falls back to the Firtal default.
func nextScheduleRun(expr, timezone string, from time.Time) (time.Time, error) {
	schedule, err := scheduleCronParser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid schedule_expr %q: %w", expr, err)
	}
	tz := timezone
	if tz == "" {
		tz = defaultScheduleTimezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", tz, err)
	}
	return schedule.Next(from.In(loc)), nil
}

// UpsertSchedule creates or replaces the (single) schedule for an eval. The
// initial next_run_at is anchored on now() so the schedule never backfills a
// missed window; changing the expression recomputes it. Returns the stored row.
func (s *Store) UpsertSchedule(ctx context.Context, workspaceID, evalID, actorID uuid.UUID, scheduleExpr, timezone string, enabled bool) (EvalSchedule, error) {
	next, err := nextScheduleRun(scheduleExpr, timezone, time.Now())
	if err != nil {
		return EvalSchedule{}, err
	}
	row := s.pool.QueryRow(ctx, `
        INSERT INTO cerebro_eval_schedule
            (workspace_id, eval_id, schedule_expr, timezone, enabled, next_run_at, created_by_id)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
        ON CONFLICT (eval_id) DO UPDATE SET
            schedule_expr = EXCLUDED.schedule_expr,
            timezone      = EXCLUDED.timezone,
            enabled       = EXCLUDED.enabled,
            next_run_at   = EXCLUDED.next_run_at,
            claimed_until = NULL
        RETURNING `+evalScheduleColumns,
		workspaceID, evalID, scheduleExpr, timezone, enabled,
		pgtype.Timestamptz{Time: next, Valid: true}, actorID)
	return scanEvalSchedule(row)
}

// GetSchedule returns the single recurring schedule for an eval.
func (s *Store) GetSchedule(ctx context.Context, workspaceID, evalID uuid.UUID) (EvalSchedule, error) {
	value, err := scanEvalSchedule(s.pool.QueryRow(ctx, `
		SELECT `+evalScheduleColumns+` FROM cerebro_eval_schedule
		WHERE workspace_id=$1 AND eval_id=$2`, workspaceID, evalID))
	if errors.Is(err, pgx.ErrNoRows) {
		return EvalSchedule{}, ErrNotFound
	}
	return value, err
}

// DeleteSchedule switches an eval back to manual-only runs. It is idempotent
// so saving "Manual only" is safe even when no schedule exists yet.
func (s *Store) DeleteSchedule(ctx context.Context, workspaceID, evalID uuid.UUID) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM cerebro_eval_schedule WHERE workspace_id=$1 AND eval_id=$2`, workspaceID, evalID)
	return err
}

// ClaimDueSchedules atomically leases up to `limit` enabled schedules whose
// next_run_at has elapsed. SKIP LOCKED plus claimed_until prevents two server
// replicas from executing the same scheduled eval; an abandoned lease retries.
func (s *Store) ClaimDueSchedules(ctx context.Context, now time.Time, limit int32) ([]EvalSchedule, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `WITH due AS (
        SELECT id FROM cerebro_eval_schedule
        WHERE enabled AND next_run_at IS NOT NULL AND next_run_at <= $1
          AND (claimed_until IS NULL OR claimed_until <= $1)
          AND EXISTS (
            SELECT 1 FROM cerebro_feature_flags f
            WHERE f.workspace_id=cerebro_eval_schedule.workspace_id
              AND f.user_id='00000000-0000-0000-0000-000000000000'
              AND f.flag_key='cerebro_evals' AND f.enabled
          )
        ORDER BY next_run_at ASC
        FOR UPDATE SKIP LOCKED
        LIMIT $2
      )
      UPDATE cerebro_eval_schedule s
      SET claimed_until=$1 + interval '10 minutes'
      FROM due WHERE s.id=due.id
      RETURNING s.`+evalScheduleColumns, pgtype.Timestamptz{Time: now, Valid: true}, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvalSchedule
	for rows.Next() {
		schedule, err := scanEvalSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, schedule)
	}
	return out, rows.Err()
}

// MarkScheduleRan records a completed run: last_run_at is stamped now and
// next_run_at is advanced to `next` (the schedule's following fire instant).
func (s *Store) MarkScheduleRan(ctx context.Context, id uuid.UUID, next time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE cerebro_eval_schedule
		SET last_run_at = now(), next_run_at = $2, claimed_until = NULL
        WHERE id = $1`, id, pgtype.Timestamptz{Time: next, Valid: true})
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("eval schedule not found")
	}
	return nil
}

// NextRun computes the schedule's following fire instant after `from`, using the
// schedule's own expression and timezone. Used by the sweeper to advance a row.
func (e EvalSchedule) NextRun(from time.Time) (time.Time, error) {
	return nextScheduleRun(e.ScheduleExpr, e.Timezone, from)
}
