package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// FIR-2395 — Case B. When the unpause sweeper creates a resume task for a
// previously-timed-out parent, the BLOCKED noise the parent posted on the
// issue must be deleted so the user only sees the live retry. Without this
// the user sees a red "Agent-run timed out" comment immediately followed by
// a fresh successful run — confusing and untrue.
func TestUnpauseRuntime_DeletesStaleTimeoutCommentForResumedTask(t *testing.T) {
	if runtimeAccountTestPool == nil {
		t.Skip("DATABASE_URL not configured; skipping unpause-cleanup integration test")
	}
	ctx := context.Background()
	pool := runtimeAccountTestPool
	queries := cerebrodb.New(pool)
	wsID := runtimeAccountTestWSID
	runtimeID := runtimeAccountTestRuntimeID

	var agentID, issueID, otherAgentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		VALUES ($1, 'fir2395-cleanup-agent', 'local', $2)
		RETURNING id`, wsID, runtimeID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_id)
		VALUES ($1, 'fir2395-cleanup-agent-other', 'local', $2)
		RETURNING id`, wsID, runtimeID).Scan(&otherAgentID); err != nil {
		t.Fatalf("create other agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_type, creator_id)
		VALUES ($1, 'fir2395-cleanup-issue', 'member', $2)
		RETURNING id`, wsID, runtimeAccountTestUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		_, _ = pool.Exec(bg, `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2)`, agentID, otherAgentID)
		_, _ = pool.Exec(bg, `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = pool.Exec(bg, `DELETE FROM agent WHERE id IN ($1, $2)`, agentID, otherAgentID)
		_, _ = pool.Exec(bg, `UPDATE agent_runtime SET paused_at = NULL, unpause_at = NULL, pause_reason = NULL WHERE id = $1`, runtimeID)
	})

	// Parent task that "timed out" 5 minutes before pause — fits the
	// transient-fail resume window (10 min before paused_at).
	pausedAt := time.Now().UTC()
	var parentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, runtime_id, status, priority,
			started_at, completed_at, failure_reason, error
		)
		VALUES ($1, $2, $3, 'failed', 0,
		        $4::timestamptz - INTERVAL '10 minutes',
		        $4::timestamptz - INTERVAL '5 minutes',
		        'timeout', 'task timed out')
		RETURNING id`,
		agentID, issueID, runtimeID, pausedAt).Scan(&parentID); err != nil {
		t.Fatalf("insert parent timeout task: %v", err)
	}
	parentIDStr := util.UUIDToString(parentID)

	// Build BLOCKED bodies with the same shape timeoutFailureCommentBody
	// emits. Use Go string concat (Go raw strings cannot contain a backtick).
	bt := "`"
	blockedHeader := "BLOCKED: Agent-run timed out before it could post a final comment.\n\n"
	parentBody := blockedHeader + "Run: " + bt + parentIDStr + bt + "\nReason: " + bt + "task timed out" + bt + "\n"
	otherTaskBody := blockedHeader + "Run: " + bt + "00000000-0000-0000-0000-000000000999" + bt + "\n"

	insertComment := func(t *testing.T, authorID pgtype.UUID, body string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
			VALUES ($1, $2, 'agent', $3, $4, 'comment')
		`, issueID, wsID, authorID, body); err != nil {
			t.Fatalf("seed comment: %v", err)
		}
	}

	// Four comments — only the third (parent BLOCKED, same agent) must
	// disappear after unpause.
	insertComment(t, agentID, "Just a heads up — looking into this.")
	insertComment(t, agentID, otherTaskBody)
	insertComment(t, agentID, parentBody)
	insertComment(t, otherAgentID, parentBody)

	// Put the runtime in a paused state anchored at pausedAt so the
	// resume sweeper's 10-min window picks the parent task up.
	if _, err := pool.Exec(ctx, `
		UPDATE agent_runtime
		SET paused_at = $2::timestamptz,
		    unpause_at = $2::timestamptz + INTERVAL '5 minutes',
		    pause_reason = 'rate_limit'
		WHERE id = $1
	`, runtimeID, pausedAt); err != nil {
		t.Fatalf("seed pause state: %v", err)
	}

	bus := events.New()
	upstreamQueries := db.New(pool)
	taskSvc := service.NewTaskService(upstreamQueries, pool, nil, bus)
	svc := &Service{Cerebro: queries, TaskSvc: taskSvc, Bus: bus}

	if _, err := svc.UnpauseRuntime(ctx, runtimeID); err != nil {
		t.Fatalf("UnpauseRuntime: %v", err)
	}

	// Resume task must have been created (parent_task_id = parentID).
	var resumed int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM agent_task_queue WHERE parent_task_id = $1
	`, parentID).Scan(&resumed); err != nil {
		t.Fatalf("count resume tasks: %v", err)
	}
	if resumed != 1 {
		t.Fatalf("expected exactly 1 resume task for parent, got %d", resumed)
	}

	// Parent's BLOCKED comment (same agent, embeds parentID) must be gone.
	var parentBlocked int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM comment
		WHERE issue_id = $1 AND author_id = $2 AND content = $3
	`, issueID, agentID, parentBody).Scan(&parentBlocked); err != nil {
		t.Fatalf("count parent BLOCKED comments: %v", err)
	}
	if parentBlocked != 0 {
		t.Fatalf("expected parent BLOCKED comment to be deleted, found %d", parentBlocked)
	}

	// Unrelated BLOCKED (different task id, same agent) must survive.
	var otherTaskBlocked int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM comment
		WHERE issue_id = $1 AND author_id = $2 AND content = $3
	`, issueID, agentID, otherTaskBody).Scan(&otherTaskBlocked); err != nil {
		t.Fatalf("count unrelated-task BLOCKED: %v", err)
	}
	if otherTaskBlocked != 1 {
		t.Fatalf("expected unrelated-task BLOCKED comment to be preserved, got %d", otherTaskBlocked)
	}

	// BLOCKED authored by a different agent (even if it embeds parentID)
	// must survive — cleanup is scoped to the parent's agent.
	var otherAgent int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM comment WHERE issue_id = $1 AND author_id = $2
	`, issueID, otherAgentID).Scan(&otherAgent); err != nil {
		t.Fatalf("count other-agent comments: %v", err)
	}
	if otherAgent != 1 {
		t.Fatalf("expected other-agent BLOCKED comment to be preserved, got %d", otherAgent)
	}

	// Unrelated prose comment must survive.
	var prose int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM comment
		WHERE issue_id = $1
		  AND content NOT LIKE 'BLOCKED:%'
	`, issueID).Scan(&prose); err != nil {
		t.Fatalf("count prose comments: %v", err)
	}
	if prose != 1 {
		t.Fatalf("expected the heads-up comment to be preserved, got %d", prose)
	}
}
