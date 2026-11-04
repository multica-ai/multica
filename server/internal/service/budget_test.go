package service

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Pure rules: period windows, the cost of a usage row, scope coverage, the
// reservation's percentile and the state bands.

func TestBudgetPeriodBoundsCoverEachKind(t *testing.T) {
	wed := time.Date(2026, time.November, 4, 15, 30, 0, 0, time.UTC) // a Wednesday
	cases := []struct {
		kind       string
		start, end time.Time
	}{
		{BudgetPeriodWeek, time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)},
		{BudgetPeriodMonth, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)},
		{BudgetPeriodQuarter, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		start, end, err := BudgetPeriodBounds(c.kind, wed)
		if err != nil || !start.Equal(c.start) || !end.Equal(c.end) {
			t.Errorf("%s: got [%v, %v) err %v; want [%v, %v)", c.kind, start, end, err, c.start, c.end)
		}
	}
	// A Sunday belongs to the week that started the Monday before it.
	sun := time.Date(2026, 11, 8, 23, 59, 0, 0, time.UTC)
	if start, _, _ := BudgetPeriodBounds(BudgetPeriodWeek, sun); !start.Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("a Sunday's week starts %v, want the Monday before", start)
	}
	if _, _, err := BudgetPeriodBounds("day", wed); err == nil {
		t.Error("an unknown period kind must be refused")
	}
}

func TestUsageRowCostPrefersTheProviderPriceAndEstimatesTheRest(t *testing.T) {
	// 1 USD in 1e-10 ticks is 1e10 ticks, which is 1,000,000 micros.
	if got := UsageRowCostMicros("anthropic", "claude-sonnet-4.6", 5, 5, 0, 0, 10_000_000_000); got != 1_000_000 {
		t.Errorf("provider price 1 USD = %d micros, want 1000000", got)
	}
	// Half a micro rounds up: 5000 ticks is one micro.
	if got := UsageRowCostMicros("anthropic", "claude-sonnet-4.6", 0, 0, 0, 0, 5_000); got != 1 {
		t.Errorf("5000 ticks = %d micros, want 1", got)
	}
	// No provider price: the shared estimate, the same figure RecordLLMUsage prices with.
	usd, ok := metrics.EstimateCostUSD("anthropic", "claude-sonnet-4.6", 1_000_000, 100_000, 0, 0)
	if !ok || usd <= 0 {
		t.Fatalf("claude-sonnet-4.6 must have a rate row (usd %v, ok %v)", usd, ok)
	}
	if got, want := UsageRowCostMicros("anthropic", "claude-sonnet-4.6", 1_000_000, 100_000, 0, 0, 0), int64(usd*1_000_000+0.5); got != want {
		t.Errorf("estimated row = %d micros, want %d", got, want)
	}
	// A model with no rate row costs nothing here: it is unpriced, not guessed.
	if got := UsageRowCostMicros("nosuchprovider", "no-such-model", 1_000_000, 0, 0, 0, 0); got != 0 {
		t.Errorf("unpriced model = %d micros, want 0", got)
	}
}

func TestBudgetCoversFollowsTheScopeOfTheRun(t *testing.T) {
	squad, floor, ini := uuidOf(1), uuidOf(2), uuidOf(3)
	sc := budgetScope{squadID: squad, floorID: floor, initiativeID: ini}
	cases := []struct {
		scopeType string
		scopeID   pgtype.UUID
		want      bool
	}{
		{BudgetScopeCompany, pgtype.UUID{}, true},
		{BudgetScopeFloor, floor, true},
		{BudgetScopeFloor, uuidOf(9), false},
		{BudgetScopeSquad, squad, true},
		{BudgetScopeSquad, floor, false},
		{BudgetScopeInitiative, ini, true},
		{BudgetScopeInitiative, squad, false},
	}
	for _, c := range cases {
		if got := budgetCovers(c.scopeType, c.scopeID, sc); got != c.want {
			t.Errorf("%s %v covers = %v, want %v", c.scopeType, util.UUIDToString(c.scopeID), got, c.want)
		}
	}
	// Nothing is attributed to a floor when the task has no squad.
	if budgetCovers(BudgetScopeFloor, floor, budgetScope{squadID: squad}) {
		t.Error("a run with no floor must not be covered by a floor budget")
	}
}

func TestReservationPercentileIsNearestRankOverTheScope(t *testing.T) {
	squad := uuidOf(1)
	var costs []finishedCost
	for i := 1; i <= 10; i++ {
		costs = append(costs, finishedCost{micros: int64(i * 10), scope: budgetScope{squadID: squad}})
	}
	costs = append(costs, finishedCost{micros: 1000, scope: budgetScope{squadID: uuidOf(7)}})
	if got := p90Cost(BudgetScopeSquad, squad, costs); got != 90 {
		t.Errorf("p90 of 10..100 = %d, want 90 (nearest rank)", got)
	}
	// Eleven values, 10..100 and 1000: nearest rank ceil(0.9*11) = 10th = 100.
	if got := p90Cost(BudgetScopeCompany, pgtype.UUID{}, costs); got != 100 {
		t.Errorf("company p90 over every squad: got %d, want 100", got)
	}
	if got := p90Cost(BudgetScopeSquad, uuidOf(8), costs); got != 0 {
		t.Errorf("a scope with no completed run reserves 0, got %d", got)
	}
}

func TestBudgetStateBandsAtSoftAndHard(t *testing.T) {
	// limit = amount + extended; soft at 80% of the limit in force.
	cases := []struct {
		spent, amount, extended int64
		want                    string
	}{
		{0, 100, 0, budgetStateOK},
		{79, 100, 0, budgetStateOK},
		{80, 100, 0, budgetStateSoft},
		{99, 100, 0, budgetStateSoft},
		{100, 100, 0, budgetStateHard},
		{120, 100, 30, budgetStateSoft},
		{130, 100, 30, budgetStateHard},
	}
	for _, c := range cases {
		if got := budgetState(c.spent, c.amount, c.extended, 80); got != c.want {
			t.Errorf("spent %d of %d+%d = %s, want %s", c.spent, c.amount, c.extended, got, c.want)
		}
	}
}

func TestValidateBudgetInputRefusesHumanRunsAndBadScopes(t *testing.T) {
	ok := BudgetInput{ScopeType: BudgetScopeCompany, Period: BudgetPeriodMonth, AmountMicros: 10, SoftPercent: 80, Mode: BudgetModeHold, Classes: []string{"routine"}}
	if err := ValidateBudgetInput(ok); err != nil {
		t.Fatalf("a company budget is valid: %v", err)
	}
	bad := map[string]BudgetInput{}
	scoped := ok
	scoped.ScopeType, scoped.ScopeID = BudgetScopeSquad, pgtype.UUID{}
	bad["squad without id"] = scoped
	withID := ok
	withID.ScopeID = uuidOf(1)
	bad["company with id"] = withID
	human := ok
	human.Classes = []string{"human"}
	bad["human class"] = human
	neg := ok
	neg.AmountMicros = -1
	bad["negative amount"] = neg
	for name, in := range bad {
		if err := ValidateBudgetInput(in); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

// newUUIDText is a fresh id for a column no test reads back.
// Task queues a task for agentID on the fixture's runtime: a queued task must
// name the runtime it can run on.
func (f budgetFixture) Task(t *testing.T, agentID string, over ...testutil.Cols) string {
	t.Helper()
	cols := testutil.Cols{"runtime_id": f.rt}
	for _, c := range over {
		for k, v := range c {
			cols[k] = v
		}
	}
	return f.Fixture.Task(t, agentID, cols)
}

func newUUIDText() string { return util.UUIDToString(dbid.NewV7()) }

// uuidOf makes a distinct, valid UUID for tests that need several ids.
func uuidOf(n byte) pgtype.UUID {
	var u pgtype.UUID
	u.Bytes[15] = n
	u.Valid = true
	return u
}

// Database-backed tests: attribution, accounting, the gate and the claim.

var budgetSeq atomic.Int64

type budgetFixture struct {
	*testutil.Fixture
	ws      pgtype.UUID
	wsText  string
	owner   string
	rt      string
	agent   string
	q       *db.Queries
	actor   GovernanceActor
	budgets *BudgetService
	acct    *BudgetAccounting
	tasks   *TaskService
	gate    *BudgetGate
}

func newBudgetFixture(t *testing.T) budgetFixture {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	n := budgetSeq.Add(1)
	base := testutil.New(pool, "", "")
	owner := base.User(t, "budget owner", fmt.Sprintf("budget-owner-%d-%d@multica.test", time.Now().UnixNano(), n))
	ws := base.Workspace(t, "budget ws", fmt.Sprintf("budget-ws-%d-%d", time.Now().UnixNano(), n))
	base.Member(t, ws, owner, "owner")
	fx := testutil.New(pool, ws, owner)
	rt := fx.Runtime(t, "budget runtime")
	agent := fx.Agent(t, "budget agent", rt, testutil.Cols{"max_concurrent_tasks": 10})
	q := db.New(pool)
	tasks := NewTaskService(q, pool, nil, events.New())
	wsUUID := util.MustParseUUID(ws)
	return budgetFixture{
		Fixture: fx, ws: wsUUID, wsText: ws, owner: owner, rt: rt, agent: agent, q: q,
		actor:   GovernanceActor{Type: GovernanceActorMember, ID: util.MustParseUUID(owner)},
		budgets: &BudgetService{Queries: q, TxStarter: pool},
		acct:    &BudgetAccounting{Queries: q, TxStarter: pool},
		tasks:   tasks,
		gate:    NewBudgetGate(events.New()),
	}
}

// usage records a usage row of ticks for a task, or updates it.
func (f budgetFixture) usage(t *testing.T, taskID string, ticks int64) {
	t.Helper()
	var n int
	f.QueryRow(t, `SELECT count(*) FROM task_usage WHERE task_id = $1`, taskID).Scan(&n)
	if n == 0 {
		f.Insert(t, "task_usage", testutil.Cols{
			"task_id": taskID, "provider": "anthropic", "model": "claude-sonnet-4.6",
			"cost_usd_ticks": ticks, "updated_at": testutil.Raw("now()"),
		})
		return
	}
	f.Exec(t, `UPDATE task_usage SET cost_usd_ticks = $1, updated_at = now() WHERE task_id = $2`, ticks, taskID)
}

// spendOf reads a budget's current period spend and run count.
func (f budgetFixture) spendOf(t *testing.T, budgetID string) (int64, int32) {
	t.Helper()
	var spent int64
	var runs int32
	f.QueryRow(t, `SELECT coalesce(sum(spent_micros),0), coalesce(sum(runs),0)::int FROM budget_period
		WHERE budget_id = $1 AND period_end > now() AND period_start <= now()`, budgetID).Scan(&spent, &runs)
	return spent, runs
}

func (f budgetFixture) company(t *testing.T, mode string, amount int64) db.Budget {
	t.Helper()
	b, err := f.budgets.Create(context.Background(), f.ws, f.actor, BudgetInput{
		ScopeType: BudgetScopeCompany, Period: BudgetPeriodMonth, AmountMicros: amount,
		SoftPercent: 80, Mode: mode,
	})
	if err != nil {
		t.Fatalf("create company budget: %v", err)
	}
	return b
}

func (f budgetFixture) queued(t *testing.T, cols testutil.Cols) string {
	t.Helper()
	return f.Task(t, f.agent, cols)
}

func (f budgetFixture) taskStatus(t *testing.T, taskID string) string {
	t.Helper()
	var s string
	f.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&s)
	return s
}

func (f budgetFixture) holdsFor(t *testing.T, taskID string) int {
	t.Helper()
	return f.Count(t, `SELECT count(*) FROM budget_hold WHERE task_id = $1`, taskID)
}

func TestBudgetAttributionFollowsSquadFloorAndInitiative(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	floor := newUUIDText()
	squad := f.Squad(t, "attr squad", f.agent, testutil.Cols{"floor_id": floor})
	homeSquad := f.Squad(t, "home squad", f.agent, testutil.Cols{"floor_id": floor})
	other := f.Squad(t, "other squad", f.agent)
	homed := f.Agent(t, "homed agent", f.rt, testutil.Cols{"home_squad_id": homeSquad})

	// A task with its own squad, and one that falls back to its agent's home.
	taskOwn := f.queued(t, testutil.Cols{"squad_id": squad})
	taskHome := f.Task(t, homed)
	taskNone := f.Task(t, f.agent)

	scopeOf := func(id string) budgetScope {
		row, err := f.q.GetBudgetTaskScope(ctx, util.MustParseUUID(id))
		if err != nil {
			t.Fatalf("scope of %s: %v", id, err)
		}
		return budgetScope{squadID: row.SquadID, floorID: row.FloorID, initiativeID: row.InitiativeID}
	}
	if s := scopeOf(taskOwn); util.UUIDToString(s.squadID) != squad || util.UUIDToString(s.floorID) != floor {
		t.Errorf("task with a squad: squad %v floor %v, want %s and %s", util.UUIDToString(s.squadID), util.UUIDToString(s.floorID), squad, floor)
	}
	if s := scopeOf(taskHome); util.UUIDToString(s.squadID) != homeSquad || util.UUIDToString(s.floorID) != floor {
		t.Errorf("task of an agent with a home squad: squad %v, want the home %s", util.UUIDToString(s.squadID), homeSquad)
	}
	if s := scopeOf(taskNone); s.squadID.Valid || s.floorID.Valid {
		t.Errorf("task of an agent with no squad is attributed to a squad %v / floor %v", util.UUIDToString(s.squadID), util.UUIDToString(s.floorID))
	}
	_ = other

	// Initiative: a task on the backing issue, and a task on an issue of the build project.
	issue := f.Issue(t, "initiative backing issue")
	project := f.Project(t, "build project")
	projIssue := f.Issue(t, "build project issue", testutil.Cols{"project_id": project})
	ini := f.Insert(t, "initiative", testutil.Cols{
		"workspace_id": f.wsText, "pipeline_id": newUUIDText(), "seq": 1, "title": "venture",
		"stage_key": "build", "issue_id": issue, "build_project_id": project,
	})
	taskBacking := f.Task(t, f.agent, testutil.Cols{"issue_id": issue})
	taskBuild := f.Task(t, f.agent, testutil.Cols{"issue_id": projIssue})
	taskUnrelated := f.Task(t, f.agent, testutil.Cols{"issue_id": f.Issue(t, "unrelated")})
	for _, id := range []string{taskBacking, taskBuild} {
		if s := scopeOf(id); util.UUIDToString(s.initiativeID) != ini {
			t.Errorf("task %s attributed to initiative %v, want %s", id, util.UUIDToString(s.initiativeID), ini)
		}
	}
	if s := scopeOf(taskUnrelated); s.initiativeID.Valid {
		t.Error("a task on an unrelated issue is attributed to an initiative")
	}

	// Coverage follows the attribution: a floor budget covers the squad's floor, a squad budget only its squad.
	floorB, err := f.budgets.Create(ctx, f.ws, f.actor, BudgetInput{ScopeType: BudgetScopeFloor, ScopeID: util.MustParseUUID(floor), Period: BudgetPeriodMonth, AmountMicros: 1, SoftPercent: 80, Mode: BudgetModeWarn})
	if err != nil {
		t.Fatalf("floor budget: %v", err)
	}
	if !budgetCovers(floorB.ScopeType, floorB.ScopeID, scopeOf(taskOwn)) || budgetCovers(floorB.ScopeType, floorB.ScopeID, scopeOf(taskNone)) {
		t.Error("a floor budget covers the squad's tasks on that floor and no others")
	}
}

func TestAccountingBooksEachTaskOnceAndTheDelta(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	b := f.company(t, BudgetModeWarn, 10_000_000)
	task := f.queued(t, nil)
	f.usage(t, task, 10_000_000_000) // 1 USD

	if err := f.acct.AccountTask(ctx, util.MustParseUUID(task)); err != nil {
		t.Fatal(err)
	}
	if spent, runs := f.spendOf(t, util.UUIDToString(b.ID)); spent != 1_000_000 || runs != 1 {
		t.Fatalf("first pass: spent %d runs %d, want 1000000 and 1", spent, runs)
	}
	// Reading the same task again adds nothing.
	if err := f.acct.AccountTask(ctx, util.MustParseUUID(task)); err != nil {
		t.Fatal(err)
	}
	if spent, runs := f.spendOf(t, util.UUIDToString(b.ID)); spent != 1_000_000 || runs != 1 {
		t.Fatalf("second pass: spent %d runs %d, want no change", spent, runs)
	}
	// The usage grows: only the difference is added, and the run is still one run.
	f.usage(t, task, 25_000_000_000)
	if err := f.acct.AccountTask(ctx, util.MustParseUUID(task)); err != nil {
		t.Fatal(err)
	}
	if spent, runs := f.spendOf(t, util.UUIDToString(b.ID)); spent != 2_500_000 || runs != 1 {
		t.Fatalf("after growth: spent %d runs %d, want 2500000 and 1", spent, runs)
	}
}

func TestCreatingABudgetCountsSpendAlreadyMade(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	task := f.queued(t, nil)
	f.usage(t, task, 30_000_000_000) // 3 USD, before any budget exists
	b := f.company(t, BudgetModeHold, 100_000_000)
	if spent, runs := f.spendOf(t, util.UUIDToString(b.ID)); spent != 3_000_000 || runs != 1 {
		t.Fatalf("a new budget starts from the spend already made: spent %d runs %d, want 3000000 and 1", spent, runs)
	}
	// A later change is booked once, to the new budget as well.
	f.usage(t, task, 40_000_000_000)
	if err := f.acct.AccountTask(ctx, util.MustParseUUID(task)); err != nil {
		t.Fatal(err)
	}
	if spent, _ := f.spendOf(t, util.UUIDToString(b.ID)); spent != 4_000_000 {
		t.Fatalf("after a later change: spent %d, want 4000000", spent)
	}
}

func TestGateHoldsRoutineAtTheLimitAndNeverHumanOrExempt(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	now := time.Now()
	b := f.company(t, BudgetModeHold, 1_000_000)
	task := f.queued(t, nil)
	f.usage(t, task, 10_000_000_000) // 1 USD: at the limit
	if err := f.acct.AccountTask(ctx, util.MustParseUUID(task)); err != nil {
		t.Fatal(err)
	}
	company := func(context.Context) (budgetScope, error) { return budgetScope{}, nil }

	adm, err := f.gate.Admit(ctx, f.q, f.ws, pgtype.UUID{}, RunClassRoutine, company, now)
	if err != nil || adm.Admit || util.UUIDToString(adm.BudgetID) != util.UUIDToString(b.ID) {
		t.Fatalf("routine at the limit: admit %v budget %v err %v; want held by the company budget", adm.Admit, util.UUIDToString(adm.BudgetID), err)
	}
	if adm, _ := f.gate.Admit(ctx, f.q, f.ws, pgtype.UUID{}, RunClassDelegated, company, now); adm.Admit {
		t.Error("delegated is in the default classes and is held at the limit")
	}
	for _, class := range []RunClass{RunClassHuman, RunClassExempt} {
		if adm, _ := f.gate.Admit(ctx, f.q, f.ws, pgtype.UUID{}, class, company, now); !adm.Admit {
			t.Errorf("%s run is held by a budget; it must never be", class)
		}
	}

	// A warn budget at the limit holds nothing.
	f.Exec(t, `UPDATE budget SET mode = 'warn' WHERE id = $1`, util.UUIDToString(b.ID))
	if adm, _ := f.gate.Admit(ctx, f.q, f.ws, pgtype.UUID{}, RunClassRoutine, company, now); !adm.Admit {
		t.Error("a warn-mode budget holds nothing")
	}
	// A budget whose classes exclude routine does not hold routine runs.
	f.Exec(t, `UPDATE budget SET mode = 'hold', classes = '{delegated}' WHERE id = $1`, util.UUIDToString(b.ID))
	if adm, _ := f.gate.Admit(ctx, f.q, f.ws, pgtype.UUID{}, RunClassRoutine, company, now); !adm.Admit {
		t.Error("a budget that does not list routine holds no routine run")
	}
}

func TestGateReservesTheTrailingP90ForInFlightRuns(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	now := time.Now()
	f.company(t, BudgetModeHold, 100_000_000) // 100 USD

	// Three completed runs at 60 USD each set the trailing p90 at 60 USD.
	for i := 0; i < 3; i++ {
		id := f.queued(t, testutil.Cols{"status": "completed", "completed_at": testutil.Raw("now()"), "budget_accounted_micros": int64(60_000_000)})
		_ = id
	}
	company := func(context.Context) (budgetScope, error) { return budgetScope{}, nil }
	inFlight := func(n int) {
		f.Exec(t, `DELETE FROM agent_task_queue WHERE agent_id = $1 AND status IN ('dispatched','running')`, f.agent)
		for i := 0; i < n; i++ {
			f.queued(t, testutil.Cols{"status": "dispatched", "dispatched_at": testutil.Raw("now()")})
		}
	}
	// One run in flight reserves 60 USD: 0 + 60 < 100, so a second run is admitted.
	inFlight(1)
	if adm, _ := f.gate.Admit(ctx, f.q, f.ws, pgtype.UUID{}, RunClassRoutine, company, now); !adm.Admit {
		t.Fatal("one run in flight (60 USD reserved of 100) must still admit")
	}
	// Two in flight reserve 120 USD, past the 100 USD limit: the gate holds before they overshoot.
	inFlight(2)
	if adm, _ := f.gate.Admit(ctx, f.q, f.ws, pgtype.UUID{}, RunClassRoutine, company, now); adm.Admit {
		t.Fatal("two runs in flight (120 USD reserved of 100) must hold the next run")
	}
}

func TestClaimHoldsOnceLeavesTheTaskQueuedAndExtendReleasesIt(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	b := f.company(t, BudgetModeHold, 1_000_000)
	spent := f.queued(t, nil)
	f.usage(t, spent, 10_000_000_000)
	if err := f.acct.AccountTask(ctx, util.MustParseUUID(spent)); err != nil {
		t.Fatal(err)
	}
	// The spent task is already claimed-by-accounting; a new routine task waits behind the limit.
	f.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, spent)
	waiting := f.queued(t, nil)

	for i := 0; i < 2; i++ {
		got, err := f.tasks.ClaimTask(ctx, util.MustParseUUID(f.agent))
		if err != nil || got != nil {
			t.Fatalf("claim %d at the limit: task %v err %v; want no task", i, got, err)
		}
	}
	if st := f.taskStatus(t, waiting); st != "queued" {
		t.Fatalf("held task is %q, want queued", st)
	}
	if n := f.holdsFor(t, waiting); n != 1 {
		t.Fatalf("held task has %d hold rows after two refusals, want exactly 1", n)
	}

	// The boss extends the period: the hold is released and the task is claimable.
	if _, err := f.budgets.Extend(ctx, f.ws, f.actor, b.ID, 5_000_000); err != nil {
		t.Fatal(err)
	}
	if n := f.Count(t, `SELECT count(*) FROM budget_hold WHERE task_id = $1 AND released_at IS NULL`, waiting); n != 0 {
		t.Fatalf("extension left %d holds in force", n)
	}
	got, err := f.tasks.ClaimTask(ctx, util.MustParseUUID(f.agent))
	if err != nil || got == nil || util.UUIDToString(got.ID) != waiting {
		t.Fatalf("after the extension the waiting task must be claimed: task %v err %v", got, err)
	}
	var reason string
	f.QueryRow(t, `SELECT release_reason FROM budget_hold WHERE task_id = $1`, waiting).Scan(&reason)
	if reason != "extended" {
		t.Errorf("hold released with reason %q, want extended", reason)
	}
	if n := f.Count(t, `SELECT count(*) FROM governance_event WHERE workspace_id = $1 AND kind = $2`, f.wsText, GovernanceKindBudgetHold); n != 1 {
		t.Errorf("governance recorded %d budget_hold entries, want 1", n)
	}
}

func TestPausedCompanyLeavesRoutineQueuedWithoutAHoldAndAdmitsHuman(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	f.Exec(t, `UPDATE workspace SET company_state = 'paused' WHERE id = $1`, f.wsText)
	routine := f.queued(t, nil)
	human := f.queued(t, testutil.Cols{"originator_source": "direct_human"})

	got, err := f.tasks.ClaimTask(ctx, util.MustParseUUID(f.agent))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || util.UUIDToString(got.ID) != human {
		t.Fatalf("paused company: claimed %v, want the human task %s (routine is refused)", got, human)
	}
	if st := f.taskStatus(t, routine); st != "queued" {
		t.Errorf("paused routine task is %q, want queued", st)
	}
	if n := f.holdsFor(t, routine); n != 0 {
		t.Errorf("a paused company writes no budget hold, found %d", n)
	}
}

func TestOverriddenHoldRunsItsTaskOnce(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	b := f.company(t, BudgetModeHold, 1_000_000)
	spent := f.queued(t, nil)
	f.usage(t, spent, 10_000_000_000)
	if err := f.acct.AccountTask(ctx, util.MustParseUUID(spent)); err != nil {
		t.Fatal(err)
	}
	f.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, spent)
	waiting := f.queued(t, nil)
	if got, _ := f.tasks.ClaimTask(ctx, util.MustParseUUID(f.agent)); got != nil {
		t.Fatal("the budget at its limit must hold the waiting task")
	}
	// Release holds once: the boss lets the held run through, and the budget stays at its limit.
	n, err := f.budgets.ReleaseHolds(ctx, f.ws, f.actor, b.ID)
	if err != nil || n != 1 {
		t.Fatalf("release holds: %d released, err %v; want 1", n, err)
	}
	if got, err := f.tasks.ClaimTask(ctx, util.MustParseUUID(f.agent)); err != nil || got == nil || util.UUIDToString(got.ID) != waiting {
		t.Fatalf("the released task must run once: task %v err %v", got, err)
	}
	// A second task of the same budget is held again: the release was once.
	again := f.queued(t, nil)
	if got, _ := f.tasks.ClaimTask(ctx, util.MustParseUUID(f.agent)); got != nil {
		t.Fatalf("a later task (%s) must be held again after a single release", again)
	}
}

// The autopilot dispatch gate (spec 4.2): a scheduled routine run is skipped
// with reason budget_hold while a budget holds routine runs; a run a person
// starts by hand is admitted.
func TestAutopilotScheduleIsSkippedWhileABudgetHoldsRoutineRuns(t *testing.T) {
	f := newCompanyFixture(t)
	ctx := context.Background()
	apFx := principalFixture{Fixture: f.Fixture, q: f.svc.Queries, svc: &AutopilotService{Queries: f.svc.Queries, TxStarter: f.svc.TxStarter, Bus: events.New(), TaskSvc: f.svc.Tasks}}
	agent := apFx.privateAgentOwnedBy(t, util.UUIDToString(f.owner), "budget gate")
	bs := &BudgetService{Queries: f.svc.Queries, TxStarter: f.svc.TxStarter}
	acct := &BudgetAccounting{Queries: f.svc.Queries, TxStarter: f.svc.TxStarter}

	if _, err := bs.Create(ctx, f.ws, GovernanceActor{Type: GovernanceActorMember, ID: f.owner}, BudgetInput{
		ScopeType: BudgetScopeCompany, Period: BudgetPeriodMonth, AmountMicros: 1_000_000, SoftPercent: 80, Mode: BudgetModeHold,
	}); err != nil {
		t.Fatal(err)
	}
	// Spend exactly the limit through a task's usage, then account it.
	var runtimeID string
	f.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agent).Scan(&runtimeID)
	task := f.Task(t, agent, testutil.Cols{"runtime_id": runtimeID})
	f.Insert(t, "task_usage", testutil.Cols{
		"task_id": task, "provider": "anthropic", "model": "claude-sonnet-4.6",
		"cost_usd_ticks": int64(10_000_000_000), "updated_at": testutil.Raw("now()"),
	})
	if err := acct.AccountTask(ctx, util.MustParseUUID(task)); err != nil {
		t.Fatal(err)
	}

	apID, trigID := apFx.autopilotWithTrigger(t, agent, util.UUIDToString(f.owner), util.UUIDToString(f.owner))
	ap, err := f.svc.Queries.GetAutopilot(ctx, util.MustParseUUID(apID))
	if err != nil {
		t.Fatal(err)
	}
	run, err := apFx.svc.DispatchAutopilot(ctx, ap, util.MustParseUUID(trigID), "schedule", nil)
	if err != nil || run == nil {
		t.Fatalf("schedule dispatch: run %v, err %v", run, err)
	}
	if run.Status != "skipped" || run.ReasonCode.String != "budget_hold" {
		t.Fatalf("scheduled run at the budget limit: status %q, reason %q; want skipped budget_hold", run.Status, run.ReasonCode.String)
	}
	byHand, _, err := apFx.svc.DispatchAutopilotManual(ctx, ap, util.MustParseUUID(trigID), nil, f.owner)
	if err != nil || byHand == nil || byHand.Status == "skipped" {
		t.Fatalf("a run a person starts by hand is held by a budget: run %v, err %v", byHand, err)
	}
}

// TestBudgetGateAdmissionSpikeP99 is the spike of spec 11.6: the claim gate's
// admission cost at p99 with 50 budgets and 10,000 queued tasks must stay under
// 2 ms. It is slow and needs a database, so it runs only when asked:
//
//	MULTICA_BUDGET_SPIKE=1 go test ./internal/service -run TestBudgetGateAdmissionSpikeP99 -v
func TestBudgetGateAdmissionSpikeP99(t *testing.T) {
	if os.Getenv("MULTICA_BUDGET_SPIKE") != "1" {
		t.Skip("set MULTICA_BUDGET_SPIKE=1 to run the admission spike")
	}
	f := newBudgetFixture(t)
	ctx := context.Background()
	f.company(t, BudgetModeHold, 1_000_000_000_000) // 1,000,000 USD: covers every run, never at its limit
	for i := 0; i < 49; i++ {
		squad := f.Squad(t, fmt.Sprintf("spike squad %d", i), f.agent)
		if _, err := f.budgets.Create(ctx, f.ws, f.actor, BudgetInput{
			ScopeType: BudgetScopeSquad, ScopeID: util.MustParseUUID(squad), Period: BudgetPeriodMonth,
			AmountMicros: 1_000_000_000, SoftPercent: 80, Mode: BudgetModeHold,
		}); err != nil {
			t.Fatal(err)
		}
	}
	f.Exec(t, `INSERT INTO agent_task_queue (agent_id, status, priority, context, runtime_id)
		SELECT $1, 'queued', 0, '{}'::jsonb, $2 FROM generate_series(1, 10000)`, f.agent, f.rt)
	f.Exec(t, `INSERT INTO agent_task_queue (agent_id, status, priority, context, runtime_id, dispatched_at)
		SELECT $1, 'dispatched', 0, '{}'::jsonb, $2, now() FROM generate_series(1, 3)`, f.agent, f.rt)
	var head string
	f.QueryRow(t, `SELECT id::text FROM agent_task_queue WHERE agent_id = $1 AND status = 'queued' ORDER BY created_at LIMIT 1`, f.agent).Scan(&head)

	const samples = 2000
	durations := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		task, err := f.q.GetAgentTask(ctx, util.MustParseUUID(head))
		if err != nil {
			t.Fatal(err)
		}
		tx, err := f.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		err = f.tasks.admitClaimedTask(ctx, f.q.WithTx(tx), f.ws, task)
		durations = append(durations, time.Since(start))
		tx.Rollback(ctx)
		if err != nil {
			t.Fatalf("admission sample %d: %v", i, err)
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p := func(q float64) time.Duration { return durations[int(float64(len(durations)-1)*q)] }
	t.Logf("claim gate admission over %d samples, 50 budgets, 10000 queued tasks: p50 %v p99 %v max %v",
		samples, p(0.50), p(0.99), durations[len(durations)-1])
	if p(0.99) >= 2*time.Millisecond {
		t.Errorf("claim gate p99 %v is at or above the 2 ms spike limit", p(0.99))
	}
}
