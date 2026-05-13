package workflows

// Phase-3 cron sweeper (JEH-1108, PR 2).
//
// Polls cerebro_workflow rows whose trigger_type='cron' once per tick and
// dispatches a synthetic TriggerEvent when the parsed ScheduleExpr's next
// fire instant has elapsed since last_fired_at. Wired into server boot
// alongside RunRetrySweeper.
//
// Design notes:
//
//   * We deliberately use robfig/cron/v3's parser (NOT the full cron.New()
//     scheduler) because the platform already has a ticker pattern for
//     long-running sweepers and we want one place to gate on
//     CEREBRO_WORKFLOWS_ENABLED and process cancellation.
//
//   * The parser accepts standard 5-field cron expressions AND the
//     descriptor shortcuts (@hourly, @daily, ...) because both forms are
//     useful in the UI ("Hver morgen kl 8" → `0 8 * * *`,
//     "Hver time" → `@hourly`).
//
//   * First-fire semantics: the very first time the sweeper sees a workflow
//     it inserts a cron_state row with last_fired_at=now() and next_fire_at=
//     schedule.Next(now()). The workflow does NOT fire on the first sweep —
//     that would silently backfill a missed week's worth of triggers if a
//     workflow was added with @hourly and the sweeper had been down.
//
//   * Bad cron expressions are caught at write time by validateWriteRequest,
//     and at sweep time as a safety net (log + skip). A single bad row never
//     blocks the rest of the sweep.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

// defaultCronTimezone is the IANA tz used when TriggerConfigCron.Timezone is
// empty. Firtal-internal default — Copenhagen office hours.
const defaultCronTimezone = "Europe/Copenhagen"

// cronParser is the parser used for both validateWriteRequest and the
// sweeper. Created once because cron.NewParser is non-trivial allocation
// (it builds a small state machine).
var cronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// RunCronSweeper polls enabled cron workflows once per tick. Started as a
// goroutine at server boot; the caller wires the cancellation context and
// the tick interval (production uses 1 minute, tests inject smaller values).
//
// Gated on Service.Enabled() per-tick — flipping CEREBRO_WORKFLOWS_ENABLED
// after boot quiesces the sweeper without restarting the process.
func (s *Service) RunCronSweeper(ctx context.Context, tick time.Duration) {
	if tick <= 0 {
		tick = time.Minute
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !s.enabled {
				continue
			}
			if err := s.sweepCron(ctx); err != nil {
				slog.Warn("workflow cron sweep failed", "error", err)
			}
		}
	}
}

// sweepCron is one iteration of the loop, split out so tests can invoke it
// deterministically without driving a ticker.
func (s *Service) sweepCron(ctx context.Context) error {
	rows, err := s.queries.ListEnabledCronWorkflowsAcrossWorkspaces(ctx)
	if err != nil {
		return fmt.Errorf("list cron workflows: %w", err)
	}
	now := nowFunc()
	for _, row := range rows {
		if err := s.sweepCronOne(ctx, row, now); err != nil {
			// Per-workflow failures are absorbed so one bad row doesn't
			// block the sweep. Logged with workflow_id so an operator can
			// trace it.
			slog.Warn("workflow cron sweep: workflow failed",
				"workflow_id", uuidString(row.ID),
				"workspace_id", uuidString(row.WorkspaceID),
				"error", err,
			)
		}
	}
	return nil
}

// sweepCronOne evaluates a single workflow's schedule. Pulled out so tests
// can poke at the per-workflow logic in isolation.
func (s *Service) sweepCronOne(ctx context.Context, row cerebrodb.CerebroWorkflow, now time.Time) error {
	var cfg TriggerConfigCron
	if len(row.TriggerConfig) > 0 {
		if err := json.Unmarshal(row.TriggerConfig, &cfg); err != nil {
			return fmt.Errorf("parse trigger_config: %w", err)
		}
	}
	if cfg.ScheduleExpr == "" {
		return fmt.Errorf("schedule_expr is empty")
	}

	tz := cfg.Timezone
	if tz == "" {
		tz = defaultCronTimezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return fmt.Errorf("load tz %q: %w", tz, err)
	}

	schedule, err := cronParser.Parse(cfg.ScheduleExpr)
	if err != nil {
		return fmt.Errorf("parse schedule %q: %w", cfg.ScheduleExpr, err)
	}

	state, err := s.queries.GetCerebroWorkflowCronState(ctx, row.ID)
	switch {
	case err == nil:
		// Have a row — fall through to the fire decision below.
	case isNoRows(err):
		// First-ever sweep for this workflow. Anchor on now() so we never
		// backfill, and compute the genuine next fire. The workflow does
		// NOT fire on this sweep.
		next := schedule.Next(now.In(loc))
		return s.queries.UpsertCerebroWorkflowCronState(ctx, cerebrodb.UpsertCerebroWorkflowCronStateParams{
			WorkflowID:  row.ID,
			LastFiredAt: pgtype.Timestamptz{Time: now, Valid: true},
			NextFireAt:  pgtype.Timestamptz{Time: next, Valid: true},
		})
	default:
		return fmt.Errorf("load cron_state: %w", err)
	}

	// Determine the reference instant we compute "next fire" from. If
	// next_fire_at is set in the state row, use it directly (it's authoritative
	// and already reflects the schedule). Otherwise recompute from
	// last_fired_at.
	var nextFire time.Time
	switch {
	case state.NextFireAt.Valid:
		nextFire = state.NextFireAt.Time
	case state.LastFiredAt.Valid:
		nextFire = schedule.Next(state.LastFiredAt.Time.In(loc))
	default:
		// Both fields nil (shouldn't happen — first-sweep insert sets both),
		// but treat as first-fire so we recover gracefully.
		nextFire = schedule.Next(now.In(loc))
		return s.queries.UpsertCerebroWorkflowCronState(ctx, cerebrodb.UpsertCerebroWorkflowCronStateParams{
			WorkflowID:  row.ID,
			LastFiredAt: pgtype.Timestamptz{Time: now, Valid: true},
			NextFireAt:  pgtype.Timestamptz{Time: nextFire, Valid: true},
		})
	}

	if now.Before(nextFire) {
		// Not yet due.
		return nil
	}

	// Fire. Synthesize a TriggerEvent with no IssueID (cron is a workspace-
	// level event). EventID is deterministic per fire instant so a sweeper
	// restart that re-evaluates the same window collapses through the
	// idempotency table.
	te := TriggerEvent{
		EventID:     "cron:" + uuidString(row.ID) + ":" + nextFire.UTC().Format(time.RFC3339),
		WorkspaceID: uuidString(row.WorkspaceID),
		Type:        TriggerCron,
		Raw: map[string]any{
			"cron": map[string]any{
				"schedule_expr": cfg.ScheduleExpr,
				"timezone":      tz,
				"fired_at":      nextFire.UTC().Format(time.RFC3339),
			},
		},
	}
	// IMPORTANT: do NOT call Dispatch here — Dispatch re-lists all
	// cron-trigger workflows in the workspace and would fan one due
	// workflow out to every cron workflow in the workspace. The sweeper
	// already loops per workflow, so we Execute on this specific row.
	wf := workflow{
		id:             row.ID,
		workspaceID:    row.WorkspaceID,
		projectID:      row.ProjectID,
		name:           row.Name,
		triggerType:    row.TriggerType,
		triggerConfig:  row.TriggerConfig,
		conditions:     row.Conditions,
		actionType:     row.ActionType,
		actionConfig:   row.ActionConfig,
		createdByID:    row.CreatedByID,
		createdByType:  row.CreatedByType,
		outboundSecret: row.OutboundWebhookSecret.String,
	}
	if !s.conditionsHold(ctx, wf, te) {
		// Conditions evaluated false — advance state but don't fire.
		advanced := schedule.Next(nextFire.In(loc))
		return s.queries.UpsertCerebroWorkflowCronState(ctx, cerebrodb.UpsertCerebroWorkflowCronStateParams{
			WorkflowID:  row.ID,
			LastFiredAt: pgtype.Timestamptz{Time: nextFire, Valid: true},
			NextFireAt:  pgtype.Timestamptz{Time: advanced, Valid: true},
		})
	}
	if err := s.Execute(ctx, wf, te); err != nil {
		// Don't advance the state on execute failure — the next sweep will
		// retry naturally. Idempotency-keyed so a retry of the same fire
		// won't double-dispatch.
		return fmt.Errorf("execute cron workflow: %w", err)
	}

	// Advance state to the next computed fire instant after `nextFire` (not
	// after `now`) so we walk the schedule rather than skipping intervals
	// after a missed tick. If now() has run far past nextFire (e.g. process
	// was down for an hour), the next sweep will see nextFire still in the
	// past and fire again — but each fire collapses through the idempotency
	// key so duplicate runs are absorbed. Choosing this over the
	// "skip-missed" alternative because spec calls for at-least-once cron
	// semantics.
	advanced := schedule.Next(nextFire.In(loc))
	return s.queries.UpsertCerebroWorkflowCronState(ctx, cerebrodb.UpsertCerebroWorkflowCronStateParams{
		WorkflowID:  row.ID,
		LastFiredAt: pgtype.Timestamptz{Time: nextFire, Valid: true},
		NextFireAt:  pgtype.Timestamptz{Time: advanced, Valid: true},
	})
}

// isNoRows wraps the pgx sentinel so the rest of the file can switch on a
// boolean without dragging the pgx import into every call site.
func isNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// validateCronSchedule is the helper used both by the sweeper (defensive)
// and the handler (write-time validation).
func validateCronSchedule(expr, tz string) error {
	if expr == "" {
		return fmt.Errorf("schedule_expr is required")
	}
	if _, err := cronParser.Parse(expr); err != nil {
		return fmt.Errorf("invalid cron schedule_expr: %w", err)
	}
	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Errorf("invalid timezone %q: %w", tz, err)
		}
	}
	return nil
}
