package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/dispatch"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// HeldRunReason reports whether the claim gate is holding a run's queued task
// now, and the reason code it would give (company_paused or budget_hold). A held
// run keeps its issue or task and its status: the refused claim leaves the task
// queued, and the run runs once the hold releases (spec 4.10(b)). The answer is
// the claim gate's own verdict, so the runs list cannot disagree with the claim.
func (s *AutopilotService) HeldRunReason(ctx context.Context, ws pgtype.UUID, run db.AutopilotRun) (dispatch.ReasonCode, bool, error) {
	if s.TaskSvc == nil || !run.TaskID.Valid || (run.Status != "issue_created" && run.Status != "running") {
		return "", false, nil
	}
	task, err := s.Queries.GetAgentTask(ctx, run.TaskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("load run task: %w", err)
	}
	if task.Status != "queued" {
		return "", false, nil
	}
	refusal, err := s.TaskSvc.claimRefusalFor(ctx, s.Queries, ws, task)
	if err != nil {
		return "", false, fmt.Errorf("check claim gate for run: %w", err)
	}
	if refusal == nil {
		return "", false, nil
	}
	return dispatch.ReasonCode(refusal.Reason), true, nil
}
