package main

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// createTestProject creates an empty project in the test workspace.
func createTestProject(t *testing.T) string {
	t.Helper()
	var projectID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project (workspace_id, title) VALUES ($1, 'project subscriber test') RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_subscriber WHERE project_id = $1`, projectID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})
	return projectID
}

// createProjectIssue files a fresh issue under projectID.
func createProjectIssue(t *testing.T, projectID string) string {
	t.Helper()
	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})
	if _, err := testPool.Exec(context.Background(), `UPDATE issue SET project_id = $1 WHERE id = $2`, projectID, issueID); err != nil {
		t.Fatalf("move issue into project: %v", err)
	}
	return issueID
}

func addProjectSubscribers(t *testing.T, projectID string, userIDs ...string) {
	t.Helper()
	for _, id := range userIDs {
		if _, err := testPool.Exec(context.Background(), `
			INSERT INTO project_subscriber (workspace_id, project_id, user_id) VALUES ($1, $2, $3)
		`, testWorkspaceID, projectID, id); err != nil {
			t.Fatalf("add project subscriber: %v", err)
		}
	}
}

// requestAs calls the real router as userID.
func requestAs(t *testing.T, userID, email, method, path string) {
	t.Helper()
	token, err := generateTestJWT(userID, email, "Project Watcher")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	req, err := http.NewRequest(method, testServer.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s %s: status %d, want 200", method, path, resp.StatusCode)
	}
}

func inboxTypes(t *testing.T, queries *db.Queries, recipientID string) []string {
	t.Helper()
	var types []string
	for _, item := range inboxItemsForRecipient(t, queries, recipientID) {
		types = append(types, item.Type)
	}
	return types
}

func inboxCountForIssue(t *testing.T, queries *db.Queries, recipientID, issueID string) int {
	t.Helper()
	n := 0
	for _, item := range inboxItemsForRecipient(t, queries, recipientID) {
		if item.IssueID.Valid && item.IssueID.String() == issueID {
			n++
		}
	}
	return n
}

func publishComment(bus *events.Bus, issueID, authorID string) {
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     authorID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000000000",
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   authorID,
				Content:    "project subscriber comment",
				Type:       "comment",
			},
			"issue_title":  "project subscriber issue",
			"issue_status": "todo",
		},
	})
}

func TestNotification_ProjectSubscriber_IssueEvents(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	watcherEmail := "notif-project-watcher@multica.ai"
	watcherID := createNotificationMember(t, watcherEmail)
	t.Cleanup(func() { cleanupTestUser(t, watcherEmail) })

	// Both an issue subscriber and a project watcher: notified once.
	bothEmail := "notif-project-both@multica.ai"
	bothID := createNotificationMember(t, bothEmail)
	t.Cleanup(func() { cleanupTestUser(t, bothEmail) })

	projectID := createTestProject(t)
	issueID := createProjectIssue(t, projectID)
	addProjectSubscribers(t, projectID, watcherID, bothID, testUserID)
	addTestSubscriber(t, issueID, "member", bothID, "commenter")

	issue := handler.IssueResponse{
		ID:          issueID,
		WorkspaceID: testWorkspaceID,
		Title:       "project subscriber issue",
		Status:      "todo",
		Priority:    "medium",
		CreatorType: "member",
		CreatorID:   testUserID,
		ProjectID:   &projectID,
	}
	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload:     map[string]any{"issue": issue},
	})

	issue.Status = "in_progress"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue":          issue,
			"status_changed": true,
			"prev_status":    "todo",
		},
	})

	if got := inboxTypes(t, queries, watcherID); len(got) != 2 || !slices.Contains(got, "issue_created") || !slices.Contains(got, "status_changed") {
		t.Fatalf("watcher inbox = %v, want issue_created + status_changed", got)
	}
	if got := inboxTypes(t, queries, bothID); len(got) != 2 {
		t.Fatalf("issue+project subscriber inbox = %v, want each event once", got)
	}
	if got := inboxTypes(t, queries, testUserID); len(got) != 0 {
		t.Fatalf("actor inbox = %v, want empty", got)
	}
}

// A member who only watches the project leaves one issue through the issue
// endpoint. That opt-out must hold for that issue alone, survive a project
// re-subscribe, and yield to an explicit issue subscribe.
func TestNotification_ProjectSubscriber_LeaveOneIssue(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	watcherEmail := "notif-project-leave@multica.ai"
	watcherID := createNotificationMember(t, watcherEmail)
	t.Cleanup(func() { cleanupTestUser(t, watcherEmail) })

	commenterEmail := "notif-project-commenter@multica.ai"
	commenterID := createNotificationMember(t, commenterEmail)
	t.Cleanup(func() { cleanupTestUser(t, commenterEmail) })

	projectID := createTestProject(t)
	leftIssueID := createProjectIssue(t, projectID)
	otherIssueID := createProjectIssue(t, projectID)

	requestAs(t, watcherID, watcherEmail, http.MethodPut, "/api/projects/"+projectID+"/subscription")
	requestAs(t, watcherID, watcherEmail, http.MethodPost, "/api/issues/"+leftIssueID+"/unsubscribe")

	publishComment(bus, leftIssueID, commenterID)
	publishComment(bus, otherIssueID, commenterID)
	if got := inboxCountForIssue(t, queries, watcherID, leftIssueID); got != 0 {
		t.Fatalf("left issue inbox items = %d, want 0", got)
	}
	if got := inboxCountForIssue(t, queries, watcherID, otherIssueID); got != 1 {
		t.Fatalf("other issue inbox items = %d, want 1", got)
	}

	// Re-subscribing to the project keeps the per-issue opt-out.
	requestAs(t, watcherID, watcherEmail, http.MethodDelete, "/api/projects/"+projectID+"/subscription")
	requestAs(t, watcherID, watcherEmail, http.MethodPut, "/api/projects/"+projectID+"/subscription")
	publishComment(bus, leftIssueID, commenterID)
	if got := inboxCountForIssue(t, queries, watcherID, leftIssueID); got != 0 {
		t.Fatalf("left issue inbox items after project re-subscribe = %d, want 0", got)
	}

	// An explicit subscribe brings the issue back as its own subscription,
	// which then outlives the project subscription.
	requestAs(t, watcherID, watcherEmail, http.MethodPost, "/api/issues/"+leftIssueID+"/subscribe")
	requestAs(t, watcherID, watcherEmail, http.MethodDelete, "/api/projects/"+projectID+"/subscription")
	publishComment(bus, leftIssueID, commenterID)
	publishComment(bus, otherIssueID, commenterID)
	if got := inboxCountForIssue(t, queries, watcherID, leftIssueID); got != 1 {
		t.Fatalf("left issue inbox items after explicit subscribe = %d, want 1", got)
	}
	if got := inboxCountForIssue(t, queries, watcherID, otherIssueID); got != 1 {
		t.Fatalf("other issue inbox items after project unsubscribe = %d, want still 1", got)
	}
}

func TestNotification_ProjectSubscriber_IssueCreatedRespectsMute(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	mutedEmail := "notif-project-muted@multica.ai"
	mutedID := createNotificationMember(t, mutedEmail)
	t.Cleanup(func() { cleanupTestUser(t, mutedEmail) })

	projectID := createTestProject(t)
	issueID := createProjectIssue(t, projectID)
	addProjectSubscribers(t, projectID, mutedID)
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO notification_preference (workspace_id, user_id, preferences)
		VALUES ($1, $2, '{"new_issues":"muted"}'::jsonb)
	`, testWorkspaceID, mutedID); err != nil {
		t.Fatalf("seed muted notification preference: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM notification_preference WHERE user_id = $1`, mutedID)
	})

	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{"issue": handler.IssueResponse{
			ID:          issueID,
			WorkspaceID: testWorkspaceID,
			Title:       "muted project issue",
			Status:      "todo",
			Priority:    "medium",
			CreatorType: "member",
			CreatorID:   testUserID,
			ProjectID:   &projectID,
		}},
	})

	if got := inboxTypes(t, queries, mutedID); len(got) != 0 {
		t.Fatalf("muted watcher inbox = %v, want empty", got)
	}
}
