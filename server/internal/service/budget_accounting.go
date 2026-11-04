package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Budget accounting (spec 4.2). A task's cost is the sum of its usage rows: the
// provider's own price (cost_usd_ticks, 1e-10 USD) when it reported one, else
// the shared rate-table estimate. The cost is booked once per task into
// budget_period, through agent_task_queue.budget_accounted_micros: each pass
// adds only the difference, so a task read twice, or by two runs, counts once.
// A task's spend belongs to the period its creation falls in.

const (
	BudgetPeriodWeek    = "week"
	BudgetPeriodMonth   = "month"
	BudgetPeriodQuarter = "quarter"

	BudgetScopeCompany    = "company"
	BudgetScopeFloor      = "floor"
	BudgetScopeSquad      = "squad"
	BudgetScopeInitiative = "initiative"

	BudgetModeWarn = "warn"
	BudgetModeHold = "hold"

	BudgetStatusActive = "active"
	BudgetStatusPaused = "paused"

	budgetStateOK   = "ok"
	budgetStateSoft = "soft"
	budgetStateHard = "hard"
)

// microsPerTick converts 1e-10 USD ticks to millionths of a dollar.
const ticksPerMicro = 10_000

// ValidBudgetPeriod, ValidBudgetScope and ValidBudgetMode are the stored values.
func ValidBudgetPeriod(p string) bool {
	return p == BudgetPeriodWeek || p == BudgetPeriodMonth || p == BudgetPeriodQuarter
}

func ValidBudgetScope(s string) bool {
	switch s {
	case BudgetScopeCompany, BudgetScopeFloor, BudgetScopeSquad, BudgetScopeInitiative:
		return true
	}
	return false
}

func ValidBudgetMode(m string) bool { return m == BudgetModeWarn || m == BudgetModeHold }

// BudgetPeriodBounds returns the window of kind that contains at, in UTC: a
// week starts on Monday, a month on the 1st, a quarter on the first of Jan, Apr,
// Jul or Oct. The end is exclusive.
func BudgetPeriodBounds(kind string, at time.Time) (time.Time, time.Time, error) {
	at = at.UTC()
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC)
	switch kind {
	case BudgetPeriodWeek:
		back := (int(at.Weekday()) + 6) % 7
		start := day.AddDate(0, 0, -back)
		return start, start.AddDate(0, 0, 7), nil
	case BudgetPeriodMonth:
		start := time.Date(at.Year(), at.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0), nil
	case BudgetPeriodQuarter:
		first := time.Month(((int(at.Month())-1)/3)*3 + 1)
		start := time.Date(at.Year(), first, 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 3, 0), nil
	}
	return time.Time{}, time.Time{}, fmt.Errorf("unknown budget period %q", kind)
}

// UsageRowCostMicros prices one usage row in millionths of a dollar. A provider
// price wins; a row with no price of its own is estimated from its tokens, and
// a model with no rate row costs nothing here (it is counted as unpriced by the
// metrics, not guessed).
func UsageRowCostMicros(provider, model string, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, costUSDTicks int64) int64 {
	if costUSDTicks > 0 {
		return (costUSDTicks + ticksPerMicro/2) / ticksPerMicro
	}
	usd, ok := metrics.EstimateCostUSD(provider, model, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens)
	if !ok {
		return 0
	}
	return int64(math.Round(usd * 1_000_000))
}

// TaskCostMicros is the cost of a task: the sum of its usage rows.
func TaskCostMicros(ctx context.Context, q *db.Queries, taskID pgtype.UUID) (int64, error) {
	rows, err := q.ListTaskUsageRowsForBudget(ctx, taskID)
	if err != nil {
		return 0, fmt.Errorf("list task usage: %w", err)
	}
	var total int64
	for _, r := range rows {
		total += UsageRowCostMicros(r.Provider, r.Model, r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.CostUsdTicks)
	}
	return total, nil
}

// budgetState is the state of spend against the limit, with the soft line at
// soft_percent of the limit in force (amount plus extensions). reserved is not
// part of it: the stored state describes spend, the gate adds the reservation.
func budgetState(spent, amount, extended int64, softPercent int32) string {
	limit := amount + extended
	switch {
	case spent >= limit:
		return budgetStateHard
	case spent*100 >= limit*int64(softPercent):
		return budgetStateSoft
	}
	return budgetStateOK
}

// budgetCovers reports whether a budget of scopeType and scopeID covers a task
// or run attributed to sc.
func budgetCovers(scopeType string, scopeID pgtype.UUID, sc budgetScope) bool {
	switch scopeType {
	case BudgetScopeCompany:
		return true
	case BudgetScopeFloor:
		return sc.floorID == scopeID
	case BudgetScopeSquad:
		return sc.squadID == scopeID
	case BudgetScopeInitiative:
		return sc.initiativeID == scopeID
	}
	return false
}

// budgetScope is where a task or run is attributed: its squad, the floor of
// that squad and the initiative it works on. An unset field matches nothing.
type budgetScope struct {
	squadID      pgtype.UUID
	floorID      pgtype.UUID
	initiativeID pgtype.UUID
}

// BudgetAccounting books task costs into budget periods. It holds no state, so
// the scheduler job, the API and the finishing task share one writer.
type BudgetAccounting struct {
	Queries   *db.Queries
	TxStarter TxStarter
}

// AccountTask brings one task's cost into the periods of every budget that
// covers it. It is idempotent: a task whose cost has not changed adds nothing.
func (a *BudgetAccounting) AccountTask(ctx context.Context, taskID pgtype.UUID) error {
	return a.inTx(ctx, func(q *db.Queries) error {
		return accountTaskInTx(ctx, q, taskID)
	})
}

// accountTaskInTx is AccountTask inside the caller's transaction.
func accountTaskInTx(ctx context.Context, q *db.Queries, taskID pgtype.UUID) error {
	scope, err := q.GetBudgetTaskScope(ctx, taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load task budget scope: %w", err)
	}
	// The workspace lock comes before the task's row lock, the same order a
	// budget creation takes, so the two cannot deadlock.
	if err := q.LockBudgetWorkspace(ctx, util.UUIDToString(scope.WorkspaceID)); err != nil {
		return fmt.Errorf("lock budget workspace: %w", err)
	}
	locked, err := q.LockBudgetTaskForAccounting(ctx, taskID)
	if err != nil {
		return fmt.Errorf("lock task for accounting: %w", err)
	}
	cost, err := TaskCostMicros(ctx, q, taskID)
	if err != nil {
		return err
	}
	delta := cost - locked.BudgetAccountedMicros
	if delta == 0 {
		return nil
	}
	if err := q.SetBudgetTaskAccounted(ctx, db.SetBudgetTaskAccountedParams{
		TaskID: taskID, BudgetAccountedMicros: cost,
	}); err != nil {
		return fmt.Errorf("set task accounted cost: %w", err)
	}
	var runs int32
	if locked.BudgetAccountedMicros == 0 && cost > 0 {
		runs = 1
	}
	sc := budgetScope{squadID: locked.SquadID, floorID: locked.FloorID, initiativeID: locked.InitiativeID}
	budgets, err := q.ListActiveBudgetsByWorkspaces(ctx, []pgtype.UUID{locked.WorkspaceID})
	if err != nil {
		return fmt.Errorf("list budgets: %w", err)
	}
	for _, b := range budgets {
		if !budgetCovers(b.ScopeType, b.ScopeID, sc) {
			continue
		}
		start, end, err := BudgetPeriodBounds(b.Period, locked.CreatedAt.Time)
		if err != nil {
			return err
		}
		if err := q.AddBudgetPeriodSpend(ctx, db.AddBudgetPeriodSpendParams{
			BudgetID:    b.ID,
			WorkspaceID: b.WorkspaceID,
			PeriodStart: pgtype.Timestamptz{Time: start, Valid: true},
			PeriodEnd:   pgtype.Timestamptz{Time: end, Valid: true},
			SpentMicros: delta,
			Runs:        runs,
		}); err != nil {
			return fmt.Errorf("add budget spend: %w", err)
		}
		if err := refreshBudgetPeriodState(ctx, q, b.ID, b.AmountMicros, b.SoftPercent, start); err != nil {
			return err
		}
	}
	return nil
}

// refreshBudgetPeriodState stores the state of one period from its counters.
func refreshBudgetPeriodState(ctx context.Context, q *db.Queries, budgetID pgtype.UUID, amount int64, softPercent int32, start time.Time) error {
	row, err := q.GetBudgetPeriod(ctx, db.GetBudgetPeriodParams{
		BudgetID: budgetID, PeriodStart: pgtype.Timestamptz{Time: start, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// No spend and no extension yet: the period has nothing to state.
		return nil
	}
	if err != nil {
		return fmt.Errorf("load budget period: %w", err)
	}
	return q.SetBudgetPeriodState(ctx, db.SetBudgetPeriodStateParams{
		BudgetID:    budgetID,
		PeriodStart: row.PeriodStart,
		State:       budgetState(row.SpentMicros, amount, row.ExtendedMicros, softPercent),
	})
}

// syncWorkspaceUsageSince brings every task of the workspace created since the
// time up to date with its usage. A new budget calls it before it is inserted,
// so the deltas land on the budgets that existed before it.
func syncWorkspaceUsageSince(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, since time.Time) error {
	if err := q.LockBudgetWorkspace(ctx, util.UUIDToString(workspaceID)); err != nil {
		return fmt.Errorf("lock budget workspace: %w", err)
	}
	ids, err := q.ListTaskIDsWithUsageSince(ctx, db.ListTaskIDsWithUsageSinceParams{
		WorkspaceID: workspaceID, Since: pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("list tasks with usage: %w", err)
	}
	for _, id := range ids {
		if err := accountTaskInTx(ctx, q, id); err != nil {
			return err
		}
	}
	return nil
}

// seedBudgetPeriod sets a new budget's current period from the accounted costs
// of the tasks in its scope. Call it after the budget is inserted and after
// syncWorkspaceUsageSince, in the same transaction, so each cost is counted once.
func seedBudgetPeriod(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, b db.Budget, now time.Time) error {
	start, end, err := BudgetPeriodBounds(b.Period, now)
	if err != nil {
		return err
	}
	since := pgtype.Timestamptz{Time: start, Valid: true}
	rows, err := q.ListBudgetTaskCostsSince(ctx, db.ListBudgetTaskCostsSinceParams{WorkspaceID: workspaceID, Since: since})
	if err != nil {
		return fmt.Errorf("list task costs: %w", err)
	}
	var spent int64
	var runs int32
	for _, r := range rows {
		if !budgetCovers(b.ScopeType, b.ScopeID, budgetScope{squadID: r.SquadID, floorID: r.FloorID, initiativeID: r.InitiativeID}) {
			continue
		}
		spent += r.BudgetAccountedMicros
		if r.BudgetAccountedMicros > 0 {
			runs++
		}
	}
	if err := q.EnsureBudgetPeriod(ctx, db.EnsureBudgetPeriodParams{
		BudgetID: b.ID, WorkspaceID: workspaceID,
		PeriodStart: since, PeriodEnd: pgtype.Timestamptz{Time: end, Valid: true},
	}); err != nil {
		return fmt.Errorf("ensure budget period: %w", err)
	}
	if spent != 0 || runs != 0 {
		if err := q.AddBudgetPeriodSpend(ctx, db.AddBudgetPeriodSpendParams{
			BudgetID: b.ID, WorkspaceID: workspaceID, PeriodStart: since,
			PeriodEnd: pgtype.Timestamptz{Time: end, Valid: true}, SpentMicros: spent, Runs: runs,
		}); err != nil {
			return fmt.Errorf("seed budget spend: %w", err)
		}
	}
	return refreshBudgetPeriodState(ctx, q, b.ID, b.AmountMicros, b.SoftPercent, start)
}

// budgetUsageCursorLookback is how far back a fresh accounting pass reads. It
// is generous because passes are idempotent: rereading a change costs nothing.
const budgetUsageCursorLookback = 24 * time.Hour

// BudgetUsageCursor is where the accounting job's next pass starts. Passes
// overlap by a second so a change that shares a timestamp with the last one
// read is not skipped when the batch is cut.
type BudgetUsageCursor struct {
	since time.Time
}

// NewBudgetUsageCursor starts a cursor a day behind now, so a process that
// starts after a gap still accounts for the usage it missed.
func NewBudgetUsageCursor(now time.Time) BudgetUsageCursor {
	return BudgetUsageCursor{since: now.Add(-budgetUsageCursorLookback)}
}

// RunUsagePass accounts every task whose usage changed since the cursor, up to
// limit tasks, and returns the advanced cursor. A failed task stops the pass
// before it, so the next pass retries it.
func (a *BudgetAccounting) RunUsagePass(ctx context.Context, cur BudgetUsageCursor, limit int32) (BudgetUsageCursor, int, error) {
	rows, err := a.Queries.ListTaskUsageTasksChangedSince(ctx, db.ListTaskUsageTasksChangedSinceParams{
		Since:    pgtype.Timestamptz{Time: cur.since, Valid: true},
		RowLimit: limit,
	})
	if err != nil {
		return cur, 0, fmt.Errorf("list changed task usage: %w", err)
	}
	next := cur
	for _, r := range rows {
		if err := a.AccountTask(ctx, r.TaskID); err != nil {
			return next, 0, err
		}
		if r.ChangedAt.Valid {
			next = BudgetUsageCursor{since: r.ChangedAt.Time.Add(-time.Second)}
		}
	}
	return next, len(rows), nil
}

// inTx runs fn in one transaction, or directly when no tx starter is wired.
func (a *BudgetAccounting) inTx(ctx context.Context, fn func(q *db.Queries) error) error {
	if a.TxStarter == nil {
		return fn(a.Queries)
	}
	tx, err := a.TxStarter.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := fn(a.Queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReleaseEndedTaskHolds releases the holds of tasks that ended without a run
// (cancelled, or finished some other way). It returns how many were released.
func (a *BudgetAccounting) ReleaseEndedTaskHolds(ctx context.Context) (int64, error) {
	return a.Queries.ReleaseBudgetHoldsForEndedTasks(ctx)
}

// ReleasePeriodRolledHolds releases the holds of a budget that were placed
// before its current period began: a new period starts with a fresh limit, so
// the old holds no longer describe the company.
func (a *BudgetAccounting) ReleasePeriodRolledHolds(ctx context.Context, ws pgtype.UUID, now time.Time) (int64, error) {
	budgetIDs, err := a.Queries.ListBudgetIDsWithHoldsInForce(ctx, ws)
	if err != nil {
		return 0, fmt.Errorf("list budgets with holds: %w", err)
	}
	if len(budgetIDs) == 0 {
		return 0, nil
	}
	budgets, err := a.Queries.ListActiveBudgetsByWorkspaces(ctx, []pgtype.UUID{ws})
	if err != nil {
		return 0, fmt.Errorf("list budgets: %w", err)
	}
	var released int64
	for _, b := range budgets {
		if !containsUUID(budgetIDs, b.ID) {
			continue
		}
		start, _, err := BudgetPeriodBounds(b.Period, now)
		if err != nil {
			return released, err
		}
		n, err := a.Queries.ReleaseBudgetHoldsCreatedBefore(ctx, db.ReleaseBudgetHoldsCreatedBeforeParams{
			BudgetID:      b.ID,
			Before:        pgtype.Timestamptz{Time: start, Valid: true},
			ReleaseReason: pgtype.Text{String: "period_rolled", Valid: true},
		})
		if err != nil {
			return released, fmt.Errorf("release rolled holds: %w", err)
		}
		released += n
	}
	return released, nil
}

func containsUUID(list []pgtype.UUID, id pgtype.UUID) bool {
	for _, x := range list {
		if x == id {
			return true
		}
	}
	return false
}
