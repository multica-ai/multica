package handler

// CEREBRO-PATCH(notifications-handler-tests): test coverage for fork notification REST API.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestListNotificationsPaginatesAndFiltersPermission(t *testing.T) {
	viewerID := createNotificationTestMember(t)
	projectID := createNotificationTestProject(t, "restricted")
	hiddenProjectID := createNotificationTestProject(t, "restricted")
	addNotificationTestProjectMember(t, projectID, viewerID)
	visibleIssueID := createNotificationTestIssue(t, projectID, viewerID, false)
	hiddenIssueID := createNotificationTestIssue(t, hiddenProjectID, testUserID, false)

	newer := insertNotificationTestItem(t, viewerID, visibleIssueID, "newer visible", false, time.Now().Add(2*time.Minute))
	insertNotificationTestItem(t, viewerID, visibleIssueID, "older visible", false, time.Now().Add(time.Minute))
	insertNotificationTestItem(t, viewerID, hiddenIssueID, "hidden metadata", false, time.Now())
	removeNotificationTestProjectMember(t, projectID, viewerID)

	req := notificationTestRequest("GET", "/api/notifications?limit=1", nil, viewerID)
	w := httptest.NewRecorder()
	testHandler.ListNotifications(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListNotifications: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var page struct {
		Items      []InboxItemResponse `json:"items"`
		NextCursor *string             `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("expected no visible notifications after project access removal, got %+v", page.Items)
	}
	if page.NextCursor != nil {
		t.Fatalf("expected no next cursor when no visible items remain, got %q", *page.NextCursor)
	}

	addNotificationTestProjectMember(t, projectID, viewerID)
	req = notificationTestRequest("GET", "/api/notifications?limit=1", nil, viewerID)
	w = httptest.NewRecorder()
	testHandler.ListNotifications(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListNotifications restored access: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode restored page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != newer {
		t.Fatalf("expected newest visible item only, got %+v", page.Items)
	}
	if page.NextCursor == nil || *page.NextCursor == "" {
		t.Fatalf("expected next cursor for limited page")
	}

	req = notificationTestRequest("GET", "/api/notifications?limit=1&cursor="+url.QueryEscape(*page.NextCursor), nil, viewerID)
	w = httptest.NewRecorder()
	testHandler.ListNotifications(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListNotifications cursor: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode cursor page: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "older visible" {
		t.Fatalf("expected second visible item, got %+v", page.Items)
	}

}

func TestNotificationsUnreadCountFiltersPermission(t *testing.T) {
	viewerID := createNotificationTestMember(t)
	projectID := createNotificationTestProject(t, "restricted")
	addNotificationTestProjectMember(t, projectID, viewerID)
	issueID := createNotificationTestIssue(t, projectID, viewerID, false)
	insertNotificationTestItem(t, viewerID, issueID, "visible unread", false, time.Now())

	req := notificationTestRequest("GET", "/api/notifications/unread-count", nil, viewerID)
	w := httptest.NewRecorder()
	testHandler.CountUnreadNotifications(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CountUnreadNotifications: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]int64
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode count: %v", err)
	}
	if body["count"] != 1 {
		t.Fatalf("expected count 1 with access, got %d", body["count"])
	}

	removeNotificationTestProjectMember(t, projectID, viewerID)
	w = httptest.NewRecorder()
	testHandler.CountUnreadNotifications(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CountUnreadNotifications without access: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode count without access: %v", err)
	}
	if body["count"] != 0 {
		t.Fatalf("expected count 0 after access removal, got %d", body["count"])
	}
}

func TestPatchNotificationReadRequiresNotificationRouteAndAccess(t *testing.T) {
	viewerID := createNotificationTestMember(t)
	projectID := createNotificationTestProject(t, "restricted")
	addNotificationTestProjectMember(t, projectID, viewerID)
	issueID := createNotificationTestIssue(t, projectID, viewerID, false)
	notificationID := insertNotificationTestItem(t, viewerID, issueID, "notification item", false, time.Now())
	inboxID := insertInboxRouteTestItem(t, viewerID, issueID, "inbox item", false, time.Now())

	req := notificationTestRequest("PATCH", "/api/notifications/"+inboxID+"/read", nil, viewerID)
	req = withURLParam(req, "id", inboxID)
	w := httptest.NewRecorder()
	testHandler.MarkNotificationRead(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("MarkNotificationRead must reject inbox route, got %d: %s", w.Code, w.Body.String())
	}

	removeNotificationTestProjectMember(t, projectID, viewerID)
	req = notificationTestRequest("PATCH", "/api/notifications/"+notificationID+"/read", nil, viewerID)
	req = withURLParam(req, "id", notificationID)
	w = httptest.NewRecorder()
	testHandler.MarkNotificationRead(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("MarkNotificationRead must reject inaccessible issue metadata, got %d: %s", w.Code, w.Body.String())
	}

	addNotificationTestProjectMember(t, projectID, viewerID)
	req = notificationTestRequest("PATCH", "/api/notifications/"+notificationID+"/read", nil, viewerID)
	req = withURLParam(req, "id", notificationID)
	w = httptest.NewRecorder()
	testHandler.MarkNotificationRead(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("MarkNotificationRead: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var read bool
	if err := testPool.QueryRow(context.Background(), `SELECT read FROM inbox_item WHERE id = $1`, notificationID).Scan(&read); err != nil {
		t.Fatalf("read notification row: %v", err)
	}
	if !read {
		t.Fatalf("expected notification item to be marked read")
	}
}

func TestPatchNotificationsReadAllOnlyMarksAccessibleNotifications(t *testing.T) {
	viewerID := createNotificationTestMember(t)
	restrictedProjectID := createNotificationTestProject(t, "restricted")
	workspaceProjectID := createNotificationTestProject(t, "workspace")
	addNotificationTestProjectMember(t, restrictedProjectID, viewerID)
	restrictedIssueID := createNotificationTestIssue(t, restrictedProjectID, viewerID, false)
	workspaceIssueID := createNotificationTestIssue(t, workspaceProjectID, testUserID, false)
	restrictedNotificationID := insertNotificationTestItem(t, viewerID, restrictedIssueID, "restricted unread", false, time.Now().Add(time.Minute))
	workspaceNotificationID := insertNotificationTestItem(t, viewerID, workspaceIssueID, "workspace unread", false, time.Now())
	removeNotificationTestProjectMember(t, restrictedProjectID, viewerID)

	req := notificationTestRequest("PATCH", "/api/notifications/read-all", nil, viewerID)
	w := httptest.NewRecorder()
	testHandler.MarkAllNotificationsRead(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("MarkAllNotificationsRead: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]int64
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["count"] != 1 {
		t.Fatalf("expected one accessible notification marked read, got %d", body["count"])
	}

	states := notificationReadStates(t, restrictedNotificationID, workspaceNotificationID)
	if states[restrictedNotificationID] {
		t.Fatalf("restricted notification must stay unread after access removal")
	}
	if !states[workspaceNotificationID] {
		t.Fatalf("workspace-access notification should be marked read")
	}
}

func TestNotificationsRejectInvalidInputs(t *testing.T) {
	tests := []struct {
		name   string
		req    *http.Request
		handle func(http.ResponseWriter, *http.Request)
	}{
		{
			name:   "invalid limit",
			req:    notificationTestRequest("GET", "/api/notifications?limit=0", nil, testUserID),
			handle: testHandler.ListNotifications,
		},
		{
			name:   "invalid cursor",
			req:    notificationTestRequest("GET", "/api/notifications?cursor=not-a-time", nil, testUserID),
			handle: testHandler.ListNotifications,
		},
		{
			name: "malformed id",
			req: func() *http.Request {
				req := notificationTestRequest("PATCH", "/api/notifications/not-a-uuid/read", nil, testUserID)
				return withURLParam(req, "id", "not-a-uuid")
			}(),
			handle: testHandler.MarkNotificationRead,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tt.handle(w, tt.req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func notificationTestRequest(method, path string, body any, userID string) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-User-ID", userID)
	member, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		panic(fmt.Sprintf("load notification test member context: %v", err))
	}
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, member))
}

func createNotificationTestMember(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	suffix := time.Now().UnixNano()
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, fmt.Sprintf("Notification Viewer %d", suffix), fmt.Sprintf("notification-viewer-%d@example.com", suffix)).Scan(&userID); err != nil {
		t.Fatalf("insert notification test user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("insert notification test member: %v", err)
	}
	return userID
}

func createNotificationTestProject(t *testing.T, access string) string {
	t.Helper()
	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title, description, status, access)
		VALUES ($1, $2, '', 'planned', $3)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Notification Project %d", time.Now().UnixNano()), access).Scan(&projectID); err != nil {
		t.Fatalf("insert notification test project: %v", err)
	}
	return projectID
}

func addNotificationTestProjectMember(t *testing.T, projectID, userID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO project_member (workspace_id, project_id, user_id, added_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (project_id, user_id) DO NOTHING
	`, testWorkspaceID, projectID, userID, testUserID); err != nil {
		t.Fatalf("insert project member: %v", err)
	}
}

func removeNotificationTestProjectMember(t *testing.T, projectID, userID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		DELETE FROM project_member WHERE project_id = $1 AND user_id = $2
	`, projectID, userID); err != nil {
		t.Fatalf("delete project member: %v", err)
	}
}

// CEREBRO-PATCH(cerebro-notifications-api): direct SQL fixtures must reserve issue numbers through the workspace counter.
func createNotificationTestIssue(t *testing.T, projectID, creatorID string, private bool) string {
	t.Helper()
	number, err := testHandler.Queries.IncrementIssueCounter(context.Background(), parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("next notification test issue number: %v", err)
	}
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (
			workspace_id, title, description, status, priority,
			creator_type, creator_id, project_id, is_private, number
		)
		VALUES ($1, $2, '', 'todo', 'none', 'member', $3, $4, $5, $6)
		RETURNING id
		-- CEREBRO-PATCH(cerebro-notifications-api): keep direct SQL fixtures aligned with workspace issue_counter.
	`, testWorkspaceID, fmt.Sprintf("Notification Issue %d", time.Now().UnixNano()), creatorID, projectID, private, number).Scan(&issueID); err != nil {
		t.Fatalf("insert notification test issue: %v", err)
	}
	return issueID
}

func insertNotificationTestItem(t *testing.T, recipientID, issueID, title string, read bool, createdAt time.Time) string {
	t.Helper()
	return insertInboxItemForNotificationTest(t, recipientID, issueID, title, read, createdAt, "notifications")
}

func insertInboxRouteTestItem(t *testing.T, recipientID, issueID, title string, read bool, createdAt time.Time) string {
	t.Helper()
	return insertInboxItemForNotificationTest(t, recipientID, issueID, title, read, createdAt, "inbox")
}

func insertInboxItemForNotificationTest(t *testing.T, recipientID, issueID, title string, read bool, createdAt time.Time, route string) string {
	t.Helper()
	var itemID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO inbox_item (
			workspace_id, recipient_type, recipient_id, type, severity,
			issue_id, title, body, details, route, read, created_at
		)
		VALUES ($1, 'member', $2, 'new_comment', 'info', $3, $4, 'body', '{}'::jsonb, $5, $6, $7)
		RETURNING id
	`, testWorkspaceID, recipientID, issueID, title, route, read, createdAt).Scan(&itemID); err != nil {
		t.Fatalf("insert inbox item: %v", err)
	}
	return itemID
}

func notificationReadStates(t *testing.T, ids ...string) map[string]bool {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `SELECT id, read FROM inbox_item WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		t.Fatalf("query read states: %v", err)
	}
	defer rows.Close()
	states := make(map[string]bool, len(ids))
	for rows.Next() {
		var id string
		var read bool
		if err := rows.Scan(&id, &read); err != nil {
			t.Fatalf("scan read state: %v", err)
		}
		states[id] = read
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate read states: %v", err)
	}
	return states
}
