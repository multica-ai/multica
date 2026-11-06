package service

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestWorkflowBindingOrdinaryTaskAndMemberRerun(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	f.Exec(t, `UPDATE issue SET status='in_progress',assignee_type='agent',assignee_id=$2 WHERE id=$1`, issue, agent)
	a, err := f.q.GetAgent(ctx, parseTestUUID(t, agent))
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := f.q.CreateAgentTask(ctx, db.CreateAgentTaskParams{AgentID: a.ID, RuntimeID: a.RuntimeID, IssueID: issue})
	if err != nil {
		t.Fatal(err)
	}
	if !ordinary.WorkflowStep.Valid || ordinary.WorkflowStep.String != "in_progress" {
		t.Fatalf("ordinary binding: %+v", ordinary.WorkflowStep)
	}
	f.Exec(t, `UPDATE agent_task_queue SET status='completed',completed_at=now(),session_id='existing-session',work_dir='/fixture/workdir' WHERE id=$1`, ordinary.ID)
	f.Exec(t, `UPDATE issue SET status='blocked' WHERE id=$1`, issue)
	if _, err := s.Tasks.RerunIssue(ctx, issue, ordinary.ID, pgtype.UUID{}, parseTestUUID(t, f.UserID), false, func(db.Agent) bool { return false }); err == nil {
		t.Fatal("rerun bypassed invoke permission")
	}
	var count int
	f.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, issue).Scan(&count)
	if count != 1 {
		t.Fatalf("denied rerun mutated tasks: %d", count)
	}
	rerun, err := s.Tasks.RerunIssue(ctx, issue, ordinary.ID, pgtype.UUID{}, parseTestUUID(t, f.UserID), false, func(db.Agent) bool { return true })
	if err != nil || rerun == nil {
		t.Fatalf("rerun: %v", err)
	}
	saved, err := f.q.GetAgentTask(ctx, rerun.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.WorkflowStep.Valid || saved.WorkflowStep.String != "blocked" || saved.RerunOfTaskID != ordinary.ID || IsFreshRerun(saved) {
		t.Fatalf("member rerun binding/lineage: %+v", saved)
	}
	parent, err := f.q.GetAgentTask(ctx, ordinary.ID)
	if err != nil || parent.WorkflowStep != ordinary.WorkflowStep || parent.SessionID.String != "existing-session" {
		t.Fatalf("rerun rewrote source: %+v / %v", parent, err)
	}
}

func TestWorkflowBindingRetryIndependentOfProviderSession(t *testing.T) {
	for _, reason := range []string{"agent_error.provider_network", "codex_semantic_inactivity"} {
		t.Run(reason, func(t *testing.T) {
			f, s, issue, agent := wakeFixture(t)
			ctx := context.Background()
			source := f.Task(t, agent, testutil.Cols{"issue_id": issue, "status": "failed", "workflow_step": "in_progress", "session_id": "source-session", "work_dir": "/fixture/workdir", "failure_reason": reason, "attempt": 1, "max_attempts": 2,
				"runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')")})
			f.Exec(t, `UPDATE issue SET status='blocked' WHERE id=$1`, issue)
			parent, err := f.q.GetAgentTask(ctx, parseTestUUID(t, source))
			if err != nil {
				t.Fatal(err)
			}
			child, err := s.Tasks.MaybeRetryFailedTask(ctx, parent)
			if err != nil || child == nil {
				t.Fatalf("retry not created: %v", err)
			}
			saved, err := f.q.GetAgentTask(ctx, child.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.WorkflowStep != parent.WorkflowStep || saved.RetryOfTaskID != parent.ID || saved.WorkDir != parent.WorkDir {
				t.Fatalf("retry lost source binding/lineage/workdir: %+v", saved)
			}
			dropped := reason == "codex_semantic_inactivity"
			if dropped != !saved.SessionID.Valid || dropped != saved.ForceFreshSession {
				t.Fatalf("session policy changed: session=%+v fresh=%v", saved.SessionID, saved.ForceFreshSession)
			}
			if !dropped && saved.SessionID != parent.SessionID {
				t.Fatal("resume-safe retry lost its session")
			}
		})
	}
}

func TestWorkflowBindingAgentWakeupNullSourceFallsBackToCurrent(t *testing.T) {
	f, s, issue, agent := wakeFixture(t)
	ctx := context.Background()
	source := f.Task(t, agent, testutil.Cols{"issue_id": issue, "status": "completed", "runtime_id": testutil.Raw("(SELECT runtime_id FROM agent WHERE id='" + agent + "')")})
	w, err := s.Create(ctx, issue, parseTestUUID(t, f.UserID), parseTestUUID(t, source), WakeupInput{AgentID: agent, Kind: "at", AfterSeconds: 1, Instruction: "check"})
	if err != nil {
		t.Fatal(err)
	}
	f.Exec(t, `UPDATE issue SET status='blocked' WHERE id=$1`, issue)
	f.Exec(t, `UPDATE issue_wakeup SET next_fire_at=now()-interval '1 second' WHERE id=$1`, w.ID)
	wakeDispatch(t, s, w)
	got, err := f.q.GetIssueWakeup(ctx, db.GetIssueWakeupParams{ID: w.ID, WorkspaceID: w.WorkspaceID})
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.q.GetAgentTask(ctx, got.LastTaskID)
	if err != nil || !task.WorkflowStep.Valid || task.WorkflowStep.String != "blocked" {
		t.Fatalf("NULL source fallback: %+v / %v", task.WorkflowStep, err)
	}
}

func TestWorkflowBindingJoinedWakeupKeepsRecipient(t *testing.T) {
	f, s, issue, agent := conditionFixture(t)
	w := wakeCreate(t, f, s, issue, WakeupInput{AgentID: agent, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "Joined check"})
	waiting := wakeWaitingRun(t, f, issue, agent, f.UserID)
	f.Exec(t, `UPDATE agent_task_queue SET workflow_step='in_progress' WHERE id=$1`, waiting)
	f.Exec(t, `UPDATE issue SET status='blocked' WHERE id=$1`, issue)
	f.Comment(t, util.UUIDToString(issue), "trigger")
	wakeTick(t, f, s, w.ID)
	if notes := wakeClaim(t, f, s, waiting); !strings.Contains(notes, "Joined check") {
		t.Fatalf("wakeup did not join: %q", notes)
	}
	task, err := f.q.GetAgentTask(context.Background(), parseTestUUID(t, waiting))
	if err != nil || !task.WorkflowStep.Valid || task.WorkflowStep.String != "in_progress" {
		t.Fatalf("join rebound recipient: %+v / %v", task.WorkflowStep, err)
	}
}

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
