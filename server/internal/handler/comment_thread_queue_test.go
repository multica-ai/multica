package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCommentThreadQueuesMergeAndExecuteIndependently(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "thread queue runtime")
	agentID := dbfx.Agent(t, "thread queue agent", runtimeID)
	issueID := dbfx.Issue(t, "thread queue issue", testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})
	rootA := dbfx.Comment(t, issueID, "thread A")
	rootB := dbfx.Comment(t, issueID, "thread B")
	replyA := dbfx.Comment(t, issueID, "A supplement", testutil.Cols{"parent_id": rootA})
	nestedA := dbfx.Comment(t, issueID, "A nested supplement", testutil.Cols{"parent_id": replyA})
	t.Cleanup(func() { dbfx.Exec(t, "DELETE FROM agent_task_queue WHERE issue_id = $1", issueID) })
	issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	enqueue := func(commentID string, pending bool, want DispatchStatus) {
		t.Helper()
		result := testHandler.enqueueCommentAgentTriggers(ctx, issue, parseUUID(commentID),
			[]commentAgentTrigger{{Agent: agent, Source: commentTriggerSourceMentionAgent, AlreadyPending: pending}})
		if result[agentID].status != want {
			t.Fatalf("enqueue %s: got %+v, want %s", commentID, result[agentID], want)
		}
	}
	enqueue(rootA, false, DispatchQueued)
	// Even a stale pending hint from another thread must not merge or defer B.
	enqueue(rootB, true, DispatchQueued)
	enqueue(replyA, true, DispatchCoalesced)
	enqueue(nestedA, true, DispatchCoalesced)
	tasks, err := testHandler.Queries.ListTasksByIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("got %d runs, want one per thread", len(tasks))
	}
	var a, b db.AgentTaskQueue
	for _, task := range tasks {
		switch uuidToString(task.CommentThreadID) {
		case rootA:
			a = task
		case rootB:
			b = task
		default:
			t.Fatalf("wrong thread scope: %s", uuidToString(task.CommentThreadID))
		}
	}
	if uuidToString(a.TriggerCommentID) != nestedA || len(a.CoalescedCommentIds) != 2 || len(b.CoalescedCommentIds) != 0 {
		t.Fatalf("wrong input batches: A=%+v B=%+v", a.TriggerCommentID, b.CoalescedCommentIds)
	}
	claim := db.ClaimAgentTaskParams{AgentID: parseUUID(agentID), RuntimeID: parseUUID(runtimeID), PrepareLeaseSecs: 60, RuntimeStaleSecs: 120}
	claimed, err := testHandler.Queries.ClaimAgentTask(ctx, claim)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != a.ID {
		t.Fatal("oldest thread should execute first")
	}
	if _, err := testHandler.Queries.ClaimAgentTask(ctx, claim); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("another thread ran concurrently on the same issue: %v", err)
	}
	// A new instruction after execution starts belongs to a successor run.
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='running', started_at=now(), delivered_comment_ids=ARRAY[$2::uuid,$3::uuid,$4::uuid] WHERE id=$1", uuidToString(a.ID), rootA, replyA, nestedA)
	followupA := dbfx.Comment(t, issueID, "A follow-up", testutil.Cols{"parent_id": rootA})
	enqueue(followupA, false, DispatchQueued)
	dbfx.Exec(t, "UPDATE agent_task_queue SET status='completed', completed_at=now() WHERE id=$1", uuidToString(a.ID))
	completed, err := testHandler.Queries.GetAgentTask(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	testHandler.reconcileCommentsOnCompletion(ctx, &completed)
	tasks, err = testHandler.Queries.ListTasksByIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("completion duplicated another thread: got %d tasks", len(tasks))
	}
	// A row-specific rerun replaces only A's pending successor.
	rerun, err := testHandler.TaskService.RerunIssue(ctx, issue.ID, a.ID, pgtype.UUID{}, parseUUID(testUserID), nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := testHandler.Queries.GetAgentTask(ctx, b.ID)
	if err != nil || other.Status != "queued" {
		t.Fatalf("rerun disturbed thread B: %s / %v", other.Status, err)
	}
	if rerun.CommentThreadID != a.CommentThreadID {
		t.Fatal("rerun lost thread scope")
	}
	if _, err := testHandler.TaskService.CancelTask(ctx, rerun.ID); err != nil {
		t.Fatal(err)
	}
	other, err = testHandler.Queries.GetAgentTask(ctx, b.ID)
	if err != nil || other.Status != "queued" {
		t.Fatalf("stop disturbed thread B: %s / %v", other.Status, err)
	}
}

func TestDeferredCommentThreadsPromoteIndependently(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "thread promotion runtime")
	agentID := dbfx.Agent(t, "thread promotion agent", runtimeID)
	issueID := dbfx.Issue(t, "thread promotion issue")
	rootA := dbfx.Comment(t, issueID, "A")
	rootB := dbfx.Comment(t, issueID, "B")
	create := func(root string) string {
		return dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "trigger_comment_id": root, "status": "deferred", "fire_at": testutil.Raw("now()-interval '1 minute'")})
	}
	a, b := create(rootA), create(rootB)
	anotherA := create(rootA)
	promoted, err := testHandler.Queries.PromoteDueDeferredTasksForRuntime(ctx, db.PromoteDueDeferredTasksForRuntimeParams{RuntimeID: parseUUID(runtimeID), RuntimeStaleSecs: 120})
	if err != nil {
		t.Fatal(err)
	}
	if len(promoted) != 2 {
		t.Fatalf("promoted %d runs, want one per thread", len(promoted))
	}
	for _, id := range []string{a, b} {
		row, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(id))
		if err != nil || row.Status != "queued" {
			t.Fatalf("thread did not promote: %s / %v", row.Status, err)
		}
	}
	row, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(anotherA))
	if err != nil || row.Status != "deferred" {
		t.Fatalf("second run in A must stay deferred: %s / %v", row.Status, err)
	}
}
