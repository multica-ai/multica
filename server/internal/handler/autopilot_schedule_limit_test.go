package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/entitlement/entitlementtest"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func finiteScheduleFixture(t *testing.T, mode string, limit any) (db.Autopilot, db.AutopilotTrigger, *service.AutopilotService) {
	t.Helper()
	agentID := dbfx.Agent(t, "Finite schedule", testRuntimeID)
	apID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id": testWorkspaceID, "title": "Finite schedule", "assignee_id": agentID,
		"status": "active", "execution_mode": mode, "created_by_type": "member", "created_by_id": testUserID,
		"issue_title_template": "Finite {{time}}",
	})
	triggerID := dbfx.Insert(t, "autopilot_trigger", testutil.Cols{
		"autopilot_id": apID, "kind": "schedule", "enabled": true, "cron_expression": "* * * * *", "timezone": "UTC",
		"created_by_type": "member", "created_by_id": testUserID, "execution_limit": limit,
	})
	q := db.New(testPool)
	ap, err := q.GetAutopilot(context.Background(), parseUUID(apID))
	if err != nil {
		t.Fatal(err)
	}
	trigger, err := q.GetAutopilotTrigger(context.Background(), parseUUID(triggerID))
	if err != nil {
		t.Fatal(err)
	}
	bus := events.New()
	svc := service.NewAutopilotService(q, testPool, bus, service.NewTaskService(q, testPool, nil, bus))
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id=$1`, ap.AssigneeID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE origin_id=$1 AND origin_type='autopilot'`, ap.ID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot_run WHERE autopilot_id=$1`, ap.ID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot_trigger WHERE autopilot_id=$1`, ap.ID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot_rule_version WHERE autopilot_id=$1`, ap.ID)
	})
	return ap, trigger, svc
}

func scheduleCount(t *testing.T, trigger db.AutopilotTrigger) int64 {
	t.Helper()
	row, err := testHandler.Queries.GetAutopilotTrigger(context.Background(), trigger.ID)
	if err != nil {
		t.Fatal(err)
	}
	return row.ExecutionCount
}

func TestAutopilotScheduleLimitCreate(t *testing.T) {
	ap, _, _ := finiteScheduleFixture(t, "run_only", nil)
	req := withURLParam(newRequest("POST", "/api/autopilots/x/triggers", map[string]any{
		"kind": "schedule", "cron_expression": "0 * * * *", "execution_limit": 1,
	}), "id", uuidToString(ap.ID))
	out := testutil.Call(t, testHandler.CreateAutopilotTrigger, req).Want(http.StatusCreated).Map()
	if out["execution_limit"] != float64(1) || out["execution_count"] != float64(0) || out["exhausted"] != false {
		t.Fatalf("finite schedule not persisted/returned: %#v", out)
	}
}

func TestAutopilotScheduleLimitValidationAndPatch(t *testing.T) {
	ap, trig, _ := finiteScheduleFixture(t, "run_only", 5)
	path := "/api/autopilots/x/triggers/y"
	patch := func(body map[string]any, want int) map[string]any {
		t.Helper()
		req := withURLParams(newRequest("PATCH", path, body), "id", uuidToString(ap.ID), "triggerId", uuidToString(trig.ID))
		return testutil.Call(t, testHandler.UpdateAutopilotTrigger, req).Want(want).Map()
	}
	for _, v := range []any{0, -1, 1.5, "2", true, 2147483648} {
		patch(map[string]any{"execution_limit": v}, http.StatusBadRequest)
	}
	patch(map[string]any{"execution_limit": 2147483647}, http.StatusOK)
	dbfx.Exec(t, `UPDATE autopilot_trigger SET execution_count=3 WHERE id=$1`, trig.ID)
	if out := patch(map[string]any{"label": "rename"}, http.StatusOK); out["execution_limit"] != float64(2147483647) || out["execution_count"] != float64(3) {
		t.Fatal(out)
	}
	if out := patch(map[string]any{"execution_limit": 2, "enabled": true}, http.StatusOK); out["exhausted"] != true || out["next_run_at"] != nil {
		t.Fatal(out)
	}
	if out := patch(map[string]any{"cron_expression": "*/5 * * * *"}, http.StatusOK); out["execution_count"] != float64(3) || out["exhausted"] != true {
		t.Fatal(out)
	}
	if out := patch(map[string]any{"execution_limit": 4}, http.StatusOK); out["exhausted"] != false || out["next_run_at"] == nil {
		t.Fatal(out)
	}
	if out := patch(map[string]any{"execution_limit": nil}, http.StatusOK); out["execution_limit"] != nil || out["execution_count"] != float64(3) || out["exhausted"] != false {
		t.Fatal(out)
	}
	dbfx.Exec(t, `UPDATE autopilot_trigger SET kind='webhook',execution_limit=NULL WHERE id=$1`, trig.ID)
	patch(map[string]any{"execution_limit": nil}, http.StatusBadRequest)
	req := withURLParam(newRequest("POST", "/api/autopilots/x/triggers", map[string]any{"kind": "webhook", "execution_limit": 1}), "id", uuidToString(ap.ID))
	testutil.Call(t, testHandler.CreateAutopilotTrigger, req).Want(http.StatusBadRequest)
	// Updating a limit must not turn the editor into the execution principal.
	row, err := testHandler.Queries.GetAutopilotTrigger(context.Background(), trig.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.CreatedByID != trig.CreatedByID {
		t.Fatal("trigger creator changed")
	}
}

func TestAutopilotScheduleLimitDurableCount(t *testing.T) {
	for _, mode := range []string{"run_only", "create_issue"} {
		for _, limit := range []any{nil, 1, 3} {
			t.Run(fmt.Sprintf("%s/%v", mode, limit), func(t *testing.T) {
				ap, trig, svc := finiteScheduleFixture(t, mode, limit)
				n := 3
				if limit != nil {
					n = limit.(int)
				}
				planned := time.Now().UTC().Truncate(time.Second)
				for i := 0; i < n; i++ {
					at := planned.Add(time.Duration(i) * 2 * time.Minute)
					run, err := svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, at)
					if err != nil {
						t.Fatal(err)
					}
					if run == nil || (!run.TaskID.Valid && !run.IssueID.Valid) {
						t.Fatalf("not accepted: %#v", run)
					}
					if count := scheduleCount(t, trig); count != int64(i+1) {
						t.Fatalf("count=%d", count)
					}
					again, err := svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, at)
					if err != nil || again.ID != run.ID {
						t.Fatalf("idempotent retry: %#v %v", again, err)
					}
					// Task failure after acceptance never refunds a finite occurrence.
					dbfx.Exec(t, `UPDATE autopilot_run SET status='failed',completed_at=now() WHERE id=$1`, run.ID)
					if run.IssueID.Valid {
						dbfx.Exec(t, `UPDATE autopilot_run SET created_at=now()-interval '2 minute' WHERE id=$1`, run.ID)
					}
				}
				if limit != nil {
					// Reconstruct all services to model a scheduler/process restart.
					bus := events.New()
					q := db.New(testPool)
					restarted := service.NewAutopilotService(q, testPool, bus, service.NewTaskService(q, testPool, nil, bus))
					run, err := restarted.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, planned.Add(20*time.Minute))
					if err != nil {
						t.Fatal(err)
					}
					if run.Status != "skipped" || scheduleCount(t, trig) != int64(n) {
						t.Fatalf("restart bypassed limit: %#v", run)
					}
					scopes, err := q.ListSchedulableAutopilotTriggers(context.Background())
					if err != nil {
						t.Fatal(err)
					}
					for _, s := range scopes {
						if s.ID == trig.ID {
							t.Fatal("exhausted scope still schedulable")
						}
					}
					row, err := q.GetAutopilotTrigger(context.Background(), trig.ID)
					if err != nil {
						t.Fatal(err)
					}
					if row.NextRunAt.Valid {
						t.Fatal("exhausted schedule still advertises next run")
					}
				}
			})
		}
	}
}

func TestAutopilotScheduleLimitConcurrentLastSlot(t *testing.T) {
	ap, trig, svc := finiteScheduleFixture(t, "run_only", 1)
	const workers = 12
	start := make(chan struct{})
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	planned := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, planned.Add(time.Duration(i)*time.Minute))
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if count := scheduleCount(t, trig); count != 1 {
		t.Fatalf("concurrent count=%d", count)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE autopilot_run_id IN (SELECT id FROM autopilot_run WHERE trigger_id=$1)`, trig.ID); n != 1 {
		t.Fatalf("accepted %d tasks", n)
	}
}

func TestAutopilotScheduleLimitQueuedAtRuntimeCapacity(t *testing.T) {
	ctx := context.Background()
	ap, trig, svc := finiteScheduleFixture(t, "run_only", 1)
	blocker := dbfx.Agent(t, "Runtime slot holder", testRuntimeID)
	blockerTask := dbfx.Task(t, blocker, testutil.Cols{"runtime_id": testRuntimeID, "status": "running"})
	dbfx.Exec(t, `UPDATE agent_runtime SET max_concurrent_tasks=1 WHERE id=$1`, testRuntimeID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE agent_runtime SET max_concurrent_tasks=NULL WHERE id=$1`, testRuntimeID)
	})
	planned := time.Now().UTC().Truncate(time.Second)
	run, err := svc.DispatchAutopilotForPlan(ctx, ap, trig.ID, "schedule", nil, planned)
	if err != nil || run == nil || !run.TaskID.Valid {
		t.Fatalf("enqueue while runtime full: %#v, %v", run, err)
	}
	q := db.New(testPool)
	tasks := service.NewTaskService(q, testPool, nil, events.New())
	claimed, err := tasks.ClaimTask(ctx, ap.AssigneeID)
	if err != nil || claimed != nil {
		t.Fatalf("full runtime claimed queued work: %#v, %v", claimed, err)
	}
	if count := scheduleCount(t, trig); count != 1 {
		t.Fatalf("waiting for claim refunded accepted occurrence: %d", count)
	}
	if _, err := svc.DispatchAutopilotForPlan(ctx, ap, trig.ID, "schedule", nil, planned.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE agent_id=$1`, ap.AssigneeID); n != 1 {
		t.Fatalf("exhausted schedule enqueued %d tasks", n)
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1`, blockerTask)
	claimed, err = tasks.ClaimTask(ctx, ap.AssigneeID)
	if err != nil || claimed == nil || claimed.ID != run.TaskID {
		t.Fatalf("accepted work did not claim after capacity freed: %#v, %v", claimed, err)
	}
	if count := scheduleCount(t, trig); count != 1 {
		t.Fatalf("claim counted acceptance again: %d", count)
	}
}

func TestAutopilotScheduleLimitConcurrentSamePlan(t *testing.T) {
	ap, trig, svc := finiteScheduleFixture(t, "run_only", 3)
	planned := time.Now().UTC().Truncate(time.Second)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			// A loser can return a retryable unique/conflict error. It must never
			// create a second downstream resource, even when stale recovery races.
			svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, planned)
		}()
	}
	close(start)
	wg.Wait()
	run, err := svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, planned)
	if err != nil {
		t.Fatal(err)
	}
	if !run.TaskID.Valid || scheduleCount(t, trig) != 1 {
		t.Fatalf("same occurrence not counted once: %#v", run)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE autopilot_run_id IN (SELECT id FROM autopilot_run WHERE trigger_id=$1)`, trig.ID); n != 1 {
		t.Fatalf("duplicate tasks=%d", n)
	}
}

type rejectScheduleCommit struct{ service.TxStarter }
type rejectedScheduleTx struct{ pgx.Tx }

func (s rejectScheduleCommit) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return rejectedScheduleTx{tx}, nil
}
func (tx rejectedScheduleTx) Commit(ctx context.Context) error {
	tx.Rollback(ctx)
	return errors.New("injected downstream transaction failure")
}

func TestAutopilotScheduleLimitRollbackAndRecovery(t *testing.T) {
	for _, mode := range []string{"run_only", "create_issue"} {
		t.Run(mode, func(t *testing.T) {
			ap, trig, svc := finiteScheduleFixture(t, mode, 1)
			planned := time.Now().UTC().Truncate(time.Second)
			svc.TxStarter = rejectScheduleCommit{testPool}
			if _, err := svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, planned); err == nil {
				t.Fatal("injected commit should fail")
			}
			if scheduleCount(t, trig) != 0 {
				t.Fatal("rolled back creation consumed a count")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM issue WHERE origin_id=$1`, ap.ID); n != 0 {
				t.Fatal("rolled back issue persisted")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE autopilot_run_id IN (SELECT id FROM autopilot_run WHERE trigger_id=$1)`, trig.ID); n != 0 {
				t.Fatal("rolled back task persisted")
			}
			// Model a crash after run INSERT but before downstream creation. Recovery
			// frees that abandoned occurrence and the accepted replacement counts once.
			at := planned.Add(time.Minute)
			dbfx.Insert(t, "autopilot_run", testutil.Cols{"autopilot_id": ap.ID, "trigger_id": trig.ID, "source": "schedule", "status": map[string]string{"run_only": "running", "create_issue": "issue_created"}[mode], "planned_at": at})
			svc.TxStarter = testPool
			run, err := svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, at)
			if err != nil {
				t.Fatal(err)
			}
			if !run.TaskID.Valid && !run.IssueID.Valid {
				t.Fatal("recovery did not create downstream work")
			}
			if scheduleCount(t, trig) != 1 {
				t.Fatal("recovery did not count exactly once")
			}
		})
	}
}

func TestAutopilotScheduleLimitSkipsAndOtherSources(t *testing.T) {
	ap, trig, svc := finiteScheduleFixture(t, "run_only", 1)
	planned := time.Now().UTC().Truncate(time.Second)
	// Offline admission, creator authorization and commercial quota all occur
	// before downstream acceptance and must leave Once available.
	dbfx.Exec(t, `UPDATE agent_runtime SET status='offline' WHERE id=$1`, testRuntimeID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE agent_runtime SET status='online' WHERE id=$1`, testRuntimeID)
	})
	run, err := svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, planned)
	if err != nil || run.Status != "skipped" {
		t.Fatalf("offline: %#v %v", run, err)
	}
	if scheduleCount(t, trig) != 0 {
		t.Fatal("offline skip consumed Once")
	}
	dbfx.Exec(t, `UPDATE agent_runtime SET status='online' WHERE id=$1`, testRuntimeID)
	dbfx.Exec(t, `UPDATE autopilot_trigger SET created_by_id=$2 WHERE id=$1`, trig.ID, uuid.NewString())
	run, err = svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, planned.Add(time.Minute))
	if err != nil || run.Status != "skipped" {
		t.Fatalf("permission: %#v %v", run, err)
	}
	if scheduleCount(t, trig) != 0 {
		t.Fatal("permission skip consumed Once")
	}
	dbfx.Exec(t, `UPDATE autopilot_trigger SET created_by_id=$2 WHERE id=$1`, trig.ID, testUserID)
	start := planned.Add(-time.Hour)
	end := planned.Add(time.Hour)
	zero := 0
	stub := entitlementtest.New()
	stub.Set(uuid.MustParse(testWorkspaceID), entitlement.GateAutopilotRuns, entitlement.Decision{Gate: entitlement.Gate{Action: entitlement.ActionEnforce, Limit: &zero, PeriodStart: &start, PeriodEnd: &end, ResetAt: &end}})
	svc.Entitlements = stub
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot_quota_reservation WHERE workspace_id=$1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM autopilot_quota_period WHERE workspace_id=$1`, testWorkspaceID)
	})
	run, err = svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, planned.Add(2*time.Minute))
	if err != nil || run.Status != "skipped" {
		t.Fatalf("quota: %#v %v", run, err)
	}
	if scheduleCount(t, trig) != 0 {
		t.Fatal("commercial quota skip consumed Once")
	}
	svc.Entitlements = nil
	run, err = svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, planned.Add(3*time.Minute))
	if err != nil || !run.TaskID.Valid {
		t.Fatalf("accepted: %#v %v", run, err)
	}
	task, err := svc.Queries.GetAutopilotTaskByRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if task.OriginatorUserID != trig.CreatedByID || task.AccountableUserID != trig.CreatedByID {
		t.Fatal("schedule lost trigger creator attribution")
	}
	manual, _, err := svc.DispatchAutopilotManual(context.Background(), ap, pgtype.UUID{}, nil, parseUUID(testUserID))
	if err != nil || !manual.TaskID.Valid {
		t.Fatalf("manual blocked by finite limit: %#v %v", manual, err)
	}
	webhook := dbfx.Insert(t, "autopilot_trigger", testutil.Cols{"autopilot_id": ap.ID, "kind": "webhook", "enabled": true, "created_by_type": "member", "created_by_id": testUserID})
	web, err := svc.DispatchAutopilot(context.Background(), ap, parseUUID(webhook), "webhook", nil)
	if err != nil || !web.TaskID.Valid {
		t.Fatalf("webhook blocked by finite limit: %#v %v", web, err)
	}
	if scheduleCount(t, trig) != 1 {
		t.Fatal("manual/webhook changed schedule count")
	}
}

func TestAutopilotScheduleLimitDuplicateSkipAndSibling(t *testing.T) {
	ap, trig, svc := finiteScheduleFixture(t, "create_issue", 3)
	ap.IssueTitleTemplate = pgtype.Text{String: "Same scheduled work", Valid: true}
	at := time.Now().UTC().Truncate(time.Second)
	run, err := svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, at)
	if err != nil || !run.IssueID.Valid {
		t.Fatalf("first: %#v %v", run, err)
	}
	skipped, err := svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, at.Add(time.Minute))
	if err != nil || skipped.Status != "skipped" || skipped.ReasonCode.String != "already_active" {
		t.Fatalf("duplicate: %#v %v", skipped, err)
	}
	if scheduleCount(t, trig) != 1 {
		t.Fatal("post-admission duplicate consumed another count")
	}
	// Only one trigger is exhausted; its sibling keeps an independent count.
	dbfx.Exec(t, `UPDATE autopilot_trigger SET execution_limit=1 WHERE id=$1`, trig.ID)
	siblingID := dbfx.Insert(t, "autopilot_trigger", testutil.Cols{"autopilot_id": ap.ID, "kind": "schedule", "enabled": true, "cron_expression": "* * * * *", "execution_limit": 1, "created_by_type": "member", "created_by_id": testUserID})
	ap.IssueTitleTemplate = pgtype.Text{String: "Independent scheduled work", Valid: true}
	siblingRun, err := svc.DispatchAutopilotForPlan(context.Background(), ap, parseUUID(siblingID), "schedule", nil, at.Add(2*time.Minute))
	if err != nil || !siblingRun.IssueID.Valid {
		t.Fatalf("sibling: %#v %v", siblingRun, err)
	}
	if scheduleCount(t, trig) != 1 {
		t.Fatal("sibling changed original trigger count")
	}
}

func TestAutopilotScheduleLimitEditsResumeFutureOccurrences(t *testing.T) {
	ap, trig, svc := finiteScheduleFixture(t, "run_only", 1)
	at := time.Now().UTC().Truncate(time.Second)
	dispatch := func(i int, accepted bool) {
		t.Helper()
		run, err := svc.DispatchAutopilotForPlan(context.Background(), ap, trig.ID, "schedule", nil, at.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if run.TaskID.Valid != accepted {
			t.Fatalf("occurrence %d accepted=%v: %#v", i, accepted, run)
		}
	}
	patch := func(body map[string]any) {
		t.Helper()
		req := withURLParams(newRequest("PATCH", "/api/autopilots/x/triggers/y", body), "id", uuidToString(ap.ID), "triggerId", uuidToString(trig.ID))
		testutil.Call(t, testHandler.UpdateAutopilotTrigger, req).Want(http.StatusOK)
	}
	dispatch(0, true)
	patch(map[string]any{"enabled": true})
	dispatch(1, false)
	patch(map[string]any{"execution_limit": 2})
	dispatch(2, true)
	patch(map[string]any{"execution_limit": 1})
	dispatch(3, false)
	patch(map[string]any{"execution_limit": nil})
	dispatch(4, true)
	if scheduleCount(t, trig) != 3 {
		t.Fatal("editing reset or lost accepted counts")
	}
	other := dbfx.User(t, "Viewer", "finite-viewer-"+uuid.NewString()+"@example.test")
	dbfx.Member(t, testWorkspaceID, other, "member")
	req := withURLParams(newRequestAs(other, "PATCH", "/api/autopilots/x/triggers/y", map[string]any{"execution_limit": 5}), "id", uuidToString(ap.ID), "triggerId", uuidToString(trig.ID))
	testutil.Call(t, testHandler.UpdateAutopilotTrigger, req).Want(http.StatusForbidden)
}

type pauseScheduleBegin struct {
	service.TxStarter
	nth, calls int
	entered    chan struct{}
	release    chan struct{}
}

func (s *pauseScheduleBegin) Begin(ctx context.Context) (pgx.Tx, error) {
	s.calls++
	if s.calls == s.nth {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.TxStarter.Begin(ctx)
}

func TestAutopilotScheduleLimitFencesRecoveredWorker(t *testing.T) {
	for _, withQuota := range []bool{false, true} {
		t.Run(fmt.Sprintf("quota=%v", withQuota), func(t *testing.T) {
			ap, trig, svc := finiteScheduleFixture(t, "run_only", 1)
			at := time.Now().UTC().Truncate(time.Second)
			nth := 1
			if withQuota {
				start := at.Add(-time.Hour)
				end := at.Add(time.Hour)
				limit := 5
				stub := entitlementtest.New()
				stub.Set(uuid.MustParse(testWorkspaceID), entitlement.GateAutopilotRuns, entitlement.Decision{Gate: entitlement.Gate{Action: entitlement.ActionEnforce, Limit: &limit, PeriodStart: &start, PeriodEnd: &end, ResetAt: &end}})
				svc.Entitlements = stub
				nth = 2 // quota transaction commits before downstream starts
				t.Cleanup(func() {
					testPool.Exec(context.Background(), `DELETE FROM autopilot_quota_reservation WHERE workspace_id=$1`, testWorkspaceID)
					testPool.Exec(context.Background(), `DELETE FROM autopilot_quota_period WHERE workspace_id=$1`, testWorkspaceID)
				})
			}
			pause := &pauseScheduleBegin{TxStarter: testPool, nth: nth, entered: make(chan struct{}), release: make(chan struct{})}
			svc.TxStarter = pause
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			finished := make(chan error, 1)
			go func() { _, err := svc.DispatchAutopilotForPlan(ctx, ap, trig.ID, "schedule", nil, at); finished <- err }()
			select {
			case <-pause.entered:
			case err := <-finished:
				t.Fatalf("worker did not reach downstream: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// A stale lease holder resumes only after another scheduler has recovered
			// and accepted its occurrence. It must not create a second task or charge it.
			replacement := service.NewAutopilotService(svc.Queries, testPool, svc.Bus, svc.TaskSvc)
			replacement.Entitlements = svc.Entitlements
			accepted, err := replacement.DispatchAutopilotForPlan(ctx, ap, trig.ID, "schedule", nil, at)
			close(pause.release)
			originalErr := <-finished
			if err != nil || accepted == nil || !accepted.TaskID.Valid {
				t.Fatalf("replacement: %#v %v", accepted, err)
			}
			if originalErr == nil {
				t.Fatal("recovered worker was not fenced")
			}
			if scheduleCount(t, trig) != 1 {
				t.Fatal("stale worker changed accepted count")
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE autopilot_run_id IN (SELECT id FROM autopilot_run WHERE trigger_id=$1)`, trig.ID); n != 1 {
				t.Fatalf("stale worker created %d tasks", n)
			}
			again, err := replacement.DispatchAutopilotForPlan(ctx, ap, trig.ID, "schedule", nil, at)
			if err != nil || again.ID != accepted.ID {
				t.Fatalf("replay lost accepted run: %#v %v", again, err)
			}
		})
	}
}

func TestAutopilotScheduleLimitMigrationRoundTrip(t *testing.T) {
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// A temporary table shadows the live fixture table on this one connection.
	// It models rows predating the migration without touching another test's data.
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE autopilot_trigger (kind text NOT NULL); INSERT INTO autopilot_trigger VALUES ('schedule'),('webhook')`); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../migrations/615_autopilot_schedule_limit.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	down, err := os.ReadFile("../../migrations/615_autopilot_schedule_limit.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		if _, err := tx.Exec(ctx, string(up)); err != nil {
			t.Fatal(err)
		}
		var defaults int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM autopilot_trigger WHERE execution_limit IS NULL AND execution_count=0`).Scan(&defaults); err != nil || defaults != 2 {
			t.Fatalf("legacy defaults: %d %v", defaults, err)
		}
		for _, sql := range []string{`UPDATE autopilot_trigger SET execution_count=-1`, `UPDATE autopilot_trigger SET execution_limit=0`, `UPDATE autopilot_trigger SET execution_limit=1 WHERE kind='webhook'`} {
			nested, err := tx.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = nested.Exec(ctx, sql)
			nested.Rollback(ctx)
			if err == nil {
				t.Fatalf("missing database constraint: %s", sql)
			}
		}
		if _, err := tx.Exec(ctx, string(down)); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM autopilot_trigger`).Scan(&n); err != nil || n != 2 {
			t.Fatalf("rollback lost rows: %d %v", n, err)
		}
	}
}

func TestAutopilotScheduleLimitLegacyDispatchCannotBypass(t *testing.T) {
	ap, trig, svc := finiteScheduleFixture(t, "run_only", 1)
	first, err := svc.DispatchAutopilot(context.Background(), ap, trig.ID, "schedule", nil)
	if err != nil || !first.TaskID.Valid {
		t.Fatalf("legacy first: %#v %v", first, err)
	}
	second, err := svc.DispatchAutopilot(context.Background(), ap, trig.ID, "schedule", nil)
	if err != nil || second.Status != "skipped" {
		t.Fatalf("legacy second: %#v %v", second, err)
	}
	if scheduleCount(t, trig) != 1 {
		t.Fatal("legacy dispatch bypassed count")
	}
}
