package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestSubtreeUnsubscribe_LosesToConcurrentRevoke covers MUL-5483 review round 8,
// finding 2.
//
// The handler validated membership from its own MVCC snapshot and only opened
// the serialized transaction afterwards, while UnsubscribeFromIssueSubtree
// upserts the root tombstone unconditionally. So:
//
//	subtree request reads "still a member"  →  revoke takes the lock, deletes
//	their subscriptions and their member row, commits  →  subtree finally gets
//	the lock and writes a fresh tombstone
//
// leaving an opt-out that outlives the membership. Because a tombstone means
// "this person chose to leave this issue" and is never resurrected by a rule,
// re-inviting them silently inherits it: the delegated rule refuses to
// subscribe them to a tree they never opted out of.
//
// Re-asserting membership inside the transaction, after the lock, is what makes
// the losing request a no-op instead.
func TestSubtreeUnsubscribe_LosesToConcurrentRevoke(t *testing.T) {
	ctx := context.Background()

	email := "subtree-revoke-race@multica.test"
	testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	var userID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Subtree Revoke Race", email,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, email)
	})

	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')`,
		testWorkspaceID, userID,
	); err != nil {
		t.Fatalf("add member: %v", err)
	}

	issueID := createRaceIssue(t)

	// The member is subscribed, as the delegated rule would have left them.
	if _, err := testHandler.Queries.AddIssueSubscriber(ctx, db.AddIssueSubscriberParams{
		IssueID:  parseUUID(issueID),
		UserType: "member",
		UserID:   parseUUID(userID),
		Reason:   "delegated",
	}); err != nil {
		t.Fatalf("seed subscriber: %v", err)
	}

	// A revoke is in flight: it holds the serialization lock, has already
	// cleared this member's subscriptions and deleted the member row, and has
	// NOT committed.
	revokeTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin revoke tx: %v", err)
	}
	defer revokeTx.Rollback(context.Background())
	rq := testHandler.Queries.WithTx(revokeTx)

	if err := rq.LockSubscriberWrites(ctx, db.LockSubscriberWritesParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		UserID:      parseUUID(userID),
	}); err != nil {
		t.Fatalf("revoke lock: %v", err)
	}
	if err := rq.DeleteSubscriptionsByMember(ctx, db.DeleteSubscriptionsByMemberParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		UserID:      parseUUID(userID),
	}); err != nil {
		t.Fatalf("revoke cleanup: %v", err)
	}
	if _, err := revokeTx.Exec(ctx,
		`DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`,
		testWorkspaceID, userID,
	); err != nil {
		t.Fatalf("revoke member delete: %v", err)
	}

	// The subtree unsubscribe arrives now, while the revoke is still open. Its
	// pre-check sees a member that is about to stop existing.
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues/"+issueID+"/unsubscribe/subtree", map[string]any{
			"user_id":   userID,
			"user_type": "member",
		})
		req = withURLParam(req, "id", issueID)
		testHandler.UnsubscribeFromIssueSubtree(w, req)
		done <- w.Code
	}()

	select {
	case code := <-done:
		t.Fatalf("subtree unsubscribe completed (status %d) while the revoke still held the lock", code)
	case <-time.After(400 * time.Millisecond):
	}

	if err := revokeTx.Commit(context.Background()); err != nil {
		t.Fatalf("commit revoke: %v", err)
	}

	select {
	case code := <-done:
		if code != http.StatusForbidden {
			t.Fatalf("expected 403 once the target stopped being a member, got %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("subtree unsubscribe never returned after the revoke committed")
	}

	// The real assertion: revoke's cleanup must be the final state. A tombstone
	// here would outlive the membership and be inherited on re-invite.
	var rows int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM issue_subscriber s
		JOIN issue i ON i.id = s.issue_id
		WHERE i.workspace_id = $1 AND s.user_type = 'member' AND s.user_id = $2
	`, testWorkspaceID, userID).Scan(&rows); err != nil {
		t.Fatalf("count subscriber rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("revoked member still has %d issue_subscriber row(s); a tombstone written after "+
			"revoke is inherited when they are re-invited and suppresses future delegated subscriptions", rows)
	}
}

func createRaceIssue(t *testing.T) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title": "subtree revoke race",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() {
		dw := httptest.NewRecorder()
		dreq := newRequest("DELETE", "/api/issues/"+issue.ID, nil)
		dreq = withURLParam(dreq, "id", issue.ID)
		testHandler.DeleteIssue(dw, dreq)
	})
	return issue.ID
}
