package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// childDoneFixture creates a parent + child pair so the parent-notification
// tests can drive the child's status changes independently. Cleanup is
// registered on the test so the rows are removed even on test failure.
type childDoneFixture struct {
	parent IssueResponse
	child  IssueResponse
}

func newChildDoneFixture(t *testing.T, parentStatus string) childDoneFixture {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "child-done parent " + time.Now().Format(time.RFC3339Nano),
		"status": parentStatus,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create parent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var parent IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&parent); err != nil {
		t.Fatalf("decode parent: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":           "child-done child " + time.Now().Format(time.RFC3339Nano),
		"status":          "in_progress",
		"parent_issue_id": parent.ID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create child: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var child IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&child); err != nil {
		t.Fatalf("decode child: %v", err)
	}

	t.Cleanup(func() {
		ctx := context.Background()
		// Cascades through comment.
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, child.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, parent.ID)
	})

	return childDoneFixture{parent: parent, child: child}
}

// updateChildStatus drives an UpdateIssue HTTP call against the child issue.
func updateChildStatus(t *testing.T, childID, status string) {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+childID, map[string]any{"status": status})
	req = withURLParam(req, "id", childID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue child status=%q: expected 200, got %d: %s", status, w.Code, w.Body.String())
	}
}

// countSystemCommentsOn returns the number of platform-generated comments on
// the given issue. The schema CHECK was widened in migration 107 to allow
// author_type='system'; this query is the canary that the migration applied
// and the helper inserts with the right author identity.
func countSystemCommentsOn(t *testing.T, issueID string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM comment WHERE issue_id = $1 AND author_type = 'system'`,
		issueID,
	).Scan(&n); err != nil {
		t.Fatalf("count system comments: %v", err)
	}
	return n
}

func systemCommentOn(t *testing.T, issueID string) (content, authorIDStr string, parentNull bool, typeStr string) {
	t.Helper()
	row := testPool.QueryRow(context.Background(),
		`SELECT content, author_id::text, parent_id IS NULL, type
		   FROM comment
		   WHERE issue_id = $1 AND author_type = 'system'
		   ORDER BY created_at DESC
		   LIMIT 1`,
		issueID)
	if err := row.Scan(&content, &authorIDStr, &parentNull, &typeStr); err != nil {
		t.Fatalf("read system comment: %v", err)
	}
	return
}

// TestChildDoneNotifiesParent — the happy path. A child transitioning from a
// non-done status into `done` while its parent is open must produce exactly
// one top-level platform-generated comment on the parent. The comment must
// reference the child by its workspace-specific identifier (NOT a hardcoded
// `MUL-` prefix — that was the bug PR #2918 review called out) and must not
// carry an `@mention` link to any member/agent/squad (those would re-trigger
// the parent's assignee, which is the noise this change removed).
func TestChildDoneNotifiesParent(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("expected exactly 1 system comment on parent, got %d", got)
	}
	content, authorID, parentNull, typeStr := systemCommentOn(t, fx.parent.ID)

	if !parentNull {
		t.Errorf("system comment must be top-level (parent_id IS NULL)")
	}
	if typeStr != "system" {
		t.Errorf("system comment type should be 'system', got %q", typeStr)
	}
	if authorID != "00000000-0000-0000-0000-000000000000" {
		t.Errorf("system comment author_id should be the zero UUID sentinel, got %q", authorID)
	}

	// Identifier substring must use the real workspace prefix (HAN-, seeded
	// in TestMain), never MUL-.
	if !strings.Contains(content, fx.child.Identifier) {
		t.Errorf("expected comment to contain child identifier %q, got: %s", fx.child.Identifier, content)
	}
	if strings.Contains(content, "MUL-") {
		t.Errorf("comment must not hardcode MUL- prefix, got: %s", content)
	}

	// The comment must contain the safe issue mention but NOT any
	// agent/member/squad mention (those would re-trigger the parent's
	// assignee).
	if !strings.Contains(content, "mention://issue/"+fx.child.ID) {
		t.Errorf("expected mention://issue/<child-id> link in comment, got: %s", content)
	}
	for _, banned := range []string{"mention://agent/", "mention://member/", "mention://squad/"} {
		if strings.Contains(content, banned) {
			t.Errorf("comment must not include %q mention (auto-mention side effect), got: %s", banned, content)
		}
	}
}

// TestChildDoneNotificationIsIdempotent — re-saving an already-done child
// must NOT fire a second notification. UpdateIssue is called with the same
// status='done' twice; only the first call is a transition and should
// produce a comment.
func TestChildDoneNotificationIsIdempotent(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "done")
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("after first done: expected 1 comment, got %d", got)
	}

	// Second save of done — should be a no-op transition.
	updateChildStatus(t, fx.child.ID, "done")
	if got := countSystemCommentsOn(t, fx.parent.ID); got != 1 {
		t.Fatalf("after second done: expected still 1 comment (idempotent), got %d", got)
	}
}

// TestChildReopenAndDoneFiresAgain — done → in_progress → done IS a real
// new completion event and should produce a second notification. This
// captures the "reopen + done counts as a new event" line from MUL-2538.
func TestChildReopenAndDoneFiresAgain(t *testing.T) {
	fx := newChildDoneFixture(t, "in_progress")

	updateChildStatus(t, fx.child.ID, "done")
	updateChildStatus(t, fx.child.ID, "in_progress")
	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 2 {
		t.Fatalf("expected 2 system comments after reopen+done cycle, got %d", got)
	}
}

// TestChildDoneSkippedWhenParentDone — when the parent is already at a
// terminal status, there is nothing for the parent assignee to advance to,
// so the notification must NOT fire.
func TestChildDoneSkippedWhenParentDone(t *testing.T) {
	fx := newChildDoneFixture(t, "done")

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Errorf("parent at 'done' should not receive notification, got %d comments", got)
	}
}

// TestChildDoneSkippedWhenParentCancelled — same as above for cancelled.
func TestChildDoneSkippedWhenParentCancelled(t *testing.T) {
	fx := newChildDoneFixture(t, "cancelled")

	updateChildStatus(t, fx.child.ID, "done")

	if got := countSystemCommentsOn(t, fx.parent.ID); got != 0 {
		t.Errorf("parent at 'cancelled' should not receive notification, got %d comments", got)
	}
}

// TestChildDoneSkippedWhenNoParent — an issue with no parent_issue_id must
// not produce any system comment on anything.
func TestChildDoneSkippedWhenNoParent(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "orphan child-done " + time.Now().Format(time.RFC3339Nano),
		"status": "in_progress",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create orphan: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var orphan IssueResponse
	json.NewDecoder(w.Body).Decode(&orphan)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, orphan.ID)
	})

	// Sanity baseline — there should be zero system comments anywhere in
	// the workspace attributable to this orphan transition. We can only
	// check that the orphan didn't somehow get one itself, but combined
	// with the no-parent code path returning early, that is sufficient.
	updateChildStatus(t, orphan.ID, "done")

	if got := countSystemCommentsOn(t, orphan.ID); got != 0 {
		t.Errorf("orphan must not receive a self-notification, got %d system comments", got)
	}
}
