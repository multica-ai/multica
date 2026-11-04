package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DigestBudgetHoldsShown is how many held runs the brief names. The count is the
// whole number in force.
const DigestBudgetHoldsShown = 5

// DigestBudget is the brief's Budget section (spec 4.2): the budgets at or above
// their soft line in the current period, and the holds in force.
type DigestBudget struct {
	Scopes       []DigestBudgetScope `json:"scopes"`
	HoldsInForce int                 `json:"holds_in_force"`
	Holds        []DigestBudgetHold  `json:"holds"`
}

// DigestBudgetScope is one budget at or above its soft line.
type DigestBudgetScope struct {
	BudgetID    string `json:"budget_id"`
	ScopeType   string `json:"scope_type"`
	ScopeID     string `json:"scope_id"`
	SpentMicros int64  `json:"spent_micros"`
	LimitMicros int64  `json:"limit_micros"`
	State       string `json:"state"`
	Mode        string `json:"mode"`
}

// DigestBudgetHold is one run held for its budget.
type DigestBudgetHold struct {
	TaskID    string    `json:"task_id"`
	RunClass  string    `json:"run_class"`
	ScopeType string    `json:"scope_type"`
	ScopeID   string    `json:"scope_id"`
	HeldAt    time.Time `json:"held_at"`
}

// computeBudget reads the budgets' current periods and the holds in force, for
// the scope. It also returns how many active budgets the scope has.
func (s *DigestService) computeBudget(ctx context.Context, workspaceID pgtype.UUID, now time.Time, sc digestScope) (DigestBudget, int, error) {
	out := DigestBudget{Scopes: []DigestBudgetScope{}, Holds: []DigestBudgetHold{}}
	states, err := s.Queries.ListWorkspaceBudgetStates(ctx, db.ListWorkspaceBudgetStatesParams{WorkspaceID: workspaceID, Now: pgtype.Timestamptz{Time: now, Valid: true}})
	if err != nil {
		return out, 0, fmt.Errorf("digest budgets: %w", err)
	}
	count := 0
	for _, b := range states {
		if b.Status != BudgetStatusActive || !sc.keeps(b.ScopeType, b.ScopeID) {
			continue
		}
		count++
		state := budgetState(b.SpentMicros, b.AmountMicros, b.ExtendedMicros, b.SoftPercent)
		if state == budgetStateOK {
			continue
		}
		out.Scopes = append(out.Scopes, DigestBudgetScope{
			BudgetID: util.UUIDToString(b.ID), ScopeType: b.ScopeType, ScopeID: util.UUIDToString(b.ScopeID),
			SpentMicros: b.SpentMicros, LimitMicros: b.AmountMicros + b.ExtendedMicros, State: state, Mode: b.Mode,
		})
	}
	holds, err := s.Queries.ListBudgetHoldsInForce(ctx, db.ListBudgetHoldsInForceParams{WorkspaceID: workspaceID, RowLimit: 200})
	if err != nil {
		return out, 0, fmt.Errorf("digest budget holds: %w", err)
	}
	inScope := holds[:0:0]
	for _, h := range holds {
		if sc.keeps(h.ScopeType, h.ScopeID) {
			inScope = append(inScope, h)
		}
	}
	out.HoldsInForce = len(inScope)
	for i, h := range inScope {
		if i == DigestBudgetHoldsShown {
			break
		}
		out.Holds = append(out.Holds, DigestBudgetHold{
			TaskID: util.UUIDToString(h.TaskID), RunClass: h.RunClass, ScopeType: h.ScopeType,
			ScopeID: util.UUIDToString(h.ScopeID), HeldAt: h.CreatedAt.Time,
		})
	}
	return out, count, nil
}
