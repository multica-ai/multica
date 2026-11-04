package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// BudgetService writes budgets and their levers (spec 4.2): create, raise or
// pause, extend a period, and release the holds once. Every change writes its
// governance entry in the same transaction.
type BudgetService struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

// DefaultCompanyBudgetMicros is the company budget a starter company gets when
// the boss names no amount: $500 a month, in warn mode, so nothing holds
// silently. The boss sets the real figure from the Budgets tab.
const DefaultCompanyBudgetMicros int64 = 500_000_000

// Budget write errors. The handler maps them to status codes.
var (
	ErrBudgetInvalid  = errors.New("invalid budget")
	ErrBudgetExists   = errors.New("an active budget already covers this scope and period")
	ErrBudgetNotFound = errors.New("budget not found")
)

// BudgetInput is a new budget.
type BudgetInput struct {
	ScopeType    string
	ScopeID      pgtype.UUID
	Period       string
	AmountMicros int64
	SoftPercent  int32
	Mode         string
	Classes      []string
}

// BudgetPatch changes a budget. A nil field stays as it is.
type BudgetPatch struct {
	AmountMicros *int64
	SoftPercent  *int32
	Mode         *string
	Classes      *[]string
	Status       *string
}

// ValidateBudgetInput checks a new budget's fields. Holds apply only to routine
// and delegated runs (spec 4.2): a human run is never held, so it is not a class
// a budget can list.
func ValidateBudgetInput(in BudgetInput) error {
	if !ValidBudgetScope(in.ScopeType) || !ValidBudgetPeriod(in.Period) || !ValidBudgetMode(in.Mode) {
		return ErrBudgetInvalid
	}
	if (in.ScopeType == BudgetScopeCompany) == in.ScopeID.Valid {
		return ErrBudgetInvalid
	}
	return validateBudgetFields(in.AmountMicros, in.SoftPercent, in.Classes)
}

func validateBudgetFields(amount int64, soft int32, classes []string) error {
	if amount < 0 || soft < 1 || soft > 100 {
		return ErrBudgetInvalid
	}
	for _, c := range classes {
		if c != string(RunClassRoutine) && c != string(RunClassDelegated) {
			return ErrBudgetInvalid
		}
	}
	return nil
}

// List returns the workspace's budgets with their current period's counters.
func (s *BudgetService) List(ctx context.Context, ws pgtype.UUID, now time.Time) ([]db.ListWorkspaceBudgetStatesRow, error) {
	return s.Queries.ListWorkspaceBudgetStates(ctx, db.ListWorkspaceBudgetStatesParams{
		WorkspaceID: ws, Now: pgtype.Timestamptz{Time: now, Valid: true},
	})
}

// Create adds a budget for a scope and period. The workspace's existing spend
// for the scope in the current period is counted into it at once.
func (s *BudgetService) Create(ctx context.Context, ws pgtype.UUID, actor GovernanceActor, in BudgetInput) (db.Budget, error) {
	var out db.Budget
	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error
		out, err = createBudgetIn(ctx, q, ws, actor, in)
		return err
	})
	return out, err
}

// createBudgetIn validates and writes a new budget inside q's transaction. The
// company import writes budgets through it, in the transaction that writes the
// rest of the company (spec 4.9).
func createBudgetIn(ctx context.Context, q *db.Queries, ws pgtype.UUID, actor GovernanceActor, in BudgetInput) (db.Budget, error) {
	if err := ValidateBudgetInput(in); err != nil {
		return db.Budget{}, err
	}
	if in.Classes == nil {
		in.Classes = []string{string(RunClassRoutine), string(RunClassDelegated)}
	}
	if err := validateBudgetFields(in.AmountMicros, in.SoftPercent, in.Classes); err != nil {
		return db.Budget{}, err
	}
	now := time.Now()
	start, _, err := BudgetPeriodBounds(in.Period, now)
	if err != nil {
		return db.Budget{}, err
	}
	if err := syncWorkspaceUsageSince(ctx, q, ws, start); err != nil {
		return db.Budget{}, err
	}
	out, err := q.InsertBudget(ctx, db.InsertBudgetParams{
		ID:           dbid.NewV7(),
		WorkspaceID:  ws,
		ScopeType:    in.ScopeType,
		ScopeID:      in.ScopeID,
		Period:       in.Period,
		AmountMicros: in.AmountMicros,
		SoftPercent:  in.SoftPercent,
		Mode:         in.Mode,
		Classes:      in.Classes,
		Status:       BudgetStatusActive,
		CreatedBy:    actor.ID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return db.Budget{}, ErrBudgetExists
		}
		return db.Budget{}, fmt.Errorf("insert budget: %w", err)
	}
	if err := seedBudgetPeriod(ctx, q, ws, out, now); err != nil {
		return db.Budget{}, err
	}
	if err := recordBudgetSet(ctx, q, ws, actor, out, "created"); err != nil {
		return db.Budget{}, err
	}
	return out, nil
}

// Update changes a budget. Raising its amount releases its holds once, so the
// runs it held are decided again against the new limit. Pausing it releases
// them too: a paused budget holds nothing.
func (s *BudgetService) Update(ctx context.Context, ws pgtype.UUID, actor GovernanceActor, id pgtype.UUID, p BudgetPatch) (db.Budget, error) {
	var out db.Budget
	err := s.inTx(ctx, func(q *db.Queries) error {
		var err error
		out, err = updateBudgetIn(ctx, q, ws, actor, id, p)
		return err
	})
	return out, err
}

// updateBudgetIn applies a patch inside q's transaction. The company import uses
// it to pause a budget it has just created.
func updateBudgetIn(ctx context.Context, q *db.Queries, ws pgtype.UUID, actor GovernanceActor, id pgtype.UUID, p BudgetPatch) (db.Budget, error) {
	prev, err := q.GetBudgetForWorkspace(ctx, db.GetBudgetForWorkspaceParams{ID: id, WorkspaceID: ws})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Budget{}, ErrBudgetNotFound
	}
	if err != nil {
		return db.Budget{}, fmt.Errorf("load budget: %w", err)
	}
	amount, soft, mode, classes, status := prev.AmountMicros, prev.SoftPercent, prev.Mode, prev.Classes, prev.Status
	if p.AmountMicros != nil {
		amount = *p.AmountMicros
	}
	if p.SoftPercent != nil {
		soft = *p.SoftPercent
	}
	if p.Mode != nil {
		mode = *p.Mode
	}
	if p.Classes != nil {
		classes = *p.Classes
	}
	if p.Status != nil {
		status = *p.Status
	}
	if !ValidBudgetMode(mode) || (status != BudgetStatusActive && status != BudgetStatusPaused) {
		return db.Budget{}, ErrBudgetInvalid
	}
	if err := validateBudgetFields(amount, soft, classes); err != nil {
		return db.Budget{}, err
	}
	out, err := q.UpdateBudget(ctx, db.UpdateBudgetParams{
		ID: id, WorkspaceID: ws, AmountMicros: amount, SoftPercent: soft,
		Mode: mode, Classes: classes, Status: status,
	})
	if err != nil {
		return db.Budget{}, fmt.Errorf("update budget: %w", err)
	}
	if amount > prev.AmountMicros || status == BudgetStatusPaused && prev.Status != BudgetStatusPaused {
		if _, err := releaseBudgetHolds(ctx, q, ws, id, "budget_changed", actor); err != nil {
			return db.Budget{}, err
		}
	}
	start, _, err := BudgetPeriodBounds(prev.Period, time.Now())
	if err != nil {
		return db.Budget{}, err
	}
	if err := refreshBudgetPeriodState(ctx, q, id, out.AmountMicros, out.SoftPercent, start); err != nil {
		return db.Budget{}, err
	}
	if err := recordBudgetSet(ctx, q, ws, actor, out, "changed"); err != nil {
		return db.Budget{}, err
	}
	return out, nil
}

// Extend raises the current period's limit by amount, once. The period's holds
// are released so their runs are decided again against the larger limit.
func (s *BudgetService) Extend(ctx context.Context, ws pgtype.UUID, actor GovernanceActor, id pgtype.UUID, amount int64) (db.Budget, error) {
	if amount <= 0 {
		return db.Budget{}, ErrBudgetInvalid
	}
	var out db.Budget
	err := s.inTx(ctx, func(q *db.Queries) error {
		b, err := q.GetBudgetForWorkspace(ctx, db.GetBudgetForWorkspaceParams{ID: id, WorkspaceID: ws})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBudgetNotFound
		}
		if err != nil {
			return fmt.Errorf("load budget: %w", err)
		}
		out = b
		start, end, err := BudgetPeriodBounds(b.Period, time.Now())
		if err != nil {
			return err
		}
		since := pgtype.Timestamptz{Time: start, Valid: true}
		if err := q.EnsureBudgetPeriod(ctx, db.EnsureBudgetPeriodParams{
			BudgetID: id, WorkspaceID: ws, PeriodStart: since,
			PeriodEnd: pgtype.Timestamptz{Time: end, Valid: true},
		}); err != nil {
			return fmt.Errorf("ensure budget period: %w", err)
		}
		if err := q.AddBudgetPeriodExtension(ctx, db.AddBudgetPeriodExtensionParams{
			BudgetID: id, PeriodStart: since, AmountMicros: amount,
		}); err != nil {
			return fmt.Errorf("extend budget period: %w", err)
		}
		if err := refreshBudgetPeriodState(ctx, q, id, b.AmountMicros, b.SoftPercent, start); err != nil {
			return err
		}
		if _, err := releaseBudgetHolds(ctx, q, ws, id, "extended", actor); err != nil {
			return err
		}
		return NewGovernanceLog().Record(ctx, q, GovernanceEvent{
			WorkspaceID: ws,
			Kind:        GovernanceKindBudgetExtended,
			ActorType:   actor.Type,
			ActorID:     actorIDOf(actor),
			SubjectType: "budget",
			SubjectID:   id,
			Summary:     fmt.Sprintf("Extended a %s budget by %s for this period.", b.Period, formatMicrosUSD(amount)),
			Details:     map[string]any{"amount_micros": amount, "period_start": start.Format(time.RFC3339), "period_end": end.Format(time.RFC3339)},
		})
	})
	return out, err
}

// ReleaseHolds releases the budget's holds once: the held runs may start now
// even though the budget is at its limit. It returns how many were released.
func (s *BudgetService) ReleaseHolds(ctx context.Context, ws pgtype.UUID, actor GovernanceActor, id pgtype.UUID) (int64, error) {
	var n int64
	err := s.inTx(ctx, func(q *db.Queries) error {
		if _, err := q.GetBudgetForWorkspace(ctx, db.GetBudgetForWorkspaceParams{ID: id, WorkspaceID: ws}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrBudgetNotFound
			}
			return fmt.Errorf("load budget: %w", err)
		}
		var err error
		n, err = releaseBudgetHolds(ctx, q, ws, id, "overridden", actor)
		return err
	})
	return n, err
}

// Periods returns a budget's periods, newest first.
func (s *BudgetService) Periods(ctx context.Context, ws, id pgtype.UUID, limit int32) ([]db.BudgetPeriod, error) {
	if _, err := s.Queries.GetBudgetForWorkspace(ctx, db.GetBudgetForWorkspaceParams{ID: id, WorkspaceID: ws}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBudgetNotFound
		}
		return nil, fmt.Errorf("load budget: %w", err)
	}
	return s.Queries.ListBudgetPeriods(ctx, db.ListBudgetPeriodsParams{BudgetID: id, RowLimit: limit})
}

// HoldsInForce returns the holds not yet released, newest first.
func (s *BudgetService) HoldsInForce(ctx context.Context, ws pgtype.UUID, limit int32) ([]db.ListBudgetHoldsInForceRow, error) {
	return s.Queries.ListBudgetHoldsInForce(ctx, db.ListBudgetHoldsInForceParams{WorkspaceID: ws, RowLimit: limit})
}

// pulseHoldCountCap bounds the holds the pulse counts in one read.
const pulseHoldCountCap int32 = 1000

// BudgetsAboveSoftLine counts the active budgets whose current period has reached
// its soft line or its limit (spec 4.8). The pulse reads it.
func (s *BudgetService) BudgetsAboveSoftLine(ctx context.Context, ws pgtype.UUID) (int, error) {
	rows, err := s.List(ctx, ws, time.Now())
	if err != nil {
		return 0, fmt.Errorf("list budgets: %w", err)
	}
	n := 0
	for _, b := range rows {
		if b.Status != BudgetStatusActive {
			continue
		}
		if budgetState(b.SpentMicros, b.AmountMicros, b.ExtendedMicros, b.SoftPercent) != budgetStateOK {
			n++
		}
	}
	return n, nil
}

// BudgetHoldsInForce counts the holds not yet released, up to pulseHoldCountCap.
func (s *BudgetService) BudgetHoldsInForce(ctx context.Context, ws pgtype.UUID) (int, error) {
	rows, err := s.HoldsInForce(ctx, ws, pulseHoldCountCap)
	if err != nil {
		return 0, fmt.Errorf("list holds: %w", err)
	}
	return len(rows), nil
}

// EnsureCompanyDefault creates the company budget at the stated default in warn
// mode when the workspace has no active company budget. It returns true when it
// created one. A workspace that already has one is left alone.
func (s *BudgetService) EnsureCompanyDefault(ctx context.Context, ws pgtype.UUID, actor GovernanceActor) (bool, error) {
	budgets, err := s.Queries.ListWorkspaceBudgetStates(ctx, db.ListWorkspaceBudgetStatesParams{
		WorkspaceID: ws, Now: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		return false, fmt.Errorf("list budgets: %w", err)
	}
	for _, b := range budgets {
		if b.ScopeType == BudgetScopeCompany && b.Status == BudgetStatusActive {
			return false, nil
		}
	}
	_, err = s.Create(ctx, ws, actor, BudgetInput{
		ScopeType: BudgetScopeCompany, Period: BudgetPeriodMonth, AmountMicros: DefaultCompanyBudgetMicros,
		SoftPercent: 80, Mode: BudgetModeWarn,
	})
	if errors.Is(err, ErrBudgetExists) {
		return false, nil
	}
	return err == nil, err
}

func (s *BudgetService) inTx(ctx context.Context, fn func(q *db.Queries) error) error {
	if s.TxStarter == nil {
		return fn(s.Queries)
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(s.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// recordBudgetSet writes the budget_set governance entry for a create or change.
func recordBudgetSet(ctx context.Context, q *db.Queries, ws pgtype.UUID, actor GovernanceActor, b db.Budget, verb string) error {
	return NewGovernanceLog().Record(ctx, q, GovernanceEvent{
		WorkspaceID: ws,
		Kind:        GovernanceKindBudgetSet,
		ActorType:   actor.Type,
		ActorID:     actorIDOf(actor),
		SubjectType: "budget",
		SubjectID:   b.ID,
		Summary: fmt.Sprintf("A %s budget of %s per %s was %s for %s (%s mode).",
			b.ScopeType, formatMicrosUSD(b.AmountMicros), b.Period, verb, b.ScopeType, b.Mode),
		Details: map[string]any{
			"scope_type":    b.ScopeType,
			"scope_id":      util.UUIDToString(b.ScopeID),
			"period":        b.Period,
			"amount_micros": b.AmountMicros,
			"soft_percent":  b.SoftPercent,
			"mode":          b.Mode,
			"classes":       b.Classes,
			"status":        b.Status,
		},
	})
}

func formatMicrosUSD(micros int64) string {
	return fmt.Sprintf("$%.2f", float64(micros)/1_000_000)
}

// DefaultInitiativeBudgetMicros is the budget an initiative gets when its gate is
// approved and its budget_request states no amount: $100 a month in warn mode. The
// amount is read when the document is written (budget_request_amount.go), so a
// request that states one sets the budget and the default is only the fallback.
const DefaultInitiativeBudgetMicros int64 = 100_000_000

// approveInitiativeBudget creates the initiative's budget inside the gate's
// decision transaction. An initiative that already has an active budget for the
// month keeps it.
func approveInitiativeBudget(ctx context.Context, q *db.Queries, ws pgtype.UUID, ini db.Initiative, stage string, actor GovernanceActor) error {
	amount := DefaultInitiativeBudgetMicros
	stated, err := q.GetInitiativeBudgetRequestAmount(ctx, db.GetInitiativeBudgetRequestAmountParams{
		WorkspaceID: ws, InitiativeID: ini.ID, StageKey: stage,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return fmt.Errorf("read the budget request amount: %w", err)
	case stated.Valid:
		amount = stated.Int64
	}
	_, err = (&BudgetService{Queries: q}).Create(ctx, ws, actor, BudgetInput{
		ScopeType: BudgetScopeInitiative, ScopeID: ini.ID, Period: BudgetPeriodMonth,
		AmountMicros: amount, SoftPercent: 80, Mode: BudgetModeWarn,
	})
	if errors.Is(err, ErrBudgetExists) {
		return nil
	}
	return err
}

// ensureBudgets is the starter company's budget step: the company budget at the
// stated default when the workspace has none. Rerunning reuses it.
func (r *companyRun) ensureBudgets(ctx context.Context) (string, string, error) {
	created, err := (&BudgetService{Queries: r.svc.Queries, TxStarter: r.svc.TxStarter}).EnsureCompanyDefault(
		ctx, r.in.WorkspaceID, GovernanceActor{Type: GovernanceActorSystem})
	if err != nil {
		return "", "", err
	}
	if created {
		return CompanyStepCreated, "created the company budget in warn mode", nil
	}
	return CompanyStepReused, "the company budget exists", nil
}
