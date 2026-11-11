package service

import (
	"context"
	"errors"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueBlockerOrdinaryAdmissionConsumesRelease(t *testing.T) {
	for _, mode := range []string{"agent", "squad", "deferred", "workflow-squad"} {
		t.Run(mode, func(t *testing.T) {
			f, s, issue, agentID := wakeFixture(t)
			ctx := context.Background()
			agent := parseTestUUID(t, agentID)
			assigneeType, assigneeID := "agent", agentID
			if mode == "squad" || mode == "workflow-squad" {
				assigneeType, assigneeID = "squad", f.Squad(t, "release owner", agentID)
				f.SquadMember(t, assigneeID, "agent", agentID)
			}
			f.Exec(t, "UPDATE issue SET status='backlog',assignee_type=$2,assignee_id=$3 WHERE id=$1", issue, assigneeType, assigneeID)
			blocker := f.Issue(t, "prerequisite")
			if err := AddIssueBlocker(ctx, f.q, parseTestUUID(t, f.WorkspaceID), issue, parseTestUUID(t, blocker), "member", parseTestUUID(t, f.UserID)); err != nil {
				t.Fatal(err)
			}
			f.Cleanup(t, "DELETE FROM issue_blocker WHERE issue_id=$1", issue)
			f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", blocker)
			if err := s.TickWorkspaces(ctx, parseTestUUID(t, f.WorkspaceID)); err != nil {
				t.Fatal(err)
			}
			f.Exec(t, "UPDATE issue SET status='todo' WHERE id=$1", issue)
			current, err := f.q.GetIssue(ctx, issue)
			if err != nil {
				t.Fatal(err)
			}
			var task db.AgentTaskQueue
			switch mode {
			case "squad":
				task, err = s.Tasks.EnqueueTaskForSquadLeaderByActor(ctx, current, agent, parseTestUUID(t, assigneeID), parseTestUUID(t, f.UserID))
			case "workflow-squad":
				task, err = s.Tasks.EnqueueTaskForWorkflowSquadLeader(ctx, current, agent, parseTestUUID(t, assigneeID), "Continue this workflow step", parseTestUUID(t, f.UserID))
			case "deferred":
				task, err = s.Tasks.EnqueueDeferredChannelIssueTask(ctx, current, time.Now().Add(time.Minute))
			default:
				task, err = s.Tasks.EnqueueTaskForIssue(ctx, current)
			}
			if err != nil {
				t.Fatal(err)
			}
			rule := blockerRule(t, f, issue)
			if rule.ConditionState != "ready" || rule.LastTaskID != task.ID || rule.NextFireAt.Valid {
				t.Fatalf("ordinary admission did not consume the release: state=%q task=%v next=%v", rule.ConditionState, rule.LastTaskID, rule.NextFireAt)
			}
			f.Exec(t, "UPDATE agent_task_queue SET status='completed',started_at=now(),completed_at=now() WHERE id=$1", task.ID)
			f.Exec(t, "UPDATE issue SET status='in_review' WHERE id=$1", issue)
			if err := s.TickWorkspaces(ctx, parseTestUUID(t, f.WorkspaceID)); err != nil {
				t.Fatal(err)
			}
			if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", issue); n != 1 {
				t.Fatalf("completed ordinary run dispatched again: %d runs", n)
			}
			// Reopening a dependency creates a new wait, rather than being
			// suppressed by the previous ordinary run's acknowledgement.
			f.Exec(t, "UPDATE issue SET status='todo' WHERE id=$1", blocker)
			f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", blocker)
			if err := s.TickWorkspaces(ctx, parseTestUUID(t, f.WorkspaceID)); err != nil {
				t.Fatal(err)
			}
			if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", issue); n != 2 {
				t.Fatalf("new wait was not delivered once: %d runs", n)
			}
		})
	}
}

func TestIssueBlockerAcknowledgementFailureRollsBackAdmission(t *testing.T) {
	f, s, issue, agentID := wakeFixture(t)
	ctx := context.Background()
	f.Exec(t, "UPDATE issue SET status='todo',assignee_type='agent',assignee_id=$2 WHERE id=$1", issue, agentID)
	blocker := f.Issue(t, "prerequisite")
	if err := AddIssueBlocker(ctx, f.q, parseTestUUID(t, f.WorkspaceID), issue, parseTestUUID(t, blocker), "member", parseTestUUID(t, f.UserID)); err != nil {
		t.Fatal(err)
	}
	f.Cleanup(t, "DELETE FROM issue_blocker WHERE issue_id=$1", issue)
	f.Exec(t, "UPDATE issue SET status='done' WHERE id=$1", blocker)
	current, err := f.q.GetIssue(ctx, issue)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("reject blocker acknowledgement")
	tasks := *s.Tasks
	tasks.TxStarter = &failNamedExecTxStarter{pool: f.Pool, queryName: "AcknowledgeIssueBlockerRelease", err: failure}
	if _, err := tasks.EnqueueTaskForIssue(ctx, current); !errors.Is(err, failure) {
		t.Fatalf("admission error = %v, want acknowledgement failure", err)
	}
	if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", issue); n != 0 {
		t.Fatalf("failed acknowledgement left %d tasks", n)
	}
	if rule := blockerRule(t, f, issue); rule.ConditionState != "" || rule.LastTaskID.Valid {
		t.Fatal("failed admission consumed the dependency release")
	}
	if err := s.TickWorkspaces(ctx, parseTestUUID(t, f.WorkspaceID)); err != nil {
		t.Fatal(err)
	}
	if n := f.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id=$1", issue); n != 1 {
		t.Fatalf("unconsumed release started %d tasks, want one", n)
	}
}
