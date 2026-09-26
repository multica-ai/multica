package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Project workflows × wakeups (MUL-7420).

// A wakeup continues the run that registered it, so its run keeps that run's
// workflow step: when someone else moved the issue in between, the wakeup's
// status changes are refused like the registering run's would be. A wakeup a
// member registered has no such run and starts from the issue's status.
func TestWakeupRunKeepsTheRegisteringRunsStep(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	source := f.Task(t, agent, testutil.Cols{
		"issue_id": issue, "status": "completed", "workflow_step": "in_progress",
		"runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')"),
	})
	fire := func(w db.IssueWakeup) db.AgentTaskQueue {
		t.Helper()
		f.Exec(t, "UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1", w.ID)
		wakeDispatch(t, s, w)
		got, _ := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
		task, err := f.q.GetAgentTask(ctx, got.LastTaskID)
		if err != nil {
			t.Fatalf("wakeup did not queue a run: %v", err)
		}
		return task
	}

	fromRun, err := s.Create(ctx, issue, parseTestUUID(t, f.UserID), parseTestUUID(t, source),
		WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 1, Instruction: "check CI, then hand to review"})
	if err != nil {
		t.Fatal(err)
	}
	fromMember := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 1, Instruction: "daily check"})
	// Someone else parks the issue before either fires.
	f.Exec(t, "UPDATE issue SET status='blocked' WHERE id=$1", issue)

	if got := fire(fromRun).WorkflowStep; got.String != "in_progress" {
		t.Fatalf("wakeup from a run works on %q, want the run's in_progress", got.String)
	}
	f.Exec(t, "UPDATE agent_task_queue SET status='cancelled',completed_at=now() WHERE issue_id=$1 AND status='queued'", issue)
	if got := fire(fromMember).WorkflowStep; got.String != "blocked" {
		t.Fatalf("wakeup from a member works on %q, want the issue's blocked", got.String)
	}
}

// A retried workflow handoff keeps its brief, as a retried wakeup keeps its
// prompt; a row from before workflow steps drops its retired note.
func TestRetryKeepsWorkflowHandoffBrief(t *testing.T) {
	f, _, issue, agent := wakeFixture(t)
	ctx := context.Background()
	runtime := testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')")
	retry := func(cols testutil.Cols) db.AgentTaskQueue {
		t.Helper()
		cols["issue_id"], cols["status"], cols["runtime_id"] = issue, "failed", runtime
		parent := f.Task(t, agent, cols)
		child, err := f.q.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: parseTestUUID(t, parent)})
		if err != nil {
			t.Fatal(err)
		}
		f.Cleanup(t, "DELETE FROM agent_task_queue WHERE id=$1", child.ID)
		f.Exec(t, "UPDATE agent_task_queue SET status='cancelled' WHERE id=$1", child.ID)
		return child
	}
	if got := retry(testutil.Cols{"handoff_note": "This project's workflow — Delivery", "workflow_step": "in_progress"}); got.HandoffNote != (pgtype.Text{String: "This project's workflow — Delivery", Valid: true}) || got.WorkflowStep.String != "in_progress" {
		t.Fatalf("retry lost the handoff brief or step: %+v / %+v", got.HandoffNote, got.WorkflowStep)
	}
	if got := retry(testutil.Cols{"handoff_note": "retired assignment note"}); got.HandoffNote.Valid {
		t.Fatalf("retry revived a retired note: %q", got.HandoffNote.String)
	}
}
