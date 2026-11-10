package main

import (
	"context"
	"slices"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// createProjectIssueWithWatchers puts a fresh issue in a fresh project and
// returns their ids.
func createProjectIssueWithWatchers(t *testing.T, watcherIDs ...string) (projectID, issueID string) {
	t.Helper()
	ctx := context.Background()
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, 'project subscriber test') RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	issueID = createTestIssue(t, testWorkspaceID, testUserID)
	if _, err := testPool.Exec(ctx, `UPDATE issue SET project_id = $1 WHERE id = $2`, projectID, issueID); err != nil {
		t.Fatalf("move issue into project: %v", err)
	}
	for _, id := range watcherIDs {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO project_subscriber (workspace_id, project_id, user_id) VALUES ($1, $2, $3)
		`, testWorkspaceID, projectID, id); err != nil {
			t.Fatalf("add project subscriber: %v", err)
		}
	}
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
		testPool.Exec(context.Background(), `DELETE FROM project_subscriber WHERE project_id = $1`, projectID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})
	return projectID, issueID
}

func inboxTypes(t *testing.T, queries *db.Queries, recipientID string) []string {
	t.Helper()
	var types []string
	for _, item := range inboxItemsForRecipient(t, queries, recipientID) {
		types = append(types, item.Type)
	}
	return types
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

	// Left this issue explicitly: the project does not bring them back.
	leftEmail := "notif-project-left@multica.ai"
	leftID := createNotificationMember(t, leftEmail)
	t.Cleanup(func() { cleanupTestUser(t, leftEmail) })

	projectID, issueID := createProjectIssueWithWatchers(t, watcherID, bothID, leftID, testUserID)
	addTestSubscriber(t, issueID, "member", bothID, "commenter")
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason, unsubscribed_at, opt_out_scope)
		VALUES ($1, 'member', $2, 'manual', now(), 'issue')
	`, issueID, leftID); err != nil {
		t.Fatalf("tombstone: %v", err)
	}

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
	if got := inboxTypes(t, queries, leftID); len(got) != 0 {
		t.Fatalf("opted-out member inbox = %v, want empty", got)
	}
	if got := inboxTypes(t, queries, testUserID); len(got) != 0 {
		t.Fatalf("actor inbox = %v, want empty", got)
	}
}
