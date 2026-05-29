package handler

// CEREBRO-PATCH(private-agent-owner-only-trigger): regression coverage for the
// owner-only @mention trigger gate on private agents (FIR-2349). Only the
// owning member's sphere — the owner, or an agent owned by that same member —
// may wake a private agent via mention. Workspace admins and cross-owner
// agents are blocked; workspace-visibility agents are unaffected.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// newTriggerTestIssueComment seeds an unassigned issue plus a comment authored
// by (authorType, authorID) that @mentions targetAgentID, then returns the
// loaded issue + comment so a test can drive enqueueMentionedAgentTasks
// directly. The issue is intentionally unassigned so only the mention branch
// fires.
func newTriggerTestIssueComment(t *testing.T, targetAgentID, authorType, authorID string) (db.Issue, db.Comment) {
	t.Helper()
	ctx := context.Background()

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
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, number)
		VALUES ($1, 'member', $2, $3, $4)
		RETURNING id
	`, testWorkspaceID, testUserID, fmt.Sprintf("private trigger gate %d", time.Now().UnixNano()), number).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	content := fmt.Sprintf("[@Target](mention://agent/%s) please take a look", targetAgentID)
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, testWorkspaceID, issueID, authorType, authorID, content).Scan(&commentID); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	comment, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(commentID))
	if err != nil {
		t.Fatalf("load comment: %v", err)
	}
	return issue, comment
}

func reassignAgentOwner(t *testing.T, agentID, ownerID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET owner_id = $1 WHERE id = $2`, ownerID, agentID); err != nil {
		t.Fatalf("reassign agent owner: %v", err)
	}
}

func TestPrivateAgentTrigger_OwnerMemberEnqueues(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	target := createHandlerTestAgent(t, "Trigger Owner Member Target", nil) // private, owner=testUserID
	issue, comment := newTriggerTestIssueComment(t, target, "member", testUserID)

	testHandler.enqueueMentionedAgentTasks(ctx, nil, issue, comment, nil, "member", testUserID, service.TaskDelegationContext{}, nil, pgtype.UUID{})

	if got := countQueuedOrDispatched(t, target, uuidToString(issue.ID)); got != 1 {
		t.Fatalf("owner member mention of private agent: expected 1 task, got %d", got)
	}
}

func TestPrivateAgentTrigger_NonOwnerMemberBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	target := createHandlerTestAgent(t, "Trigger NonOwner Member Target", nil)
	other := createWorkspaceMemberUser(t, "member")
	issue, comment := newTriggerTestIssueComment(t, target, "member", other)

	testHandler.enqueueMentionedAgentTasks(ctx, nil, issue, comment, nil, "member", other, service.TaskDelegationContext{}, nil, pgtype.UUID{})

	if got := countQueuedOrDispatched(t, target, uuidToString(issue.ID)); got != 0 {
		t.Fatalf("non-owner member mention of private agent: expected 0 tasks, got %d", got)
	}
}

func TestPrivateAgentTrigger_AdminNonOwnerBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	target := createHandlerTestAgent(t, "Trigger Admin Target", nil)
	admin := createWorkspaceMemberUser(t, "admin")
	issue, comment := newTriggerTestIssueComment(t, target, "member", admin)

	testHandler.enqueueMentionedAgentTasks(ctx, nil, issue, comment, nil, "member", admin, service.TaskDelegationContext{}, nil, pgtype.UUID{})

	// Owner-only trigger removes the admin override that canAccessPrivateAgent
	// still grants for view/edit — an admin cannot wake someone else's agent.
	if got := countQueuedOrDispatched(t, target, uuidToString(issue.ID)); got != 0 {
		t.Fatalf("admin non-owner mention of private agent: expected 0 tasks, got %d", got)
	}
}

func TestPrivateAgentTrigger_SameOwnerAgentEnqueues(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	actor := createHandlerTestAgent(t, "Trigger SameOwner Actor", nil)  // owner=testUserID
	target := createHandlerTestAgent(t, "Trigger SameOwner Target", nil) // owner=testUserID
	issue, comment := newTriggerTestIssueComment(t, target, "agent", actor)

	testHandler.enqueueMentionedAgentTasks(ctx, nil, issue, comment, nil, "agent", actor, service.TaskDelegationContext{}, nil, pgtype.UUID{})

	if got := countQueuedOrDispatched(t, target, uuidToString(issue.ID)); got != 1 {
		t.Fatalf("same-owner agent mention of private agent: expected 1 task, got %d", got)
	}
}

func TestPrivateAgentTrigger_CrossOwnerAgentBlocked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	otherUser := createWorkspaceMemberUser(t, "member")
	actor := createHandlerTestAgent(t, "Trigger CrossOwner Actor", nil)
	reassignAgentOwner(t, actor, otherUser)
	target := createHandlerTestAgent(t, "Trigger CrossOwner Target", nil) // owner=testUserID
	issue, comment := newTriggerTestIssueComment(t, target, "agent", actor)

	testHandler.enqueueMentionedAgentTasks(ctx, nil, issue, comment, nil, "agent", actor, service.TaskDelegationContext{}, nil, pgtype.UUID{})

	if got := countQueuedOrDispatched(t, target, uuidToString(issue.ID)); got != 0 {
		t.Fatalf("cross-owner agent mention of private agent: expected 0 tasks, got %d", got)
	}
}

func TestPrivateAgentTrigger_WorkspaceAgentUnaffected(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	target := createHandlerTestAgent(t, "Trigger Workspace Target", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET visibility = 'workspace' WHERE id = $1`, target); err != nil {
		t.Fatalf("make agent workspace-visible: %v", err)
	}
	other := createWorkspaceMemberUser(t, "member")
	issue, comment := newTriggerTestIssueComment(t, target, "member", other)

	testHandler.enqueueMentionedAgentTasks(ctx, nil, issue, comment, nil, "member", other, service.TaskDelegationContext{}, nil, pgtype.UUID{})

	if got := countQueuedOrDispatched(t, target, uuidToString(issue.ID)); got != 1 {
		t.Fatalf("non-owner member mention of workspace agent: expected 1 task, got %d", got)
	}
}
