package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fetchTimeline issues a GET /timeline request and returns the decoded entries
// + HTTP status. The endpoint returns a flat array of TimelineEntry sorted by
// (created_at, id) ascending (oldest first); see ListTimeline / #1929.
func fetchTimeline(t *testing.T, issueID string) ([]TimelineEntry, int) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/timeline", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListTimeline(w, req)
	var entries []TimelineEntry
	if w.Code == http.StatusOK {
		json.NewDecoder(w.Body).Decode(&entries)
	}
	return entries, w.Code
}

// createIssueForTimeline returns a freshly-created issue id and registers a
// cleanup so its timeline rows are deleted after the test.
func createIssueForTimeline(t *testing.T, title string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM activity_log WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issue.ID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
	return issue.ID
}

// seedTimelineEntries inserts <commentN> comments + <activityN> activities for
// the given issue with ascending timestamps. Returns the inserted ids in the
// order they were inserted (chronologically ascending).
func seedTimelineEntries(t *testing.T, issueID string, commentN, activityN int) (commentIDs, activityIDs []string) {
	t.Helper()
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Duration(commentN+activityN) * time.Minute)

	for i := 0; i < commentN; i++ {
		var id string
		ts := base.Add(time.Duration(i) * time.Minute)
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $5)
			RETURNING id
		`, issueID, testWorkspaceID, testUserID, fmt.Sprintf("comment %d", i), ts).Scan(&id); err != nil {
			t.Fatalf("seed comment %d: %v", i, err)
		}
		commentIDs = append(commentIDs, id)
	}
	for i := 0; i < activityN; i++ {
		var id string
		ts := base.Add(time.Duration(commentN+i) * time.Minute)
		if err := testPool.QueryRow(ctx, `
			INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
			VALUES ($1, $2, 'member', $3, 'status_changed', '{"from":"todo","to":"in_progress"}'::jsonb, $4)
			RETURNING id
		`, testWorkspaceID, issueID, testUserID, ts).Scan(&id); err != nil {
			t.Fatalf("seed activity %d: %v", i, err)
		}
		activityIDs = append(activityIDs, id)
	}
	return
}

func TestListTimeline_ReturnsAllEntriesAscending(t *testing.T) {
	issueID := createIssueForTimeline(t, "All entries test")
	commentIDs, _ := seedTimelineEntries(t, issueID, 5, 0)

	entries, status := fetchTimeline(t, issueID)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	// Handler tests don't register the activity listener (that lives in
	// cmd/server), so issue creation does not seed an auto-activity here.
	// We assert directly on the seeded comments.
	commentEntries := []TimelineEntry{}
	for _, e := range entries {
		if e.Type == "comment" {
			commentEntries = append(commentEntries, e)
		}
	}
	if got, want := len(commentEntries), len(commentIDs); got != want {
		t.Fatalf("comment count = %d, want %d", got, want)
	}
	for i, e := range commentEntries {
		if e.ID != commentIDs[i] {
			t.Errorf("entry %d: id = %s, want %s", i, e.ID, commentIDs[i])
		}
	}
}

func TestListTimeline_MergesCommentsAndActivities(t *testing.T) {
	issueID := createIssueForTimeline(t, "Merged entries test")
	seedTimelineEntries(t, issueID, 3, 2)

	entries, status := fetchTimeline(t, issueID)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	// Verify chronological non-decreasing order across types.
	for i := 1; i < len(entries); i++ {
		if entries[i-1].CreatedAt > entries[i].CreatedAt {
			t.Errorf("not chronological at %d: %q then %q",
				i, entries[i-1].CreatedAt, entries[i].CreatedAt)
		}
	}
	// 3 seeded comments + 2 seeded activities = 5. Handler tests don't
	// register the activity listener, so there is no auto issue-created row.
	if got, want := len(entries), 5; got != want {
		t.Fatalf("entries = %d, want %d", got, want)
	}
}

// fetchTimelineWrapped exercises the legacy wrapped response shape that
// stale Multica.app v0.2.26+ builds still expect — sending any of
// limit/before/after/around makes the server emit a TimelinePage-style
// object (entries DESC, null cursors, has_more_*=false) instead of the new
// flat array. Used to verify the boundary-compat path doesn't regress.
func fetchTimelineWrapped(t *testing.T, issueID, query string) (timelinePaginatedResponse, int) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/timeline?"+query, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListTimeline(w, req)
	var resp timelinePaginatedResponse
	if w.Code == http.StatusOK {
		json.NewDecoder(w.Body).Decode(&resp)
	}
	return resp, w.Code
}

// Boundary-compat: a stale client between #2128 and #1929 sends ?limit=50
// and parses the response with TimelinePageSchema. The handler must keep
// returning the wrapped object so that path doesn't fall back to an empty
// timeline.
func TestListTimeline_LegacyWrappedShape_OnPaginationParams(t *testing.T) {
	issueID := createIssueForTimeline(t, "Legacy wrapped shape test")
	commentIDs, _ := seedTimelineEntries(t, issueID, 3, 0)

	resp, status := fetchTimelineWrapped(t, issueID, "limit=50")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if resp.HasMoreBefore || resp.HasMoreAfter {
		t.Errorf("has_more_*: want false/false, got before=%v after=%v",
			resp.HasMoreBefore, resp.HasMoreAfter)
	}
	if resp.NextCursor != nil || resp.PrevCursor != nil {
		t.Errorf("cursors: want nil/nil, got next=%v prev=%v", resp.NextCursor, resp.PrevCursor)
	}
	// DESC order: most recent comment first; activity from issue-creation
	// sits at the bottom.
	commentEntries := []TimelineEntry{}
	for _, e := range resp.Entries {
		if e.Type == "comment" {
			commentEntries = append(commentEntries, e)
		}
	}
	if got, want := len(commentEntries), len(commentIDs); got != want {
		t.Fatalf("comment count = %d, want %d", got, want)
	}
	for i, e := range commentEntries {
		want := commentIDs[len(commentIDs)-1-i]
		if e.ID != want {
			t.Errorf("DESC entry %d: id = %s, want %s", i, e.ID, want)
		}
	}
}

func TestListTimeline_LegacyWrappedShape_AroundFillsTargetIndex(t *testing.T) {
	issueID := createIssueForTimeline(t, "Around target index test")
	commentIDs, _ := seedTimelineEntries(t, issueID, 5, 0)
	anchor := commentIDs[2] // pick a middle comment

	resp, status := fetchTimelineWrapped(t, issueID, "around="+anchor)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if resp.TargetIndex == nil {
		t.Fatalf("target_index: want non-nil for around mode")
	}
	if got := resp.Entries[*resp.TargetIndex].ID; got != anchor {
		t.Errorf("target_index points at %s, want anchor %s", got, anchor)
	}
}

func TestListTimeline_EmptyIssue(t *testing.T) {
	issueID := createIssueForTimeline(t, "Empty timeline test")
	entries, status := fetchTimeline(t, issueID)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	// Handler tests don't wire the activity listener, so a freshly-created
	// issue with no comments has an empty timeline.
	if got := len(entries); got != 0 {
		t.Fatalf("entries = %d, want 0", got)
	}
}
