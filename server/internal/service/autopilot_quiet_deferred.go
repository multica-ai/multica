package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Quiet hours defer routine runs; they do not drop them (spec 4.5). A routine
// autopilot run that falls in the window is stored as a deferred run with reason
// quiet_hours, and ReleaseQuietDeferredRuns dispatches it once the window has
// closed. A pause or a budget hold never defers: a scheduled run is skipped, and
// a webhook run is held at claim.

// recordDeferredRun stores a run that quiet hours deferred. It is deferred, so
// nothing runs, and it keeps its plan time, so a repeated plan finds it.
func (s *AutopilotService) recordDeferredRun(
	ctx context.Context,
	autopilot db.Autopilot,
	triggerID pgtype.UUID,
	source string,
	payload []byte,
	plannedAt pgtype.Timestamptz,
	webhookDeliveryID pgtype.UUID,
) (*db.AutopilotRun, error) {
	run, err := s.Queries.CreateAutopilotRun(ctx, db.CreateAutopilotRunParams{
		ID:                dbid.NewV7(),
		AutopilotID:       autopilot.ID,
		TriggerID:         triggerID,
		Source:            source,
		Status:            "deferred",
		TriggerPayload:    payload,
		SquadID:           autopilotSquadAttribution(autopilot),
		PlannedAt:         plannedAt,
		WebhookDeliveryID: webhookDeliveryID,
		ReasonCode:        pgtype.Text{String: string(dispatch.ReasonQuietHours), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("record deferred run: %w", err)
	}
	return &run, nil
}

// ReleaseQuietDeferredRuns dispatches the deferred runs whose window has closed.
// A run still inside quiet hours stays deferred. A scheduled run that a pause or
// a budget hold now refuses is skipped with that reason; a webhook run is
// dispatched and held at claim, as a webhook run admitted during a hold is (see
// dispatchAutopilot). It returns how many it dispatched.
func (s *AutopilotService) ReleaseQuietDeferredRuns(ctx context.Context, limit int32) (int, error) {
	runs, err := s.Queries.ListQuietDeferredAutopilotRuns(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("list deferred runs: %w", err)
	}
	dispatched := 0
	for _, run := range runs {
		ap, err := s.Queries.GetAutopilot(ctx, run.AutopilotID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return dispatched, fmt.Errorf("load autopilot of deferred run: %w", err)
		}
		if ap.Status != "active" {
			continue
		}
		reason, code, held := s.companyHold(ctx, ap, run.Source)
		if held && code == dispatch.ReasonQuietHours {
			continue
		}
		if held && run.Source != "webhook" {
			if _, err := s.Queries.UpdateAutopilotRunSkipped(ctx, db.UpdateAutopilotRunSkippedParams{
				ID:            run.ID,
				FailureReason: pgtype.Text{String: reason, Valid: true},
				ReasonCode:    pgtype.Text{String: string(code), Valid: true},
			}); err != nil {
				return dispatched, fmt.Errorf("skip deferred run: %w", err)
			}
			continue
		}
		run := run
		if _, _, err := s.dispatchAutopilotRun(ctx, ap, run.TriggerID, run.Source, &run, pgtype.UUID{}); err != nil {
			slog.Warn("dispatch deferred autopilot run failed",
				"run_id", util.UUIDToString(run.ID), "error", err)
			continue
		}
		dispatched++
	}
	return dispatched, nil
}

// autopilotBudgetHold is the budget check for an autopilot run. It returns the
// budget that holds the run and whether it does.
func (s *AutopilotService) autopilotBudgetHold(ctx context.Context, ap db.Autopilot, class RunClass) (pgtype.UUID, bool, error) {
	scopeOf := func(ctx context.Context) (budgetScope, error) {
		var squad pgtype.UUID
		if ap.AssigneeType == "squad" {
			squad = ap.AssigneeID
		} else if ap.AssigneeID.Valid {
			agent, err := s.Queries.GetAgent(ctx, ap.AssigneeID)
			if err != nil {
				return budgetScope{}, fmt.Errorf("load autopilot assignee: %w", err)
			}
			squad = agent.HomeSquadID
		}
		sc := budgetScope{squadID: squad}
		if squad.Valid {
			row, err := s.Queries.GetBudgetSquadFloor(ctx, squad)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
			case err != nil:
				return budgetScope{}, fmt.Errorf("load squad floor: %w", err)
			default:
				sc.floorID = row.FloorID
			}
		}
		return sc, nil
	}
	adm, err := NewBudgetGate(s.Bus).Admit(ctx, s.Queries, ap.WorkspaceID, pgtype.UUID{}, class, scopeOf, time.Now())
	if err != nil {
		return pgtype.UUID{}, false, err
	}
	return adm.BudgetID, !adm.Admit, nil
}
