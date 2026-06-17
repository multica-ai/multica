package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// TestDelegationOriginTraceFallback covers CEREBRO-PATCH(delegation-origin-trace):
// an agent run with no original_user_id, on an AGENT-created issue where NO member
// has commented, must still inherit a human by following the issue's
// origin_type='agent_task' pointer back to the task that created the issue — when
// that creating task carried a human. This is the gap that left Push-Pilot-style
// agent→agent handoffs on agent-created sub-issues blocked (TECH-3629), even
// though the "På vegne af" sidebar could already resolve the same human.
func TestDelegationOriginTraceFallback(t *testing.T) {
	ctx := context.Background()

	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT id, runtime_id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}

	// A creating task that carries a human principal (the member who ultimately
	// started the chain). It has its own issue, but that is irrelevant here.
	var creatingTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, original_user_id)
		VALUES ($1, $2, 'completed', 0, NULL, $3)
		RETURNING id
	`, agentID, runtimeID, testUserID).Scan(&creatingTaskID); err != nil {
		t.Fatalf("create human-carrying task: %v", err)
	}

	// An AGENT-created issue stamped with origin_type='agent_task' → the creating
	// task above. No member ever comments on it, so the latest-member-comment
	// fallback cannot apply — only the origin trace can recover the human.
	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id, number, origin_type, origin_id)
		VALUES ($1, 'agent', $2, $3, 'agent', $2, $4, 'agent_task', $5)
		RETURNING id
	`, testWorkspaceID, agentID, "delegation origin-trace fallback", number, creatingTaskID).Scan(&issueID); err != nil {
		t.Fatalf("create agent issue with origin: %v", err)
	}

	// An origin-less running task on that issue — the agent that wants to fan out.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, original_user_id)
		VALUES ($1, $2, 'running', 0, $3, NULL)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create origin-less task: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1 OR id = $2`, issueID, creatingTaskID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	got, err := testHandler.TaskService.CommentDelegationContext(ctx, "agent", agentID, taskID, "mention")
	if err != nil {
		t.Fatalf("expected delegation to resolve via origin trace, got error: %v", err)
	}
	if !got.OriginalUserID.Valid || util.UUIDToString(got.OriginalUserID) != testUserID {
		t.Fatalf("expected original user %s via origin trace, got %v", testUserID, util.UUIDToString(got.OriginalUserID))
	}
}
