package service

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// The budget gate (spec 4.2). It admits a queued run unless a hold-mode budget
// that covers the run's scope and lists its class is at its limit. The limit
// test is spent + reserved >= amount + extended, where reserved is the number
// of the scope's tasks in flight times the scope's trailing 90th-percentile
// task cost, so runs admitted together cannot overshoot far.
//
// Admission reads the period counters (budget_period) and the in-flight tasks;
// it never reads task_usage. The 90th percentile is recomputed at most every
// budgetReservationTTL per workspace, not per admission.

const (
	// budgetReservationWindow is how far back the reservation's percentile looks.
	budgetReservationWindow = 30 * 24 * time.Hour
	// budgetReservationTTL is how long a workspace's cost sample is reused.
	budgetReservationTTL = 10 * time.Minute

	// VerdictReasonBudgetHold is the reason a run is held for its budget. It is
	// the dispatch reason code of the same name.
	VerdictReasonBudgetHold = "budget_hold"

	// budgetEventHold is the realtime event a new hold publishes.
	budgetEventHold = "budget:hold"
)

// budgetAdmission is the gate's answer for one run.
type budgetAdmission struct {
	Admit    bool
	BudgetID pgtype.UUID
}

// BudgetGate decides budget admission. It keeps only the reservation's sample,
// so any service can hold one; the TaskService and the autopilot each use one.
type BudgetGate struct {
	Bus *events.Bus
	now func() time.Time

	mu       sync.Mutex
	finished map[string]finishedSample
}

type finishedSample struct {
	at    time.Time
	costs []finishedCost
}

type finishedCost struct {
	micros int64
	scope  budgetScope
}

// NewBudgetGate returns a gate that publishes holds on bus (nil publishes nothing).
func NewBudgetGate(bus *events.Bus) *BudgetGate {
	return &BudgetGate{Bus: bus, now: time.Now, finished: map[string]finishedSample{}}
}

// Admit decides whether a run of class may start. scopeOf resolves the run's
// attribution and is called only when a hold-mode budget could apply, so the
// common case reads just the budget states. excludeTask is the task being
// admitted, which is already in flight inside its own claim transaction and
// must not count against itself; the zero UUID excludes nothing.
func (g *BudgetGate) Admit(ctx context.Context, q *db.Queries, ws, excludeTask pgtype.UUID, class RunClass, scopeOf func(context.Context) (budgetScope, error), now time.Time) (budgetAdmission, error) {
	if class != RunClassRoutine && class != RunClassDelegated {
		return budgetAdmission{Admit: true}, nil
	}
	states, err := q.ListWorkspaceBudgetStates(ctx, db.ListWorkspaceBudgetStatesParams{
		WorkspaceID: ws, Now: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return budgetAdmission{}, fmt.Errorf("load budget states: %w", err)
	}
	var holding []db.ListWorkspaceBudgetStatesRow
	for _, b := range states {
		if b.Status == BudgetStatusActive && b.Mode == BudgetModeHold && slices.Contains(b.Classes, string(class)) {
			holding = append(holding, b)
		}
	}
	if len(holding) == 0 {
		return budgetAdmission{Admit: true}, nil
	}
	sc, err := scopeOf(ctx)
	if err != nil {
		return budgetAdmission{}, fmt.Errorf("resolve run scope: %w", err)
	}
	var covering []db.ListWorkspaceBudgetStatesRow
	for _, b := range holding {
		if budgetCovers(b.ScopeType, b.ScopeID, sc) {
			covering = append(covering, b)
		}
	}
	if len(covering) == 0 {
		return budgetAdmission{Admit: true}, nil
	}
	inflight, err := q.ListBudgetInflightTasks(ctx, db.ListBudgetInflightTasksParams{
		WorkspaceID: ws, ExcludeTaskID: excludeTask,
	})
	if err != nil {
		return budgetAdmission{}, fmt.Errorf("list in-flight tasks: %w", err)
	}
	finished, err := g.finishedCosts(ctx, q, ws, now)
	if err != nil {
		return budgetAdmission{}, err
	}
	for _, b := range covering {
		var inScope int64
		for _, r := range inflight {
			if budgetCovers(b.ScopeType, b.ScopeID, budgetScope{squadID: r.SquadID, floorID: r.FloorID, initiativeID: r.InitiativeID}) {
				inScope++
			}
		}
		reserved := inScope * p90Cost(b.ScopeType, b.ScopeID, finished)
		if b.SpentMicros+reserved < b.AmountMicros+b.ExtendedMicros {
			continue
		}
		// At the limit. A hold the boss released once lets this run through.
		if excludeTask.Valid {
			overridden, err := q.HasOverriddenBudgetHold(ctx, excludeTask)
			if err != nil {
				return budgetAdmission{}, fmt.Errorf("check budget override: %w", err)
			}
			if overridden {
				continue
			}
		}
		return budgetAdmission{Admit: false, BudgetID: b.ID}, nil
	}
	return budgetAdmission{Admit: true}, nil
}

// CacheRefreshedAt is when the gate last read the workspace's completed task
// costs for its reservation. It is the zero time when the gate has not read them.
// Health reports its age (spec 4.9).
func (g *BudgetGate) CacheRefreshedAt(_ context.Context, ws pgtype.UUID) (time.Time, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.finished[util.UUIDToString(ws)].at, nil
}

// finishedCosts returns the workspace's completed task costs for the
// reservation, recomputed when the cached sample is older than the TTL.
func (g *BudgetGate) finishedCosts(ctx context.Context, q *db.Queries, ws pgtype.UUID, now time.Time) ([]finishedCost, error) {
	key := util.UUIDToString(ws)
	g.mu.Lock()
	defer g.mu.Unlock()
	if s, ok := g.finished[key]; ok && now.Sub(s.at) < budgetReservationTTL {
		return s.costs, nil
	}
	rows, err := q.ListBudgetFinishedTaskCosts(ctx, db.ListBudgetFinishedTaskCostsParams{
		WorkspaceID: ws, Since: pgtype.Timestamptz{Time: now.Add(-budgetReservationWindow), Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("load finished task costs: %w", err)
	}
	costs := make([]finishedCost, 0, len(rows))
	for _, r := range rows {
		if r.BudgetAccountedMicros <= 0 {
			continue
		}
		costs = append(costs, finishedCost{
			micros: r.BudgetAccountedMicros,
			scope:  budgetScope{squadID: r.SquadID, floorID: r.FloorID, initiativeID: r.InitiativeID},
		})
	}
	g.finished[key] = finishedSample{at: now, costs: costs}
	return costs, nil
}

// p90Cost is the nearest-rank 90th percentile of the completed costs that a
// budget's scope covers. It is 0 when the scope has no completed run yet.
func p90Cost(scopeType string, scopeID pgtype.UUID, costs []finishedCost) int64 {
	var values []int64
	for _, c := range costs {
		if budgetCovers(scopeType, scopeID, c.scope) {
			values = append(values, c.micros)
		}
	}
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	rank := (9*len(values) + 9) / 10 // ceil(0.9 * n)
	return values[rank-1]
}

// RecordHold writes the hold of a refused task: one row per task, a governance
// entry and a realtime event. A task already held adds nothing. It runs outside
// the claim transaction, which has rolled back, so the task stays queued.
func (g *BudgetGate) RecordHold(ctx context.Context, q *db.Queries, ws pgtype.UUID, task db.AgentTaskQueue, class RunClass, budgetID pgtype.UUID) error {
	n, err := q.InsertBudgetHoldIfNone(ctx, db.InsertBudgetHoldIfNoneParams{
		ID:          dbid.NewV7(),
		WorkspaceID: ws,
		BudgetID:    budgetID,
		TaskID:      task.ID,
		AgentID:     task.AgentID,
		SquadID:     task.SquadID,
		RunClass:    string(class),
	})
	if err != nil {
		return fmt.Errorf("record budget hold: %w", err)
	}
	if n == 0 {
		return nil
	}
	if err := NewGovernanceLog().Record(ctx, q, GovernanceEvent{
		WorkspaceID: ws,
		Kind:        GovernanceKindBudgetHold,
		ActorType:   GovernanceActorSystem,
		SubjectType: "budget",
		SubjectID:   budgetID,
		Summary:     fmt.Sprintf("Held a %s run: its budget is at its limit.", class),
		Details: map[string]any{
			"task_id":   util.UUIDToString(task.ID),
			"run_class": string(class),
			"agent_id":  util.UUIDToString(task.AgentID),
		},
	}); err != nil {
		return err
	}
	if g.Bus != nil {
		g.Bus.Publish(events.Event{
			Type:        budgetEventHold,
			WorkspaceID: util.UUIDToString(ws),
			ActorType:   GovernanceActorSystem,
			TaskID:      util.UUIDToString(task.ID),
			Payload: map[string]any{
				"task_id":   util.UUIDToString(task.ID),
				"budget_id": util.UUIDToString(budgetID),
				"run_class": string(class),
			},
		})
	}
	return nil
}

// maxClaimGateRefusals bounds how many refused candidates one claim call
// walks past. Each refusal costs a rolled-back transaction, so the bound keeps
// a poll cheap when a long run of queued work is held; the rest is reached on a
// later poll, once the holds written here make those rows cheap to skip.
const maxClaimGateRefusals = 16

// claimRefusal is a claim the gate refused. Its transaction rolls back, so the
// task stays queued and the claim moves on to the next candidate.
type claimRefusal struct {
	WorkspaceID pgtype.UUID
	Task        db.AgentTaskQueue
	Class       RunClass
	Reason      string
	BudgetID    pgtype.UUID
}

// recordClaimRefusal writes what a refusal leaves behind, outside the rolled
// back transaction. Only a budget refusal holds the task; a paused or quiet
// company leaves it queued without a row, so the company's own state says why.
func (s *TaskService) recordClaimRefusal(ctx context.Context, refusal *claimRefusal) {
	if refusal.Reason != VerdictReasonBudgetHold {
		return
	}
	gate := s.Budget
	if gate == nil {
		gate = NewBudgetGate(s.Bus)
	}
	if err := gate.RecordHold(ctx, s.Queries, refusal.WorkspaceID, refusal.Task, refusal.Class, refusal.BudgetID); err != nil {
		slog.Warn("record budget hold failed; the task stays queued",
			"task_id", util.UUIDToString(refusal.Task.ID), "error", err)
	}
}

func (e *claimRefusal) Error() string {
	return fmt.Sprintf("claim of task %s refused: %s", util.UUIDToString(e.Task.ID), e.Reason)
}

// admitClaimedTask is the claim gate, run inside the claim transaction for the
// task ClaimAgentTask just selected. A refusal is returned as a *claimRefusal;
// the caller records what it leaves behind after the transaction rolls back.
func (s *TaskService) admitClaimedTask(ctx context.Context, q *db.Queries, ws pgtype.UUID, task db.AgentTaskQueue) error {
	refusal, err := s.claimRefusalFor(ctx, q, ws, task)
	if err != nil {
		return err
	}
	if refusal != nil {
		return refusal
	}
	// The task runs now, so any hold it had is over.
	_, err = q.ReleaseBudgetHoldForTask(ctx, db.ReleaseBudgetHoldForTaskParams{
		TaskID: task.ID, ReleaseReason: pgtype.Text{String: "admitted", Valid: true},
	})
	if err != nil {
		return fmt.Errorf("release admitted hold: %w", err)
	}
	return nil
}

// claimRefusalFor is the claim gate's verdict on a queued task at this moment,
// or nil when the gate admits it. It only reads state, so the runs list asks
// the same question the claim asks. Human and exempt runs are always admitted
// without reading anything. A routine or delegated run is refused by the company
// controls (paused, or quiet hours) or by a hold-mode budget at its limit.
func (s *TaskService) claimRefusalFor(ctx context.Context, q *db.Queries, ws pgtype.UUID, task db.AgentTaskQueue) (*claimRefusal, error) {
	class, err := RunClassOfTask(ctx, q, task)
	if err != nil {
		return nil, err
	}
	if class == RunClassHuman || class == RunClassExempt {
		return nil, nil
	}
	now := time.Now()
	verdict, err := CompanyVerdict(ctx, q, ws, class, now)
	if err != nil {
		return nil, err
	}
	if !verdict.Admit {
		return &claimRefusal{WorkspaceID: ws, Task: task, Class: class, Reason: verdict.Reason}, nil
	}
	gate := s.Budget
	if gate == nil {
		gate = NewBudgetGate(s.Bus)
	}
	adm, err := gate.Admit(ctx, q, ws, task.ID, class, budgetScopeOfTask(q, task.ID), now)
	if err != nil {
		return nil, err
	}
	if !adm.Admit {
		return &claimRefusal{WorkspaceID: ws, Task: task, Class: class, Reason: VerdictReasonBudgetHold, BudgetID: adm.BudgetID}, nil
	}
	return nil, nil
}

// budgetScopeOfTask resolves a queued task's attribution when the gate needs it.
func budgetScopeOfTask(q *db.Queries, taskID pgtype.UUID) func(context.Context) (budgetScope, error) {
	return func(ctx context.Context) (budgetScope, error) {
		row, err := q.GetBudgetTaskScope(ctx, taskID)
		if err != nil {
			return budgetScope{}, err
		}
		return budgetScope{squadID: row.SquadID, floorID: row.FloorID, initiativeID: row.InitiativeID}, nil
	}
}

// releaseBudgetHolds releases every hold of a budget with reason and records one
// governance entry for the batch. It returns the number released.
func releaseBudgetHolds(ctx context.Context, q *db.Queries, ws, budgetID pgtype.UUID, reason string, actor GovernanceActor) (int64, error) {
	n, err := q.ReleaseBudgetHoldsForBudget(ctx, db.ReleaseBudgetHoldsForBudgetParams{
		BudgetID: budgetID, ReleaseReason: pgtype.Text{String: reason, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("release budget holds: %w", err)
	}
	if n == 0 {
		return 0, nil
	}
	if err := NewGovernanceLog().Record(ctx, q, GovernanceEvent{
		WorkspaceID: ws,
		Kind:        GovernanceKindBudgetRelease,
		ActorType:   actor.Type,
		ActorID:     actorIDOf(actor),
		SubjectType: "budget",
		SubjectID:   budgetID,
		Summary:     fmt.Sprintf("Released %d held run(s): %s.", n, budgetReleaseText(reason)),
		Details:     map[string]any{"released": n, "reason": reason},
	}); err != nil {
		return n, err
	}
	return n, nil
}

func budgetReleaseText(reason string) string {
	switch reason {
	case "extended":
		return "the budget was extended"
	case "budget_changed":
		return "the budget changed"
	case "overridden":
		return "the boss released them once"
	case "period_rolled":
		return "the period rolled"
	case "cancelled":
		return "the task ended"
	case "admitted":
		return "the budget no longer holds them"
	}
	return reason
}
