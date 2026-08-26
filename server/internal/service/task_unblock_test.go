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

// unblockFixture builds two issues (blocker + dependent) with a blocked_by edge
// and a CANCELLED task on the dependent issue, mirroring the production bug
// (blocker task 1938cec0 / issue 2080fe8f, dependent task 00c7f45c). Returns the
// service, blocker issue id, and the dependent's cancelled task id.
func unblockFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (*TaskService, string, string) {
	t.Helper()
	suffix := time.Now().UnixNano()

	var userID, workspaceID, rtID, agentID, blockerID, dependentID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1,$2) RETURNING id`,
		"Unblock Test", fmt.Sprintf("unblock-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO workspace (name, slug, description, issue_prefix) VALUES ($1,$2,$3,$4) RETURNING id`,
		"Unblock Test", fmt.Sprintf("unblock-%d", suffix), "temp unblock test", "UBK").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1,$2,'owner')`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at, visibility, owner_id)
		VALUES ($1, 'daemon-unblock', 'Unblock RT', 'cloud', 'unblock_provider', 'online', 'test runtime', '{}'::jsonb, now(), 'private', $2)
		RETURNING id`, workspaceID, userID).Scan(&rtID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id)
		VALUES ($1, 'Unblock Agent', '', 'cloud', '{}'::jsonb, $2, 'private', 5, $3)
		RETURNING id`, workspaceID, rtID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	mkIssue := func(n int, st string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
			VALUES ($1, $2, $3, 'none', $4, 'member', $5, $6)
			RETURNING id`, workspaceID, fmt.Sprintf("unblock issue %d", n), st, userID, 800000+n, n).Scan(&id); err != nil {
			t.Fatalf("create issue %d: %v", n, err)
		}
		return id
	}
	blockerID = mkIssue(1, "in_progress")
	dependentID = mkIssue(2, "in_progress")
	if _, err := pool.Exec(ctx, `
		INSERT INTO issue_dependency (issue_id, depends_on_issue_id, type)
		VALUES ($1, $2, 'blocked_by')`, dependentID, blockerID); err != nil {
		t.Fatalf("create dependency: %v", err)
	}
	var dependentTaskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, status, priority, context, runtime_id)
		VALUES ($1, $2, 'cancelled', 0, '{}'::jsonb, $3)
		RETURNING id`, agentID, dependentID, rtID).Scan(&dependentTaskID); err != nil {
		t.Fatalf("create cancelled task: %v", err)
	}

	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM agent_task_queue WHERE issue_id IN ($1,$2)`, dependentID, blockerID)
		pool.Exec(c, `DELETE FROM issue_dependency WHERE issue_id IN ($1,$2) OR depends_on_issue_id IN ($1,$2)`, dependentID, blockerID)
		pool.Exec(c, `DELETE FROM issue WHERE workspace_id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM agent WHERE id = $1`, agentID)
		pool.Exec(c, `DELETE FROM agent_runtime WHERE id = $1`, rtID)
		pool.Exec(c, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
		pool.Exec(c, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return NewTaskService(db.New(pool), pool, nil, events.New()), blockerID, dependentTaskID
}

// TestAutoSyncFlipsBlockerDoneAndUnblocksDependent verifies the bug fix: a
// completed blocker task flips its issue to done (no longer gated on a branch
// push / in_review), and the dependent's cancelled task is reactivated to queued
// so the blocked_by claim gate clears.
func TestAutoSyncFlipsBlockerDoneAndUnblocksDependent(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	svc, blockerID, dependentTaskID := unblockFixture(t, ctx, pool)

	blockerUUID := util.MustParseUUID(blockerID)
	// Simulate a blocker task completing (no branch) on the blocker issue.
	svc.autoSyncIssueStatusOnCompletion(ctx, db.AgentTaskQueue{IssueID: blockerUUID}, "", nil)

	var blockerStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, blockerID).Scan(&blockerStatus); err != nil {
		t.Fatalf("read blocker status: %v", err)
	}
	if blockerStatus != "done" {
		t.Fatalf("blocker issue status = %q, want done", blockerStatus)
	}

	var dependentStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, dependentTaskID).Scan(&dependentStatus); err != nil {
		t.Fatalf("read dependent task status: %v", err)
	}
	if dependentStatus != "queued" {
		t.Fatalf("dependent task status = %q, want queued", dependentStatus)
	}
}

// TestMaybeUnblockDependentsSkipsOpenBlocker confirms a dependent is NOT
// reactivated while any blocker remains open.
func TestMaybeUnblockDependentsSkipsOpenBlocker(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	svc, blockerID, dependentTaskID := unblockFixture(t, ctx, pool)

	// Blocker still open: dependents must stay cancelled.
	svc.maybeUnblockDependents(ctx, util.MustParseUUID(blockerID))

	var dependentStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, dependentTaskID).Scan(&dependentStatus); err != nil {
		t.Fatalf("read dependent task status: %v", err)
	}
	if dependentStatus != "cancelled" {
		t.Fatalf("dependent task status = %q, want still cancelled", dependentStatus)
	}
}
