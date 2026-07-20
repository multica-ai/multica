package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestCreateComment_BumpsIssueUpdatedAt pins MUL-5009: a new comment counts as
// activity on the issue, so updated_at advances. This is what lets the
// "Updated date" Kanban/list sort surface recently-discussed cards, not only
// cards whose status changed.
func TestCreateComment_BumpsIssueUpdatedAt(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	issueID := createCommentTriggerPreviewIssue(t, "comment bumps updated_at", "", "")

	var before time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at FROM issue WHERE id = $1`, issueID).Scan(&before); err != nil {
		t.Fatalf("read updated_at before: %v", err)
	}

	// Guarantee a measurable wall-clock gap so the bump is unambiguous; now() is
	// evaluated per statement and the issue was inserted moments ago.
	time.Sleep(10 * time.Millisecond)

	w := httptest.NewRecorder()
	r := withURLParam(
		newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": "a fresh comment"}),
		"id", issueID,
	)
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var after time.Time
	if err := testPool.QueryRow(ctx, `SELECT updated_at FROM issue WHERE id = $1`, issueID).Scan(&after); err != nil {
		t.Fatalf("read updated_at after: %v", err)
	}

	if !after.After(before) {
		t.Fatalf("issue updated_at was not bumped by a new comment: before=%s after=%s", before, after)
	}
}
