package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCompleteTask_RejectsUnmentionedOpinionFallbackBeforeStatusFlip(t *testing.T) {
	p := newResolveOriginatorPool(t)
	ctx := context.Background()
	workspaceID, _, responderID, issueID := seedAttributionFixture(t, p)
	requesterID := insertReplyAdmissionAgent(t, ctx, p, workspaceID)
	parentID := insertReplyAdmissionParent(t, ctx, p, workspaceID, issueID, requesterID)
	taskID := insertReplyAdmissionRunningTask(t, ctx, p, issueID, responderID, parentID)

	svc := NewTaskService(db.New(p), p, nil, events.New())
	result, _ := json.Marshal(map[string]string{
		"output": "The review is sound and the cost constraint is binding.",
	})
	if _, err := svc.CompleteTask(ctx, util.MustParseUUID(taskID), result, "", "", "", false, "", ""); err == nil {
		t.Fatal("expected completion to be rejected when fallback omits requester mention")
	}

	var status string
	if err := p.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "running" {
		t.Fatalf("task status = %q, want running after admission rejection", status)
	}
	var comments int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1 AND author_id = $2`, issueID, responderID).Scan(&comments); err != nil {
		t.Fatalf("count fallback comments: %v", err)
	}
	if comments != 0 {
		t.Fatalf("rejected fallback persisted %d agent comments", comments)
	}
}

func TestCompleteTask_AllowsMentionedOpinionFallback(t *testing.T) {
	p := newResolveOriginatorPool(t)
	ctx := context.Background()
	workspaceID, _, responderID, issueID := seedAttributionFixture(t, p)
	requesterID := insertReplyAdmissionAgent(t, ctx, p, workspaceID)
	parentID := insertReplyAdmissionParent(t, ctx, p, workspaceID, issueID, requesterID)
	taskID := insertReplyAdmissionRunningTask(t, ctx, p, issueID, responderID, parentID)

	svc := NewTaskService(db.New(p), p, nil, events.New())
	result, _ := json.Marshal(map[string]string{
		"output": "[@Requester](mention://agent/" + requesterID + ")\n\nThe review is sound and the cost constraint is binding.",
	})
	if _, err := svc.CompleteTask(ctx, util.MustParseUUID(taskID), result, "", "", "", false, "", ""); err != nil {
		t.Fatalf("mentioned completion rejected: %v", err)
	}

	var status string
	if err := p.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "completed" {
		t.Fatalf("task status = %q, want completed", status)
	}
	var comments int
	if err := p.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1 AND author_id = $2`, issueID, responderID).Scan(&comments); err != nil {
		t.Fatalf("count fallback comments: %v", err)
	}
	if comments != 1 {
		t.Fatalf("expected one mentioned fallback comment, got %d", comments)
	}
}

func insertReplyAdmissionAgent(t *testing.T, ctx context.Context, p *pgxpool.Pool, workspaceID string) string {
	t.Helper()
	var runtimeID string
	if err := p.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE workspace_id = $1::uuid LIMIT 1`, workspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("load admission runtime: %v", err)
	}
	var agentID string
	if err := p.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		SELECT $1::uuid, 'reply-admission-requester-' || $1, 'cloud', '{}'::jsonb, $2, 'workspace', 1, user_id
		FROM member WHERE workspace_id = $1::uuid AND role = 'owner' LIMIT 1 RETURNING id`, workspaceID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("insert admission requester: %v", err)
	}
	return agentID
}

func insertReplyAdmissionParent(t *testing.T, ctx context.Context, p *pgxpool.Pool, workspaceID, issueID, requesterID string) string {
	t.Helper()
	var parentID string
	if err := p.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, 'Codex, what is your opinion on this review?') RETURNING id`,
		workspaceID, issueID, requesterID).Scan(&parentID); err != nil {
		t.Fatalf("insert admission parent: %v", err)
	}
	return parentID
}

func insertReplyAdmissionRunningTask(t *testing.T, ctx context.Context, p *pgxpool.Pool, issueID, responderID, parentID string) string {
	t.Helper()
	var runtimeID string
	if err := p.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, responderID).Scan(&runtimeID); err != nil {
		t.Fatalf("load responder runtime: %v", err)
	}
	var taskID string
	if err := p.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, context, dispatched_at, started_at)
		VALUES ($1, $2, $3, $4, 'running', 0, '{}'::jsonb, now(), now()) RETURNING id`,
		responderID, runtimeID, issueID, parentID).Scan(&taskID); err != nil {
		t.Fatalf("insert admission task: %v", err)
	}
	return taskID
}
