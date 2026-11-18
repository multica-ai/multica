package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCodexOverloadCommentRunOnMemberAssignedIssueRetries(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	_, err := pool.Exec(ctx, `UPDATE issue SET status='in_progress', assignee_type='member', assignee_id=$2 WHERE id=$1`, issueID, userID)
	if err != nil {
		t.Fatal(err)
	}
	var commentID string
	err = pool.QueryRow(ctx, `INSERT INTO comment (issue_id,workspace_id,author_type,author_id,content,type)
		VALUES ($1,$2,'member',$3,$4,'comment') RETURNING id`, issueID, workspaceID, userID, "[@Agent](mention://agent/"+agentID+") Please investigate").Scan(&commentID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `UPDATE agent SET model='gpt-6-sol' WHERE id=$1`, agentID)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	issue, err := svc.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	parent, err := svc.EnqueueTaskForMention(ctx, issue, util.MustParseUUID(agentID), util.MustParseUUID(commentID), OriginNamed)
	if err != nil {
		t.Fatalf("enqueue valid mention task: %v", err)
	}
	parentID := parent.ID
	_, err = pool.Exec(ctx, `UPDATE agent_task_queue SET status='running',attempt=1,max_attempts=2,session_id='codex-session' WHERE id=$1`, parentID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id=$1 OR id=$1`, parentID)
	})
	failed, transitioned, err := svc.FailTaskWithTransition(ctx, parentID, "busy", "codex-session", "", "", "agent_error.codex_server_overloaded", false, "", "", "gpt-6-sol")
	if err != nil || !transitioned || failed == nil || failed.Status != "failed" {
		t.Fatalf("fail parent: task=%v transitioned=%v err=%v", failed, transitioned, err)
	}
	var childID pgtype.UUID
	var status string
	err = pool.QueryRow(ctx, `SELECT id,status FROM agent_task_queue WHERE retry_of_task_id=$1`, parentID).Scan(&childID, &status)
	if err != nil {
		t.Fatalf("valid comment-triggered run on member-assigned issue must get one retry child: %v", err)
	}
	if status != "deferred" {
		t.Fatalf("retry status=%q, want deferred", status)
	}
}
