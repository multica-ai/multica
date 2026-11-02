package messagedelivery

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Regression coverage for the OL-25 second review (8 findings on
// 34ccc512e). All addresses and message contents are synthetic; no external
// transport is used.

// R7: the production ScanOnce path must derive the source kind from the
// run's persisted task link, not the autopilot's current configuration.
func TestRereviewScannerPreservesTaskBackedRunOnly(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newMDFixture(t, "rereview-mode", nil)
	fx.groupRoute(t, "oc_rereview_mode")
	task := testFx.Insert(t, "agent_task_queue", testutil.Cols{
		"agent_id": testAgent, "status": "completed", "completed_at": testutil.Raw("now()"), "priority": 0,
	})
	run := fx.run(t, "completed", testutil.Cols{"task_id": task, "issue_id": nil, "result": nil})
	testFx.Exec(t, `UPDATE autopilot SET execution_mode = 'create_issue' WHERE id = $1`, fx.autopilot)
	svc := newTestService(nil, nil)
	if err := svc.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	d, _, err := svc.GetDelivery(ctx, uuidOf(t, testWSID), uuidOf(t, fx.autopilot), uuidOf(t, firstDeliveryForRun(t, run)))
	if err != nil {
		t.Fatal(err)
	}
	if d.SourceKind != SourceKindRunOnly {
		t.Fatalf("production scanner discarded persisted task_id: source_kind=%s content=%s", d.SourceKind, d.ContentSnapshot)
	}
}

// R7: the stale-task scan filters on the run's OWN task link; a mode edit
// must not hide the task from the syncer.
func TestRereviewStaleTaskSurvivesModeEdit(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	resetScanCursor(t, scannerRunOnlyTask)
	fx := newMDFixture(t, "rereview-stale-mode", nil)
	run := fx.run(t, "running", testutil.Cols{"result": nil, "completed_at": nil})
	task := testFx.Insert(t, "agent_task_queue", testutil.Cols{
		"agent_id": testAgent, "status": "completed", "completed_at": testutil.Raw("now()"),
		"priority": 0, "autopilot_run_id": run,
	})
	testFx.Exec(t, `UPDATE autopilot_run SET task_id = $1 WHERE id = $2`, task, run)
	testFx.Exec(t, `UPDATE autopilot SET execution_mode = 'create_issue' WHERE id = $1`, fx.autopilot)
	syncer := &fakeSyncer{}
	svc := newTestService(nil, syncer)
	if err := svc.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	for _, got := range syncer.tasks {
		if uuidEqual(got.ID, task) {
			return
		}
	}
	t.Fatalf("completed task linked to active run was never synchronized after mode edit; task calls=%d", len(syncer.tasks))
}

// R4: the persisted cursor must carry the scan position ACROSS ticks, so a
// full page budget of long-running candidates cannot starve the row behind
// them — two ticks reach candidates past a 20,000-row budget.
func TestRereviewStaleIssueScanCrossesPassBudget(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	resetScanCursor(t, scannerIssueStatus)
	fx := newMDFixture(t, "rereview-scan-budget", nil)
	ws := testFx.Insert(t, "workspace", testutil.Cols{
		"name": "rereview bulk scan", "slug": "rereview-bulk-" + fx.autopilot, "description": "", "issue_prefix": "RB",
	})
	testFx.Exec(t, `UPDATE autopilot SET workspace_id = $1 WHERE id = $2`, ws, fx.autopilot)
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM autopilot_run WHERE autopilot_id = $1`, fx.autopilot); err != nil {
			t.Error(err)
		}
		if _, err := testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1`, ws); err != nil {
			t.Error(err)
		}
		testFx.Exec(t, `DELETE FROM labrastro_message_scan_cursor WHERE scanner = $1`, scannerIssueStatus)
	})
	// Nonterminal issues legitimately remain candidates after every sync.
	// Two ticks at the full per-tick budget cover 2× the candidate count.
	nonterminal := scanRowBudgetPerTick * 2
	testFx.Exec(t, `WITH inserted AS (
		INSERT INTO issue (workspace_id, number, title, status, priority, creator_type, creator_id, position, origin_type, updated_at)
		SELECT $1::uuid, n, 'rereview long running', 'in_progress', 'none', 'member', $2::uuid, 0, 'autopilot', now() - interval '1 day'
		FROM generate_series(1, $4::integer) AS n RETURNING id
	) INSERT INTO autopilot_run (autopilot_id, source, status, issue_id)
	SELECT $3::uuid, 'schedule', 'issue_created', id FROM inserted`, ws, testUID, fx.autopilot, nonterminal)
	terminal := testFx.Issue(t, "rereview missed after budget", testutil.Cols{
		"workspace_id": ws, "status": "done", "origin_type": "autopilot",
	})
	fx.run(t, "issue_created", testutil.Cols{"issue_id": terminal, "completed_at": nil, "result": nil})
	syncer := &fakeSyncer{}
	svc := newTestService(nil, syncer)
	for i := 0; i < 2; i++ {
		if err := svc.ScanOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, got := range syncer.issues {
		if uuidEqual(got.ID, terminal) {
			return
		}
	}
	t.Fatalf("%d nonterminal candidates starved the terminal issue across two ticks; calls=%d", nonterminal, len(syncer.issues))
}

type rereviewVerifier struct{ onGroup func() error }

func (v rereviewVerifier) VerifyGroupTarget(context.Context, VerifyTargetRequest) error {
	return v.onGroup()
}
func (v rereviewVerifier) VerifyTopicTarget(_ context.Context, r VerifyTargetRequest) (string, error) {
	return r.ChatID, nil
}

// R3: test-send must not recreate rows in a workspace whose deletion
// committed while the remote verification was in flight.
func TestRereviewTestSendCannotRecreateDeletedWorkspace(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newMDFixture(t, "rereview-test-delete", nil)
	ws := testFx.Insert(t, "workspace", testutil.Cols{
		"name": "rereview test delete", "slug": "rereview-delete-" + fx.autopilot, "description": "", "issue_prefix": "RD",
	})
	testFx.Exec(t, `UPDATE autopilot SET workspace_id = $1 WHERE id = $2`, ws, fx.autopilot)
	testFx.Exec(t, `UPDATE channel_installation SET workspace_id = $1 WHERE id = $2`, ws, fx.install)
	routeID := fx.groupRoute(t, "oc_rereview_test_delete")
	testFx.Exec(t, `UPDATE labrastro_message_route SET workspace_id = $1 WHERE id = $2`, ws, routeID)
	svc := newTestService(&fakeSender{}, nil)
	route, err := svc.Queries.GetLabrastroMessageRoute(ctx, db.GetLabrastroMessageRouteParams{ID: uuidOf(t, routeID), WorkspaceID: uuidOf(t, ws)})
	if err != nil {
		t.Fatal(err)
	}
	// ResolveTarget has loaded installation before it calls the remote verifier.
	// Commit a deletion while that remote call is in progress, then return success.
	called := false
	svc.Verifier = rereviewVerifier{onGroup: func() error {
		called = true
		tx, err := testPool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		for _, table := range []string{"labrastro_message_receipt", "labrastro_message_delivery", "labrastro_message_route", "channel_installation", "autopilot"} {
			if _, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE workspace_id = $1", ws); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, ws); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}}
	_, sendErr := svc.TestSend(ctx, route, loadMember(t))
	if !called {
		t.Fatal("deletion callback was not reached")
	}
	if n := countDeliveries(t, `workspace_id = $1`, ws); n != 0 {
		t.Fatalf("test-send recreated %d orphan delivery after deletion committed (returned error=%v)", n, sendErr)
	}
}

// R13: an old test-send request whose lease expired and was re-claimed must
// not overwrite the new owner's state — and losing that write is a benign
// race, not a caller-facing error.
func TestRereviewTestSendCannotOverwriteNewLease(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newMDFixture(t, "rereview-test-lease", nil)
	routeID := fx.groupRoute(t, "oc_rereview_test_lease")
	sender := &fakeSender{}
	svc := newTestService(sender, nil)
	route, err := svc.Queries.GetLabrastroMessageRoute(ctx, db.GetLabrastroMessageRouteParams{ID: uuidOf(t, routeID), WorkspaceID: uuidOf(t, testWSID)})
	if err != nil {
		t.Fatal(err)
	}
	var claimed db.LabrastroMessageDelivery
	sender.fn = func(req SendRequest) (SendResult, error) {
		var deliveryID string
		if err := testPool.QueryRow(ctx, `SELECT id FROM labrastro_message_delivery WHERE route_id = $1 AND source_kind = 'test_send'`, routeID).Scan(&deliveryID); err != nil {
			t.Fatal(err)
		}
		testFx.Exec(t, `UPDATE labrastro_message_delivery SET lease_expires_at = now() - interval '1 second' WHERE id = $1`, deliveryID)
		if err := svc.requeueExpiredClaims(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.RetryDelivery(ctx, uuidOf(t, testWSID), uuidOf(t, fx.autopilot), uuidOf(t, deliveryID)); err != nil {
			t.Fatal(err)
		}
		claimed, err = svc.Queries.ClaimDueLabrastroMessageDelivery(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !uuidEqual(claimed.ID, deliveryID) {
			t.Fatal("unexpected queue row")
		}
		return SendResult{}, &SendError{Class: ClassPermanent, Err: errors.New("synthetic old request rejection")}
	}
	if _, err := svc.TestSend(ctx, route, loadMember(t)); err != nil {
		t.Fatal(err)
	}
	current, err := svc.Queries.GetLabrastroMessageDelivery(ctx, db.GetLabrastroMessageDeliveryParams{ID: claimed.ID, WorkspaceID: uuidOf(t, testWSID)})
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != DeliveryStatusSending || current.LeaseToken != claimed.LeaseToken {
		t.Fatalf("old test-send overwrote new claim: status=%s token_matches_new=%v", current.Status, current.LeaseToken == claimed.LeaseToken)
	}
}

// R11: a linked task's raw failure text (paths, credentials) must never
// enter the external status card, even through the REAL sync boundary.
func TestRereviewFailedTaskRawReasonStaysOutOfStatusCard(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newMDFixture(t, "rereview-error-boundary", nil)
	route := fx.groupRoute(t, "oc_rereview_error_boundary")
	testFx.Exec(t, `UPDATE labrastro_message_route SET conditions = 'failure' WHERE id = $1`, route)
	issue := testFx.Issue(t, "rereview synthetic failure", testutil.Cols{"status": "in_progress", "origin_type": "autopilot"})
	run := fx.run(t, "issue_created", testutil.Cols{"issue_id": issue, "result": nil, "completed_at": nil})
	const rawReason = "issue execution failed in /workspaces/synthetic-private-path"
	taskID := testFx.Insert(t, "agent_task_queue", testutil.Cols{
		"agent_id": testAgent, "issue_id": issue, "status": "failed", "error": rawReason, "completed_at": testutil.Raw("now()"), "priority": 0,
	})
	testFx.Exec(t, `UPDATE autopilot_run SET task_id=$1 WHERE id=$2`, taskID, run)
	q := db.New(testPool)
	task, err := q.GetAgentTask(ctx, uuidOf(t, taskID))
	if err != nil {
		t.Fatal(err)
	}
	apSvc := service.NewAutopilotService(q, testPool, events.New(), nil)
	apSvc.SyncRunFromLinkedIssueTask(ctx, task)
	sender := &fakeSender{}
	svc := newTestService(sender, nil)
	if err := svc.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	for _, req := range sender.requests() {
		if strings.Contains(string(req.Message.Body), "synthetic-private-path") {
			t.Fatalf("raw task failure entered external status card for run %s: %q", run, req.Message.Body)
		}
	}
	if sender.count() != 1 {
		t.Fatalf("expected one synthetic send; got %d", sender.count())
	}
}

// R12: the compensator must reconnect a lost linked-task failure to the
// EXISTING SyncRunFromLinkedIssueTask state machine.
func TestRereviewScannerRecoversLinkedIssueTaskFailure(t *testing.T) {
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	resetScanCursor(t, scannerLinkedFailure)
	fx := newMDFixture(t, "rereview-linked-task", nil)
	issue := testFx.Issue(t, "rereview linked task", testutil.Cols{"status": "in_progress", "origin_type": "autopilot"})
	run := fx.run(t, "issue_created", testutil.Cols{"issue_id": issue, "result": nil, "completed_at": nil})
	taskID := testFx.Insert(t, "agent_task_queue", testutil.Cols{
		"agent_id": testAgent, "issue_id": issue, "status": "failed", "error": "synthetic terminal task failure", "completed_at": testutil.Raw("now()"), "priority": 0,
	})
	testFx.Exec(t, `UPDATE autopilot_run SET task_id=$1 WHERE id=$2`, taskID, run)
	q := db.New(testPool)
	apSvc := service.NewAutopilotService(q, testPool, events.New(), nil)
	svc := newTestService(nil, apSvc)
	if err := svc.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	afterScan, err := q.GetAutopilotRun(ctx, uuidOf(t, run))
	if err != nil {
		t.Fatal(err)
	}
	// Verify the existing state machine accepts this persisted terminal task.
	task, err := q.GetAgentTask(ctx, uuidOf(t, taskID))
	if err != nil {
		t.Fatal(err)
	}
	apSvc.SyncRunFromLinkedIssueTask(ctx, task)
	afterDirect, err := q.GetAutopilotRun(ctx, uuidOf(t, run))
	if err != nil {
		t.Fatal(err)
	}
	if afterDirect.Status != "failed" {
		t.Fatalf("setup: existing sync did not finalize task: %s", afterDirect.Status)
	}
	if afterScan.Status != "failed" {
		t.Fatalf("event loss left run %s after scan; existing linked-task sync finalizes it as %s", afterScan.Status, afterDirect.Status)
	}
}

// Delay only the final SQL statement. Each service still performs its real
// active-run lookup and status mapping first, modeling two concurrent replicas.
type rereviewGateDB struct {
	*pgxpool.Pool
	ready   chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *rereviewGateDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if strings.Contains(query, "-- name: UpdateAutopilotRunTerminalWithQuota") {
		g.once.Do(func() { close(g.ready) })
		select {
		case <-g.release:
		case <-ctx.Done():
		}
	}
	return g.Pool.QueryRow(ctx, query, args...)
}

// R6: the terminal boundary's first-write-wins guard — two concurrent real
// syncers cannot overwrite the first terminal's structured status.
func TestRereviewFirstTerminalStatusCannotBeOverwrittenConcurrently(t *testing.T) {
	for _, secondStatus := range []string{"done", "cancelled"} {
		t.Run(secondStatus, func(t *testing.T) {
			rereviewFirstTerminalRace(t, secondStatus)
		})
	}
}

func rereviewFirstTerminalRace(t *testing.T, secondStatus string) {
	t.Helper()
	if testPool == nil {
		t.Skip("database not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fx := newMDFixture(t, "rereview-first-concurrent", nil)
	issueID := testFx.Issue(t, "rereview first concurrent", testutil.Cols{"status": "in_review", "origin_type": "autopilot"})
	run := fx.run(t, "issue_created", testutil.Cols{"issue_id": issueID, "result": nil, "completed_at": nil})
	q := db.New(testPool)
	issue, err := q.GetIssue(ctx, uuidOf(t, issueID))
	if err != nil {
		t.Fatal(err)
	}
	gateA := &rereviewGateDB{Pool: testPool, ready: make(chan struct{}), release: make(chan struct{})}
	gateB := &rereviewGateDB{Pool: testPool, ready: make(chan struct{}), release: make(chan struct{})}
	var releaseA, releaseB sync.Once
	doneA, doneB := make(chan struct{}), make(chan struct{})
	defer func() {
		releaseA.Do(func() { close(gateA.release) })
		releaseB.Do(func() { close(gateB.release) })
		cancel()
		<-doneA
		<-doneB
	}()
	serviceA := service.NewAutopilotService(db.New(gateA), testPool, events.New(), nil)
	serviceB := service.NewAutopilotService(db.New(gateB), testPool, events.New(), nil)
	secondIssue := issue
	secondIssue.Status = secondStatus
	go func() { defer close(doneA); serviceA.SyncRunFromIssue(ctx, issue) }()
	go func() { defer close(doneB); serviceB.SyncRunFromIssue(ctx, secondIssue) }()
	for _, ready := range []chan struct{}{gateA.ready, gateB.ready} {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatal("both terminal writes did not reach synchronization gate")
		}
	}
	releaseA.Do(func() { close(gateA.release) })
	<-doneA
	first, err := q.GetAutopilotRun(ctx, uuidOf(t, run))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first.Result), "in_review") {
		t.Fatalf("first write did not freeze in_review: %s", first.Result)
	}
	releaseB.Do(func() { close(gateB.release) })
	<-doneB
	last, err := q.GetAutopilotRun(ctx, uuidOf(t, run))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(last.Result), "in_review") {
		t.Fatalf("second stale writer replaced first terminal: first=%s last=%s", first.Result, last.Result)
	}
	if last.Status != "completed" || !last.CompletedAt.Time.Equal(first.CompletedAt.Time) {
		t.Fatalf("stale %s event overwrote the first terminal state: %+v", secondStatus, last)
	}
}
