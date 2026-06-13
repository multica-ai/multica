package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// TestDelegationLatestHumanFallback covers CEREBRO-PATCH(delegation-latest-human-fallback):
// an agent run whose task carries no original_user_id, working on an issue that
// was NOT created by a member (here: agent-created), must still inherit a human
// principal from the most recent member comment on the issue. Without this an
// agent replying to a person on its own self-created issue cannot fan out to
// another agent — the exact failure observed when Mia replied to Jesper and
// could not start Sara.
func TestDelegationLatestHumanFallback(t *testing.T) {
	ctx := context.Background()

	// Seeded workspace-visible agent (has a runtime).
	var agentID, runtimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT id, runtime_id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}

	// Agent-created issue (creator_type='agent'), assigned to the same agent —
	// so the existing issue-creator-member fallback does NOT apply.
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
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id, number)
		VALUES ($1, 'agent', $2, $3, 'agent', $2, $4)
		RETURNING id
	`, testWorkspaceID, agentID, "delegation latest-human fallback", number).Scan(&issueID); err != nil {
		t.Fatalf("create agent issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// An origin-less running task for (agent, issue) — mirrors a wakeup-started
	// or otherwise un-seeded run.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id, original_user_id)
		VALUES ($1, $2, 'running', 0, $3, NULL)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create origin-less task: %v", err)
	}

	// Before any human has commented: no human to inherit → denied.
	if _, err := testHandler.TaskService.CommentDelegationContext(ctx, "agent", agentID, taskID, "mention"); err == nil {
		t.Fatalf("expected delegation denied before any member comment, got nil error")
	}

	// A human (member) comments on the issue.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, $4)
	`, testWorkspaceID, issueID, testUserID, "Ja vi skal vel have det ind?"); err != nil {
		t.Fatalf("insert member comment: %v", err)
	}

	// Now the agent run inherits that human as its delegation origin.
	got, err := testHandler.TaskService.CommentDelegationContext(ctx, "agent", agentID, taskID, "mention")
	if err != nil {
		t.Fatalf("expected delegation to resolve via latest member comment, got error: %v", err)
	}
	if !got.OriginalUserID.Valid || util.UUIDToString(got.OriginalUserID) != testUserID {
		t.Fatalf("expected original user %s, got %v", testUserID, util.UUIDToString(got.OriginalUserID))
	}
}
