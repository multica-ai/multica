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
		"title":      title,
		"status":     "todo",
		"project_id": testProjectID,
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

func TestListTimeline_LegacyWrappedShape_ReturnsFullTimeline(t *testing.T) {
	issueID := createIssueForTimeline(t, "Full wrapped timeline test")
	seedTimelineEntries(t, issueID, 7, 7)

	resp, code := fetchTimelineWrapped(t, issueID, "limit=10")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if len(resp.Entries) != 14 {
		t.Fatalf("expected full 14-entry timeline, got %d", len(resp.Entries))
	}
	if resp.HasMoreBefore || resp.HasMoreAfter {
		t.Fatalf("legacy wrapper should not expose cursor paging, got before=%v after=%v", resp.HasMoreBefore, resp.HasMoreAfter)
	}
}

func TestListTimeline_AroundLegacyWrapper_ReturnsFullTimeline(t *testing.T) {
	issueID := createIssueForTimeline(t, "Around mixed count test")
	ctx := context.Background()
	base := time.Now().UTC().Add(-60 * time.Minute)

	// Plant 4 older comments and 4 older activities (before anchor).
	for i := 0; i < 4; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		if _, err := testPool.Exec(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $5)
		`, issueID, testWorkspaceID, testUserID, fmt.Sprintf("old-comment-%d", i), ts); err != nil {
			t.Fatalf("seed old comment: %v", err)
		}
		ts2 := base.Add(time.Duration(i)*time.Minute + 30*time.Second)
		if _, err := testPool.Exec(ctx, `
			INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
			VALUES ($1, $2, 'member', $3, 'status_changed', '{"from":"todo","to":"in_progress"}'::jsonb, $4)
		`, testWorkspaceID, issueID, testUserID, ts2); err != nil {
			t.Fatalf("seed old activity: %v", err)
		}
	}

	// Anchor comment at t=0 relative to the older entries.
	anchorTS := base.Add(10 * time.Minute)
	var anchorID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at)
		VALUES ($1, $2, 'member', $3, 'anchor', 'comment', $4, $4)
		RETURNING id
	`, issueID, testWorkspaceID, testUserID, anchorTS).Scan(&anchorID); err != nil {
		t.Fatalf("seed anchor: %v", err)
	}

	// Plant 4 newer comments and 4 newer activities (after anchor).
	for i := 0; i < 4; i++ {
		ts := anchorTS.Add(time.Duration(i+1) * time.Minute)
		if _, err := testPool.Exec(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, created_at, updated_at)
			VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $5)
		`, issueID, testWorkspaceID, testUserID, fmt.Sprintf("new-comment-%d", i), ts); err != nil {
			t.Fatalf("seed new comment: %v", err)
		}
		ts2 := anchorTS.Add(time.Duration(i+1)*time.Minute + 30*time.Second)
		if _, err := testPool.Exec(ctx, `
			INSERT INTO activity_log (workspace_id, issue_id, actor_type, actor_id, action, details, created_at)
			VALUES ($1, $2, 'member', $3, 'status_changed', '{"from":"todo","to":"in_progress"}'::jsonb, $4)
		`, testWorkspaceID, issueID, testUserID, ts2); err != nil {
			t.Fatalf("seed new activity: %v", err)
		}
	}

	resp, code := fetchTimelineWrapped(t, issueID, "around="+anchorID+"&limit=10")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if resp.TargetIndex == nil {
		t.Fatalf("expected target_index in around mode")
	}
	if len(resp.Entries) == 0 || resp.Entries[*resp.TargetIndex].ID != anchorID {
		t.Fatalf("anchor not at target_index")
	}
	if len(resp.Entries) != 17 {
		t.Fatalf("expected full 17-entry timeline, got %d", len(resp.Entries))
	}
	if resp.HasMoreBefore || resp.HasMoreAfter {
		t.Fatalf("legacy wrapper should not expose cursor paging, got before=%v after=%v", resp.HasMoreBefore, resp.HasMoreAfter)
	}
}

func TestUpdateCommentPermission_NonAuthorForbidden(t *testing.T) {
	ctx := context.Background()

	otherEmail := "update-comment-nonauthor@multica.ai"
	var otherUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Update Comment NonAuthor", otherEmail).Scan(&otherUserID); err != nil {
		t.Fatalf("create non-author user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, otherUserID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, otherUserID); err != nil {
		t.Fatalf("add non-author membership: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{"title": "update permission non-author", "project_id": testProjectID})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{"content": "author comment"})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var comment CommentResponse
	json.NewDecoder(w.Body).Decode(&comment)

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/comments/"+comment.ID, map[string]any{"content": "hijack edit"})
	req = withURLParam(req, "commentId", comment.ID)
	req.Header.Set("X-User-ID", otherUserID)
	testHandler.UpdateComment(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-author update, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateCommentPermission_AdminAllowed(t *testing.T) {
	ctx := context.Background()

	adminEmail := "update-comment-admin@multica.ai"
	var adminUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Update Comment Admin", adminEmail).Scan(&adminUserID); err != nil {
		t.Fatalf("create admin user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, adminUserID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'admin')
	`, testWorkspaceID, adminUserID); err != nil {
		t.Fatalf("add admin membership: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{"title": "update permission admin", "project_id": testProjectID})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{"content": "member authored"})
	req = withURLParam(req, "id", issueID)
	testHandler.CreateComment(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var comment CommentResponse
	json.NewDecoder(w.Body).Decode(&comment)

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/comments/"+comment.ID, map[string]any{"content": "admin edit"})
	req = withURLParam(req, "commentId", comment.ID)
	req.Header.Set("X-User-ID", adminUserID)
	testHandler.UpdateComment(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin update, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateCommentPermission_AgentOwnerAllowed(t *testing.T) {
	ctx := context.Background()

	suffix := time.Now().UnixNano()
	ownerEmail := fmt.Sprintf("update-comment-agent-owner-%d@multica.ai", suffix)
	var ownerUserID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Update Comment Agent Owner", ownerEmail).Scan(&ownerUserID); err != nil {
		t.Fatalf("create agent-owner user: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, ownerUserID)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, ownerUserID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, ownerUserID); err != nil {
		t.Fatalf("add agent-owner membership: %v", err)
	}

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, $5, now())
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Update Comment Runtime %d", suffix), "update_comment_runtime", "Update comment runtime", ownerUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Update Comment Agent %d", suffix), runtimeID, ownerUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{"title": "update permission agent owner", "project_id": testProjectID})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	issueID := issue.ID

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'agent', $3, $4, 'comment')
		RETURNING id
	`, issueID, testWorkspaceID, agentID, "agent authored").Scan(&commentID); err != nil {
		t.Fatalf("insert agent comment: %v", err)
	}

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/comments/"+commentID, map[string]any{"content": "owner edit agent comment"})
	req = withURLParam(req, "commentId", commentID)
	req.Header.Set("X-User-ID", ownerUserID)
	testHandler.UpdateComment(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for agent-owner update, got %d: %s", w.Code, w.Body.String())
	}
}
