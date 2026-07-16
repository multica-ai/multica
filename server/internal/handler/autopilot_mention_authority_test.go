package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// autopilotDelegationFixture builds the MUL-4857 scenario: an agent-authored
// @mention delegation comment on an issue, where the authoring run is
// UNATTRIBUTED (empty originator) exactly as a schedule/webhook autopilot run
// is. When originType == "autopilot" the issue is stamped autopilot-origin with
// the given member as the autopilot creator, so invokeAuthorityForAutopilotIssue
// can recover that creator as the invoke authority.
type autopilotDelegationFixture struct {
	Issue         db.Issue
	LeaderAgentID string // the autopilot-dispatched agent authoring the comment
	Comment       db.Comment
}

func newAutopilotDelegationFixture(t *testing.T, targetAgentID, autopilotCreatorUserID, originType string) autopilotDelegationFixture {
	t.Helper()
	ctx := context.Background()

	// The seeded workspace agent stands in for the autopilot-dispatched leader
	// that authors the delegation comment (distinct from the mentioned target).
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}

	// A member-created autopilot; assignee is the target agent (any valid agent
	// satisfies the assignee FK).
	var autopilotID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
		VALUES ($1, 'MUL-4857 delegation', $2, 'create_issue', 'member', $3) RETURNING id
	`, testWorkspaceID, targetAgentID, autopilotCreatorUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("create autopilot: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID) })

	// Next per-workspace issue number (default 0 would trip uq_issue_workspace_number).
	var number int
	if err := testPool.QueryRow(ctx, `
		UPDATE workspace
		SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}

	// The issue mirrors an autopilot-created issue (creator is the dispatched
	// agent; provenance is origin_type=autopilot + origin_id). When originType is
	// not "autopilot" the issue carries no origin, so no creator can be recovered.
	var originTypeArg, originIDArg any
	if originType == "autopilot" {
		originTypeArg = "autopilot"
		originIDArg = autopilotID
	}
	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id, number, origin_type, origin_id)
		VALUES ($1, 'agent', $2, 'MUL-4857 delegation issue', 'agent', $2, $3, $4, $5)
		RETURNING id
	`, testWorkspaceID, leaderID, number, originTypeArg, originIDArg).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// The delegation comment: authored by the leader agent, mentioning the target.
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
		VALUES ($1, $2, 'agent', $3, $4) RETURNING id
	`, testWorkspaceID, issueID, leaderID, "[@Worker](mention://agent/"+targetAgentID+") please take this").Scan(&commentID); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	comment, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(commentID))
	if err != nil {
		t.Fatalf("load comment: %v", err)
	}
	return autopilotDelegationFixture{Issue: issue, LeaderAgentID: leaderID, Comment: comment}
}

// TestComputeCommentAgentTriggers_AutopilotDelegationUsesCreatorAuthority is the
// MUL-4857 regression: an unattributed autopilot run that @mentions a DEFAULT
// private agent must still enqueue that agent — keyed on the autopilot creator's
// invoke rights (the same principal the first dispatch used), never by reopening
// unrestricted agent-to-agent invocation. The fallback is scoped to
// autopilot-origin issues and preserves least privilege.
func TestComputeCommentAgentTriggers_AutopilotDelegationUsesCreatorAuthority(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// agentID: private agent owned by ownerID. plainMemberID: an unrelated member.
	agentID, ownerID, plainMemberID := privateAgentTestFixture(t)

	// mentionTriggersTarget reports whether the private agent is in the resolved
	// trigger set. The authoring run is unattributed (OriginatorUserID empty),
	// matching a schedule/webhook autopilot dispatch, so admission hinges purely
	// on the issue's autopilot origin + the creator's invoke rights.
	mentionTriggersTarget := func(t *testing.T, fx autopilotDelegationFixture) bool {
		t.Helper()
		triggers, _ := testHandler.computeCommentAgentTriggers(
			ctx, fx.Issue, fx.Comment.Content, nil, "agent", fx.LeaderAgentID,
			commentTriggerComputeOptions{ExcludeTriggerCommentID: fx.Comment.ID},
		)
		for _, tr := range triggers {
			if uuidToString(tr.Agent.ID) == agentID {
				return true
			}
		}
		return false
	}

	t.Run("autopilot creator owns target -> mention triggers", func(t *testing.T) {
		fx := newAutopilotDelegationFixture(t, agentID, ownerID, "autopilot")
		if !mentionTriggersTarget(t, fx) {
			t.Fatal("expected the private agent to be triggered via the autopilot-creator authority fallback")
		}
	})

	t.Run("non-autopilot issue stays fail-closed", func(t *testing.T) {
		// Same private agent + empty originator, but the issue has no autopilot
		// origin: the fallback must not fire, preserving the MUL-3963 A2A gate.
		fx := newAutopilotDelegationFixture(t, agentID, ownerID, "")
		if mentionTriggersTarget(t, fx) {
			t.Fatal("a non-autopilot unattributed run must not invoke a private agent")
		}
	})

	t.Run("autopilot creator cannot invoke target -> still denied", func(t *testing.T) {
		// The creator (plainMemberID) is neither the private agent's owner nor on
		// any allow-list, so the fallback resolves them but the gate still denies:
		// the fix restores authority, it does not grant blanket access.
		fx := newAutopilotDelegationFixture(t, agentID, plainMemberID, "autopilot")
		if mentionTriggersTarget(t, fx) {
			t.Fatal("autopilot creator without invoke rights must not reach a private agent")
		}
	})
}
