package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// dependencyGateFixture provisions one online runtime + agent with one queued
// task on issue A, and issue B. When blocked is true an issue_dependency row
// (A 'blocked_by' B) exists. Returns agentID, blockerIssueID.
func dependencyGateFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, blocked bool) (string, string) {
	t.Helper()
	suffix := time.Now().UnixNano()

	var userID, workspaceID, rtID, agentID, issueAID, issueBID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1,$2) RETURNING id`,
		"Dep Gate Test", fmt.Sprintf("dep-gate-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1,$2,$3,$4) RETURNING id`,
		"Dep Gate Test", fmt.Sprintf("dep-gate-%d", suffix), "temp dep gate test workspace", "DGT").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1,$2,'owner')`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, 'daemon-dep-gate', 'Dep Gate RT', 'cloud', 'dep_gate_provider', 'online', 'test runtime', '{}'::jsonb, now(), 'private', $2)
		RETURNING id`, workspaceID, userID).Scan(&rtID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'Dep Gate Agent', '', 'cloud', '{}'::jsonb, $2, 'private', 5, $3)
		RETURNING id`, workspaceID, rtID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	mkIssue := func(n int) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
			VALUES ($1, $2, 'in_progress', 'none', $3, 'member', $4, $5)
			RETURNING id`, workspaceID, fmt.Sprintf("dep gate issue %d", n), userID, 900000+n, n).Scan(&id); err != nil {
			t.Fatalf("create issue %d: %v", n, err)
		}
		return id
	}
	issueAID = mkIssue(1)
	issueBID = mkIssue(2)
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, status, priority, context, runtime_id)
		VALUES ($1, $2, 'queued', 0, '{}'::jsonb, $3)`, agentID, issueAID, rtID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if blocked {
		if _, err := pool.Exec(ctx, `
			INSERT INTO issue_dependency (issue_id, depends_on_issue_id, type)
			VALUES ($1, $2, 'blocked_by')`, issueAID, issueBID); err != nil {
			t.Fatalf("create dependency: %v", err)
		}
	}

	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		pool.Exec(c, `DELETE FROM issue_dependency WHERE issue_id IN ($1,$2) OR depends_on_issue_id IN ($1,$2)`, issueAID, issueBID)
		pool.Exec(c, `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM agent WHERE id = $1`, agentID)
		pool.Exec(c, `DELETE FROM agent_runtime WHERE id = $1`, rtID)
		pool.Exec(c, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
		pool.Exec(c, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return agentID, issueBID
}

func claimDependencyGateTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool, agentID string) *db.AgentTaskQueue {
	t.Helper()
	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	task, err := svc.ClaimTask(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	return task
}

// TestClaimTask_BlockedByOpenDependency verifies GAP-13: a queued task whose
// issue has a 'blocked_by' edge to a non-terminal blocker stays queued.
func TestClaimTask_BlockedByOpenDependency(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	agentID, _ := dependencyGateFixture(t, ctx, pool, true)

	if task := claimDependencyGateTask(t, ctx, pool, agentID); task != nil {
		t.Fatalf("task %s dispatched while blocker open; want still queued", util.UUIDToString(task.ID))
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue ORDER BY created_at DESC LIMIT 1`).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "queued" {
		t.Fatalf("task status = %q, want queued behind blocker", status)
	}
}

// TestClaimTask_DependencyClearedDispatches verifies unblocking: once the
// blocker reaches a terminal status the same task dispatches normally.
func TestClaimTask_DependencyClearedDispatches(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	agentID, blockerID := dependencyGateFixture(t, ctx, pool, true)

	if _, err := pool.Exec(ctx, `UPDATE issue SET status = 'done' WHERE id = $1`, blockerID); err != nil {
		t.Fatalf("close blocker: %v", err)
	}
	task := claimDependencyGateTask(t, ctx, pool, agentID)
	if task == nil {
		t.Fatal("no task dispatched after blocker closed; want dispatch")
	}
	if task.Status != "dispatched" {
		t.Fatalf("task status = %q, want dispatched", task.Status)
	}
}

// TestClaimTask_NoDependencyUnchanged verifies opt-in: no dependency rows means
// dispatch behaves exactly as before.
func TestClaimTask_NoDependencyUnchanged(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	agentID, _ := dependencyGateFixture(t, ctx, pool, false)

	task := claimDependencyGateTask(t, ctx, pool, agentID)
	if task == nil || task.Status != "dispatched" {
		t.Fatalf("task = %+v, want dispatched with no dependency edges", task)
	}
}
