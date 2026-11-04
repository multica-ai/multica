package service

import (
	"context"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// drainClaims claims queued tasks for agentID until the claim gate returns none,
// completing each one it claims so the next claim can proceed (the test agent
// runs one task at a time). It returns the claimed task ids.
func drainClaims(t *testing.T, f companyFixture, agentID string) []string {
	t.Helper()
	ctx := context.Background()
	var ids []string
	for i := 0; i < 10; i++ {
		got, err := f.svc.Tasks.ClaimTask(ctx, util.MustParseUUID(agentID))
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if got == nil {
			return ids
		}
		id := util.UUIDToString(got.ID)
		ids = append(ids, id)
		f.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, id)
	}
	t.Fatal("claim kept returning tasks; the drain did not settle")
	return nil
}

// A webhook delivery that starts a routine run is checked against the company
// controls at admission (spec 4.5), the same as a scheduled run, with one
// difference: a paused company or a budget at its limit does not stop the run
// at admission. Its issue and queued task are created, and the claim gate holds
// the task until the hold releases (spec 4.10(b)). Quiet hours still defer it
// and the sweep releases it. A run a person starts and the Secretary's brief
// are exempt. The worker that replays a durable delivery must not dispatch a
// run that quiet hours hold.
func TestWebhookRunIsHeldAtClaimByTheCompanyControls(t *testing.T) {
	f := newCompanyFixture(t)
	ctx := context.Background()
	apFx := principalFixture{Fixture: f.Fixture, q: f.svc.Queries, svc: &AutopilotService{Queries: f.svc.Queries, TxStarter: f.svc.TxStarter, Bus: events.New(), TaskSvc: f.svc.Tasks}}
	agent := apFx.privateAgentOwnedBy(t, util.UUIDToString(f.owner), "webhook gate")
	owner := util.UUIDToString(f.owner)

	// newAutopilot starts a fresh autopilot with its trigger, so each delivery is
	// the only run of its autopilot.
	newAutopilot := func(t *testing.T) (db.Autopilot, string) {
		t.Helper()
		apID, trigID := apFx.autopilotWithTrigger(t, agent, owner, owner)
		ap, err := f.svc.Queries.GetAutopilot(ctx, util.MustParseUUID(apID))
		if err != nil {
			t.Fatal(err)
		}
		return ap, trigID
	}
	// deliver admits a webhook delivery and then runs the worker's replay of it.
	deliver := func(t *testing.T, ap db.Autopilot, trig string) *db.AutopilotRun {
		t.Helper()
		deliveryID := dbid.NewV7()
		run, err := apFx.svc.AdmitAutopilotWebhookDelivery(ctx, ap, util.MustParseUUID(trig), []byte(`{"metric":"signups"}`), deliveryID)
		if err != nil || run == nil {
			t.Fatalf("admit webhook delivery: run %v, err %v", run, err)
		}
		replayed, err := apFx.svc.DispatchAutopilotForWebhookDelivery(ctx, ap, util.MustParseUUID(trig), []byte(`{"metric":"signups"}`), deliveryID)
		if err != nil {
			t.Fatalf("worker replay: %v", err)
		}
		if replayed.ID != run.ID {
			t.Fatalf("worker replay made run %s, want the admitted run %s", util.UUIDToString(replayed.ID), util.UUIDToString(run.ID))
		}
		reloaded, err := f.svc.Queries.GetAutopilotRun(ctx, replayed.ID)
		if err != nil {
			t.Fatal(err)
		}
		return &reloaded
	}
	heldBy := func(t *testing.T, run *db.AutopilotRun) (string, bool) {
		t.Helper()
		code, held, err := apFx.svc.HeldRunReason(ctx, f.ws, *run)
		if err != nil {
			t.Fatalf("held run reason: %v", err)
		}
		return string(code), held
	}

	// Running: the delivery is admitted, an issue and a task are created, and the
	// claim gate admits the task.
	ap, trig := newAutopilot(t)
	running := deliver(t, ap, trig)
	if running.Status == "skipped" || running.Status == "deferred" || !running.TaskID.Valid {
		t.Fatalf("a running company: status %q, task %v; want a dispatched run", running.Status, running.TaskID.Valid)
	}
	if code, held := heldBy(t, running); held {
		t.Fatalf("a running company holds the run at claim: %q", code)
	}

	// Paused: the run is admitted with its task queued, not skipped. The claim
	// gate holds the task and never returns it while paused.
	if _, err := f.svc.Set(ctx, f.ws, f.actor(), CompanyStatePaused); err != nil {
		t.Fatal(err)
	}
	ap, trig = newAutopilot(t)
	held := deliver(t, ap, trig)
	if held.Status == "skipped" || held.Status == "deferred" {
		t.Fatalf("paused webhook run: status %q, reason %q; want it admitted and held at claim", held.Status, held.ReasonCode.String)
	}
	if !held.TaskID.Valid {
		t.Fatal("a held webhook run has no queued task to hold")
	}
	if code, isHeld := heldBy(t, held); !isHeld || code != "company_paused" {
		t.Fatalf("paused webhook run: held %v reason %q; want held company_paused", isHeld, code)
	}
	for _, id := range drainClaims(t, f, agent) {
		if id == util.UUIDToString(held.TaskID) {
			t.Fatal("the claim gate returned the held webhook task while the company was paused")
		}
	}
	if queued := f.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id = $1 AND status = 'queued'`, util.UUIDToString(held.TaskID)); queued != 1 {
		t.Fatal("the held webhook task left the queue while paused")
	}

	// A run a person starts by hand is exempt while paused.
	ap, trig = newAutopilot(t)
	byHand, _, err := apFx.svc.DispatchAutopilotManual(ctx, ap, util.MustParseUUID(trig), nil, f.owner)
	if err != nil || byHand == nil || byHand.Status == "skipped" {
		t.Fatalf("manual run while paused: run %+v, err %v; want admitted", byHand, err)
	}
	if code, isHeld := heldBy(t, byHand); isHeld {
		t.Fatalf("a person's run is held while paused: %q", code)
	}

	// The Secretary's brief is exempt while paused: the autopilot is the Secretary's
	// daily brief, assigned to the workspace's Secretary agent.
	ap, trig = newAutopilot(t)
	apFx.Exec(t, `UPDATE workspace SET secretary_agent_id = $2 WHERE id = $1`, f.WorkspaceID, agent)
	apFx.Exec(t, `UPDATE autopilot SET title = $2 WHERE id = $1`, util.UUIDToString(ap.ID), DailyBriefTitle)
	ap, err = f.svc.Queries.GetAutopilot(ctx, ap.ID)
	if err != nil {
		t.Fatal(err)
	}
	if brief := deliver(t, ap, trig); brief.Status == "skipped" {
		t.Fatalf("the Secretary's brief was held while paused: %q", brief.FailureReason.String)
	} else if code, isHeld := heldBy(t, brief); isHeld {
		t.Fatalf("the Secretary's brief is held while paused: %q", code)
	}
	apFx.Exec(t, `UPDATE workspace SET secretary_agent_id = NULL WHERE id = $1`, f.WorkspaceID)

	// Resumed: the held webhook run is no longer held, and the claim gate returns
	// its task.
	if _, err := f.svc.Set(ctx, f.ws, f.actor(), CompanyStateRunning); err != nil {
		t.Fatal(err)
	}
	if code, isHeld := heldBy(t, held); isHeld {
		t.Fatalf("after resume the webhook run is still held: %q", code)
	}
	claimedAfterResume := drainClaims(t, f, agent)
	found := false
	for _, id := range claimedAfterResume {
		found = found || id == util.UUIDToString(held.TaskID)
	}
	if !found {
		t.Fatalf("after resume the held webhook task was not claimed; claimed %v", claimedAfterResume)
	}

	// Quiet hours defer the webhook run rather than drop it, and the worker replay
	// does not dispatch it.
	now := time.Now().UTC()
	if _, err := f.svc.SetQuietHours(ctx, f.ws, QuietHours{Enabled: true, Start: now.Add(-time.Hour).Format("15:04"), End: now.Add(time.Hour).Format("15:04"), Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	ap, trig = newAutopilot(t)
	deferred := deliver(t, ap, trig)
	if deferred.Status != "deferred" || deferred.ReasonCode.String != "quiet_hours" {
		t.Fatalf("quiet webhook run: status %q, reason %q; want deferred quiet_hours", deferred.Status, deferred.ReasonCode.String)
	}
	if deferred.IssueID.Valid || deferred.TaskID.Valid {
		t.Fatal("a deferred webhook run was dispatched before the window closed")
	}
	if n, err := apFx.svc.ReleaseQuietDeferredRuns(ctx, 100); err != nil || n != 0 {
		t.Fatalf("sweep inside the window dispatched %d (err %v); want none", n, err)
	}

	// The window closes: the sweep dispatches the deferred webhook run.
	if _, err := f.svc.SetQuietHours(ctx, f.ws, QuietHours{Enabled: false, Start: "22:00", End: "07:00", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	if n, err := apFx.svc.ReleaseQuietDeferredRuns(ctx, 100); err != nil || n != 1 {
		t.Fatalf("sweep after the window dispatched %d (err %v); want 1", n, err)
	}
	released, err := f.svc.Queries.GetAutopilotRun(ctx, deferred.ID)
	if err != nil || released.Status == "deferred" || released.Status == "skipped" {
		t.Fatalf("released webhook run: status %q (err %v); want it dispatched", released.Status, err)
	}
}

// A webhook run admitted while its squad's budget is at its limit is held at
// claim, not skipped: its task waits, the hold is recorded once, and Extend
// releases the task to the claim (spec 4.10(b)).
func TestWebhookRunHeldByBudgetWaitsAtClaimUntilExtend(t *testing.T) {
	f := newBudgetFixture(t)
	ctx := context.Background()
	apFx := principalFixture{Fixture: f.Fixture, q: f.q, svc: &AutopilotService{Queries: f.q, TxStarter: sharedTestPool(t), Bus: events.New(), TaskSvc: f.tasks}}
	budget := f.company(t, BudgetModeHold, 1_000_000)

	// The company has spent its whole budget on one completed run.
	spent := f.queued(t, nil)
	f.usage(t, spent, 10_000_000_000)
	if err := f.acct.AccountTask(ctx, util.MustParseUUID(spent)); err != nil {
		t.Fatal(err)
	}
	f.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, spent)

	apID, trigID := apFx.autopilotWithTrigger(t, f.agent, f.owner, f.owner)
	ap, err := f.q.GetAutopilot(ctx, util.MustParseUUID(apID))
	if err != nil {
		t.Fatal(err)
	}
	deliveryID := dbid.NewV7()
	trig := util.MustParseUUID(trigID)
	if _, err := apFx.svc.AdmitAutopilotWebhookDelivery(ctx, ap, trig, []byte(`{}`), deliveryID); err != nil {
		t.Fatalf("admit: %v", err)
	}
	run, err := apFx.svc.DispatchAutopilotForWebhookDelivery(ctx, ap, trig, []byte(`{}`), deliveryID)
	if err != nil {
		t.Fatalf("worker replay: %v", err)
	}
	if run.Status == "skipped" || !run.TaskID.Valid {
		t.Fatalf("budget-held webhook run: status %q, task %v; want admitted with a queued task", run.Status, run.TaskID.Valid)
	}
	reloaded, err := f.q.GetAutopilotRun(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if code, held, err := apFx.svc.HeldRunReason(ctx, f.ws, reloaded); err != nil || !held || code != VerdictReasonBudgetHold {
		t.Fatalf("budget-held webhook run: held %v code %q err %v; want held budget_hold", held, code, err)
	}

	// The claim refuses the task at the limit and records one hold for it.
	for i := 0; i < 2; i++ {
		if got, err := f.tasks.ClaimTask(ctx, util.MustParseUUID(f.agent)); err != nil || got != nil {
			t.Fatalf("claim %d at the limit: task %v err %v; want no task", i, got, err)
		}
	}
	if n := f.Count(t, `SELECT count(*) FROM budget_hold WHERE task_id = $1 AND released_at IS NULL`, util.UUIDToString(run.TaskID)); n != 1 {
		t.Fatalf("held webhook task has %d holds in force, want exactly 1", n)
	}

	// Extend releases the hold: the task is no longer held, and the claim returns it.
	if _, err := f.budgets.Extend(ctx, f.ws, f.actor, budget.ID, 5_000_000); err != nil {
		t.Fatal(err)
	}
	if _, held, _ := apFx.svc.HeldRunReason(ctx, f.ws, reloaded); held {
		t.Fatal("after Extend the webhook run is still held")
	}
	got, err := f.tasks.ClaimTask(ctx, util.MustParseUUID(f.agent))
	if err != nil || got == nil || util.UUIDToString(got.ID) != util.UUIDToString(run.TaskID) {
		t.Fatalf("after Extend the held webhook task must be claimed: task %v err %v", got, err)
	}
}
