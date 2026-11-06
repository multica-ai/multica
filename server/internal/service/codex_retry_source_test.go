package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type retryMentionFixture struct {
	pool            *pgxpool.Pool
	svc             *TaskService
	parentID        pgtype.UUID
	commentID       string
	parentCommentID string
	issueID         string
	userID          string
	agentID         string
}

func newRetryMentionFixture(t *testing.T) retryMentionFixture {
	return newRetryCommentFixture(t, false)
}

func newRetryCommentFixture(t *testing.T, replyToAgent bool) retryMentionFixture {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='in_progress',assignee_type='member',assignee_id=$2 WHERE id=$1`, issueID, userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent SET model='gpt-6-sol' WHERE id=$1`, agentID); err != nil {
		t.Fatal(err)
	}
	var parentID string
	var parentArg any
	content := "[@Agent](mention://agent/" + agentID + ") Please investigate"
	if replyToAgent {
		content = "Please continue"
		if err := pool.QueryRow(ctx, `INSERT INTO comment (issue_id,workspace_id,author_type,author_id,content,type)
			VALUES ($1,$2,'agent',$3,'prior answer','comment') RETURNING id`, issueID, workspaceID, agentID).Scan(&parentID); err != nil {
			t.Fatal(err)
		}
		parentArg = parentID
	}
	var commentID string
	if err := pool.QueryRow(ctx, `INSERT INTO comment (issue_id,workspace_id,author_type,author_id,content,type,parent_id)
		VALUES ($1,$2,'member',$3,$4,'comment',$5) RETURNING id`, issueID, workspaceID, userID, content, parentArg).Scan(&commentID); err != nil {
		t.Fatal(err)
	}
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	issue, err := svc.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	var parent db.AgentTaskQueue
	if replyToAgent {
		parent, err = svc.EnqueueTaskForThreadParent(ctx, issue, util.MustParseUUID(agentID), util.MustParseUUID(commentID))
	} else {
		parent, err = svc.EnqueueTaskForMention(ctx, issue, util.MustParseUUID(agentID), util.MustParseUUID(commentID), OriginNamed)
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running',attempt=1,max_attempts=2,session_id='codex-session' WHERE id=$1`, parent.ID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id=$1 OR id=$1`, parent.ID)
	})
	return retryMentionFixture{pool: pool, svc: svc, parentID: parent.ID, commentID: commentID, parentCommentID: parentID, issueID: issueID, userID: userID, agentID: agentID}
}

func (f retryMentionFixture) fail(t *testing.T) {
	t.Helper()
	failed, transitioned, err := f.svc.FailTaskWithTransition(context.Background(), f.parentID, "busy", "codex-session", "", "", "agent_error.codex_server_overloaded", false, "", "", "gpt-6-sol")
	if err != nil || !transitioned || failed == nil || failed.Status != "failed" {
		t.Fatalf("fail parent: transitioned=%v err=%v task=%v", transitioned, err, failed)
	}
}

func (f retryMentionFixture) child(t *testing.T) db.AgentTaskQueue {
	t.Helper()
	var id pgtype.UUID
	if err := f.pool.QueryRow(context.Background(), `SELECT id FROM agent_task_queue WHERE retry_of_task_id=$1`, f.parentID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	child, err := f.svc.Queries.GetAgentTask(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return child
}

func (f retryMentionFixture) claimProblem(t *testing.T) string {
	t.Helper()
	child := f.child(t)
	agent, err := f.svc.Queries.GetAgent(context.Background(), util.MustParseUUID(f.agentID))
	if err != nil {
		t.Fatal(err)
	}
	isCodex, problem := f.svc.CodexRetryClaimProblem(context.Background(), child, agent)
	if !isCodex {
		t.Fatal("child was not recognized as Codex overload retry")
	}
	return problem
}

func TestCodexRetryCommentSourceAdmission(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change string
		args   func(retryMentionFixture) []any
	}{
		{"edited", `UPDATE comment SET content='new request',revision=revision+1 WHERE id=$1`, func(f retryMentionFixture) []any { return []any{f.commentID} }},
		{"deleted", `UPDATE comment SET deleted_at=now(),revision=revision+1 WHERE id=$1`, func(f retryMentionFixture) []any { return []any{f.commentID} }},
		{"member_removed", `DELETE FROM member WHERE workspace_id=(SELECT workspace_id FROM issue WHERE id=$1) AND user_id=$2`, func(f retryMentionFixture) []any { return []any{f.issueID, f.userID} }},
	} {
		t.Run(tc.name+"_before_failure", func(t *testing.T) {
			f := newRetryMentionFixture(t)
			if _, err := f.pool.Exec(context.Background(), tc.change, tc.args(f)...); err != nil {
				t.Fatal(err)
			}
			f.fail(t)
			var children int
			if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id=$1`, f.parentID).Scan(&children); err != nil || children != 0 {
				t.Fatalf("children=%d err=%v, want no retry", children, err)
			}
		})
		t.Run(tc.name+"_before_claim", func(t *testing.T) {
			f := newRetryMentionFixture(t)
			f.fail(t)
			if problem := f.claimProblem(t); problem != "" {
				t.Fatalf("valid child claim: %s", problem)
			}
			if _, err := f.pool.Exec(context.Background(), tc.change, tc.args(f)...); err != nil {
				t.Fatal(err)
			}
			if problem := f.claimProblem(t); problem == "" {
				t.Fatal("changed source was admitted at claim")
			}
		})
	}
}

func TestCodexRetryExplicitMentionSurvivesAssignmentChange(t *testing.T) {
	f := newRetryMentionFixture(t)
	if _, err := f.pool.Exec(context.Background(), `UPDATE issue SET assignee_type=NULL,assignee_id=NULL WHERE id=$1`, f.issueID); err != nil {
		t.Fatal(err)
	}
	f.fail(t)
	if problem := f.claimProblem(t); problem != "" {
		t.Fatalf("explicit mention was stopped by unrelated assignee change: %s", problem)
	}
}

func TestCodexRetryCommentInvokeRightRevokedBeforeClaim(t *testing.T) {
	f := newRetryMentionFixture(t)
	ctx := context.Background()
	var newOwner string
	if err := f.pool.QueryRow(ctx, `INSERT INTO "user" (name,email) VALUES ('new owner',$1) RETURNING id`,
		fmt.Sprintf("retry-owner-%d@multica.test", time.Now().UnixNano())).Scan(&newOwner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, newOwner) })
	if _, err := f.pool.Exec(ctx, `UPDATE agent SET owner_id=$2,permission_mode='public_to' WHERE id=$1`, f.agentID, newOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO agent_invocation_target (agent_id,target_type,target_id,created_by)
		SELECT $1,'workspace',workspace_id,$2 FROM issue WHERE id=$3`, f.agentID, f.userID, f.issueID); err != nil {
		t.Fatal(err)
	}
	f.fail(t)
	if problem := f.claimProblem(t); problem != "" {
		t.Fatalf("allowed comment claim: %s", problem)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM agent_invocation_target WHERE agent_id=$1`, f.agentID); err != nil {
		t.Fatal(err)
	}
	if problem := f.claimProblem(t); problem == "" {
		t.Fatal("revoked invocation right admitted at claim")
	}
}

func TestCodexRetryReplyParentEditStopsClaim(t *testing.T) {
	f := newRetryCommentFixture(t, true)
	f.fail(t)
	if problem := f.claimProblem(t); problem != "" {
		t.Fatalf("valid reply claim: %s", problem)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE comment SET content='revised answer',revision=revision+1 WHERE id=$1`, f.parentCommentID); err != nil {
		t.Fatal(err)
	}
	if problem := f.claimProblem(t); problem == "" {
		t.Fatal("edited parent comment admitted at claim")
	}
}

func TestCodexRetryAgentCommentKeepsVerifiedOriginator(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	workspaceID, userID, sourceAgentID, issueID := seedAttributionFixture(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='in_progress' WHERE id=$1`, issueID); err != nil {
		t.Fatal(err)
	}
	var targetAgentID string
	if err := pool.QueryRow(ctx, `INSERT INTO agent (workspace_id,name,runtime_mode,runtime_config,runtime_id,visibility,
		max_concurrent_tasks,owner_id,instructions,custom_env,custom_args,model)
		SELECT workspace_id,'target','cloud','{}'::jsonb,runtime_id,'workspace',1,owner_id,'','{}'::jsonb,'[]'::jsonb,'gpt-6-sol'
		FROM agent WHERE id=$1 RETURNING id`, sourceAgentID).Scan(&targetAgentID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM agent WHERE id=$1`, targetAgentID) })
	var sourceTaskID pgtype.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO agent_task_queue (agent_id,runtime_id,issue_id,status,priority,originator_user_id,accountable_user_id)
		SELECT id,runtime_id,$2,'running',0,$3,$3 FROM agent WHERE id=$1 RETURNING id`, sourceAgentID, issueID, userID).Scan(&sourceTaskID); err != nil {
		t.Fatal(err)
	}
	var commentID string
	if err := pool.QueryRow(ctx, `INSERT INTO comment (issue_id,workspace_id,author_type,author_id,source_task_id,content,type)
		VALUES ($1,$2,'agent',$3,$4,$5,'comment') RETURNING id`, issueID, workspaceID, sourceAgentID, sourceTaskID,
		"[@Target](mention://agent/"+targetAgentID+") Please investigate").Scan(&commentID); err != nil {
		t.Fatal(err)
	}
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	issue, err := svc.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	parent, err := svc.EnqueueTaskForMention(ctx, issue, util.MustParseUUID(targetAgentID), util.MustParseUUID(commentID), OriginNamed)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id=$1 OR id=$1 OR id=$2`, parent.ID, sourceTaskID)
	})
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running',attempt=1,max_attempts=2,session_id='codex-session' WHERE id=$1`, parent.ID); err != nil {
		t.Fatal(err)
	}
	failed, transitioned, err := svc.FailTaskWithTransition(ctx, parent.ID, "busy", "codex-session", "", "", "agent_error.codex_server_overloaded", false, "", "", "gpt-6-sol")
	if err != nil || !transitioned || failed == nil {
		t.Fatalf("fail parent: task=%v transitioned=%v err=%v", failed, transitioned, err)
	}
	var childID pgtype.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM agent_task_queue WHERE retry_of_task_id=$1`, parent.ID).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	child, err := svc.Queries.GetAgentTask(ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	target, err := svc.Queries.GetAgent(ctx, util.MustParseUUID(targetAgentID))
	if err != nil {
		t.Fatal(err)
	}
	if codex, problem := svc.CodexRetryClaimProblem(ctx, child, target); !codex || problem != "" {
		t.Fatalf("valid delegated source: codex=%v problem=%q", codex, problem)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET originator_user_id=NULL,accountable_user_id=NULL WHERE id=$1`, sourceTaskID); err != nil {
		t.Fatal(err)
	}
	if codex, problem := svc.CodexRetryClaimProblem(ctx, child, target); !codex || problem == "" {
		t.Fatalf("changed delegation admitted: codex=%v problem=%q", codex, problem)
	}
}
