package continuation

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestStagedWorkspaceDeletionRemovesContinuationOnlyInTarget(t *testing.T) {
	f, m, c, _, _ := fixture(t)
	neighbor, other, otherContract, _, _ := fixture(t)
	ctx := context.Background()
	put(t, m, c, 0)
	put(t, other, otherContract, 0)
	must(t, m.Reconcile(ctx, c.Key))
	must(t, other.Reconcile(ctx, otherContract.Key))
	tx, err := f.Pool.Begin(ctx)
	must(t, err)
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, "SELECT id FROM workspace WHERE id=$1 FOR UPDATE", c.WorkspaceID)
	must(t, err)
	var workspace pgtype.UUID
	must(t, workspace.Scan(c.WorkspaceID))
	must(t, db.New(tx).DeleteWorkspaceIssueRoots(ctx, workspace))
	must(t, tx.Commit(ctx))
	for _, table := range []string{"issue", "issue_continuation", "issue_continuation_incident"} {
		if got := f.Count(t, "SELECT count(*) FROM "+table+" WHERE workspace_id=$1", c.WorkspaceID); got != 0 {
			t.Errorf("staged deletion retained %d %s rows", got, table)
		}
		if got := neighbor.Count(t, "SELECT count(*) FROM "+table+" WHERE workspace_id=$1", otherContract.WorkspaceID); got != 1 {
			t.Errorf("staged deletion changed neighbor %s rows: %d", table, got)
		}
	}
}

func pendingOnce(t *testing.T, f *testutil.Fixture, c *Contract, agent string) string {
	t.Helper()
	c.ReceiverTaskID = ""
	c.WakeupRevision = 1
	c.WakeupID = f.Insert(t, "issue_wakeup", testutil.Cols{
		"id": uuid.NewString(), "workspace_id": c.WorkspaceID, "issue_id": c.IssueID,
		"agent_id": agent, "created_by": c.OwnerID, "instruction": "Continue repair",
		"kind": "event", "mode": "once", "enabled": false,
	})
	return f.Insert(t, "issue_wakeup_receipt", testutil.Cols{
		"id": uuid.NewString(), "wakeup_id": c.WakeupID, "revision": 1,
		"event_key": uuid.NewString(), "event_type": "task.terminal", "payload": testutil.Raw("'{}'::jsonb"),
	})
}

func TestOncePendingReceiptEnrollmentAndReservedReceiver(t *testing.T) {
	f, m, c, agent, runtime := fixture(t)
	ctx := context.Background()
	receipt := pendingOnce(t, f, &c, agent)
	m.Notify = true
	if r := put(t, m, c, 0); r.State != "awaiting_event" {
		t.Fatalf("pending once enrollment: %s", r.State)
	}
	must(t, m.Reconcile(ctx, c.Key))
	must(t, m.Deliver(ctx, c.Key))
	if got := f.Count(t, "SELECT count(*) FROM issue_continuation_incident WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatal("pending once receipt produced a false incident")
	}
	receiver := f.Task(t, agent, testutil.Cols{"issue_id": c.IssueID, "runtime_id": runtime})
	f.Exec(t, "UPDATE issue_wakeup_receipt SET task_id=$2 WHERE id=$1", receipt, receiver)
	for _, status := range []string{"queued", "dispatched"} {
		f.Exec(t, "UPDATE agent_task_queue SET status=$2 WHERE id=$1", receiver, status)
		must(t, m.Deliver(ctx, c.Key))
		if r := current(t, m, c.Key); r.State != "receiver_queued" || r.ReceiverTaskID != receiver {
			t.Fatalf("reserved %s receiver not tracked: %+v", status, r)
		}
	}
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatal("notified while the pending receipt had a live receiver")
	}
	f.Exec(t, "UPDATE issue_wakeup_receipt SET task_id=NULL WHERE id=$1", receipt)
	must(t, m.Reconcile(ctx, c.Key))
	if r := current(t, m, c.Key); r.State != "awaiting_event" || r.ReceiverTaskID != "" {
		t.Fatalf("released reservation kept a stale receiver: %+v", r)
	}
	f.Exec(t, "UPDATE issue_wakeup_receipt SET task_id=$2 WHERE id=$1", receipt, receiver)
	f.Exec(t, "UPDATE agent_task_queue SET status='failed',completed_at=now() WHERE id=$1", receiver)
	must(t, m.Deliver(ctx, c.Key))
	if got := f.Count(t, "SELECT count(*) FROM issue_continuation_incident WHERE issue_id=$1 AND reason='receiver_failed' AND outbox_state='delivered'", c.IssueID); got != 1 {
		t.Fatal("reserved receiver failure was hidden by pending receipt")
	}
}

func TestOncePendingReceiptDoesNotHideInvalidationOrDeadline(t *testing.T) {
	for _, tc := range []struct {
		name, update, reason string
		reserved             bool
	}{
		{"explicit disable", "UPDATE issue_wakeup SET disabled_at=now() WHERE id=$1", "handoff_changed", false},
		{"explicit disable reserved", "UPDATE issue_wakeup SET disabled_at=now() WHERE id=$1", "handoff_changed", true},
		{"revision changed", "UPDATE issue_wakeup SET revision=revision+1 WHERE id=$1", "handoff_changed", false},
		{"no pending receipt", "UPDATE issue_wakeup_receipt SET processed_at=now() WHERE wakeup_id=$1", "handoff_changed", false},
		{"expired deadline", "", "deadline", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, m, c, agent, runtime := fixture(t)
			receipt := pendingOnce(t, f, &c, agent)
			if tc.reserved {
				receiver := f.Task(t, agent, testutil.Cols{"issue_id": c.IssueID, "runtime_id": runtime, "status": "running"})
				f.Exec(t, "UPDATE issue_wakeup_receipt SET task_id=$2 WHERE id=$1", receipt, receiver)
			}
			c.DueAt = time.Now().Add(-time.Minute)
			put(t, m, c, 0)
			if tc.update != "" {
				f.Exec(t, tc.update, c.WakeupID)
				if _, err := m.Put(context.Background(), Change{Contract: c, ExpectedVersion: 1}); err == nil {
					t.Fatal("enrolled an invalidated wakeup")
				}
			}
			m.Notify = true
			must(t, m.Deliver(context.Background(), c.Key))
			if got := f.Count(t, "SELECT count(*) FROM issue_continuation_incident WHERE issue_id=$1 AND reason=$2 AND outbox_state='delivered'", c.IssueID, tc.reason); got != 1 {
				t.Fatalf("pending receipt hid %s", tc.reason)
			}
		})
	}
}

func TestDisabledIssuedWakeupTracksReceiver(t *testing.T) {
	for _, tc := range []struct {
		name, status, reservedStatus, want string
		retry                              bool
	}{
		{name: "running", status: "running", reservedStatus: "cancelled", want: "receiver_running"},
		{name: "cancelled", status: "cancelled", reservedStatus: "running", want: "needs_reconcile"},
		{name: "running retry", status: "failed", retry: true, want: "receiver_running"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, m, c, agent, runtime := fixture(t)
			ctx := context.Background()
			receiver := c.ReceiverTaskID
			f.Exec(t, "UPDATE agent_task_queue SET status=$2 WHERE id=$1", receiver, tc.status)
			pendingOnce(t, f, &c, agent)
			f.Exec(t, "UPDATE issue_wakeup SET last_task_id=$2 WHERE id=$1", c.WakeupID, receiver)
			if tc.retry {
				receiver = f.Task(t, agent, testutil.Cols{"issue_id": c.IssueID, "runtime_id": runtime, "status": "running", "retry_of_task_id": receiver, "attempt": 2})
			}
			put(t, m, c, 0)
			if tc.reservedStatus != "" {
				reserved := f.Task(t, agent, testutil.Cols{"issue_id": c.IssueID, "runtime_id": runtime, "status": tc.reservedStatus})
				f.Exec(t, "UPDATE issue_wakeup_receipt SET task_id=$2 WHERE wakeup_id=$1", c.WakeupID, reserved)
			}
			// Use the production close query: it also disables an already
			// consumed once rule, without stopping its running receiver.
			f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", c.IssueID)
			var issue pgtype.UUID
			must(t, issue.Scan(c.IssueID))
			must(t, db.New(f.Pool).DisableIssueWakeups(ctx, issue))
			m.Notify = true
			must(t, m.Deliver(ctx, c.Key))
			if r := current(t, m, c.Key); r.State != tc.want || r.ReceiverTaskID != receiver {
				t.Fatalf("disabled issued wakeup: state=%s receiver=%s, want %s/%s", r.State, r.ReceiverTaskID, tc.want, receiver)
			}
			if tc.status == "cancelled" {
				if got := f.Count(t, "SELECT count(*) FROM issue_continuation_incident WHERE issue_id=$1 AND reason='receiver_cancelled' AND outbox_state='delivered'", c.IssueID); got != 1 {
					t.Fatal("cancelled receiver was hidden or reported as handoff_changed")
				}
			} else if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 0 {
				t.Fatal("closing the wakeup notified despite its live receiver")
			}
		})
	}
}

func TestDisabledIssuedWakeupAllowsCoverage(t *testing.T) {
	f, m, c, agent, _ := fixture(t)
	ctx := context.Background()
	receiver := c.ReceiverTaskID
	f.Exec(t, "UPDATE agent_task_queue SET status='completed' WHERE id=$1", receiver)
	pendingOnce(t, f, &c, agent)
	f.Exec(t, "UPDATE issue_wakeup SET last_task_id=$2 WHERE id=$1", c.WakeupID, receiver)
	r := put(t, m, c, 0)
	f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", c.IssueID)
	var issue pgtype.UUID
	must(t, issue.Scan(c.IssueID))
	must(t, db.New(f.Pool).DisableIssueWakeups(ctx, issue))
	c = r.Contract
	c.CoverageConfirmed = true
	c.ResultCommentID = f.Comment(t, c.IssueID, "Verified result covers the remainder", testutil.Cols{
		"author_type": "agent", "author_id": agent, "source_task_id": receiver,
	})
	r = put(t, m, c, r.Version)
	if r.State != "resolved" || r.WakeupID != c.WakeupID {
		t.Fatalf("could not resolve the closed issue with its original binding: %+v", r)
	}
	m.Notify = true
	must(t, m.Deliver(ctx, c.Key))
	if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 0 {
		t.Fatal("resolved disabled handoff produced an inbox incident")
	}
	f.Exec(t, "UPDATE issue_wakeup SET revision=revision+1 WHERE id=$1", c.WakeupID)
	if _, err := m.Put(ctx, Change{Contract: c, ExpectedVersion: r.Version}); err == nil {
		t.Fatal("issued receiver bypassed the wakeup revision guard")
	}
}

func incidentSnapshot(t *testing.T, f *testutil.Fixture, c Contract) string {
	t.Helper()
	var snapshot string
	must(t, f.Pool.QueryRow(context.Background(), "SELECT row_to_json(i)::text FROM issue_continuation_incident i WHERE issue_id=$1 AND generation=$2", c.IssueID, c.Generation).Scan(&snapshot))
	return snapshot
}

func TestDeliveredIncidentIsImmutable(t *testing.T) {
	f, m, c, _, _ := fixture(t)
	m.Notify = true
	put(t, m, c, 0)
	must(t, m.Deliver(context.Background(), c.Key))
	before := incidentSnapshot(t, f, c)
	c.ExternalOutcome = "unknown"
	put(t, m, c, current(t, m, c.Key).Version)
	must(t, m.Deliver(context.Background(), c.Key))
	if after := incidentSnapshot(t, f, c); after != before {
		t.Fatalf("delivered incident changed:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestManualSuccessorRearmsAfterDeliveredIncident(t *testing.T) {
	for _, status := range []string{"failed", "completed"} {
		t.Run(status, func(t *testing.T) {
			f, m, c, agent, runtime := fixture(t)
			ctx := context.Background()
			m.Notify = true
			put(t, m, c, 0)
			must(t, m.Deliver(ctx, c.Key))
			before := incidentSnapshot(t, f, c)
			old := current(t, m, c.Key)
			next := c
			next.Generation++
			next.ReceiverTaskID = f.Task(t, agent, testutil.Cols{"issue_id": c.IssueID, "runtime_id": runtime})
			next.Evidence = "Operator verified this manual successor covers the unfinished repair"
			r, err := m.Put(ctx, Change{Contract: next, ExpectedVersion: old.Version, NewGeneration: true})
			must(t, err)
			if r.Generation != 2 || r.State != "receiver_queued" || r.CoverageConfirmed {
				t.Fatalf("manual rearm fabricated resolution: %+v", r)
			}
			must(t, m.Deliver(ctx, c.Key))
			if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 1 {
				t.Fatal("live successor caused a second signal")
			}
			f.Exec(t, "UPDATE agent_task_queue SET status=$2,completed_at=now() WHERE id=$1", next.ReceiverTaskID, status)
			must(t, m.Deliver(ctx, c.Key))
			must(t, m.Deliver(ctx, c.Key))
			if got := f.Count(t, "SELECT count(*) FROM inbox_item WHERE issue_id=$1", c.IssueID); got != 2 {
				t.Fatalf("second receiver outcome did not produce exactly one new signal: %d", got)
			}
			if after := incidentSnapshot(t, f, c); before != after {
				t.Fatal("new generation changed the old delivered incident")
			}
			if got := f.Count(t, "SELECT count(*) FROM issue_continuation WHERE issue_id=$1 AND history @> '[{\"state\":\"needs_reconcile\",\"contract\":{\"generation\":1}}]'", c.IssueID); got != 1 {
				t.Fatal("manual adoption lost the unresolved generation history")
			}
		})
	}
}

func TestManualRearmRequiresExplicitFreshLiveSuccessor(t *testing.T) {
	f, m, c, agent, runtime := fixture(t)
	ctx := context.Background()
	m.Notify = true
	put(t, m, c, 0)
	must(t, m.Deliver(ctx, c.Key))
	old := current(t, m, c.Key)
	next := c
	next.Generation++
	next.ReceiverTaskID = f.Task(t, agent, testutil.Cols{"issue_id": c.IssueID, "runtime_id": runtime})
	foreignIssue := f.Issue(t, "Other remainder", testutil.Cols{"project_id": m.ProjectID})
	foreignReceiver := f.Task(t, agent, testutil.Cols{"issue_id": foreignIssue, "runtime_id": runtime})
	for _, tc := range []struct {
		name   string
		mutate func(*Change)
	}{
		{"same generation", func(ch *Change) { ch.Generation--; ch.NewGeneration = false }},
		{"missing explicit flag", func(ch *Change) { ch.NewGeneration = false }},
		{"skipped generation", func(ch *Change) { ch.Generation++ }},
		{"stale CAS", func(ch *Change) { ch.ExpectedVersion-- }},
		{"same terminal receiver", func(ch *Change) { ch.ReceiverTaskID = c.ReceiverTaskID }},
		{"missing receiver", func(ch *Change) { ch.ReceiverTaskID = "" }},
		{"foreign receiver", func(ch *Change) { ch.ReceiverTaskID = foreignReceiver }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch := Change{Contract: next, ExpectedVersion: old.Version, NewGeneration: true}
			tc.mutate(&ch)
			if _, err := m.Put(ctx, ch); err == nil {
				t.Fatal("accepted invalid manual rearm")
			}
		})
	}
	livePrior := f.Task(t, agent, testutil.Cols{"issue_id": c.IssueID, "runtime_id": runtime, "status": "running", "retry_of_task_id": c.ReceiverTaskID, "attempt": 2})
	if _, err := m.Put(ctx, Change{Contract: next, ExpectedVersion: old.Version, NewGeneration: true}); err == nil {
		t.Fatal("replaced an active predecessor lineage with another receiver")
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='failed',completed_at=now() WHERE id=$1", livePrior)
	f.Exec(t, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1", next.ReceiverTaskID)
	if _, err := m.Put(ctx, Change{Contract: next, ExpectedVersion: old.Version, NewGeneration: true}); err == nil {
		t.Fatal("accepted terminal successor as manual rearm")
	}
}
