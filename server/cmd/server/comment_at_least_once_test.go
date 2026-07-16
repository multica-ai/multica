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

// TestMergeCommentIntoPendingTask_RecomputesOriginatorAndSkipsDispatched is the
// MUL-4195 second-round regression test for the merge query. It pins two
// properties:
//
//   - Recompute-on-merge (review must-fix #1): folding a DIFFERENT member's
//     comment into a queued task re-stamps originator_user_id (and would
//     re-stamp the overlay) to the new comment's originator, so the single
//     coalescing run carries the latest instruction's identity instead of the
//     original user's — and the comment is never dropped. The earlier gate
//     returned ErrNoRows here, which then hit the one-pending-per-(issue,agent)
//     unique index on the fresh-enqueue fallback and silently lost the comment.
//   - Pre-claim-only target (review must-fix #2): a DISPATCHED task is never a
//     merge target (its claim response is already built), so merging returns
//     ErrNoRows and the comment is left to completion reconciliation.
func TestMergeCommentIntoPendingTask_RecomputesOriginatorAndSkipsDispatched(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	queries := db.New(testPool)

	agentID := getAgentID(t)
	issueID := createIssueAssignedToAgent(t, "Merge recompute test", agentID)
	clearTasks(t, issueID) // drop the assignment task so we start clean
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	now := time.Now()
	cidA := insertCommentAt(t, issueID, "member", testUserID, "first, from originator A", now.Add(-2*time.Minute))
	cidB := insertCommentAt(t, issueID, "member", testUserID, "second, folds in", now.Add(-1*time.Minute))

	// A second member to act as a DIFFERENT originator.
	otherUserID := createWorkspaceMember(t, "merge-recompute-other")

	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	// Seed a queued task originated by testUserID and triggered by cidA.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, originator_user_id, accountable_user_id)
		VALUES ($1, $2, $3, $4, 'queued', 0, $5, $5)
	`, agentID, runtimeID, issueID, cidA, testUserID); err != nil {
		t.Fatalf("seed queued task: %v", err)
	}

	pgIssue := toPgUUID(t, issueID)
	pgAgent := toPgUUID(t, agentID)

	// A DIFFERENT originator (userB) now MERGES (no gate): trigger repointed to
	// cidB, originator re-stamped to userB, cidA preserved as coalesced.
	if _, err := queries.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID:              pgIssue,
		AgentID:              pgAgent,
		NewTriggerCommentID:  toPgUUID(t, cidB),
		NewOriginatorUserID:  toPgUUID(t, otherUserID),
		NewAccountableUserID: toPgUUID(t, otherUserID),
		NewTriggerSummary:    pgtype.Text{String: "second, folds in", Valid: true},
	}); err != nil {
		t.Fatalf("recompute merge should succeed for a different originator, got %v", err)
	}
	if got := latestTriggerCommentID(t, issueID); got != cidB {
		t.Errorf("merge must repoint the trigger to %s, got %s", cidB, got)
	}
	if ids := coalescedCommentIDs(t, issueID); !containsID(ids, cidA) {
		t.Errorf("merge must preserve %s as coalesced, got %v", cidA, ids)
	}
	if got := taskOriginator(t, issueID, agentID); got != otherUserID {
		t.Errorf("merge must re-stamp originator to the new comment's originator %s, got %s", otherUserID, got)
	}

	// Now flip the task to 'dispatched' and confirm a further comment is NOT a
	// merge target — its claim response is already built, so merging would
	// falsely mark an undelivered comment as delivered (must-fix #2).
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE issue_id = $1`, issueID); err != nil {
		t.Fatalf("flip task to dispatched: %v", err)
	}
	cidC := insertCommentAt(t, issueID, "member", testUserID, "third, arrives after dispatch", now)
	if _, err := queries.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID:              pgIssue,
		AgentID:              pgAgent,
		NewTriggerCommentID:  toPgUUID(t, cidC),
		NewOriginatorUserID:  toPgUUID(t, testUserID),
		NewAccountableUserID: toPgUUID(t, testUserID),
		NewTriggerSummary:    pgtype.Text{String: "third", Valid: true},
	}); err != pgx.ErrNoRows {
		t.Fatalf("merge into a dispatched task must return pgx.ErrNoRows, got %v", err)
	}
}

// createWorkspaceMember inserts a fresh user + workspace member and returns the
// user id, for tests that need a second distinct originator.
func createWorkspaceMember(t *testing.T, slug string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	email := slug + "-" + time.Now().Format("150405.000000") + "@example.test"
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Merge Test "+slug, email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin')`,
		testWorkspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, userID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

// taskOriginator returns originator_user_id (as text) of the most recent task
// for (issue, agent).
func taskOriginator(t *testing.T, issueID, agentID string) string {
	t.Helper()
	var originator string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(originator_user_id::text, '')
		  FROM agent_task_queue
		 WHERE issue_id = $1 AND agent_id = $2
		 ORDER BY created_at DESC
		 LIMIT 1
	`, issueID, agentID).Scan(&originator); err != nil {
		t.Fatalf("read task originator: %v", err)
	}
	return originator
}

// TestMergeCommentIntoPendingTask_TargetsQueuedNotDeferred is the MUL-4195
// round-4 regression test. When a `(issue, agent)` pair has BOTH an older
// queued task (the run about to be claimed) and a newer deferred
// assignee-fallback task, a new comment's merge must land on the QUEUED task —
// the one that will actually run next — not the newer deferred fallback. An
// earlier `status IN ('queued','deferred') ORDER BY created_at DESC` target
// picked the deferred row, so the comment missed the imminent run and the
// deferred fallback could later promote into a duplicate. The merge now matches
// `status = 'queued'` only; the deferred row is left to its own escalation
// lifecycle.
func TestMergeCommentIntoPendingTask_TargetsQueuedNotDeferred(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	queries := db.New(testPool)

	agentID := getAgentID(t)
	issueID := createIssueAssignedToAgent(t, "Merge target queued-vs-deferred test", agentID)
	clearTasks(t, issueID)
	t.Cleanup(func() {
		clearTasks(t, issueID)
		resp := authRequest(t, "DELETE", "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	now := time.Now()
	cidQueued := insertCommentAt(t, issueID, "member", testUserID, "queued task trigger", now.Add(-3*time.Minute))
	cidDeferred := insertCommentAt(t, issueID, "member", testUserID, "deferred fallback trigger", now.Add(-2*time.Minute))
	cidNew := insertCommentAt(t, issueID, "member", testUserID, "new comment, must fold into queued", now.Add(-1*time.Minute))

	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load runtime: %v", err)
	}

	// Older queued task (the imminent run) ...
	var queuedTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, created_at)
		VALUES ($1, $2, $3, $4, 'queued', 0, now() - interval '3 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID, cidQueued).Scan(&queuedTaskID); err != nil {
		t.Fatalf("seed queued task: %v", err)
	}
	// ... and a NEWER deferred assignee-fallback task for the same (issue, agent).
	var deferredTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, trigger_comment_id, status, priority, created_at, fire_at)
		VALUES ($1, $2, $3, $4, 'deferred', 0, now() - interval '2 minutes', now() + interval '5 minutes')
		RETURNING id
	`, agentID, runtimeID, issueID, cidDeferred).Scan(&deferredTaskID); err != nil {
		t.Fatalf("seed deferred task: %v", err)
	}

	row, err := queries.MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
		IssueID:              toPgUUID(t, issueID),
		AgentID:              toPgUUID(t, agentID),
		NewTriggerCommentID:  toPgUUID(t, cidNew),
		NewOriginatorUserID:  toPgUUID(t, testUserID),
		NewAccountableUserID: toPgUUID(t, testUserID),
		NewTriggerSummary:    pgtype.Text{String: "new comment", Valid: true},
	})
	if err != nil {
		t.Fatalf("merge should target the queued task, got %v", err)
	}
	// The merge must have hit the QUEUED task, not the deferred one.
	if got := pgUUIDToText(row.ID); got != queuedTaskID {
		t.Fatalf("merge must target the queued task %s, got %s", queuedTaskID, got)
	}

	// Queued task: trigger repointed to the new comment, old trigger coalesced.
	var queuedTrigger string
	var queuedCoalesced []string
	if err := testPool.QueryRow(ctx, `
		SELECT trigger_comment_id::text, coalesced_comment_ids::text[] FROM agent_task_queue WHERE id = $1
	`, queuedTaskID).Scan(&queuedTrigger, &queuedCoalesced); err != nil {
		t.Fatalf("read queued task: %v", err)
	}
	if queuedTrigger != cidNew {
		t.Errorf("queued task trigger must be repointed to %s, got %s", cidNew, queuedTrigger)
	}
	if !containsID(queuedCoalesced, cidQueued) {
		t.Errorf("queued task must coalesce its old trigger %s, got %v", cidQueued, queuedCoalesced)
	}

	// Deferred fallback must be UNTOUCHED (still its own trigger, still deferred).
	var deferredTrigger, deferredStatus string
	if err := testPool.QueryRow(ctx, `
		SELECT trigger_comment_id::text, status FROM agent_task_queue WHERE id = $1
	`, deferredTaskID).Scan(&deferredTrigger, &deferredStatus); err != nil {
		t.Fatalf("read deferred task: %v", err)
	}
	if deferredTrigger != cidDeferred {
		t.Errorf("deferred fallback trigger must be untouched (%s), got %s", cidDeferred, deferredTrigger)
	}
	if deferredStatus != "deferred" {
		t.Errorf("deferred fallback status must stay 'deferred', got %s", deferredStatus)
	}
}
