package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCodexOverloadWorkflowHandoffRetriesCurrentStep(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	var squadID string
	if err := pool.QueryRow(ctx, `INSERT INTO squad (workspace_id,name,leader_id,creator_id) VALUES ($1,'retry handoff',$2,$3) RETURNING id`, workspaceID, agentID, userID).Scan(&squadID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET status='in_review',assignee_type='squad',assignee_id=$2,revision=revision+1 WHERE id=$1`, issueID, squadID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent SET model='gpt-6-sol' WHERE id=$1`, agentID); err != nil {
		t.Fatal(err)
	}
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	issue, err := svc.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	parent, err := svc.EnqueueTaskForWorkflowSquadLeader(ctx, issue, util.MustParseUUID(agentID), util.MustParseUUID(squadID), "handoff brief", util.MustParseUUID(userID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id=$1 OR id=$1`, parent.ID)
	})
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running',attempt=1,max_attempts=2,session_id='codex-session' WHERE id=$1`, parent.ID); err != nil {
		t.Fatal(err)
	}
	failed, transitioned, err := svc.FailTaskWithTransition(ctx, parent.ID, "busy", "codex-session", "", "", "agent_error.codex_server_overloaded", false, "", "", "gpt-6-sol")
	if err != nil || !transitioned || failed == nil || failed.Status != "failed" {
		t.Fatalf("fail handoff: task=%v transitioned=%v err=%v", failed, transitioned, err)
	}
	var childID pgtype.UUID
	var status, route, step string
	var revision int64
	err = pool.QueryRow(ctx, `SELECT id,status,context #>> '{codex_retry_source,route}',workflow_step, (context->>'workflow_handoff_revision')::bigint FROM agent_task_queue WHERE retry_of_task_id=$1`, parent.ID).Scan(&childID, &status, &route, &step, &revision)
	if err != nil {
		t.Fatalf("current workflow handoff must get retry child: %v", err)
	}
	if status != "deferred" || route != "workflow_squad_handoff" || step != issue.Status || revision != issue.Revision {
		t.Fatalf("child status=%s route=%s step=%s revision=%d, want handoff revision=%d", status, route, step, revision, issue.Revision)
	}
	child, err := svc.Queries.GetAgentTask(ctx, childID)
	if err != nil {
		t.Fatal(err)
	}
	agent, err := svc.Queries.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	if codex, problem := svc.CodexRetryClaimProblem(ctx, child, agent); !codex || problem != "" {
		t.Fatalf("current handoff claim: codex=%v problem=%q", codex, problem)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET revision=revision+1 WHERE id=$1`, issueID); err != nil {
		t.Fatal(err)
	}
	if codex, problem := svc.CodexRetryClaimProblem(ctx, child, agent); !codex || problem == "" {
		t.Fatalf("changed handoff revision admitted: codex=%v problem=%q", codex, problem)
	}
}

func TestCodexOverloadWorkflowHandoffRejectsChangedSource(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change string
		args   func(issueID, squadID, userID, replacementAgentID string) []any
	}{
		{"revision", `UPDATE issue SET revision=revision+1 WHERE id=$1`, func(issueID, _, _, _ string) []any { return []any{issueID} }},
		{"step", `UPDATE issue SET status='todo',revision=revision+1 WHERE id=$1`, func(issueID, _, _, _ string) []any { return []any{issueID} }},
		{"assignee", `UPDATE issue SET assignee_type='member',assignee_id=$2 WHERE id=$1`, func(issueID, _, userID, _ string) []any { return []any{issueID, userID} }},
		{"leader", `UPDATE squad SET leader_id=$2 WHERE id=$1`, func(_, squadID, _, replacementAgentID string) []any { return []any{squadID, replacementAgentID} }},
		{"member_removed", `DELETE FROM member WHERE workspace_id=(SELECT workspace_id FROM issue WHERE id=$1) AND user_id=$2`, func(issueID, _, userID, _ string) []any { return []any{issueID, userID} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := newResolveOriginatorPool(t)
			ctx := context.Background()
			workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
			var squadID string
			if err := pool.QueryRow(ctx, `INSERT INTO squad (workspace_id,name,leader_id,creator_id) VALUES ($1,'changed handoff',$2,$3) RETURNING id`, workspaceID, agentID, userID).Scan(&squadID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE issue SET status='in_review',assignee_type='squad',assignee_id=$2,revision=revision+1 WHERE id=$1`, issueID, squadID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `UPDATE agent SET model='gpt-6-sol' WHERE id=$1`, agentID); err != nil {
				t.Fatal(err)
			}
			var replacementAgentID string
			if err := pool.QueryRow(ctx, `INSERT INTO agent (workspace_id,name,runtime_mode,runtime_config,runtime_id,visibility,max_concurrent_tasks,owner_id,instructions,custom_env,custom_args)
				SELECT workspace_id,'replacement leader',runtime_mode,runtime_config,runtime_id,visibility,max_concurrent_tasks,owner_id,instructions,custom_env,custom_args FROM agent WHERE id=$1 RETURNING id`, agentID).Scan(&replacementAgentID); err != nil {
				t.Fatal(err)
			}
			svc := NewTaskService(db.New(pool), pool, nil, events.New())
			issue, err := svc.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
			if err != nil {
				t.Fatal(err)
			}
			parent, err := svc.EnqueueTaskForWorkflowSquadLeader(ctx, issue, util.MustParseUUID(agentID), util.MustParseUUID(squadID), "handoff brief", util.MustParseUUID(userID))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE parent_task_id=$1 OR id=$1`, parent.ID)
			})
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status='running',attempt=1,max_attempts=2,session_id='codex-session' WHERE id=$1`, parent.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, tc.change, tc.args(issueID, squadID, userID, replacementAgentID)...); err != nil {
				t.Fatal(err)
			}
			if _, transitioned, err := svc.FailTaskWithTransition(ctx, parent.ID, "busy", "codex-session", "", "", "agent_error.codex_server_overloaded", false, "", "", "gpt-6-sol"); err != nil || !transitioned {
				t.Fatalf("fail parent: transitioned=%v err=%v", transitioned, err)
			}
			var children int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE retry_of_task_id=$1`, parent.ID).Scan(&children); err != nil || children != 0 {
				t.Fatalf("service created child for changed %s: children=%d err=%v", tc.name, children, err)
			}
			// Exercise the SQL guard independently of the service precheck.
			if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET context=context || jsonb_build_object('codex_retry_model','gpt-6-sol','codex_retry_deadline',(now()+interval '30 minutes')::text) WHERE id=$1`, parent.ID); err != nil {
				t.Fatal(err)
			}
			_, err = svc.Queries.CreateRetryTask(ctx, db.CreateRetryTaskParams{ID: parent.ID, FireAt: pgtype.Timestamptz{Time: time.Now().Add(time.Minute), Valid: true}})
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("SQL admitted changed %s: %v", tc.name, err)
			}
		})
	}
}
