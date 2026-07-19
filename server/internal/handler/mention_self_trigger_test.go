package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A member mention is informational and must not fall through to either
// assignee route when the author is that assignee (or its squad leader).
// This is database-free because member mentions stop at the trigger boundary.
func TestComputeCommentAgentTriggers_AssigneeAuthorMemberMentionIsNoop(t *testing.T) {
	h := &Handler{}
	memberMention := "decision needed from [@Dmitry](mention://member/22222222-2222-2222-2222-222222222222)"

	for _, assigneeType := range []string{"agent", "squad"} {
		t.Run(assigneeType, func(t *testing.T) {
			issue := db.Issue{
				AssigneeType: pgtype.Text{String: assigneeType, Valid: true},
				AssigneeID:   util.MustParseUUID("11111111-1111-1111-1111-111111111111"),
			}
			triggers, targets := h.computeCommentAgentTriggers(context.Background(), issue, memberMention, nil, "agent", "11111111-1111-1111-1111-111111111111", commentTriggerComputeOptions{})
			if len(triggers) != 0 || len(targets) != 0 {
				t.Fatalf("member-only comment by %s owner produced triggers=%d targets=%d", assigneeType, len(triggers), len(targets))
			}
		})
	}
}

// enqueueMentionedAgentTasksForTest mirrors the production comment path for
// @mention triggers: compute the cascade trigger set, then enqueue it. Kept as a
// test helper so these integration tests keep asserting enqueue side effects.
func enqueueMentionedAgentTasksForTest(t *testing.T, ctx context.Context, issue db.Issue, comment db.Comment, parentComment *db.Comment, authorType, authorID string, opts commentTriggerComputeOptions) {
	t.Helper()
	triggers, _ := testHandler.computeCommentAgentTriggers(ctx, issue, comment.Content, parentComment, authorType, authorID, opts)
	testHandler.enqueueCommentAgentTriggers(ctx, issue, comment.ID, triggers)
}

// selfMentionFixture wires the seeded "Handler Test Agent" as J plus two
// fresh issues so we can exercise the agent-self-mention path on the @mention
// branch of computeCommentAgentTriggers. The three tests below cover
// the narrow loop-safe self-mention behavior:
//
//   - cross-issue self-mention from a trusted task enqueues
//   - same-issue self-mention with an in-flight task does not enqueue a follow-up
//   - same-issue self-mention with a queued/dispatched task is deduped
//     (HasPendingTaskForIssueAndAgent still does its job)
type selfMentionFixture struct {
	JID        string
	RuntimeID  string
	IssueAID   string // primary issue (used for same-issue scenarios)
	IssueA     db.Issue
	IssueBID   string // a second issue (used for the cross-issue scenario)
	IssueB     db.Issue
	CommentAID string // a comment on IssueA authored by J — used as the trigger
	CommentA   db.Comment
	CommentBID string // a comment on IssueB authored by J — used as the trigger
	CommentB   db.Comment
}

func newSelfMentionFixture(t *testing.T) selfMentionFixture {
	t.Helper()
	ctx := context.Background()

	// Reuse the seeded workspace-visible agent — it already has a runtime.
	var jID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at ASC LIMIT 1
	`, testWorkspaceID).Scan(&jID); err != nil {
		t.Fatalf("load seeded agent: %v", err)
	}
	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, jID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}

	insertIssue := func(title string) string {
		t.Helper()
		// Pick the next per-workspace issue number; without it both inserts
		// land on the default number=0 and trip uq_issue_workspace_number.
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (workspace_id, creator_type, creator_id, title, assignee_type, assignee_id, number)
			VALUES ($1, 'member', $2, $3, 'agent', $4, $5)
			RETURNING id
		`, testWorkspaceID, testUserID, title, jID, number).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, id)
			testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, id)
			testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id)
		})
		return id
	}

	insertJComment := func(issueID, content string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (workspace_id, issue_id, author_type, author_id, content)
			VALUES ($1, $2, 'agent', $3, $4)
			RETURNING id
		`, testWorkspaceID, issueID, jID, content).Scan(&id); err != nil {
			t.Fatalf("create comment on %s: %v", issueID, err)
		}
		return id
	}

	issueAID := insertIssue("self-mention test A (same-issue scenarios)")
	issueBID := insertIssue("self-mention test B (parent issue, cross-issue handoff)")

	commentAID := insertJComment(issueAID, "[@J](mention://agent/"+jID+") follow-up coming")
	commentBID := insertJComment(issueBID, "Child issue done — [@J](mention://agent/"+jID+") please wrap up here")

	issueA, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueAID))
	if err != nil {
		t.Fatalf("load issueA: %v", err)
	}
	issueB, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueBID))
	if err != nil {
		t.Fatalf("load issueB: %v", err)
	}
	commentA, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(commentAID))
	if err != nil {
		t.Fatalf("load commentA: %v", err)
	}
	commentB, err := testHandler.Queries.GetComment(ctx, util.MustParseUUID(commentBID))
	if err != nil {
		t.Fatalf("load commentB: %v", err)
	}

	return selfMentionFixture{
		JID:        jID,
		RuntimeID:  runtimeID,
		IssueAID:   issueAID,
		IssueA:     issueA,
		IssueBID:   issueBID,
		IssueB:     issueB,
		CommentAID: commentAID,
		CommentA:   commentA,
		CommentBID: commentBID,
		CommentB:   commentB,
	}
}

// countQueuedOrDispatched returns the number of queued|dispatched tasks for
// (agent, issue). Mirrors the predicate used by HasPendingTaskForIssueAndAgent.
func countQueuedOrDispatched(t *testing.T, agentID, issueID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')
	`, issueID, agentID).Scan(&n); err != nil {
		t.Fatalf("count queued/dispatched tasks: %v", err)
	}
	return n
}

// TestEnqueueMentionedAgentTasks_SelfMentionCrossIssueEnqueues preserves the
// explicit child→parent handoff when the trusted authoring task is on another issue.
func TestEnqueueMentionedAgentTasks_SelfMentionCrossIssueEnqueues(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSelfMentionFixture(t)

	if got := countQueuedOrDispatched(t, fx.JID, fx.IssueBID); got != 0 {
		t.Fatalf("before: expected 0 pending tasks on parent issue, got %d", got)
	}
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status)
		VALUES ($1, $2, $3, 'running') RETURNING id
	`, fx.JID, fx.RuntimeID, fx.IssueAID).Scan(&taskID); err != nil {
		t.Fatalf("seed child task: %v", err)
	}
	opts := commentTriggerComputeOptions{AuthoringTaskID: util.MustParseUUID(taskID)}
	triggers, targets := testHandler.computeCommentAgentTriggers(ctx, fx.IssueB, fx.CommentB.Content, nil, "agent", fx.JID, opts)
	if len(triggers) != 1 || len(targets) != 1 || targets[0].ExecAgentID != fx.JID {
		t.Fatalf("cross-issue self-handoff = triggers:%d targets:%+v, want one executable target", len(triggers), targets)
	}

	enqueueMentionedAgentTasksForTest(t, ctx, fx.IssueB, fx.CommentB, nil, "agent", fx.JID, opts)

	if got := countQueuedOrDispatched(t, fx.JID, fx.IssueBID); got != 1 {
		t.Fatalf("after self-mention from another issue: expected one queued task, got %d", got)
	}
}

// TestEnqueueMentionedAgentTasks_SelfMentionWhileRunningQueuesFollowup proves
// that a self-mention posted in the same issue an agent is currently running
// in is covered by that run and does not enqueue a follow-up.
func TestEnqueueMentionedAgentTasks_SelfMentionWhileRunningIsNoop(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSelfMentionFixture(t)

	// Seed a running task for J on issue A — this is the agent's current run.
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status)
		VALUES ($1, $2, $3, 'running') RETURNING id
	`, fx.JID, fx.RuntimeID, fx.IssueAID).Scan(&taskID); err != nil {
		t.Fatalf("seed running task: %v", err)
	}

	if got := countQueuedOrDispatched(t, fx.JID, fx.IssueAID); got != 0 {
		t.Fatalf("before: expected 0 queued/dispatched tasks (only the running task), got %d", got)
	}
	opts := commentTriggerComputeOptions{AuthoringTaskID: util.MustParseUUID(taskID)}
	triggers, targets := testHandler.computeCommentAgentTriggers(ctx, fx.IssueA, fx.CommentA.Content, nil, "agent", fx.JID, opts)
	if len(triggers) != 0 || len(targets) != 1 || targets[0].Status != DispatchDeferred || targets[0].ReasonCode != ReasonAlreadyActive {
		t.Fatalf("active self-mention outcome = triggers:%d targets:%+v, want deferred/already_active", len(triggers), targets)
	}

	enqueueMentionedAgentTasksForTest(t, ctx, fx.IssueA, fx.CommentA, nil, "agent", fx.JID, opts)

	if got := countQueuedOrDispatched(t, fx.JID, fx.IssueAID); got != 0 {
		t.Fatalf("after self-mention while running: expected no queued follow-up, got %d", got)
	}
}

// TestEnqueueMentionedAgentTasks_SelfMentionDedupesAgainstPendingTask locks in
// that removing the self-trigger `continue` did NOT remove the standard
// HasPendingTaskForIssueAndAgent dedupe. If a queued or dispatched task
// already exists for the same agent on the same issue, a fresh self-mention
// must NOT pile on another duplicate.
func TestEnqueueMentionedAgentTasks_SelfMentionDedupesAgainstPendingTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	fx := newSelfMentionFixture(t)

	cases := []struct {
		name   string
		status string
	}{
		{name: "queued task blocks duplicate", status: "queued"},
		{name: "dispatched task blocks duplicate", status: "dispatched"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, fx.IssueAID); err != nil {
				t.Fatalf("reset tasks: %v", err)
			}
			if _, err := testPool.Exec(ctx, `
				INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status)
				VALUES ($1, $2, $3, $4)
			`, fx.JID, fx.RuntimeID, fx.IssueAID, tc.status); err != nil {
				t.Fatalf("seed %s task: %v", tc.status, err)
			}

			before := countQueuedOrDispatched(t, fx.JID, fx.IssueAID)
			if before != 1 {
				t.Fatalf("before: expected 1 pre-existing %s task, got %d", tc.status, before)
			}

			enqueueMentionedAgentTasksForTest(t, ctx, fx.IssueA, fx.CommentA, nil, "agent", fx.JID, commentTriggerComputeOptions{})

			after := countQueuedOrDispatched(t, fx.JID, fx.IssueAID)
			if after != 1 {
				t.Fatalf("after self-mention with pre-existing %s task: expected dedupe (still 1), got %d", tc.status, after)
			}
		})
	}
}
