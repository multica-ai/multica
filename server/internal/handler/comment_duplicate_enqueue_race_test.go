package handler

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
)

const (
	dupRaceHeadA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	dupRaceHeadB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// dupRaceFixture creates a workspace-invocable agent and an issue assigned to it.
func dupRaceFixture(t *testing.T, agentName string, issueNumber int) (agentID, issueID, runtimeID string) {
	t.Helper()
	ctx := context.Background()
	agentID = createHandlerTestAgent(t, agentName, nil)
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position, assignee_type, assignee_id)
		VALUES ($1, 'dup-enqueue-race fixture', 'in_progress', 'none', $2, 'member', $3, 0, 'agent', $4)
		RETURNING id
	`, testWorkspaceID, testUserID, issueNumber, agentID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID) })
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID) })
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })
	return agentID, issueID, runtimeID
}

func insertDupRaceComment(t *testing.T, issueID, content, age string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at)
		VALUES ($1, $2, 'member', $3, $4, 'comment', now() - $5::interval)
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, content, age).Scan(&id); err != nil {
		t.Fatalf("insert comment %q: %v", content, err)
	}
	return id
}

func commentCovered(t *testing.T, issueID, agentID, commentID, statusFilter string) bool {
	t.Helper()
	var ok bool
	if err := testPool.QueryRow(context.Background(), `
		SELECT $3::uuid = trigger_comment_id OR $3::uuid = ANY(coalesced_comment_ids)
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = $4
	`, issueID, agentID, commentID, statusFilter).Scan(&ok); err != nil {
		t.Fatalf("check covered %s: %v", commentID, err)
	}
	return ok
}

// TestCommentEnqueueRaceQueuedWinnerFoldsLoser: when the lost-race winner is
// still QUEUED and shares the reviewed head, the losing comment is folded into
// it (the merge makes the newer comment the trigger and pushes the prior trigger
// into coalesced), so the single run covers both. Coalesced outcome, one pending
// task, and no warning / constraint-name leak.
func TestCommentEnqueueRaceQueuedWinnerFoldsLoser(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, issueID, _ := dupRaceFixture(t, "dup-race-queued", 999311)
	agentUUID := util.MustParseUUID(agentID)
	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, agentUUID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	winnerCommentID := insertDupRaceComment(t, issueID, "first instruction", "6 minutes")
	if _, err := testHandler.TaskService.EnqueueTaskForMention(ctx, issue, agentUUID, util.MustParseUUID(winnerCommentID)); err != nil {
		t.Fatalf("enqueue winning task: %v", err)
	}
	loserCommentID := insertDupRaceComment(t, issueID, "second distinct instruction", "1 minute")

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent}
	results := testHandler.enqueueCommentAgentTriggers(ctx, issue, util.MustParseUUID(loserCommentID), []commentAgentTrigger{trigger})

	if res := results[agentID]; res.status != DispatchCoalesced {
		t.Fatalf("queued-winner race: got status %q reason %q, want coalesced", res.status, res.reason)
	}
	if !commentCovered(t, issueID, agentID, loserCommentID, "queued") {
		t.Fatal("losing comment was NOT folded into the queued winner — its instruction would be dropped")
	}
	if !commentCovered(t, issueID, agentID, winnerCommentID, "queued") {
		t.Fatal("winner comment is no longer covered after the fold")
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("pending task count = %d, want exactly 1", n)
	}
	for _, leak := range []string{"idx_one_pending_task_per_issue_agent", "level=WARN", "level=ERROR"} {
		if strings.Contains(logs.String(), leak) {
			t.Fatalf("benign enqueue race leaked %q into logs:\n%s", leak, logs.String())
		}
	}
}

// TestCommentEnqueueRaceDispatchedWinnerDurablyCoversLoser is the regression for
// Elon round-2 must-fix 1: the losing comment PRECEDES a winner that is already
// DISPATCHED before the merge retry, so completion reconcile's created_at window
// cannot see it. The race path must durably register it as a planned (but NOT
// delivered) input on the active winner, and a follow-up must actually cover it
// after the winner completes — never a bare coalesced success that drops it.
func TestCommentEnqueueRaceDispatchedWinnerDurablyCoversLoser(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, issueID, runtimeID := dupRaceFixture(t, "dup-race-dispatched", 999312)
	agentUUID := util.MustParseUUID(agentID)
	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, agentUUID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	// Losing comment PREDATES the winner task; winner is already dispatched with
	// its claim receipt (delivered = its own trigger only).
	loserCommentID := insertDupRaceComment(t, issueID, "losing instruction (predates winner)", "10 minutes")
	winnerCommentID := insertDupRaceComment(t, issueID, "winning instruction", "6 minutes")
	var winnerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, dispatched_at)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'dispatched', 0, now() - interval '5 minutes', now() - interval '4 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID, winnerCommentID).Scan(&winnerTaskID); err != nil {
		t.Fatalf("insert dispatched winner: %v", err)
	}

	trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent}
	results := testHandler.enqueueCommentAgentTriggers(ctx, issue, util.MustParseUUID(loserCommentID), []commentAgentTrigger{trigger})

	// Truthful outcome: deferred (a follow-up will cover it), NOT coalesced.
	if res := results[agentID]; res.status != DispatchDeferred {
		t.Fatalf("dispatched-winner race: got status %q reason %q, want deferred", res.status, res.reason)
	}
	// Durably planned on the winner, but NOT faked as delivered.
	var plannedHasLoser, deliveredHasLoser bool
	if err := testPool.QueryRow(ctx, `
		SELECT $2::uuid = ANY(coalesced_comment_ids), $2::uuid = ANY(delivered_comment_ids)
		FROM agent_task_queue WHERE id = $1
	`, winnerTaskID, loserCommentID).Scan(&plannedHasLoser, &deliveredHasLoser); err != nil {
		t.Fatalf("read winner planned/delivered: %v", err)
	}
	if !plannedHasLoser {
		t.Fatal("losing comment was NOT registered as a planned input on the dispatched winner")
	}
	if deliveredHasLoser {
		t.Fatal("losing comment must NOT be faked as delivered on the dispatched winner")
	}
	if n := pendingTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("pending task count = %d, want exactly 1 (the dispatched winner)", n)
	}

	// The winner completes; reconcile must replay the planned-but-undelivered
	// loser as a bounded follow-up — proving the drop is actually closed.
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running', started_at = now() - interval '1 minute' WHERE id = $1`, winnerTaskID); err != nil {
		t.Fatalf("advance winner to running: %v", err)
	}
	if w := completeTaskViaHandler(t, winnerTaskID, "done"); w.Code != 200 {
		t.Fatalf("complete winner: got %d: %s", w.Code, w.Body.String())
	}
	if n := queuedTaskCountForAgentIssue(t, issueID, agentID); n != 1 {
		t.Fatalf("expected exactly 1 follow-up covering the loser after completion, got %d", n)
	}
	if !commentCovered(t, issueID, agentID, loserCommentID, "queued") {
		t.Fatal("follow-up does not cover the losing comment — the instruction was dropped")
	}
}

// TestCommentEnqueueRaceDifferentHeadNotCoalesced is the regression for Elon
// round-2 must-fix 2: the physical unique index is only (issue_id, agent_id), so
// a comment for a NEW head (PR updated A→B) collides with the still-pending
// head-A task. It must NOT be folded into the head-A run as coalesced (TEN-356),
// and head B must still earn its own coverage after the head-A task completes.
func TestCommentEnqueueRaceDifferentHeadNotCoalesced(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, issueID, runtimeID := dupRaceFixture(t, "dup-race-headshift", 999313)
	agentUUID := util.MustParseUUID(agentID)

	// Link a PR at head B so ResolveIssueReviewSHA returns B for new enqueues.
	var prID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO github_pull_request (workspace_id, installation_id, repo_owner, repo_name, pr_number, title, state, html_url, pr_created_at, pr_updated_at, head_sha)
		VALUES ($1, 1, 'multica-ai', 'multica', 999313, 'review PR', 'open', 'https://example.test/pr', now(), now(), $2)
		RETURNING id
	`, testWorkspaceID, dupRaceHeadB).Scan(&prID); err != nil {
		t.Fatalf("seed PR: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue_pull_request WHERE pull_request_id = $1`, prID) })
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id = $1`, prID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO issue_pull_request (issue_id, pull_request_id) VALUES ($1, $2)`, issueID, prID); err != nil {
		t.Fatalf("link PR: %v", err)
	}

	issue, err := testHandler.Queries.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, agentUUID)
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}

	// Pending winner stamped for the OLD head A (queued), and a NEWER head-B comment.
	headAcommentID := insertDupRaceComment(t, issueID, "review head A", "6 minutes")
	var winnerTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, delivered_comment_ids, status, priority, created_at, context)
		VALUES ($1, $2, $3, $4, ARRAY[$4::uuid], 'queued', 0, now() - interval '5 minutes', jsonb_build_object('head_sha', $5::text))
		RETURNING id
	`, agentID, runtimeID, issueID, headAcommentID, dupRaceHeadA).Scan(&winnerTaskID); err != nil {
		t.Fatalf("insert head-A winner: %v", err)
	}
	headBcommentID := insertDupRaceComment(t, issueID, "please review head B", "1 minute")

	trigger := commentAgentTrigger{Agent: agent, Source: commentTriggerSourceMentionAgent}
	results := testHandler.enqueueCommentAgentTriggers(ctx, issue, util.MustParseUUID(headBcommentID), []commentAgentTrigger{trigger})

	// Not coalesced into the head-A run: deferred instead.
	if res := results[agentID]; res.status != DispatchDeferred {
		t.Fatalf("different-head race: got status %q reason %q, want deferred (not coalesced into old head)", res.status, res.reason)
	}
	// The head-A task is untouched: the head-B comment was NOT folded in, and the
	// task still carries head A.
	var headBinA bool
	var winnerHead string
	if err := testPool.QueryRow(ctx, `
		SELECT $2::uuid = ANY(coalesced_comment_ids), COALESCE(context->>'head_sha','')
		FROM agent_task_queue WHERE id = $1
	`, winnerTaskID, headBcommentID).Scan(&headBinA, &winnerHead); err != nil {
		t.Fatalf("read head-A winner: %v", err)
	}
	if headBinA {
		t.Fatal("head-B comment was wrongly folded into the head-A run (TEN-356 violation)")
	}
	if winnerHead != dupRaceHeadA {
		t.Fatalf("head-A winner head_sha changed to %q, want unchanged %q", winnerHead, dupRaceHeadA)
	}

	// After the head-A task completes, reconcile enqueues fresh coverage for the
	// newer head-B comment — and that follow-up is stamped for head B.
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'running', started_at = now() - interval '1 minute' WHERE id = $1`, winnerTaskID); err != nil {
		t.Fatalf("advance head-A winner to running: %v", err)
	}
	if w := completeTaskViaHandler(t, winnerTaskID, "done"); w.Code != 200 {
		t.Fatalf("complete head-A winner: got %d: %s", w.Code, w.Body.String())
	}
	var followupHead string
	if err := testPool.QueryRow(ctx, `
		SELECT COALESCE(context->>'head_sha','')
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
		ORDER BY created_at DESC LIMIT 1
	`, issueID, agentID).Scan(&followupHead); err != nil {
		t.Fatalf("read follow-up head: %v", err)
	}
	if followupHead != dupRaceHeadB {
		t.Fatalf("follow-up head_sha = %q, want head B %q (head B must earn its own coverage)", followupHead, dupRaceHeadB)
	}
}
