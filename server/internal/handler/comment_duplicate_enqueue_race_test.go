package handler

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

// TestCommentEnqueueRaceFoldsLosingCommentIntoWinner is the regression for the
// Elon re-review of #5914 (PR #5958). When a fresh comment-trigger enqueue loses
// the race to a concurrent sibling — resolution saw no pending task, but the
// insert tripped idx_one_pending_task_per_issue_agent — the losing comment must
// NOT be reported as a bare coalesced success and then dropped. It must be
// durably folded into the winning task's coalesced_comment_ids, so completion
// reconcile delivers it via planned_comment_ids even though it was persisted
// before the winning task. The benign race must also produce no warning log and
// must never leak the raw Postgres constraint name.
//
// The race is reproduced deterministically: pre-create the winning QUEUED task,
// then drive enqueueCommentAgentTriggers with AlreadyPending=false so the fresh
// enqueue really collides on the unique index.
func TestCommentEnqueueRaceFoldsLosingCommentIntoWinner(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "dup-race-target", nil)
	agentUUID := util.MustParseUUID(agentID)

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'dup-enqueue-race fixture', 'in_progress', 'none', $2, 'member', 999311, 0, 'agent', $3)
		RETURNING id
	`, testWorkspaceID, testUserID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID) })
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("setup: load issue: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, agentUUID)
	if err != nil {
		t.Fatalf("setup: load agent: %v", err)
	}

	// The winner's trigger comment, then the winning QUEUED task.
	var winnerCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, 'first instruction', 'comment')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&winnerCommentID); err != nil {
		t.Fatalf("setup: winner comment: %v", err)
	}
	if _, err := testHandler.TaskService.EnqueueTaskForMention(ctx, issue, agentUUID, util.MustParseUUID(winnerCommentID)); err != nil {
		t.Fatalf("setup: enqueue winning task: %v", err)
	}

	// The losing comment — distinct content that must not be dropped.
	var loserCommentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'member', $3, 'second distinct instruction', 'comment')
		RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&loserCommentID); err != nil {
		t.Fatalf("setup: loser comment: %v", err)
	}

	// Capture logs at Info+ so a leaked warning / constraint name is visible
	// (the benign race logs only at debug, which is filtered out here).
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	// AlreadyPending=false forces the fresh-enqueue path, which collides.
	trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent}
	results := testHandler.enqueueCommentAgentTriggers(ctx, issue, util.MustParseUUID(loserCommentID), []commentAgentTrigger{trigger})

	// Outcome is a success-shaped coalesced, not a blocked internal_error.
	res, ok := results[agentID]
	if !ok {
		t.Fatalf("no enqueue result for agent %s: %+v", agentID, results)
	}
	if res.status != DispatchCoalesced {
		t.Fatalf("race outcome: got status %q reason %q, want coalesced", res.status, res.reason)
	}

	// Durable fold: the winning task's planned batch now covers BOTH comments,
	// so completion reconcile delivers each via planned_comment_ids regardless
	// of timestamp. The merge makes the newer (losing) comment the trigger and
	// pushes the prior trigger into coalesced_comment_ids, so a comment is
	// "covered" if it is the trigger OR in the coalesced set. Without the fix
	// the losing comment would be in neither and its instruction lost.
	covered := func(cid string) bool {
		var ok bool
		if err := testPool.QueryRow(ctx, `
			SELECT $3::uuid = trigger_comment_id OR $3::uuid = ANY(coalesced_comment_ids)
			FROM agent_task_queue
			WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
		`, issueID, agentID, cid).Scan(&ok); err != nil {
			t.Fatalf("check covered %s: %v", cid, err)
		}
		return ok
	}
	if !covered(loserCommentID) {
		t.Fatal("losing comment was NOT folded into the winning task — its instruction would be dropped")
	}
	if !covered(winnerCommentID) {
		t.Fatal("winner comment is no longer covered after the fold")
	}

	// Still exactly one pending task — no duplicate run spawned.
	var n int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched')
	`, issueID, agentID).Scan(&n); err != nil {
		t.Fatalf("count pending tasks: %v", err)
	}
	if n != 1 {
		t.Fatalf("pending task count = %d, want exactly 1", n)
	}

	// No warning/error and no raw constraint name leaked for the benign race.
	logStr := logs.String()
	for _, leak := range []string{"idx_one_pending_task_per_issue_agent", "level=WARN", "level=ERROR"} {
		if strings.Contains(logStr, leak) {
			t.Fatalf("benign enqueue race leaked %q into logs:\n%s", leak, logStr)
		}
	}
}
