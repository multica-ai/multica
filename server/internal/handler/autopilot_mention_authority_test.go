package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// autopilotDelegationFixture builds the MUL-4857 create_issue scenario: a
// member-created autopilot creates an issue, and its dispatched leader agent runs
// a task ON that issue and authors an @mention delegation comment whose
// source_task_id points back at that leader task. The authoring run is
// UNATTRIBUTED (originator NULL) exactly as a schedule/webhook autopilot run is.
//
// This is the shape the authority fallback must recognise — but ONLY through the
// verified lineage of the speaking task (author == task agent, task.issue_id ==
// this issue), never from the issue's autopilot provenance alone. The fields are
// exposed so negative cases can rewrite the comment's source_task_id to a foreign
// task and prove the fallback then fails closed.
type autopilotDelegationFixture struct {
	Issue         db.Issue
	LeaderAgentID string // the autopilot-dispatched agent authoring the comment
	LeaderTaskID  string // its running task on this issue (comment.source_task_id)
	Comment       db.Comment
	AutopilotID   string
	RuntimeID     string
}

func newAutopilotDelegationFixture(t *testing.T, targetAgentID, autopilotCreatorUserID, originType string) autopilotDelegationFixture {
	t.Helper()
	ctx := context.Background()

	runtimeID := handlerTestRuntimeID(t)

	// The seeded workspace agent stands in for the autopilot-dispatched leader
	// that authors the delegation comment (distinct from the mentioned target).
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}

	// A member-created autopilot; assignee is the target agent (any valid agent
	// satisfies the assignee reference).
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
	// leader agent; provenance is origin_type=autopilot + origin_id). When
	// originType is not "autopilot" the issue carries no origin, so no creator
	// can be recovered even from a perfectly-lineaged task.
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

	// The leader's dispatch task, running ON this issue. In create_issue mode the
	// leader task is enqueued through the ordinary issue-assignment path, so it
	// carries NO autopilot_run_id — the lineage that matters is agent + issue.
	// Unattributed (originator NULL) like a schedule/webhook autopilot run.
	var leaderTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'running', 0) RETURNING id
	`, leaderID, runtimeID, issueID).Scan(&leaderTaskID); err != nil {
		t.Fatalf("create leader task: %v", err)
	}

	// The delegation comment: authored by the leader agent, mentioning the target,
	// with source_task_id pointing back at the leader's running task (the lineage
	// the reconcile/edit path reads).
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content, source_task_id)
		VALUES ($1, $2, 'agent', $3, $4, $5) RETURNING id
	`, testWorkspaceID, issueID, leaderID, "[@Worker](mention://agent/"+targetAgentID+") please take this", leaderTaskID).Scan(&commentID); err != nil {
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
	return autopilotDelegationFixture{
		Issue:         issue,
		LeaderAgentID: leaderID,
		LeaderTaskID:  leaderTaskID,
		Comment:       comment,
		AutopilotID:   autopilotID,
		RuntimeID:     runtimeID,
	}
}

// setCommentSourceTask rewrites the fixture comment's source_task_id and reloads
// the row, so a test can point the lineage at a foreign task (or clear it).
func setCommentSourceTask(t *testing.T, fx *autopilotDelegationFixture, sourceTaskID any) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE comment SET source_task_id = $1 WHERE id = $2`, sourceTaskID, uuidToString(fx.Comment.ID)); err != nil {
		t.Fatalf("rewrite comment source_task_id: %v", err)
	}
	c, err := testHandler.Queries.GetComment(context.Background(), fx.Comment.ID)
	if err != nil {
		t.Fatalf("reload comment: %v", err)
	}
	fx.Comment = c
}

// seedTaskOnIssue inserts a running task for the given agent on the given issue
// and returns its id, for building foreign-lineage negative cases.
func seedTaskOnIssue(t *testing.T, agentID, issueID, runtimeID string) string {
	t.Helper()
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'running', 0) RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed task on issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

// TestAutopilotDelegationAuthority_LineageBinding is the MUL-4857 fix, guarded by
// the review's confused-deputy finding: an unattributed autopilot run may borrow
// its autopilot creator's invoke rights to delegate mid-chain, but ONLY when the
// speaking task's lineage is verified against THIS issue — never from the issue's
// autopilot provenance plus an empty originator alone.
func TestAutopilotDelegationAuthority_LineageBinding(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// agentID: private (default) agent owned by ownerID. plainMemberID: unrelated.
	agentID, ownerID, plainMemberID := privateAgentTestFixture(t)

	// authorityFor resolves the delegation authority the reconcile/edit path uses,
	// straight from the persisted comment's source_task_id lineage.
	authorityFor := func(fx autopilotDelegationFixture) string {
		return testHandler.autopilotDelegationAuthorityFromComment(ctx, fx.Issue, fx.Comment)
	}
	// mentionTriggersTarget wires that resolved authority into the trigger compute
	// exactly as the live paths do, and reports whether the private target fires.
	mentionTriggersTarget := func(fx autopilotDelegationFixture) bool {
		triggers, _ := testHandler.computeCommentAgentTriggers(
			ctx, fx.Issue, fx.Comment.Content, nil, "agent", fx.LeaderAgentID,
			commentTriggerComputeOptions{
				ExcludeTriggerCommentID:            fx.Comment.ID,
				AutopilotDelegationAuthorityUserID: authorityFor(fx),
			},
		)
		for _, tr := range triggers {
			if uuidToString(tr.Agent.ID) == agentID {
				return true
			}
		}
		return false
	}

	t.Run("verified lineage + creator owns target -> triggers", func(t *testing.T) {
		fx := newAutopilotDelegationFixture(t, agentID, ownerID, "autopilot")
		if got := authorityFor(fx); got != ownerID {
			t.Fatalf("delegation authority = %q, want autopilot creator %q", got, ownerID)
		}
		if !mentionTriggersTarget(fx) {
			t.Fatal("expected the private agent to be triggered via the lineage-verified autopilot-creator authority")
		}
	})

	t.Run("creator cannot invoke target -> still denied", func(t *testing.T) {
		// Lineage is perfect but the creator (plainMemberID) is neither the target's
		// owner nor on any allow-list: the authority resolves but the gate denies.
		fx := newAutopilotDelegationFixture(t, agentID, plainMemberID, "autopilot")
		if got := authorityFor(fx); got != plainMemberID {
			t.Fatalf("delegation authority = %q, want %q", got, plainMemberID)
		}
		if mentionTriggersTarget(fx) {
			t.Fatal("autopilot creator without invoke rights must not reach a private agent")
		}
	})

	t.Run("non-autopilot issue -> no authority", func(t *testing.T) {
		fx := newAutopilotDelegationFixture(t, agentID, ownerID, "")
		if got := authorityFor(fx); got != "" {
			t.Fatalf("non-autopilot issue must resolve no authority, got %q", got)
		}
		if mentionTriggersTarget(fx) {
			t.Fatal("a non-autopilot unattributed run must not invoke a private agent")
		}
	})

	t.Run("missing source task -> no authority", func(t *testing.T) {
		// The previous fix's blind spot: an unattributed comment with no verifiable
		// lineage (source_task_id NULL) must NOT inherit the creator's authority.
		fx := newAutopilotDelegationFixture(t, agentID, ownerID, "autopilot")
		setCommentSourceTask(t, &fx, nil)
		if got := authorityFor(fx); got != "" {
			t.Fatalf("comment without source_task_id must resolve no authority, got %q", got)
		}
		if mentionTriggersTarget(fx) {
			t.Fatal("a comment with no verifiable task lineage must not borrow creator authority")
		}
	})

	t.Run("source task on a different issue -> no authority", func(t *testing.T) {
		// Confused-deputy: a run working on ANOTHER issue comments here. Its task's
		// issue_id != this issue, so it cannot borrow this autopilot's authority even
		// though its agent authored the comment.
		fx := newAutopilotDelegationFixture(t, agentID, ownerID, "autopilot")
		other := newAutopilotDelegationFixture(t, agentID, ownerID, "autopilot")
		foreignTask := seedTaskOnIssue(t, fx.LeaderAgentID, uuidToString(other.Issue.ID), fx.RuntimeID)
		setCommentSourceTask(t, &fx, foreignTask)
		if got := authorityFor(fx); got != "" {
			t.Fatalf("cross-issue source task must resolve no authority, got %q", got)
		}
		if mentionTriggersTarget(fx) {
			t.Fatal("a task from a different issue must not borrow this autopilot's creator authority")
		}
	})

	t.Run("author is not the source task's agent -> no authority", func(t *testing.T) {
		// The comment author is the leader, but its source task belongs to a
		// different agent (the target). Author/agent mismatch fails closed.
		fx := newAutopilotDelegationFixture(t, agentID, ownerID, "autopilot")
		mismatchTask := seedTaskOnIssue(t, agentID, uuidToString(fx.Issue.ID), fx.RuntimeID)
		setCommentSourceTask(t, &fx, mismatchTask)
		if got := authorityFor(fx); got != "" {
			t.Fatalf("author != task agent must resolve no authority, got %q", got)
		}
		if mentionTriggersTarget(fx) {
			t.Fatal("a source task owned by a different agent must not confer authority on the comment author")
		}
	})
}

// TestCreateComment_AutopilotLeaderMentionEnqueuesPrivateWorker is the MUL-4857
// end-to-end: the autopilot-dispatched leader posts an @mention delegation on the
// autopilot-created issue through the real HTTP CreateComment surface (X-Agent-ID
// + X-Task-ID), and the mentioned DEFAULT-private worker is actually enqueued —
// keyed on the autopilot creator's invoke rights, resolved from the request's
// trusted X-Task-ID lineage. This exercises handler -> comment persistence ->
// trigger -> enqueue, not just the compute function.
func TestCreateComment_AutopilotLeaderMentionEnqueuesPrivateWorker(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	// Private worker owned by ownerID; the autopilot is created by that same owner
	// so the creator legitimately owns the worker.
	workerID, ownerID, _ := privateAgentTestFixture(t)
	fx := newAutopilotDelegationFixture(t, workerID, ownerID, "autopilot")
	issueID := uuidToString(fx.Issue.ID)

	// The leader posts the mention comment in its agent identity. resolveActor
	// trusts the header pair because fx.LeaderTaskID belongs to the leader agent.
	w := httptest.NewRecorder()
	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@Worker](mention://agent/" + workerID + ") please handle",
	})
	r.Header.Set("X-Agent-ID", fx.LeaderAgentID)
	r.Header.Set("X-Task-ID", fx.LeaderTaskID)
	r = withURLParam(r, "id", issueID)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("leader mention CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var workerTasks int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, issueID, workerID).Scan(&workerTasks); err != nil {
		t.Fatalf("count worker tasks: %v", err)
	}
	if workerTasks != 1 {
		t.Fatalf("expected the private worker to be enqueued once via autopilot-creator authority, got %d queued tasks", workerTasks)
	}

	// The enqueued run must stay UNATTRIBUTED: the creator authority is used for
	// the gate only, never written onto the delegated task's originator (MUL-4302).
	var workerOriginatorValid bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT originator_user_id IS NOT NULL FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, issueID, workerID).Scan(&workerOriginatorValid); err != nil {
		t.Fatalf("read worker originator: %v", err)
	}
	if workerOriginatorValid {
		t.Fatal("the delegated worker task must remain unattributed; the creator authority is authorization-only")
	}
}
