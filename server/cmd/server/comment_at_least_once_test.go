package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// coalescedCommentIDs returns the coalesced_comment_ids of the most recent
// not-yet-started task for an issue, as text ids.
func coalescedCommentIDs(t *testing.T, issueID string) []string {
	t.Helper()
	var ids []string
	err := testPool.QueryRow(context.Background(),
		`SELECT COALESCE(array_agg(c::text), '{}')
		   FROM (
		     SELECT unnest(coalesced_comment_ids) AS c
		       FROM (
		         SELECT coalesced_comment_ids
		           FROM agent_task_queue
		          WHERE issue_id = $1 AND status IN ('queued', 'dispatched', 'waiting_local_directory', 'deferred')
		          ORDER BY created_at DESC
		          LIMIT 1
		       ) t
		   ) s`,
		issueID).Scan(&ids)
	if err != nil {
		t.Fatalf("failed to read coalesced_comment_ids: %v", err)
	}
	return ids
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// TestConsecutiveCommentsMergeNotDropped is the MUL-4195 regression test.
//
// Before the fix, a second/third comment posted while the agent already had a
// queued task was silently DROPPED by the HasPendingTaskForIssueAndAgent dedup:
// only the first comment survived and the later instructions were lost. The fix
// folds each new comment into the pending task instead — still one task (no
// concurrent runs), but every deliberate comment is preserved: the trigger is
// repointed to the newest comment and the earlier ones are recorded in
// coalesced_comment_ids so the single run must address them all.
func TestConsecutiveCommentsMergeNotDropped(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	agentID := getAgentID(t)
	issueID := createIssueAssignedToAgent(t, "Merge-not-drop test", agentID)
	clearTasks(t, issueID) // drop the assignment task so we start clean
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	// Three deliberate comments in a row, before any run starts.
	cidA := postComment(t, issueID, "First instruction", nil)
	cidB := postComment(t, issueID, "Second, correcting the first", nil)
	cidC := postComment(t, issueID, "Third, one more detail", nil)

	// Still exactly one task: we bound concurrency to one run per (issue,agent).
	if n := countPendingTasksForAgent(t, issueID, agentID); n != 1 {
		t.Fatalf("expected exactly 1 pending task after 3 comments, got %d", n)
	}

	// The trigger points at the NEWEST comment so the injected prompt shows the
	// latest deliberate instruction.
	if got := latestTriggerCommentID(t, issueID); got != cidC {
		t.Errorf("expected trigger_comment_id to be repointed to newest comment %s, got %s", cidC, got)
	}

	// The earlier comments are preserved (not dropped) as coalesced comments.
	coalesced := coalescedCommentIDs(t, issueID)
	if !containsID(coalesced, cidA) {
		t.Errorf("expected coalesced_comment_ids to preserve first comment %s; got %v", cidA, coalesced)
	}
	if !containsID(coalesced, cidB) {
		t.Errorf("expected coalesced_comment_ids to preserve second comment %s; got %v", cidB, coalesced)
	}
	// The current trigger must not also appear in the coalesced set.
	if containsID(coalesced, cidC) {
		t.Errorf("newest comment %s should be the trigger, not a coalesced entry; got %v", cidC, coalesced)
	}
}

// TestGetLatestMemberCommentForIssueSince pins the completion-reconciliation
// query (MUL-4195): it must surface member comments newer than the run's
// started_at anchor and ignore agent-authored comments (the anti-loop rule).
func TestGetLatestMemberCommentForIssueSince(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	queries := db.New(testPool)

	agentID := getAgentID(t)
	issueID := createIssue(t, "Reconcile query test")
	t.Cleanup(func() {
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	anchor := time.Now()
	// A member comment BEFORE the anchor — must not qualify.
	insertCommentAt(t, issueID, "member", testUserID, "old member comment", anchor.Add(-10*time.Minute))
	// An agent comment AFTER the anchor — must be ignored (loop safety).
	insertCommentAt(t, issueID, "agent", agentID, "agent reply after start", anchor.Add(2*time.Minute))

	pgAnchor := pgtype.Timestamptz{Time: anchor, Valid: true}
	pgIssue := toPgUUID(t, issueID)

	// With only an older member comment and a newer agent comment, nothing
	// qualifies → ErrNoRows (no spurious follow-up).
	if _, err := queries.GetLatestMemberCommentForIssueSince(ctx, db.GetLatestMemberCommentForIssueSinceParams{
		IssueID: pgIssue,
		Since:   pgAnchor,
	}); err != pgx.ErrNoRows {
		t.Fatalf("expected pgx.ErrNoRows when only an agent comment is newer, got %v", err)
	}

	// Now a member comment AFTER the anchor — this is the deliberate input that
	// must earn a follow-up.
	wantID := insertCommentAt(t, issueID, "member", testUserID, "new member instruction", anchor.Add(5*time.Minute))
	got, err := queries.GetLatestMemberCommentForIssueSince(ctx, db.GetLatestMemberCommentForIssueSinceParams{
		IssueID: pgIssue,
		Since:   pgAnchor,
	})
	if err != nil {
		t.Fatalf("expected the newer member comment, got error %v", err)
	}
	if pgUUIDToText(got.ID) != wantID {
		t.Errorf("expected latest member comment %s, got %s", wantID, pgUUIDToText(got.ID))
	}
}

// insertCommentAt inserts a comment with an explicit created_at and author, and
// returns its id. Used to construct precise before/after-anchor scenarios.
func insertCommentAt(t *testing.T, issueID, authorType, authorID, content string, at time.Time) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 'comment', $6, $6)
		RETURNING id::text
	`, issueID, testWorkspaceID, authorType, authorID, content, at).Scan(&id)
	if err != nil {
		t.Fatalf("insertCommentAt: %v", err)
	}
	return id
}

func toPgUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return u
}

func pgUUIDToText(u pgtype.UUID) string {
	v, err := u.Value()
	if err != nil || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// TestMergeCommentIntoPendingTask_OriginatorGate is the MUL-4195 review
// must-fix #1 regression test. A pending task carries its originator's
// runtime_mcp_overlay / runtime_connected_apps and audit attribution. Folding a
// DIFFERENT member's comment into it would make the run answer B's instruction
// under A's connected-app capabilities and audit identity — a permission /
// attribution bug. The merge query must therefore only fold a comment into a
// pending task whose originator matches; a mismatch returns pgx.ErrNoRows so the
// caller enqueues a fresh follow-up carrying the correct context.
func TestMergeCommentIntoPendingTask_OriginatorGate(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	queries := db.New(testPool)

	agentID := getAgentID(t)
	issueID := createIssueAssignedToAgent(t, "Originator gate test", agentID)
	clearTasks(t, issueID) // drop the assignment task so we start clean
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	now := time.Now()
	cidA := insertCommentAt(t, issueID, "member", testUserID, "first, from originator A", now.Add(-2*time.Minute))
	cidB := insertCommentAt(t, issueID, "member", testUserID, "second, folds in", now.Add(-1*time.Minute))

	// Seed a queued task originated by testUserID and triggered by cidA.
	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, originator_user_id)
		VALUES ($1, $2, $3, $4, 'queued', 0, $5)
	`, agentID, runtimeID, issueID, cidA, testUserID); err != nil {
		t.Fatalf("seed queued task: %v", err)
	}

	pgIssue := toPgUUID(t, issueID)
	pgAgent := toPgUUID(t, agentID)
	pgCidB := toPgUUID(t, cidB)
	summary := pgtype.Text{String: "second, folds in", Valid: true}

	// A DIFFERENT originator must NOT merge: the gate blocks it, so the caller
	// falls back to a fresh follow-up instead of reusing A's overlay/attribution.
	differentOriginator := toPgUUID(t, "000000ff-0000-0000-0000-0000000000ff")
	if _, err := queries.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID:             pgIssue,
		AgentID:             pgAgent,
		NewTriggerCommentID: pgCidB,
		NewOriginatorUserID: differentOriginator,
		NewTriggerSummary:   summary,
	}); err != pgx.ErrNoRows {
		t.Fatalf("expected pgx.ErrNoRows when the new comment's originator differs, got %v", err)
	}
	// The pending task is untouched: trigger still cidA, nothing coalesced.
	if got := latestTriggerCommentID(t, issueID); got != cidA {
		t.Errorf("mismatched-originator merge must leave the trigger at %s, got %s", cidA, got)
	}
	if ids := coalescedCommentIDs(t, issueID); len(ids) != 0 {
		t.Errorf("mismatched-originator merge must not coalesce anything, got %v", ids)
	}

	// The SAME originator merges as before: trigger repointed to cidB, cidA
	// preserved as a coalesced comment.
	if _, err := queries.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID:             pgIssue,
		AgentID:             pgAgent,
		NewTriggerCommentID: pgCidB,
		NewOriginatorUserID: toPgUUID(t, testUserID),
		NewTriggerSummary:   summary,
	}); err != nil {
		t.Fatalf("same-originator merge should succeed, got %v", err)
	}
	if got := latestTriggerCommentID(t, issueID); got != cidB {
		t.Errorf("same-originator merge must repoint the trigger to %s, got %s", cidB, got)
	}
	if ids := coalescedCommentIDs(t, issueID); !containsID(ids, cidA) {
		t.Errorf("same-originator merge must preserve %s as coalesced, got %v", cidA, ids)
	}
}
