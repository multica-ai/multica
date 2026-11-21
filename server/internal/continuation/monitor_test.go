package continuation

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDecideRemainder(t *testing.T) {
	now := time.Now().UTC()
	later := now.Add(time.Hour)
	past := now.Add(-time.Second)
	base := Contract{DueAt: later, WaitKind: "none", ExternalOutcome: "none"}
	cases := []struct {
		name, status, wait, outcome, want string
		until                             *time.Time
		resolved, changed, missing        bool
	}{
		{name: "handshake before model output", status: "failed", want: "needs_reconcile"},
		{name: "worktree failure", status: "failed", want: "needs_reconcile"},
		{name: "completed is not resolved", status: "completed", want: "needs_reconcile"},
		{name: "cancelled", status: "cancelled", want: "needs_reconcile"},
		{name: "queued successor", status: "queued", want: "receiver_queued"},
		{name: "running successor including offline daemon", status: "running", want: "receiver_running"},
		{name: "dispatched successor", status: "dispatched", want: "receiver_queued"},
		{name: "local directory wait", status: "waiting_local_directory", want: "receiver_queued"},
		{name: "CI wait", wait: "ci", until: &later, want: "awaiting_event"},
		{name: "owner wait", wait: "owner", until: &later, want: "awaiting_event"},
		{name: "quota wait", wait: "quota", until: &later, want: "awaiting_event"},
		{name: "expired wait", wait: "ci", until: &past, want: "needs_reconcile"},
		{name: "external operation outcome unknown", outcome: "unknown", want: "needs_reconcile"},
		{name: "explicit covered result", resolved: true, want: "resolved"},
		{name: "edited handoff", changed: true, want: "needs_reconcile"},
		{name: "missing receiver", missing: true, want: "needs_reconcile"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			if tc.wait != "" {
				c.WaitKind = tc.wait
			}
			c.WaitUntil = tc.until
			if tc.outcome != "" {
				c.ExternalOutcome = tc.outcome
			}
			c.CoverageConfirmed = tc.resolved
			state, _ := decide(c, facts{status: tc.status, receiverPresent: tc.status != "", handoffChanged: tc.changed, receiverMissing: tc.missing}, now)
			if state != tc.want {
				t.Fatalf("got %s, want %s", state, tc.want)
			}
		})
	}
	base.DueAt = past
	if _, reason := decide(base, facts{}, now); reason != "deadline" {
		t.Fatalf("expired deadline: %s", reason)
	}
}

func fixture(t *testing.T) (*testutil.Fixture, *Monitor, Contract, string, string) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("explicit migrated test DATABASE_URL required")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = pool.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	f := testutil.New(pool, "", "")
	f.UserID = f.User(t, "Continuation owner", uuid.NewString()+"@example.test")
	f.WorkspaceID = f.Workspace(t, "Continuation test", uuid.NewString())
	f.Member(t, f.WorkspaceID, f.UserID, "owner")
	project := f.Project(t, "Pilot")
	issue := f.Issue(t, "Repair", testutil.Cols{"project_id": project, "status": "in_review"})
	runtime := f.Runtime(t, "Receiver runtime")
	agent := f.Agent(t, "Receiver", runtime)
	receiver := f.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": runtime, "status": "failed", "failure_reason": "environment_prepare_failed", "completed_at": testutil.Raw("now()")})
	c := Contract{Key: Key{WorkspaceID: f.WorkspaceID, IssueID: issue, ChainID: uuid.NewString(), RemainderID: uuid.NewString()}, Generation: 1, OwnerID: f.UserID, ReceiverTaskID: receiver, DueAt: time.Now().Add(time.Hour), WaitKind: "none", ExternalOutcome: "none", Evidence: "Explicit repair handoff"}
	f.Cleanup(t, "DELETE FROM inbox_item WHERE issue_id=$1", issue)
	f.Cleanup(t, "DELETE FROM issue_continuation_incident WHERE issue_id=$1", issue)
	f.Cleanup(t, "DELETE FROM issue_continuation WHERE issue_id=$1", issue)
	return f, &Monitor{Pool: pool, ProjectID: project}, c, agent, runtime
}

func put(t *testing.T, m *Monitor, c Contract, version int64) Record {
	t.Helper()
	r, err := m.Put(context.Background(), Change{Contract: c, ExpectedVersion: version})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func current(t *testing.T, m *Monitor, k Key) Record {
	t.Helper()
	r, err := m.Get(context.Background(), k)
	must(t, err)
	return r
}

func TestIncidentDeliveryRaceRestartAndRollback(t *testing.T) {
	f, m, c, _, runtime := fixture(t)
	ctx := context.Background()
	put(t, m, c, 0)
	must(t, m.Reconcile(ctx, c.Key))
	if got := f.Count(t, "SELECT count(*) FROM issue_continuation_incident WHERE issue_id=$1", c.IssueID); got != 1 {
		t.Fatalf("incident count %d", got)
	}
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatalf("record-only rollout delivered %d rows", got)
	}
	// Host loss cannot remove a pending incident; no runtime/worktree is read
	// or started to deliver it. A fresh monitor represents a server restart.
	f.Exec(t, "UPDATE agent_runtime SET status='offline' WHERE id=$1", runtime)
	restarted := &Monitor{Pool: m.Pool, ProjectID: m.ProjectID, Notify: true}
	errs := make(chan error, 12)
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(2)
		go func() { defer wg.Done(); errs <- restarted.Reconcile(ctx, c.Key) }()
		go func() { defer wg.Done(); errs <- restarted.Deliver(ctx, c.Key) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	if got := f.Count(t, "SELECT count(*) FROM issue_continuation_incident WHERE issue_id=$1", c.IssueID); got != 1 {
		t.Fatalf("race created %d incidents", got)
	}
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1 AND recipient_id=$2 AND recipient_type='member'", c.IssueID, f.UserID); got != 1 {
		t.Fatalf("race created %d owner signals", got)
	}
	var terminal, delivered time.Time
	must(t, f.Pool.QueryRow(ctx, "SELECT terminal_at,notified_at FROM issue_continuation_incident WHERE issue_id=$1", c.IssueID).Scan(&terminal, &delivered))
	t.Logf("terminal=%s inbox_receipt=%s latency=%s (database delivery, not human read)", terminal.Format(time.RFC3339Nano), delivered.Format(time.RFC3339Nano), delivered.Sub(terminal))
	if delivered.Sub(terminal) > 5*time.Minute {
		t.Fatal("test database delivery exceeded five minutes")
	}
	// Lost caller ACK/broadcast after commit and runtime reconnect: same row.
	f.Exec(t, "UPDATE agent_runtime SET status='online' WHERE id=$1", runtime)
	must(t, (&Monitor{Pool: m.Pool, ProjectID: m.ProjectID, Notify: true}).Deliver(ctx, c.Key))
	m.Notify = false
	must(t, m.Deliver(ctx, c.Key))
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 1 {
		t.Fatalf("reconnect/rollback duplicated %d rows", got)
	}
	if got := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", c.IssueID); got != 1 {
		t.Fatal("detector created a task")
	}
}

func TestCrashBeforeACKRollsBackInboxAndCanResume(t *testing.T) {
	f, m, c, _, _ := fixture(t)
	ctx := context.Background()
	m.Notify = true
	put(t, m, c, 0)
	must(t, m.Reconcile(ctx, c.Key))
	// Inject a crash-equivalent transaction failure after the inbox insert,
	// precisely when the durable receipt would be acknowledged.
	name := "fail_ack_" + uuid.NewString()[:8]
	f.Exec(t, `CREATE FUNCTION `+name+`() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.issue_id='`+c.IssueID+`' AND NEW.outbox_state='delivered' THEN RAISE EXCEPTION 'injected before ACK'; END IF; RETURN NEW; END $$`)
	t.Cleanup(func() { f.Pool.Exec(ctx, "DROP FUNCTION IF EXISTS "+name+"() CASCADE") })
	f.Exec(t, `CREATE TRIGGER `+name+` BEFORE UPDATE ON issue_continuation_incident FOR EACH ROW EXECUTE FUNCTION `+name+`()`)
	if err := m.Deliver(ctx, c.Key); err == nil {
		t.Fatal("injected failure was not observed")
	}
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatal("inbox survived rolled-back ACK")
	}
	f.Exec(t, "DROP TRIGGER "+name+" ON issue_continuation_incident")
	must(t, m.Deliver(ctx, c.Key))
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 1 {
		t.Fatalf("resume delivered %d rows", got)
	}
}

func TestSuccessorAndWaitRecheckedBeforeSend(t *testing.T) {
	f, m, c, agent, runtime := fixture(t)
	ctx := context.Background()
	r := put(t, m, c, 0)
	must(t, m.Reconcile(ctx, c.Key))
	c.ReceiverTaskID = f.Task(t, agent, testutil.Cols{"issue_id": c.IssueID, "runtime_id": runtime})
	r = put(t, m, c, r.Version)
	m.Notify = true
	must(t, m.Deliver(ctx, c.Key))
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatal("notified despite accepted successor")
	}
	for _, kind := range []string{"ci", "owner", "quota"} {
		c.ReceiverTaskID = ""
		c.WaitKind = kind
		future := time.Now().Add(time.Hour)
		c.WaitUntil = &future
		r = put(t, m, c, r.Version)
		must(t, m.Deliver(ctx, c.Key))
		r = current(t, m, c.Key)
		if r.State != "awaiting_event" {
			t.Fatalf("%s wait state %s", kind, r.State)
		}
	}
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatal("notified during confirmed waits")
	}
	c.WaitKind = "none"
	c.WaitUntil = nil
	c.ExternalOutcome = "unknown"
	put(t, m, c, r.Version)
	must(t, m.Deliver(ctx, c.Key))
	if got := current(t, m, c.Key).State; got != "needs_reconcile" {
		t.Fatalf("unknown external outcome: %s", got)
	}
	if got := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", c.IssueID); got != 2 {
		t.Fatal("unknown outcome repeated an operation")
	}
}

func TestCASResolutionAndGeneration(t *testing.T) {
	f, m, c, _, _ := fixture(t)
	ctx := context.Background()
	r := put(t, m, c, 0)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := m.Put(ctx, Change{Contract: c, ExpectedVersion: r.Version})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	conflicts := 0
	for err := range errs {
		if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			must(t, err)
		}
	}
	if conflicts != 1 {
		t.Fatalf("CAS conflicts %d", conflicts)
	}
	r = current(t, m, c.Key)
	c.Generation++
	if _, err := m.Put(ctx, Change{Contract: c, ExpectedVersion: r.Version, NewGeneration: true}); err == nil {
		t.Fatal("reset unresolved generation")
	}
	c.Generation--
	c.CoverageConfirmed = true
	c.ResultCommentID = uuid.NewString()
	if _, err := m.Put(ctx, Change{Contract: c, ExpectedVersion: r.Version}); err == nil {
		t.Fatal("accepted an invented result")
	}
	c.ResultCommentID = f.Comment(t, c.IssueID, "Verified result covers the enrolled repair.")
	r = put(t, m, c, r.Version)
	if r.State != "resolved" {
		t.Fatalf("resolution state %s", r.State)
	}
	c.Generation++
	if _, err := m.Put(ctx, Change{Contract: c, ExpectedVersion: r.Version, NewGeneration: true}); err == nil {
		t.Fatal("new remainder inherited the previous resolution")
	}
	c.Generation--
	c.CoverageConfirmed = false
	c.ResultCommentID = ""
	if _, err := m.Put(ctx, Change{Contract: c, ExpectedVersion: r.Version}); err == nil {
		t.Fatal("reopened resolved generation")
	}
	c.Generation++
	_, err := m.Put(ctx, Change{Contract: c, ExpectedVersion: r.Version, NewGeneration: true})
	must(t, err)
	if got := f.Count(t, "SELECT jsonb_array_length(history) FROM issue_continuation WHERE issue_id=$1", c.IssueID); got < 3 {
		t.Fatal("explicit amendment history lost")
	}
}

func TestWakeupReceiptSurvivesDisableAndWorktreeRecovery(t *testing.T) {
	f, m, c, agent, _ := fixture(t)
	ctx := context.Background()
	receiver := c.ReceiverTaskID
	c.ReceiverTaskID = ""
	c.WakeupID = f.Insert(t, "issue_wakeup", testutil.Cols{"id": uuid.NewString(), "workspace_id": f.WorkspaceID, "issue_id": c.IssueID, "agent_id": agent, "created_by": f.UserID, "instruction": "Continue repair", "kind": "event", "mode": "once", "enabled": true})
	c.WakeupRevision = 1
	r := put(t, m, c, 0)
	f.Exec(t, "UPDATE issue_wakeup SET last_task_id=$2,enabled=false WHERE id=$1", c.WakeupID, receiver)
	// Restoring the worktree does not fabricate a result from the failed run.
	must(t, m.Reconcile(ctx, c.Key))
	r = current(t, m, c.Key)
	if r.State != "needs_reconcile" || r.ReceiverTaskID != receiver {
		t.Fatalf("lost consumed wakeup: %+v", r)
	}
	f.Exec(t, "UPDATE issue_wakeup SET revision=revision+1 WHERE id=$1", c.WakeupID)
	must(t, m.Reconcile(ctx, c.Key))
	var reason string
	must(t, f.Pool.QueryRow(ctx, "SELECT reason FROM issue_continuation_incident WHERE issue_id=$1", c.IssueID).Scan(&reason))
	if reason != "handoff_changed" {
		t.Fatalf("revision reason %s", reason)
	}
}

func TestProjectScopePrivateAccessAndDeletion(t *testing.T) {
	f, m, c, _, _ := fixture(t)
	ctx := context.Background()
	put(t, m, c, 0)
	must(t, m.Reconcile(ctx, c.Key))
	foreign := *m
	foreign.ProjectID = uuid.NewString()
	foreign.Notify = true
	if err := foreign.Deliver(ctx, c.Key); err == nil {
		t.Fatal("delivered outside configured project")
	}
	f.Exec(t, "UPDATE project SET visibility='private' WHERE id=$1", m.ProjectID)
	m.Notify = true
	if err := m.Deliver(ctx, c.Key); err == nil {
		t.Fatal("owner role bypassed private project membership")
	}
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatal("private metadata leaked")
	}
	var issue, workspace pgtype.UUID
	must(t, issue.Scan(c.IssueID))
	must(t, workspace.Scan(c.WorkspaceID))
	must(t, db.New(f.Pool).DeleteIssue(ctx, db.DeleteIssueParams{ID: issue, WorkspaceID: workspace}))
	if got := f.Count(t, "SELECT count(*) FROM issue_continuation_incident WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatal("issue deletion retained private incident")
	}
	if got := f.Count(t, "SELECT count(*) FROM issue_continuation WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatal("issue deletion retained private contract")
	}
}

func TestDisabledMonitorDoesNotQueryDatabase(t *testing.T) {
	m := &Monitor{}
	must(t, m.Tick(context.Background()))
	must(t, m.Deliver(context.Background(), Key{}))
}

func TestResolutionRequiresTheExactReceiverRun(t *testing.T) {
	f, m, c, agent, runtime := fixture(t)
	r := put(t, m, c, 0)
	other := f.Task(t, agent, testutil.Cols{"issue_id": c.IssueID, "runtime_id": runtime})
	c.CoverageConfirmed = true
	c.ResultCommentID = f.Comment(t, c.IssueID, "Result from the same agent's unrelated run", testutil.Cols{
		"author_type": "agent", "author_id": agent, "source_task_id": other,
	})
	if _, err := m.Put(context.Background(), Change{Contract: c, ExpectedVersion: r.Version}); err == nil {
		t.Fatal("accepted a different run's result")
	}
	c.ResultCommentID = f.Comment(t, c.IssueID, "Verified receiver result covers the remainder", testutil.Cols{
		"author_type": "agent", "author_id": agent, "source_task_id": c.ReceiverTaskID,
	})
	if got := put(t, m, c, r.Version).State; got != "resolved" {
		t.Fatalf("exact receiver result: %s", got)
	}
}

func TestGuestRecipientNeedsProjectMembership(t *testing.T) {
	f, m, c, _, _ := fixture(t)
	put(t, m, c, 0)
	f.Exec(t, "UPDATE member SET role='guest' WHERE workspace_id=$1 AND user_id=$2", c.WorkspaceID, c.OwnerID)
	m.Notify = true
	if err := m.Deliver(context.Background(), c.Key); err == nil {
		t.Fatal("guest without project membership received a signal")
	}
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatal("guest access leaked private metadata")
	}
}

func TestUncommittedSuccessorBlocksDeliveryUntilVisible(t *testing.T) {
	f, m, c, agent, runtime := fixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	put(t, m, c, 0)
	must(t, m.Reconcile(ctx, c.Key))
	tx, err := f.Pool.Begin(ctx)
	must(t, err)
	defer tx.Rollback(context.Background())
	// Use the production enqueue fence; its issue KEY SHARE must conflict
	// with delivery's final decision, even before this child is visible.
	child := uuid.NewString()
	_, err = tx.Exec(ctx, `INSERT INTO agent_task_queue(id,agent_id,issue_id,runtime_id,status,priority,retry_of_task_id,attempt)
	    SELECT $1,$2,$3,$4,'queued',0,$5,2 WHERE lock_task_owner_rows($2,$3,$4)`, child, agent, c.IssueID, runtime, c.ReceiverTaskID)
	must(t, err)
	f.Cleanup(t, "DELETE FROM agent_task_queue WHERE id=$1", child)
	var writerPID int32
	must(t, tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&writerPID))
	m.Notify = true
	done := make(chan error, 1)
	go func() { done <- m.Deliver(ctx, c.Key) }()
	for {
		var blocked bool
		must(t, f.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))", writerPID).Scan(&blocked))
		if blocked {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("delivery did not wait for the successor: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	must(t, tx.Commit(ctx))
	must(t, <-done)
	if got := current(t, m, c.Key); got.ReceiverTaskID != child || got.State != "receiver_queued" {
		t.Fatalf("delivery missed the committed successor: %+v", got)
	}
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatal("notified while successor enqueue was committing")
	}
}

func TestExistingPlatformRetryIsAnActiveSuccessor(t *testing.T) {
	f, m, c, agent, runtime := fixture(t)
	ctx := context.Background()
	put(t, m, c, 0)
	must(t, m.Reconcile(ctx, c.Key))
	child := f.Task(t, agent, testutil.Cols{"issue_id": c.IssueID, "runtime_id": runtime, "retry_of_task_id": c.ReceiverTaskID, "attempt": 2})
	m.Notify = true
	must(t, m.Deliver(ctx, c.Key))
	r := current(t, m, c.Key)
	if r.State != "receiver_queued" || r.ReceiverTaskID != child {
		t.Fatalf("active retry was ignored: %+v", r)
	}
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatal("notified while existing retry is active")
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1", child)
	must(t, m.Deliver(ctx, c.Key))
	if got := current(t, m, c.Key).State; got != "needs_reconcile" {
		t.Fatal("completed retry was treated as resolved")
	}
}

func TestTickScopeAndExpiredDeadline(t *testing.T) {
	f, m, c, _, _ := fixture(t)
	ctx := context.Background()
	c.ReceiverTaskID = ""
	c.DueAt = time.Now().Add(-time.Second)
	put(t, m, c, 0)
	other := *m
	other.ProjectID = uuid.NewString()
	other.Notify = true
	must(t, other.Tick(ctx))
	if got := f.Count(t, "SELECT count(*) FROM issue_continuation_incident WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatal("tick crossed project scope")
	}
	m.Notify = true
	must(t, m.Tick(ctx))
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 1 {
		t.Fatalf("deadline tick delivered %d rows", got)
	}
}
