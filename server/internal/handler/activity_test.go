package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fetchTimeline issues a GET /timeline request with the given query string and
// returns the decoded TimelineResponse + HTTP status.
func fetchTimeline(t *testing.T, issueID, query string) (TimelineResponse, int) {
	t.Helper()
	url := "/api/issues/" + issueID + "/timeline"
	if query != "" {
		url += "?" + query
	}
	w := httptest.NewRecorder()
	req := newRequest("GET", url, nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListTimeline(w, req)
	var resp TimelineResponse
	if w.Code == http.StatusOK {
		json.NewDecoder(w.Body).Decode(&resp)
	}
	return resp, w.Code
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
// the given issue with descending timestamps (oldest first → newest last) so
// callers can reason about ordering. Returns the inserted comment + activity
// IDs in the order they were inserted (chronologically ascending).
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

func TestListTimeline_DefaultLatestPage(t *testing.T) {
	issueID := createIssueForTimeline(t, "Latest page test")
	seedTimelineEntries(t, issueID, 60, 60) // 120 total; default limit is 50

	resp, code := fetchTimeline(t, issueID, "")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(resp.Entries) != 50 {
		t.Fatalf("expected 50 entries on default page, got %d", len(resp.Entries))
	}
	if !resp.HasMoreBefore {
		t.Fatalf("expected has_more_before=true with 120 total entries")
	}
	if resp.HasMoreAfter {
		t.Fatalf("latest page must report has_more_after=false")
	}
	if resp.NextCursor == nil {
		t.Fatalf("expected next_cursor on full page")
	}
	// DESC order: first entry's timestamp must be >= last entry's.
	if resp.Entries[0].CreatedAt < resp.Entries[len(resp.Entries)-1].CreatedAt {
		t.Fatalf("expected DESC order, first=%s last=%s",
			resp.Entries[0].CreatedAt, resp.Entries[len(resp.Entries)-1].CreatedAt)
	}
}

func TestListTimeline_BeforeCursorWalksOlder(t *testing.T) {
	issueID := createIssueForTimeline(t, "Before cursor test")
	seedTimelineEntries(t, issueID, 30, 30) // 60 total

	first, _ := fetchTimeline(t, issueID, "limit=20")
	if len(first.Entries) != 20 {
		t.Fatalf("first page: expected 20, got %d", len(first.Entries))
	}
	if first.NextCursor == nil {
		t.Fatalf("first page should have next_cursor")
	}

	second, code := fetchTimeline(t, issueID, "limit=20&before="+*first.NextCursor)
	if code != http.StatusOK {
		t.Fatalf("second page: expected 200, got %d", code)
	}
	if len(second.Entries) != 20 {
		t.Fatalf("second page: expected 20, got %d", len(second.Entries))
	}
	if !second.HasMoreAfter {
		t.Fatalf("second page must report has_more_after=true (we paged backward)")
	}
	// No overlap: oldest of first page must be strictly newer than newest of second.
	firstTail := first.Entries[len(first.Entries)-1]
	secondHead := second.Entries[0]
	if firstTail.CreatedAt < secondHead.CreatedAt {
		t.Fatalf("pages overlap: firstTail=%s secondHead=%s",
			firstTail.CreatedAt, secondHead.CreatedAt)
	}
}

func TestListTimeline_AfterCursorWalksNewer(t *testing.T) {
	issueID := createIssueForTimeline(t, "After cursor test")
	seedTimelineEntries(t, issueID, 30, 30)

	first, _ := fetchTimeline(t, issueID, "limit=20")
	if first.NextCursor == nil {
		t.Fatalf("first page should have next_cursor")
	}
	older, _ := fetchTimeline(t, issueID, "limit=20&before="+*first.NextCursor)
	if older.PrevCursor == nil {
		t.Fatalf("older page should have prev_cursor")
	}

	// Walk back forward: ?after=older.prev_cursor should land on entries
	// newer than the older page's newest, i.e. overlap with first page.
	newer, code := fetchTimeline(t, issueID, "limit=20&after="+*older.PrevCursor)
	if code != http.StatusOK {
		t.Fatalf("after page: expected 200, got %d", code)
	}
	if len(newer.Entries) == 0 {
		t.Fatalf("after page should not be empty")
	}
	if !newer.HasMoreBefore {
		t.Fatalf("after page must report has_more_before=true")
	}
}

func TestListTimeline_AroundAnchorsOnTarget(t *testing.T) {
	issueID := createIssueForTimeline(t, "Around test")
	commentIDs, _ := seedTimelineEntries(t, issueID, 50, 0)
	// commentIDs[0] is the OLDEST. Pick the 2nd-oldest as the anchor — far
	// from the latest page so we can verify around mode actually works.
	target := commentIDs[1]

	resp, code := fetchTimeline(t, issueID, "around="+target+"&limit=20")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if resp.TargetIndex == nil {
		t.Fatalf("expected target_index in around mode")
	}
	if len(resp.Entries) == 0 || resp.Entries[*resp.TargetIndex].ID != target {
		t.Fatalf("target_index does not point at target id; got %s",
			resp.Entries[*resp.TargetIndex].ID)
	}
	// Should have entries on both sides of the anchor (the 2nd-oldest has
	// 1 older + many newer).
	if !resp.HasMoreAfter {
		t.Fatalf("around 2nd-oldest should report has_more_after=true")
	}
}

func TestListTimeline_AroundUnknownTarget(t *testing.T) {
	issueID := createIssueForTimeline(t, "Around 404 test")
	seedTimelineEntries(t, issueID, 5, 0)

	bogus := "00000000-0000-0000-0000-000000000001"
	_, code := fetchTimeline(t, issueID, "around="+bogus)
	if code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown anchor, got %d", code)
	}
}

func TestListTimeline_LimitOverMaxRejected(t *testing.T) {
	issueID := createIssueForTimeline(t, "Limit cap test")
	seedTimelineEntries(t, issueID, 1, 0)

	_, code := fetchTimeline(t, issueID, "limit=500")
	if code != http.StatusBadRequest {
		t.Fatalf("expected 400 for limit=500, got %d", code)
	}
}

func TestListTimeline_MutuallyExclusiveCursorParams(t *testing.T) {
	issueID := createIssueForTimeline(t, "Mutex test")
	seedTimelineEntries(t, issueID, 1, 0)

	_, code := fetchTimeline(t, issueID, "before=abc&after=def")
	if code != http.StatusBadRequest {
		t.Fatalf("before+after should 400, got %d", code)
	}
}

func TestListTimeline_InvalidCursorRejected(t *testing.T) {
	issueID := createIssueForTimeline(t, "Bad cursor test")
	seedTimelineEntries(t, issueID, 1, 0)

	_, code := fetchTimeline(t, issueID, "before=not-base64-json")
	if code != http.StatusBadRequest {
		t.Fatalf("invalid cursor should 400, got %d", code)
	}
}

func TestListTimeline_MergedCommentAndActivity(t *testing.T) {
	issueID := createIssueForTimeline(t, "Merge test")
	ctx := context.Background()

	// Use explicit, well-separated timestamps so the DESC ordering assertion
	// is deterministic regardless of clock granularity.
	older := time.Now().UTC().Add(-2 * time.Hour)
	newer := older.Add(1 * time.Hour)

	// Older row: activity.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
		VALUES ($1, $2, 'member', $3, 'created', '{}'::jsonb, $4)
	`, testWorkspaceID, issueID, testUserID, older); err != nil {
		t.Fatalf("seed activity: %v", err)
	}
	// Newer row: comment.
	if _, err := testPool.Exec(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at)
		VALUES ($1, $2, 'member', $3, 'merge test comment', 'comment', $4, $4)
	`, issueID, testWorkspaceID, testUserID, newer); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	resp, code := fetchTimeline(t, issueID, "")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(resp.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(resp.Entries))
	}
	// DESC: comment (newer) at index 0, activity (older) at index 1.
	if resp.Entries[0].Type != "comment" || resp.Entries[1].Type != "activity" {
		t.Fatalf("merge order wrong: got %s/%s, want comment/activity",
			resp.Entries[0].Type, resp.Entries[1].Type)
	}
	if !strings.Contains(*resp.Entries[0].Content, "merge test") {
		t.Fatalf("comment content lost in merge: %v", resp.Entries[0].Content)
	}
}
