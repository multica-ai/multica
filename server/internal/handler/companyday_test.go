package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// companyDayClock is the fake clock for the scheduler-side calls of the run. The
// API calls read the real clock; the schedules, the digest's cutoffs, the
// retrospective windows and the scorecard week are evaluated at the times the
// scenarios name.
type companyDayClock struct{ now time.Time }

func (c *companyDayClock) Now() time.Time  { return c.now }
func (c *companyDayClock) Set(t time.Time) { c.now = t }

// companyDay is the workspace the six scenarios run in: the iniWorkspace pipeline
// and squads, a boss who owns the workspace, the Secretary, and a Research squad.
type companyDay struct {
	iniWorkspace
	Owner        string
	Secretary    string
	Research     string
	ResearchLead string
	ResearchTask string
	Loc          *time.Location
	Clock        *companyDayClock
}

func newCompanyDay(t *testing.T) *companyDay {
	t.Helper()
	fx := newIniWorkspace(t)
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	suffix := uuid.NewString()[:8]
	d := &companyDay{iniWorkspace: fx, Loc: loc, Clock: &companyDayClock{}}
	d.Owner = dbfx.User(t, "Company Day Owner "+suffix, "companyday-owner-"+suffix+"@example.com")
	dbfx.Member(t, fx.ID, d.Owner, "owner")
	d.Secretary = iniAgent(t, fx.ID, "Secretary "+suffix)
	dbfx.Exec(t, `UPDATE workspace SET secretary_agent_id = $1 WHERE id = $2`, d.Secretary, fx.ID)

	d.ResearchLead = iniAgent(t, fx.ID, "Research Leader "+suffix)
	d.ResearchTask = createHandlerTestTaskForAgent(t, d.ResearchLead)
	d.Research = dbfx.Squad(t, "Research "+suffix, d.ResearchLead, testutil.Cols{"workspace_id": fx.ID})
	dbfx.SquadMember(t, d.Research, "agent", d.ResearchLead, testutil.Cols{"role": "leader"})
	fx.putSquadOnFloor(t, d.Research, "Research")
	fx.putSquadOnFloor(t, d.Product, "Product")
	return d
}

func (d *companyDay) analysisLeader(t *testing.T) string {
	t.Helper()
	var leader string
	dbfx.QueryRow(t, `SELECT leader_id::text FROM squad WHERE id = $1`, d.Analysis).Scan(&leader)
	return leader
}

// companyDayBrief is a brief with every required heading, one decision named.
func companyDayBrief(headline string, decisions ...string) string {
	items := "None."
	if len(decisions) > 0 {
		items = "- " + strings.Join(decisions, "\n- ")
	}
	return strings.Join([]string{
		"## Headline", headline, "",
		"## Needs your decision", items, "",
		"## Progress", "Product finished the pricing teardown.", "",
		"## Blockers and risks", "None.", "",
		"## Watch", "A competitor moved to usage pricing.", "",
		"## FYI", "Spend is in line.", "",
	}, "\n")
}

// companyDayHeadlineLines counts the non-empty lines under the Headline heading.
func companyDayHeadlineLines(body string) int {
	in, n := false, 0
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			in = line == "## Headline"
		case in && strings.TrimSpace(line) != "":
			n++
		}
	}
	return n
}

// TestCompanyDay runs the six company-day scenarios of the roadmap in order over
// the handler API, with scripted leaders and the fake clock for schedules. The
// scenarios share one workspace, so each one builds on the state the previous left.
// The parts that need WeKnora or a live model are skipped here and are run by
// hand from docs/engineering/runbooks/company-day.md.
func TestCompanyDay(t *testing.T) {
	d := newCompanyDay(t)
	ctx := context.Background()
	ws := parseUUID(d.ID)
	var decisionID, approvedInitiative, rejectedInitiative, budgetID string

	t.Run("1 morning: the 08:00 brief", func(t *testing.T) {
		// The Secretary's daily autopilot is 08:00 in the boss's timezone.
		at := time.Date(2026, 11, 4, 8, 0, 0, 0, d.Loc)
		next, err := service.NextOccurrenceAfterUTC("0 8 * * *", d.Loc.String(), at.Add(-time.Minute))
		if err != nil || !next.Equal(at) {
			t.Fatalf("the 08:00 schedule fires at %v (err %v), want %v", next, err, at)
		}
		d.Clock.Set(at)

		e := digestEnv{WorkspaceID: d.ID, RuntimeID: handlerTestRuntimeID(t), AgentID: d.Secretary}
		e.issue(t, d.Product, "Pricing teardown", "done", testutil.Cols{"number": 9001})

		var decision ProposalResponse
		testutil.Call(t, testHandler.CreateProposal,
			d.asAgent(newRequest("POST", "/api/proposals", proposalBody(d.Product, "Pilot usage pricing", nil)), d.Leader, d.Task)).
			Want(http.StatusCreated).JSON(&decision)
		decisionID = decision.ID

		body := companyDayBrief("Pricing teardown shipped. One decision waits.", "Pilot usage pricing: approve or refuse.")
		e.publish(t, "", d.Secretary, briefRequestBody(map[string]any{
			"title": "Morning brief", "body": body, "proposal_ids": []string{decisionID},
		})).Want(http.StatusCreated)

		var briefs int
		dbfx.QueryRow(t, `SELECT count(*) FROM brief WHERE workspace_id = $1 AND kind = 'daily'`, d.ID).Scan(&briefs)
		if briefs != 1 {
			t.Fatalf("want one daily brief, found %d", briefs)
		}
		if n := companyDayHeadlineLines(body); n < 1 || n > 3 {
			t.Fatalf("headline has %d lines, the spec allows at most three", n)
		}
		if len(body) > 20000 {
			t.Fatalf("brief body is %d bytes, over the 20000 cap", len(body))
		}

		dg, err := (&service.DigestService{Queries: testHandler.Queries, Now: d.Clock.Now}).
			Compute(ctx, ws, at.Add(-24*time.Hour), "")
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		found := false
		for _, sq := range dg.Squads {
			if sq.ID == d.Product {
				found = sq.Done >= 1 && sq.PendingProposalCount >= 1
			}
		}
		if !found {
			t.Fatalf("the 08:00 digest should name the Product squad with its done issue and pending decision")
		}
	})

	t.Run("2 discovery: opportunities are capped and one is approved", func(t *testing.T) {
		evidence := []string{uuid.NewString()}
		var filed []ProposalResponse
		for i := 1; i <= 4; i++ {
			req := d.asAgent(newRequest("POST", "/api/proposals", proposalBody(d.Research, fmt.Sprintf("Opportunity %d", i),
				map[string]any{"kind": "opportunity", "payload": map[string]any{
					"thesis": "SMBs pay late.", "market": "SMB", "target_stage": "validate", "evidence_doc_ids": evidence,
				}})), d.ResearchLead, d.ResearchTask)
			if i <= 3 {
				var p ProposalResponse
				testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusCreated).JSON(&p)
				filed = append(filed, p)
			} else {
				testutil.Call(t, testHandler.CreateProposal, req).Want(http.StatusConflict)
			}
		}
		// The boss approves one opportunity from the brief.
		testutil.Call(t, testHandler.DecideProposal,
			withURLParam(d.as(newRequest("POST", "/api/proposals/"+filed[0].ID+"/decision", map[string]any{"decision": "approve"}), d.Owner), "id", filed[0].ID)).
			Want(http.StatusOK)
		dbfx.QueryRow(t, `SELECT id::text FROM initiative WHERE workspace_id = $1 AND source_proposal_id = $2 AND stage_key = 'validate'`,
			d.ID, filed[0].ID).Scan(&approvedInitiative)
		if approvedInitiative == "" {
			t.Fatalf("approving the opportunity should create an initiative at validate")
		}
	})

	t.Run("3 venture: validate gate, then define attested by the analysis leader", func(t *testing.T) {
		d.linkStage(t, approvedInitiative, "validation_report", "competitor_profile")
		if _, err := testHandler.RunInitiativeSweep(ctx); err != nil {
			t.Fatalf("sweep: %v", err)
		}
		gate := pendingGates(t, d.ID)[approvedInitiative]
		if gate == "" {
			t.Fatalf("a ready initiative should have a pending stage gate")
		}
		testutil.Call(t, testHandler.DecideProposal,
			withURLParam(d.as(newRequest("POST", "/api/proposals/"+gate+"/decision", map[string]any{"decision": "approve"}), d.Owner), "id", gate)).
			Want(http.StatusOK)
		if st := d.detail(t, approvedInitiative).Initiative; st.StageKey != "define" || st.OwnerSquadID == nil || *st.OwnerSquadID != d.Analysis {
			t.Fatalf("after the gate the initiative should be at define under the analysis squad, got %+v", st)
		}

		d.linkStage(t, approvedInitiative, "product_brief", "analysis", "risk_register")
		if _, err := testHandler.initiativeService().Attest(ctx, service.ActionParams{
			WorkspaceID: ws, InitiativeID: parseUUID(approvedInitiative),
			Actor: service.InitiativeActor{Type: "agent", ID: parseUUID(d.analysisLeader(t))},
		}); err != nil {
			t.Fatalf("attest define: %v", err)
		}
		if st := d.detail(t, approvedInitiative).Initiative.StageKey; st != "plan" {
			t.Fatalf("after the attest the initiative should be at plan, got %s", st)
		}
	})

	t.Run("4 ask: the decision trail", func(t *testing.T) {
		detail := d.detail(t, approvedInitiative)
		if len(detail.Events) < 2 {
			t.Fatalf("the timeline should record the approval and the attest, got %d events", len(detail.Events))
		}
		var approved int
		dbfx.QueryRow(t, `SELECT count(*) FROM proposal WHERE workspace_id = $1 AND state = 'approved' AND decided_by IS NOT NULL`, d.ID).Scan(&approved)
		if approved < 2 {
			t.Fatalf("the decisions the answer cites should be in the records, found %d approved", approved)
		}
		// The cited answer over knowledge-base documents needs WeKnora and a model.
		t.Skip("the cited answer needs WeKnora and a live model; see runbooks/company-day.md step 6")
	})

	t.Run("5 control: budget hold at the limit, extend, pause", func(t *testing.T) {
		req := d.as(newRequest("POST", "/api/budgets", map[string]any{
			"scope_type": "company", "period": "month", "amount_micros": 1_000_000, "soft_percent": 80, "mode": "hold",
		}), d.Owner)
		var b BudgetResponse
		testutil.Call(t, testHandler.CreateBudget, req).Want(http.StatusCreated).JSON(&b)
		budgetID = b.ID

		// Spend past the limit on a finished run.
		rt := handlerTestRuntimeID(t)
		spent := dbfx.Task(t, d.Leader, testutil.Cols{"status": "completed", "completed_at": testutil.Raw("now()"), "runtime_id": rt})
		dbfx.Insert(t, "task_usage", testutil.Cols{
			"task_id": spent, "provider": "anthropic", "model": "claude-sonnet-4.6",
			"cost_usd_ticks": int64(10_000_000_000), "updated_at": testutil.Raw("now()"),
		})
		if err := (&service.BudgetAccounting{Queries: testHandler.Queries, TxStarter: testPool}).AccountTask(ctx, parseUUID(spent)); err != nil {
			t.Fatalf("account spend: %v", err)
		}

		// The brief's budget section warns the boss.
		dg, err := (&service.DigestService{Queries: testHandler.Queries, Now: d.Clock.Now}).Compute(ctx, ws, d.Clock.Now().Add(-24*time.Hour), "")
		if err != nil {
			t.Fatalf("digest: %v", err)
		}
		if len(dg.Budget.Scopes) == 0 {
			t.Fatalf("the digest should list the budget at its soft line")
		}

		// A routine run for a squad agent is held at the claim with a reason.
		agent := iniAgent(t, d.ID, "Routine Agent")
		queued := dbfx.Task(t, agent, testutil.Cols{"status": "queued", "runtime_id": rt})
		tasks := service.NewTaskService(testHandler.Queries, testPool, nil, events.New())
		for i := 0; i < 2; i++ {
			got, err := tasks.ClaimTask(ctx, parseUUID(agent))
			if err != nil || got != nil {
				t.Fatalf("claim %d at the limit: task %v err %v, want none", i, got, err)
			}
		}
		if n := dbfx.Count(t, `SELECT count(*) FROM budget_hold WHERE task_id = $1`, queued); n != 1 {
			t.Fatalf("the held task should have one hold row, found %d", n)
		}

		// The boss extends and the held run is admitted.
		testutil.Call(t, testHandler.ExtendBudget,
			withURLParam(d.as(newRequest("POST", "/api/budgets/"+budgetID+"/extend", map[string]any{"amount_micros": 5_000_000}), d.Owner), "id", budgetID)).
			Want(http.StatusOK)
		if got, err := tasks.ClaimTask(ctx, parseUUID(agent)); err != nil || got == nil {
			t.Fatalf("after the extension the held run should be claimed: task %v err %v", got, err)
		}

		// One click pauses the whole company; chat-style human runs still admit.
		testutil.Call(t, testHandler.SetCompanyState,
			d.as(newRequest("PUT", "/api/company/company-state", map[string]any{"state": "paused", "stop_running": true}), d.Owner)).
			Want(http.StatusOK)
		routine, err := service.CompanyVerdict(ctx, testHandler.Queries, ws, service.RunClassRoutine, d.Clock.Now())
		if err != nil || routine.Admit {
			t.Fatalf("a paused company must not admit a routine run: %+v err %v", routine, err)
		}
		human, err := service.CompanyVerdict(ctx, testHandler.Queries, ws, service.RunClassHuman, d.Clock.Now())
		if err != nil || !human.Admit {
			t.Fatalf("a paused company still admits a human run: %+v err %v", human, err)
		}
		testutil.Call(t, testHandler.SetCompanyState,
			d.as(newRequest("PUT", "/api/company/company-state", map[string]any{"state": "running"}), d.Owner)).
			Want(http.StatusOK)
	})

	t.Run("6 improvement: retrospective, weekly scorecards, rejected gate", func(t *testing.T) {
		// A second opportunity is approved and its validate gate rejected.
		var second ProposalResponse
		dbfx.QueryRow(t, `SELECT id::text FROM proposal WHERE workspace_id = $1 AND kind = 'opportunity' AND state = 'pending' LIMIT 1`, d.ID).Scan(&second.ID)
		if second.ID == "" {
			t.Fatalf("a pending opportunity should remain from discovery")
		}
		testutil.Call(t, testHandler.DecideProposal,
			withURLParam(d.as(newRequest("POST", "/api/proposals/"+second.ID+"/decision", map[string]any{"decision": "approve"}), d.Owner), "id", second.ID)).
			Want(http.StatusOK)
		dbfx.QueryRow(t, `SELECT id::text FROM initiative WHERE workspace_id = $1 AND source_proposal_id = $2`, d.ID, second.ID).Scan(&rejectedInitiative)
		d.linkStage(t, rejectedInitiative, "validation_report", "competitor_profile")
		if _, err := testHandler.RunInitiativeSweep(ctx); err != nil {
			t.Fatalf("sweep: %v", err)
		}
		gate := pendingGates(t, d.ID)[rejectedInitiative]
		testutil.Call(t, testHandler.DecideProposal,
			withURLParam(d.as(newRequest("POST", "/api/proposals/"+gate+"/decision", map[string]any{"decision": "reject", "note": "Evidence too thin."}), d.Owner), "id", gate)).
			Want(http.StatusOK)

		// The boss closes the failed initiative; the retrospective job then writes its retro.
		if _, err := testHandler.initiativeService().Close(ctx, service.ActionParams{
			WorkspaceID: ws, InitiativeID: parseUUID(rejectedInitiative),
			Actor: service.InitiativeActor{Type: "member", ID: parseUUID(d.Owner)}, Note: "Evidence too thin to continue.",
		}); err != nil {
			t.Fatalf("close the failed initiative: %v", err)
		}
		if _, err := (&service.RetroService{Queries: testHandler.Queries, Issues: testHandler.IssueService, Now: time.Now}).
			EvaluateWorkspace(ctx, ws); err != nil {
			t.Fatalf("retro: %v", err)
		}
		if n := dbfx.Count(t, `SELECT count(*) FROM retro_trigger WHERE workspace_id = $1`, d.ID); n < 1 {
			t.Fatalf("the closed initiative should fire a retrospective, found %d triggers", n)
		}

		// The weekly review scores the completed week (Monday 2 to Sunday 8 November).
		later := time.Date(2026, 11, 9, 9, 0, 0, 0, d.Loc)
		d.Clock.Set(later)
		if _, err := (&service.ScorecardService{Queries: testHandler.Queries, Now: d.Clock.Now}).RunWorkspace(ctx, ws); err != nil {
			t.Fatalf("scorecards: %v", err)
		}
		if n := dbfx.Count(t, `SELECT count(*) FROM scorecard WHERE workspace_id = $1`, d.ID); n < 1 {
			t.Fatalf("the weekly review should write scorecards, found %d", n)
		}
		// The instruction change with its diff and rollback is covered by the
		// instruction revision and org change suites, not by this run.
		t.Log("instruction change diff and rollback are covered by the instruction revision suite, not repeated here")
	})
}
