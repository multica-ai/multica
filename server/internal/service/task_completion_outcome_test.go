package service

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

func TestCompletionAnsweredCommentIDsUsesDeliveredSubsetOnly(t *testing.T) {
	delivered := dbid.NewV7()
	coalescedOnly := dbid.NewV7()
	task := db.AgentTaskQueue{
		TriggerCommentID:    coalescedOnly,
		CoalescedCommentIds: []pgtype.UUID{coalescedOnly},
		DeliveredCommentIds: []pgtype.UUID{delivered, delivered},
	}
	got := completionAnsweredCommentIDs(task)
	if len(got) != 1 || got[0] != delivered {
		t.Fatalf("answered comments = %v, want only delivered %s", got, util.UUIDToString(delivered))
	}
}

func TestIssueCompletionDoesNotAdoptOrdinaryProgressCommentAsFinal(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	queries := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	userUUID := util.MustParseUUID(userID)
	agentUUID := util.MustParseUUID(agentID)
	issueUUID := util.MustParseUUID(issueID)
	svc := &TaskService{Queries: queries, TxStarter: pool, Bus: events.New()}

	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID: issueUUID, WorkspaceID: workspaceUUID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: agentUUID,
		CreatorType: "member", CreatorID: userUUID, Priority: "medium",
	})
	if err != nil {
		t.Fatalf("enqueue issue task: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running', started_at=now() WHERE id=$1`, task.ID); err != nil {
		t.Fatalf("start issue task: %v", err)
	}
	progressID := dbid.NewV7()
	if _, err := queries.CreateComment(ctx, db.CreateCommentParams{
		ID: progressID, IssueID: issueUUID, WorkspaceID: workspaceUUID,
		AuthorType: "agent", AuthorID: agentUUID, Content: "处理中：已完成第一步。", Type: "comment",
		SourceTaskID: task.ID,
	}); err != nil {
		t.Fatalf("create progress comment: %v", err)
	}

	completed, transitioned, err := svc.CompleteTaskWithTransition(
		ctx, task.ID, []byte(`{"output":"最终结论：已完成。"}`), "", "", "", false, "", "",
	)
	if err != nil || !transitioned || completed == nil {
		t.Fatalf("complete task = (%v, %v, %v)", completed, transitioned, err)
	}

	var outcomeKind, content, finalCommentID string
	if err := pool.QueryRow(ctx, `
		SELECT outcome_kind, content, final_comment_id::text
		FROM agent_task_completion_outcome WHERE task_id=$1
	`, task.ID).Scan(&outcomeKind, &content, &finalCommentID); err != nil {
		t.Fatalf("read completion outcome: %v", err)
	}
	if outcomeKind != "final" || content != "最终结论：已完成。" {
		t.Fatalf("completion outcome = (%q, %q)", outcomeKind, content)
	}
	if finalCommentID == util.UUIDToString(progressID) {
		t.Fatalf("ordinary progress comment %s was adopted as final", finalCommentID)
	}
	var finalContent string
	if err := pool.QueryRow(ctx, `SELECT content FROM comment WHERE id=$1`, finalCommentID).Scan(&finalContent); err != nil {
		t.Fatalf("read final comment: %v", err)
	}
	if finalContent != "最终结论：已完成。" {
		t.Fatalf("final comment content = %q", finalContent)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE source_task_id=$1`, task.ID).Scan(&count); err != nil {
		t.Fatalf("count task comments: %v", err)
	}
	if count != 2 {
		t.Fatalf("task comments = %d, want progress plus authoritative final", count)
	}
}

func TestCompletionOutboxExpiresTheLastCrashedLeaseToDead(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	queries := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	userUUID := util.MustParseUUID(userID)
	agentUUID := util.MustParseUUID(agentID)
	issueUUID := util.MustParseUUID(issueID)
	svc := &TaskService{Queries: queries, TxStarter: pool, Bus: events.New()}
	task, err := svc.EnqueueTaskForIssue(ctx, db.Issue{
		ID: issueUUID, WorkspaceID: workspaceUUID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: agentUUID,
		CreatorType: "member", CreatorID: userUUID, Priority: "medium",
	})
	if err != nil {
		t.Fatalf("enqueue issue task: %v", err)
	}
	commentID := dbid.NewV7()
	if _, err := queries.CreateComment(ctx, db.CreateCommentParams{
		ID: commentID, IssueID: issueUUID, WorkspaceID: workspaceUUID,
		AuthorType: "agent", AuthorID: agentUUID, Content: "最终结果", Type: "comment",
		SourceTaskID: task.ID,
	}); err != nil {
		t.Fatalf("create final comment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_completion_outcome(
			task_id, workspace_id, issue_id, agent_id, outcome_kind, content,
			result_sha256, final_comment_id, answered_comment_ids
		) VALUES($1,$2,$3,$4,'final','最终结果',$5,$6,'{}')
	`, task.ID, workspaceUUID, issueUUID, agentUUID, strings.Repeat("a", 64), commentID); err != nil {
		t.Fatalf("create completion outcome: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO task_completion_outbox(
			task_id, workspace_id, issue_id, agent_id, comment_id, issue_revision,
			state, attempts, lease_owner, lease_expires_at
		) VALUES($1,$2,$3,$4,$5,1,'publishing',$6,'last-worker',now()-interval '1 second')
	`, task.ID, workspaceUUID, issueUUID, agentUUID, commentID, taskCompletionOutboxMaxAttempts); err != nil {
		t.Fatalf("create exhausted completion outbox: %v", err)
	}

	result, err := svc.DeliverTaskCompletionOutbox(ctx, 10)
	if err != nil || result.Claimed != 0 {
		t.Fatalf("completion outbox delivery = %+v, %v", result, err)
	}
	var state, lastError string
	if err := pool.QueryRow(ctx, `
		SELECT state, last_error FROM task_completion_outbox WHERE task_id=$1
	`, task.ID).Scan(&state, &lastError); err != nil {
		t.Fatalf("read completion outbox: %v", err)
	}
	if state != "dead" || lastError == "" {
		t.Fatalf("completion outbox = (%q, %q), want visible dead letter", state, lastError)
	}
}
