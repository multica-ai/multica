package main

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// notificationTest helpers — reuse the integration test fixtures from TestMain
// (testPool, testUserID, testWorkspaceID are set in integration_test.go).

// inboxItemsForRecipient returns all non-archived inbox items for a given recipient.
func inboxItemsForRecipient(t *testing.T, queries *db.Queries, recipientID string) []db.ListInboxItemsRow {
	t.Helper()
	items, err := queries.ListInboxItems(context.Background(), db.ListInboxItemsParams{
		WorkspaceID:   util.MustParseUUID(testWorkspaceID),
		RecipientType: "member",
		RecipientID:   util.MustParseUUID(recipientID),
	})
	if err != nil {
		t.Fatalf("ListInboxItems: %v", err)
	}
	return items
}

// cleanupInboxForIssue deletes all inbox items related to a given issue.
func cleanupInboxForIssue(t *testing.T, issueID string) {
	t.Helper()
	testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
}

// addTestSubscriber manually inserts a subscriber for an issue.
func addTestSubscriber(t *testing.T, issueID, userType, userID, reason string) {
	t.Helper()
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (issue_id, user_type, user_id) DO NOTHING
	`, issueID, userType, userID, reason)
	if err != nil {
		t.Fatalf("addTestSubscriber: %v", err)
	}
}

// createTestSubIssue inserts an issue with parent_issue_id set and returns its UUID.
// Picks the next per-workspace number to avoid colliding with the
// uq_issue_workspace_number unique constraint (parent + sub created in the
// same test would otherwise both default to number=0).
func createTestSubIssue(t *testing.T, workspaceID, creatorID, parentIssueID string) string {
	t.Helper()
	ctx := context.Background()
	var issueID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, parent_issue_id, number)
		VALUES ($1, 'sub-issue test', 'todo', 'medium', 'member', $2, 0, $3,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, workspaceID, creatorID, parentIssueID).Scan(&issueID)
	if err != nil {
		t.Fatalf("createTestSubIssue: %v", err)
	}
	return issueID
}

// newNotificationBus creates a bus with subscriber + notification listeners registered.
func newNotificationBus(t *testing.T, queries *db.Queries) *events.Bus {
	t.Helper()
	bus := events.New()
	registerSubscriberListeners(bus, queries)
	registerNotificationListeners(bus, queries)
	return bus
}

// TestNotification_IssueCreated_AssigneeNotified verifies that when an issue is
// created with an assignee different from the creator, the assignee receives an
// "issue_assigned" inbox notification and the creator receives nothing.
func TestNotification_IssueCreated_AssigneeNotified(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	assigneeEmail := "notif-assignee-created@multica.ai"
	assigneeID := createTestUser(t, assigneeEmail)
	t.Cleanup(func() { cleanupTestUser(t, assigneeEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	// Track inbox:new events
	var inboxEvents []events.Event
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		inboxEvents = append(inboxEvents, e)
	})

	assigneeType := "member"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:           issueID,
				WorkspaceID:  testWorkspaceID,
				Title:        "notif test issue",
				Status:       "todo",
				Priority:     "medium",
				CreatorType:  "member",
				CreatorID:    testUserID,
				AssigneeType: &assigneeType,
				AssigneeID:   &assigneeID,
			},
		},
	})

	// Assignee should have an inbox item
	items := inboxItemsForRecipient(t, queries, assigneeID)
	if len(items) != 1 {
		t.Fatalf("expected 1 inbox item for assignee, got %d", len(items))
	}
	if items[0].Type != "issue_assigned" {
		t.Fatalf("expected type 'issue_assigned', got %q", items[0].Type)
	}
	if items[0].Severity != "action_required" {
		t.Fatalf("expected severity 'action_required', got %q", items[0].Severity)
	}

	// Creator (actor) should NOT have any inbox items
	creatorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(creatorItems) != 0 {
		t.Fatalf("expected 0 inbox items for creator, got %d", len(creatorItems))
	}

	// At least one inbox:new event should have been published
	if len(inboxEvents) < 1 {
		t.Fatal("expected at least 1 inbox:new event")
	}
}

// TestNotification_IssueCreated_SelfAssign verifies that when the creator
// assigns the issue to themselves, no notification is generated.
func TestNotification_IssueCreated_SelfAssign(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	var inboxEvents []events.Event
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		inboxEvents = append(inboxEvents, e)
	})

	assigneeType := "member"
	assigneeID := testUserID // self-assign
	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:           issueID,
				WorkspaceID:  testWorkspaceID,
				Title:        "self-assign issue",
				Status:       "todo",
				Priority:     "medium",
				CreatorType:  "member",
				CreatorID:    testUserID,
				AssigneeType: &assigneeType,
				AssigneeID:   &assigneeID,
			},
		},
	})

	items := inboxItemsForRecipient(t, queries, testUserID)
	if len(items) != 0 {
		t.Fatalf("expected 0 inbox items for self-assign, got %d", len(items))
	}
	if len(inboxEvents) != 0 {
		t.Fatalf("expected 0 inbox:new events for self-assign, got %d", len(inboxEvents))
	}
}

// TestNotification_IssueCreated_NoAssignee verifies that when an issue is
// created without an assignee, no notifications are generated.
func TestNotification_IssueCreated_NoAssignee(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	var inboxEvents []events.Event
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		inboxEvents = append(inboxEvents, e)
	})

	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "no assignee issue",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
		},
	})

	items := inboxItemsForRecipient(t, queries, testUserID)
	if len(items) != 0 {
		t.Fatalf("expected 0 inbox items for no-assignee issue, got %d", len(items))
	}
	if len(inboxEvents) != 0 {
		t.Fatalf("expected 0 inbox:new events, got %d", len(inboxEvents))
	}
}

// TestNotification_StatusChanged verifies that all subscribers except the actor
// receive a "status_changed" notification when an issue status changes.
func TestNotification_StatusChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	// Create two extra users as subscribers
	sub1Email := "notif-sub1-status@multica.ai"
	sub1ID := createTestUser(t, sub1Email)
	t.Cleanup(func() { cleanupTestUser(t, sub1Email) })

	sub2Email := "notif-sub2-status@multica.ai"
	sub2ID := createTestUser(t, sub2Email)
	t.Cleanup(func() { cleanupTestUser(t, sub2Email) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	// Manually add subscribers before the event fires
	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", sub1ID, "assignee")
	addTestSubscriber(t, issueID, "member", sub2ID, "commenter")

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID, // actor is the creator
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "status test issue",
				Status:      "in_progress",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed": false,
			"status_changed":   true,
			"prev_status":      "todo",
		},
	})

	// Actor (testUserID) should NOT get a notification
	actorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(actorItems) != 0 {
		t.Fatalf("expected 0 inbox items for actor, got %d", len(actorItems))
	}

	// sub1 should get a status_changed notification
	sub1Items := inboxItemsForRecipient(t, queries, sub1ID)
	if len(sub1Items) != 1 {
		t.Fatalf("expected 1 inbox item for sub1, got %d", len(sub1Items))
	}
	if sub1Items[0].Type != "status_changed" {
		t.Fatalf("expected type 'status_changed', got %q", sub1Items[0].Type)
	}
	if sub1Items[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", sub1Items[0].Severity)
	}
	// Title is now just the issue title; details contain from/to
	expectedTitle := "status test issue"
	if sub1Items[0].Title != expectedTitle {
		t.Fatalf("expected title %q, got %q", expectedTitle, sub1Items[0].Title)
	}

	// sub2 should also get a status_changed notification
	sub2Items := inboxItemsForRecipient(t, queries, sub2ID)
	if len(sub2Items) != 1 {
		t.Fatalf("expected 1 inbox item for sub2, got %d", len(sub2Items))
	}
	if sub2Items[0].Type != "status_changed" {
		t.Fatalf("expected type 'status_changed', got %q", sub2Items[0].Type)
	}
}

// TestNotification_CommentCreated verifies that all subscribers except the
// commenter receive a "new_comment" notification.
func TestNotification_CommentCreated(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	commenterEmail := "notif-commenter@multica.ai"
	commenterID := createTestUser(t, commenterEmail)
	t.Cleanup(func() { cleanupTestUser(t, commenterEmail) })

	sub1Email := "notif-sub1-comment@multica.ai"
	sub1ID := createTestUser(t, sub1Email)
	t.Cleanup(func() { cleanupTestUser(t, sub1Email) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	// Pre-add subscribers: creator and sub1. The commenter will also be added
	// by subscriber_listeners when the event fires.
	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", sub1ID, "assignee")

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     commenterID, // commenter is the actor
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000000000",
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   commenterID,
				Content:    "test comment content",
				Type:       "comment",
			},
			"issue_title":  "comment test issue",
			"issue_status": "todo",
		},
	})

	// Creator should get a new_comment notification
	creatorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(creatorItems) != 1 {
		t.Fatalf("expected 1 inbox item for creator, got %d", len(creatorItems))
	}
	if creatorItems[0].Type != "new_comment" {
		t.Fatalf("expected type 'new_comment', got %q", creatorItems[0].Type)
	}
	if creatorItems[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", creatorItems[0].Severity)
	}

	// sub1 should also get a new_comment notification
	sub1Items := inboxItemsForRecipient(t, queries, sub1ID)
	if len(sub1Items) != 1 {
		t.Fatalf("expected 1 inbox item for sub1, got %d", len(sub1Items))
	}
	if sub1Items[0].Type != "new_comment" {
		t.Fatalf("expected type 'new_comment', got %q", sub1Items[0].Type)
	}

	// Commenter (actor) should NOT get a notification
	commenterItems := inboxItemsForRecipient(t, queries, commenterID)
	if len(commenterItems) != 0 {
		t.Fatalf("expected 0 inbox items for commenter, got %d", len(commenterItems))
	}
}

// TestNotification_AssigneeChanged verifies the full assignee change flow:
// - New assignee gets "issue_assigned" (Direct)
// - Old assignee gets "unassigned" (Direct)
// - Other subscribers get "assignee_changed" (Subscriber), excluding actor + old + new
// - Actor gets nothing
func TestNotification_AssigneeChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	oldAssigneeEmail := "notif-old-assignee@multica.ai"
	oldAssigneeID := createTestUser(t, oldAssigneeEmail)
	t.Cleanup(func() { cleanupTestUser(t, oldAssigneeEmail) })

	newAssigneeEmail := "notif-new-assignee@multica.ai"
	newAssigneeID := createTestUser(t, newAssigneeEmail)
	t.Cleanup(func() { cleanupTestUser(t, newAssigneeEmail) })

	bystanderEmail := "notif-bystander@multica.ai"
	bystanderID := createTestUser(t, bystanderEmail)
	t.Cleanup(func() { cleanupTestUser(t, bystanderEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	// Pre-add subscribers: creator, old assignee, bystander
	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", oldAssigneeID, "assignee")
	addTestSubscriber(t, issueID, "member", bystanderID, "commenter")

	newAssigneeType := "member"
	oldAssigneeType := "member"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID, // actor is the creator
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:           issueID,
				WorkspaceID:  testWorkspaceID,
				Title:        "assignee change issue",
				Status:       "todo",
				Priority:     "medium",
				CreatorType:  "member",
				CreatorID:    testUserID,
				AssigneeType: &newAssigneeType,
				AssigneeID:   &newAssigneeID,
			},
			"assignee_changed":   true,
			"status_changed":     false,
			"prev_assignee_type": &oldAssigneeType,
			"prev_assignee_id":   &oldAssigneeID,
		},
	})

	// New assignee should get "issue_assigned"
	newItems := inboxItemsForRecipient(t, queries, newAssigneeID)
	if len(newItems) != 1 {
		t.Fatalf("expected 1 inbox item for new assignee, got %d", len(newItems))
	}
	if newItems[0].Type != "issue_assigned" {
		t.Fatalf("expected type 'issue_assigned', got %q", newItems[0].Type)
	}
	if newItems[0].Severity != "action_required" {
		t.Fatalf("expected severity 'action_required', got %q", newItems[0].Severity)
	}

	// Old assignee should get "unassigned"
	oldItems := inboxItemsForRecipient(t, queries, oldAssigneeID)
	if len(oldItems) != 1 {
		t.Fatalf("expected 1 inbox item for old assignee, got %d", len(oldItems))
	}
	if oldItems[0].Type != "unassigned" {
		t.Fatalf("expected type 'unassigned', got %q", oldItems[0].Type)
	}
	if oldItems[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", oldItems[0].Severity)
	}

	// Bystander should get "assignee_changed"
	bystanderItems := inboxItemsForRecipient(t, queries, bystanderID)
	if len(bystanderItems) != 1 {
		t.Fatalf("expected 1 inbox item for bystander, got %d", len(bystanderItems))
	}
	if bystanderItems[0].Type != "assignee_changed" {
		t.Fatalf("expected type 'assignee_changed', got %q", bystanderItems[0].Type)
	}
	if bystanderItems[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", bystanderItems[0].Severity)
	}

	// Actor (testUserID / creator) should NOT get any notification
	actorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(actorItems) != 0 {
		t.Fatalf("expected 0 inbox items for actor, got %d", len(actorItems))
	}
}

// TestNotification_TaskCompleted verifies that task:completed events do NOT
// create inbox notifications (completion is visible from the status change).
func TestNotification_TaskCompleted(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	// The agent ID (acting as system actor)
	agentID := "00000000-0000-0000-0000-aaaaaaaaaaaa"

	// Pre-add subscribers: creator and the agent
	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "agent", agentID, "assignee")

	bus.Publish(events.Event{
		Type:        protocol.EventTaskCompleted,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"task_id":  "00000000-0000-0000-0000-bbbbbbbbbbbb",
			"agent_id": agentID,
			"issue_id": issueID,
			"status":   "completed",
		},
	})

	// No inbox notification should be created for task:completed
	creatorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(creatorItems) != 0 {
		t.Fatalf("expected 0 inbox items for creator on task:completed, got %d", len(creatorItems))
	}
}

// TestNotification_TaskFailed verifies that subscribers get a "task_failed"
// notification when a task fails, excluding the agent.
func TestNotification_TaskFailed(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	agentID := "00000000-0000-0000-0000-aaaaaaaaaaaa"

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "agent", agentID, "assignee")

	bus.Publish(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"task_id":  "00000000-0000-0000-0000-bbbbbbbbbbbb",
			"agent_id": agentID,
			"issue_id": issueID,
			"status":   "failed",
		},
	})

	creatorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(creatorItems) != 1 {
		t.Fatalf("expected 1 inbox item for creator, got %d", len(creatorItems))
	}
	if creatorItems[0].Type != "task_failed" {
		t.Fatalf("expected type 'task_failed', got %q", creatorItems[0].Type)
	}
	if creatorItems[0].Severity != "action_required" {
		t.Fatalf("expected severity 'action_required', got %q", creatorItems[0].Severity)
	}
}

// TestNotification_PriorityChanged verifies that all subscribers except the actor
// receive a "priority_changed" notification when an issue priority changes.
func TestNotification_PriorityChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	sub1Email := "notif-sub1-priority@multica.ai"
	sub1ID := createTestUser(t, sub1Email)
	t.Cleanup(func() { cleanupTestUser(t, sub1Email) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", sub1ID, "assignee")

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "priority test issue",
				Status:      "todo",
				Priority:    "high",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed": false,
			"status_changed":   false,
			"priority_changed": true,
			"prev_priority":    "medium",
		},
	})

	// Actor should NOT get a notification
	actorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(actorItems) != 0 {
		t.Fatalf("expected 0 inbox items for actor, got %d", len(actorItems))
	}

	// sub1 should get a priority_changed notification
	sub1Items := inboxItemsForRecipient(t, queries, sub1ID)
	if len(sub1Items) != 1 {
		t.Fatalf("expected 1 inbox item for sub1, got %d", len(sub1Items))
	}
	if sub1Items[0].Type != "priority_changed" {
		t.Fatalf("expected type 'priority_changed', got %q", sub1Items[0].Type)
	}
	if sub1Items[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", sub1Items[0].Severity)
	}
	// Title is now just the issue title; details contain from/to
	expectedTitle := "priority test issue"
	if sub1Items[0].Title != expectedTitle {
		t.Fatalf("expected title %q, got %q", expectedTitle, sub1Items[0].Title)
	}
}

// TestNotification_DueDateChanged verifies that all subscribers except the actor
// receive a "due_date_changed" notification when an issue due date changes.
func TestNotification_DueDateChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	sub1Email := "notif-sub1-duedate@multica.ai"
	sub1ID := createTestUser(t, sub1Email)
	t.Cleanup(func() { cleanupTestUser(t, sub1Email) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", sub1ID, "assignee")

	dueDate := "2026-04-15T00:00:00Z"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "due date test issue",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
				DueDate:     &dueDate,
			},
			"assignee_changed": false,
			"status_changed":   false,
			"due_date_changed": true,
		},
	})

	// Actor should NOT get a notification
	actorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(actorItems) != 0 {
		t.Fatalf("expected 0 inbox items for actor, got %d", len(actorItems))
	}

	// sub1 should get a due_date_changed notification
	sub1Items := inboxItemsForRecipient(t, queries, sub1ID)
	if len(sub1Items) != 1 {
		t.Fatalf("expected 1 inbox item for sub1, got %d", len(sub1Items))
	}
	if sub1Items[0].Type != "due_date_changed" {
		t.Fatalf("expected type 'due_date_changed', got %q", sub1Items[0].Type)
	}
	if sub1Items[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", sub1Items[0].Severity)
	}
}

// TestNotification_StartDateChanged verifies that subscribers (except the actor)
// receive a "start_date_changed" notification when an issue start date changes.
func TestNotification_StartDateChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	sub1Email := "notif-sub1-startdate@multica.ai"
	sub1ID := createTestUser(t, sub1Email)
	t.Cleanup(func() { cleanupTestUser(t, sub1Email) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", sub1ID, "assignee")

	startDate := "2026-04-01T00:00:00Z"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "start date test issue",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
				StartDate:   &startDate,
			},
			"assignee_changed":   false,
			"status_changed":     false,
			"start_date_changed": true,
		},
	})

	// Actor should NOT get a notification
	actorItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(actorItems) != 0 {
		t.Fatalf("expected 0 inbox items for actor, got %d", len(actorItems))
	}

	sub1Items := inboxItemsForRecipient(t, queries, sub1ID)
	if len(sub1Items) != 1 {
		t.Fatalf("expected 1 inbox item for sub1, got %d", len(sub1Items))
	}
	if sub1Items[0].Type != "start_date_changed" {
		t.Fatalf("expected type 'start_date_changed', got %q", sub1Items[0].Type)
	}
	if sub1Items[0].Severity != "info" {
		t.Fatalf("expected severity 'info', got %q", sub1Items[0].Severity)
	}
}

// TestNotification_ParentBubble_StatusChanged verifies that a status_changed
// event on a sub-issue bubbles to subscribers of the parent issue.
func TestNotification_ParentBubble_StatusChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	parentSubEmail := "notif-parent-sub-status@multica.ai"
	parentSubID := createTestUser(t, parentSubEmail)
	t.Cleanup(func() { cleanupTestUser(t, parentSubEmail) })

	parentID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, parentID)
		cleanupTestIssue(t, parentID)
	})
	subID := createTestSubIssue(t, testWorkspaceID, testUserID, parentID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, subID)
		cleanupTestIssue(t, subID)
	})

	// Subscribe a watcher to the parent only — they should hear about
	// status changes on the sub-issue.
	addTestSubscriber(t, parentID, "member", parentSubID, "manual")

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          subID,
				WorkspaceID: testWorkspaceID,
				Title:       "sub-issue status bubble",
				Status:      "done",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed": false,
			"status_changed":   true,
			"prev_status":      "in_progress",
		},
	})

	items := inboxItemsForRecipient(t, queries, parentSubID)
	if len(items) != 1 {
		t.Fatalf("expected 1 inbox item bubbled to parent subscriber, got %d", len(items))
	}
	if items[0].Type != "status_changed" {
		t.Fatalf("expected type 'status_changed', got %q", items[0].Type)
	}
	// The inbox item should point to the sub-issue, not the parent.
	if util.UUIDToString(items[0].IssueID) != subID {
		t.Fatalf("expected inbox item issue_id=%s (sub-issue), got %s",
			subID, util.UUIDToString(items[0].IssueID))
	}
}

// TestNotification_ParentBubble_NewCommentSuppressed verifies that comments
// on a sub-issue do NOT bubble to subscribers of the parent issue. Comments
// are the loudest signal and we explicitly want to keep them off the parent
// watcher's inbox.
func TestNotification_ParentBubble_NewCommentSuppressed(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	commenterEmail := "notif-parent-bubble-commenter@multica.ai"
	commenterID := createTestUser(t, commenterEmail)
	t.Cleanup(func() { cleanupTestUser(t, commenterEmail) })

	parentSubEmail := "notif-parent-sub-comment@multica.ai"
	parentSubID := createTestUser(t, parentSubEmail)
	t.Cleanup(func() { cleanupTestUser(t, parentSubEmail) })

	parentID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, parentID)
		cleanupTestIssue(t, parentID)
	})
	subID := createTestSubIssue(t, testWorkspaceID, testUserID, parentID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, subID)
		cleanupTestIssue(t, subID)
	})

	addTestSubscriber(t, parentID, "member", parentSubID, "manual")

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     commenterID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000000000",
				IssueID:    subID,
				AuthorType: "member",
				AuthorID:   commenterID,
				Content:    "comment on sub-issue",
				Type:       "comment",
			},
			"issue_title":  "sub-issue comment bubble",
			"issue_status": "todo",
		},
	})

	items := inboxItemsForRecipient(t, queries, parentSubID)
	if len(items) != 0 {
		t.Fatalf("expected 0 inbox items bubbled to parent subscriber for new_comment, got %d", len(items))
	}
}

// TestNotification_ParentBubble_PriorityChangeSuppressed verifies that a
// priority change on a sub-issue does NOT bubble to parent subscribers.
func TestNotification_ParentBubble_PriorityChangeSuppressed(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	parentSubEmail := "notif-parent-sub-priority@multica.ai"
	parentSubID := createTestUser(t, parentSubEmail)
	t.Cleanup(func() { cleanupTestUser(t, parentSubEmail) })

	parentID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, parentID)
		cleanupTestIssue(t, parentID)
	})
	subID := createTestSubIssue(t, testWorkspaceID, testUserID, parentID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, subID)
		cleanupTestIssue(t, subID)
	})

	addTestSubscriber(t, parentID, "member", parentSubID, "manual")

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          subID,
				WorkspaceID: testWorkspaceID,
				Title:       "sub-issue priority bubble",
				Status:      "todo",
				Priority:    "high",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed": false,
			"status_changed":   false,
			"priority_changed": true,
			"prev_priority":    "medium",
		},
	})

	items := inboxItemsForRecipient(t, queries, parentSubID)
	if len(items) != 0 {
		t.Fatalf("expected 0 inbox items bubbled to parent subscriber for priority_changed, got %d", len(items))
	}
}

func notificationEventsForRecipient(t *testing.T, queries *db.Queries, recipientID string) []db.NotificationEvent {
	t.Helper()
	items, err := queries.ListNotificationEventsByRecipient(context.Background(), db.ListNotificationEventsByRecipientParams{
		WorkspaceID:     util.MustParseUUID(testWorkspaceID),
		RecipientUserID: util.MustParseUUID(recipientID),
	})
	if err != nil {
		t.Fatalf("ListNotificationEventsByRecipient: %v", err)
	}
	return items
}

func notificationDeliveriesForEvent(t *testing.T, queries *db.Queries, eventID string) []db.NotificationDelivery {
	t.Helper()
	items, err := queries.ListNotificationDeliveriesByEvent(context.Background(), util.MustParseUUID(eventID))
	if err != nil {
		t.Fatalf("ListNotificationDeliveriesByEvent: %v", err)
	}
	return items
}

func issueIdentifierForTest(t *testing.T, queries *db.Queries, issueID string) string {
	t.Helper()

	workspace, err := queries.GetWorkspace(context.Background(), util.MustParseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	issue, err := queries.GetIssueInWorkspace(context.Background(), db.GetIssueInWorkspaceParams{
		ID:          util.MustParseUUID(issueID),
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("GetIssueInWorkspace: %v", err)
	}
	return fmt.Sprintf("%s-%d", workspace.IssuePrefix, issue.Number)
}

func createNotificationBindingForUser(t *testing.T, userID, provider string) string {
	t.Helper()

	var bindingID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO external_account_binding (
			user_id, provider, external_user_id, display_name, status, metadata
		)
		VALUES ($1, $2, $3, $4, 'active', '{}'::jsonb)
		RETURNING id
	`, userID, provider, provider+"-external-user", "Bound "+provider).Scan(&bindingID); err != nil {
		t.Fatalf("createNotificationBindingForUser: %v", err)
	}
	return bindingID
}

func enableNotificationPreferenceForUser(t *testing.T, userID, channel, eventType, bindingID string) {
	t.Helper()

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO notification_channel_preference (
			user_id, channel, event_type, enabled, binding_id
		)
		VALUES ($1, $2, $3, true, $4)
		ON CONFLICT (user_id, channel, event_type)
		DO UPDATE SET enabled = EXCLUDED.enabled, binding_id = EXCLUDED.binding_id
	`, userID, channel, eventType, bindingID); err != nil {
		t.Fatalf("enableNotificationPreferenceForUser: %v", err)
	}
}

func TestNotification_MentionedCommentCreatesCanonicalNotification(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	mentionedEmail := "notif-mentioned@multica.ai"
	mentionedID := createTestUser(t, mentionedEmail)
	t.Cleanup(func() { cleanupTestUser(t, mentionedEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	commentID := "00000000-0000-0000-0000-000000000123"
	commentContent := "ping [@Mentioned](mention://member/" + mentionedID + ") please check this"
	issueTitle := "mentioned issue"

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, commentID, issueID, testWorkspaceID, "member", testUserID, commentContent, "comment"); err != nil {
		t.Fatalf("insert comment: %v", err)
	}

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         commentID,
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   testUserID,
				Content:    commentContent,
				Type:       "comment",
			},
			"issue_title":  issueTitle,
			"issue_status": "todo",
			"app_origin":   "http://localhost:3000",
		},
	})

	inboxItems := inboxItemsForRecipient(t, queries, mentionedID)
	if len(inboxItems) != 1 {
		t.Fatalf("expected 1 inbox item for mentioned user, got %d", len(inboxItems))
	}
	if inboxItems[0].Type != "mentioned" {
		t.Fatalf("expected inbox type 'mentioned', got %q", inboxItems[0].Type)
	}
	if !inboxItems[0].Body.Valid || inboxItems[0].Body.String != commentContent {
		t.Fatalf("expected inbox body %q, got %#v", commentContent, inboxItems[0].Body)
	}

	events := notificationEventsForRecipient(t, queries, mentionedID)
	if len(events) != 1 {
		t.Fatalf("expected 1 canonical notification event, got %d", len(events))
	}
	if events[0].Type != "mentioned" {
		t.Fatalf("expected notification type 'mentioned', got %q", events[0].Type)
	}
	if events[0].Title != issueTitle {
		t.Fatalf("expected notification title %q, got %q", issueTitle, events[0].Title)
	}
	if !events[0].Body.Valid || events[0].Body.String != commentContent {
		t.Fatalf("expected notification body %q, got %#v", commentContent, events[0].Body)
	}
	if !events[0].CommentID.Valid || util.UUIDToString(events[0].CommentID) != commentID {
		t.Fatalf("expected notification comment_id %q, got %q", commentID, util.UUIDToString(events[0].CommentID))
	}

	workspace, err := queries.GetWorkspace(context.Background(), util.MustParseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	expectedLink := "http://localhost:3000/" + workspace.Slug + "/issues/" + issueIdentifierForTest(t, queries, issueID) + "?comment=" + commentID
	if !events[0].Link.Valid || events[0].Link.String != expectedLink {
		t.Fatalf("expected notification link %q, got %#v", expectedLink, events[0].Link)
	}

	deliveries := notificationDeliveriesForEvent(t, queries, util.UUIDToString(events[0].ID))
	if len(deliveries) != 1 {
		t.Fatalf("expected 1 notification delivery, got %d", len(deliveries))
	}
	if deliveries[0].Channel != "inbox" {
		t.Fatalf("expected delivery channel 'inbox', got %q", deliveries[0].Channel)
	}
	if deliveries[0].Status != "sent" {
		t.Fatalf("expected delivery status 'sent', got %q", deliveries[0].Status)
	}
	if deliveries[0].AttemptCount != 1 {
		t.Fatalf("expected delivery attempt_count 1, got %d", deliveries[0].AttemptCount)
	}
	if !deliveries[0].SentAt.Valid {
		t.Fatal("expected delivery sent_at to be populated")
	}
}

func TestNotification_MentionedCommentQueuesDingTalkDeliveryWhenEnabled(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	mentionedEmail := "notif-mentioned-dingtalk@multica.ai"
	mentionedID := createTestUser(t, mentionedEmail)
	t.Cleanup(func() { cleanupTestUser(t, mentionedEmail) })

	bindingID := createNotificationBindingForUser(t, mentionedID, "dingtalk")
	enableNotificationPreferenceForUser(t, mentionedID, "dingtalk", "mentioned", bindingID)
	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	commentID := "00000000-0000-0000-0000-000000000456"
	commentContent := "ding [@Mentioned](mention://member/" + mentionedID + ") now"

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, commentID, issueID, testWorkspaceID, "member", testUserID, commentContent, "comment"); err != nil {
		t.Fatalf("insert comment: %v", err)
	}

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         commentID,
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   testUserID,
				Content:    commentContent,
				Type:       "comment",
			},
			"issue_title":  "mentioned issue",
			"issue_status": "todo",
			"app_origin":   "http://localhost:3000",
		},
	})

	events := notificationEventsForRecipient(t, queries, mentionedID)
	if len(events) != 1 {
		t.Fatalf("expected 1 canonical notification event, got %d", len(events))
	}

	deliveries := notificationDeliveriesForEvent(t, queries, util.UUIDToString(events[0].ID))
	if len(deliveries) != 2 {
		t.Fatalf("expected 2 notification deliveries, got %d", len(deliveries))
	}

	if deliveries[0].Channel != "inbox" {
		t.Fatalf("expected first delivery channel 'inbox', got %q", deliveries[0].Channel)
	}
	if deliveries[1].Channel != "dingtalk" {
		t.Fatalf("expected second delivery channel 'dingtalk', got %q", deliveries[1].Channel)
	}
	if deliveries[1].Status != "pending" {
		t.Fatalf("expected dingtalk delivery status 'pending', got %q", deliveries[1].Status)
	}
	if deliveries[1].AttemptCount != 0 {
		t.Fatalf("expected dingtalk delivery attempt_count 0, got %d", deliveries[1].AttemptCount)
	}
	if deliveries[1].SentAt.Valid {
		t.Fatal("expected pending dingtalk delivery sent_at to be empty")
	}

	var snapshot struct {
		BindingID         string          `json:"binding_id"`
		Provider          string          `json:"provider"`
		ExternalUserID    string          `json:"external_user_id"`
		NotificationEvent json.RawMessage `json:"notification_event"`
	}
	if err := json.Unmarshal(deliveries[1].PayloadSnapshot, &snapshot); err != nil {
		t.Fatalf("unmarshal dingtalk payload snapshot: %v", err)
	}
	if snapshot.BindingID != bindingID {
		t.Fatalf("expected binding_id %q, got %q", bindingID, snapshot.BindingID)
	}
	if snapshot.Provider != "dingtalk" {
		t.Fatalf("expected provider 'dingtalk', got %q", snapshot.Provider)
	}
	if len(snapshot.NotificationEvent) == 0 {
		t.Fatal("expected nested notification_event payload in dingtalk snapshot")
	}
	var nested struct {
		IssueIdentifier string `json:"issue_identifier"`
		Link            string `json:"link"`
		ActorName       string `json:"actor_name"`
	}
	if err := json.Unmarshal(snapshot.NotificationEvent, &nested); err != nil {
		t.Fatalf("unmarshal nested notification_event: %v", err)
	}
	if nested.ActorName != integrationTestName {
		t.Fatalf("expected nested actor_name %q, got %q", integrationTestName, nested.ActorName)
	}
	expectedIdentifier := issueIdentifierForTest(t, queries, issueID)
	if nested.IssueIdentifier != expectedIdentifier {
		t.Fatalf("expected nested issue_identifier %q, got %q", expectedIdentifier, nested.IssueIdentifier)
	}
	workspace, err := queries.GetWorkspace(context.Background(), util.MustParseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	expectedLink := "http://localhost:3000/" + workspace.Slug + "/issues/" + expectedIdentifier + "?comment=" + commentID
	if nested.Link != expectedLink {
		t.Fatalf("expected nested link %q, got %q", expectedLink, nested.Link)
	}
}

func TestNotification_MentionedCommentQueuesEmailDeliveryWhenEnabled(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	mentionedEmail := "notif-mentioned-email@multica.ai"
	mentionedID := createTestUser(t, mentionedEmail)
	t.Cleanup(func() { cleanupTestUser(t, mentionedEmail) })

	bindingID := createNotificationBindingForUser(t, mentionedID, "email")
	enableNotificationPreferenceForUser(t, mentionedID, "email", "mentioned", bindingID)
	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	commentID := "00000000-0000-0000-0000-000000000567"
	commentContent := "email [@Mentioned](mention://member/" + mentionedID + ") now"

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, commentID, issueID, testWorkspaceID, "member", testUserID, commentContent, "comment"); err != nil {
		t.Fatalf("insert comment: %v", err)
	}

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         commentID,
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   testUserID,
				Content:    commentContent,
				Type:       "comment",
			},
			"issue_title":  "mentioned issue",
			"issue_status": "todo",
			"app_origin":   "http://localhost:3000",
		},
	})

	events := notificationEventsForRecipient(t, queries, mentionedID)
	if len(events) != 1 {
		t.Fatalf("expected 1 canonical notification event, got %d", len(events))
	}

	deliveries := notificationDeliveriesForEvent(t, queries, util.UUIDToString(events[0].ID))
	if len(deliveries) != 2 {
		t.Fatalf("expected 2 notification deliveries, got %d", len(deliveries))
	}
	if deliveries[1].Channel != "email" {
		t.Fatalf("expected second delivery channel 'email', got %q", deliveries[1].Channel)
	}
	if deliveries[1].Status != "pending" {
		t.Fatalf("expected email delivery status 'pending', got %q", deliveries[1].Status)
	}

	var snapshot struct {
		BindingID         string          `json:"binding_id"`
		Provider          string          `json:"provider"`
		NotificationEvent json.RawMessage `json:"notification_event"`
	}
	if err := json.Unmarshal(deliveries[1].PayloadSnapshot, &snapshot); err != nil {
		t.Fatalf("unmarshal email payload snapshot: %v", err)
	}
	if snapshot.BindingID != bindingID {
		t.Fatalf("expected binding_id %q, got %q", bindingID, snapshot.BindingID)
	}
	if snapshot.Provider != "email" {
		t.Fatalf("expected provider 'email', got %q", snapshot.Provider)
	}

	var nested struct {
		ActorName string `json:"actor_name"`
	}
	if err := json.Unmarshal(snapshot.NotificationEvent, &nested); err != nil {
		t.Fatalf("unmarshal nested notification_event: %v", err)
	}
	if nested.ActorName != integrationTestName {
		t.Fatalf("expected nested actor_name %q, got %q", integrationTestName, nested.ActorName)
	}
}

func TestNotification_SelfMentionQueuesDingTalkDeliveryWhenEnabled(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM notification_channel_preference WHERE user_id = $1`, testUserID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM external_account_binding WHERE user_id = $1`, testUserID)
	})

	bindingID := createNotificationBindingForUser(t, testUserID, "dingtalk")
	enableNotificationPreferenceForUser(t, testUserID, "dingtalk", "mentioned", bindingID)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	commentID := "00000000-0000-0000-0000-000000000789"
	commentContent := "self [@Me](mention://member/" + testUserID + ") now"

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, commentID, issueID, testWorkspaceID, "member", testUserID, commentContent, "comment"); err != nil {
		t.Fatalf("insert comment: %v", err)
	}

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         commentID,
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   testUserID,
				Content:    commentContent,
				Type:       "comment",
			},
			"issue_title":  "self mentioned issue",
			"issue_status": "todo",
		},
	})

	inboxItems := inboxItemsForRecipient(t, queries, testUserID)
	if len(inboxItems) != 1 {
		t.Fatalf("expected 1 inbox item for self mention, got %d", len(inboxItems))
	}

	events := notificationEventsForRecipient(t, queries, testUserID)
	if len(events) != 1 {
		t.Fatalf("expected 1 canonical notification event, got %d", len(events))
	}

	deliveries := notificationDeliveriesForEvent(t, queries, util.UUIDToString(events[0].ID))
	if len(deliveries) != 2 {
		t.Fatalf("expected 2 notification deliveries, got %d", len(deliveries))
	}
	if deliveries[1].Channel != "dingtalk" {
		t.Fatalf("expected second delivery channel 'dingtalk', got %q", deliveries[1].Channel)
	}
	if deliveries[1].Status != "pending" {
		t.Fatalf("expected dingtalk delivery status 'pending', got %q", deliveries[1].Status)
	}

	var snapshot struct {
		NotificationEvent json.RawMessage `json:"notification_event"`
	}
	if err := json.Unmarshal(deliveries[1].PayloadSnapshot, &snapshot); err != nil {
		t.Fatalf("unmarshal dingtalk payload snapshot: %v", err)
	}
	var nested struct {
		IssueIdentifier string `json:"issue_identifier"`
		ActorName       string `json:"actor_name"`
	}
	if err := json.Unmarshal(snapshot.NotificationEvent, &nested); err != nil {
		t.Fatalf("unmarshal nested notification_event: %v", err)
	}
	if nested.ActorName != integrationTestName {
		t.Fatalf("expected nested actor_name %q, got %q", integrationTestName, nested.ActorName)
	}
	if expected := issueIdentifierForTest(t, queries, issueID); nested.IssueIdentifier != expected {
		t.Fatalf("expected nested issue_identifier %q, got %q", expected, nested.IssueIdentifier)
	}
}

// countInboxByTypeForRecipient counts inbox rows of a given type for a
// recipient, including archived rows. Used to distinguish "row never created"
// from "row archived."
func countInboxByTypeForRecipient(t *testing.T, recipientID, notifType string) (active, archived int) {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `
		SELECT archived FROM inbox_item
		WHERE workspace_id = $1 AND recipient_type = 'member' AND recipient_id = $2 AND type = $3
	`, testWorkspaceID, recipientID, notifType)
	if err != nil {
		t.Fatalf("countInboxByTypeForRecipient: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var isArchived bool
		if err := rows.Scan(&isArchived); err != nil {
			t.Fatalf("countInboxByTypeForRecipient scan: %v", err)
		}
		if isArchived {
			archived++
		} else {
			active++
		}
	}
	return active, archived
}

// publishStatusChange is a small helper to publish the issue:updated event
// shape used by the notification listener for status-only transitions.
func publishStatusChange(bus *events.Bus, issueID, newStatus, prevStatus string) {
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "task_failed dismiss test",
				Status:      newStatus,
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed": false,
			"status_changed":   true,
			"prev_status":      prevStatus,
		},
	})
}

// TestNotification_StatusChange_ArchivesStaleTaskFailed verifies that when an
// issue transitions into a terminal status (in_review/done/cancelled), any
// existing task_failed inbox rows for that issue are archived for every
// affected member recipient, an inbox:batch-archived event fires per
// recipient, and sibling notifications on the same issue are untouched.
func TestNotification_StatusChange_ArchivesStaleTaskFailed(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	subEmail := "notif-archive-task-failed-sub@multica.ai"
	subID := createTestUser(t, subEmail)
	t.Cleanup(func() { cleanupTestUser(t, subEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")
	addTestSubscriber(t, issueID, "member", subID, "assignee")

	agentID := "00000000-0000-0000-0000-aaaaaaaaaaaa"

	// Two failed runs land before the status flip.
	for i := 0; i < 2; i++ {
		bus.Publish(events.Event{
			Type:        protocol.EventTaskFailed,
			WorkspaceID: testWorkspaceID,
			ActorType:   "system",
			Payload: map[string]any{
				"task_id":  "00000000-0000-0000-0000-bbbbbbbbbbbb",
				"agent_id": agentID,
				"issue_id": issueID,
			},
		})
	}

	// A separate non-task notification on the same issue, so we can prove
	// the archive scope is narrow. Use a comment-like notification by
	// directly inserting a row of a different type.
	_, err := testPool.Exec(context.Background(), `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, details)
		VALUES ($1, 'member', $2, 'new_comment', 'info', $3, 'sibling notification', '{}')
	`, testWorkspaceID, testUserID, issueID)
	if err != nil {
		t.Fatalf("seed sibling notification: %v", err)
	}

	if active, _ := countInboxByTypeForRecipient(t, testUserID, "task_failed"); active != 2 {
		t.Fatalf("precondition: expected 2 active task_failed rows for creator, got %d", active)
	}
	if active, _ := countInboxByTypeForRecipient(t, subID, "task_failed"); active != 2 {
		t.Fatalf("precondition: expected 2 active task_failed rows for sub, got %d", active)
	}

	// Track the batch-archived events fired during the status change.
	var batchArchived []events.Event
	bus.Subscribe(protocol.EventInboxBatchArchived, func(e events.Event) {
		batchArchived = append(batchArchived, e)
	})

	publishStatusChange(bus, issueID, "in_review", "in_progress")

	// task_failed rows are archived for both recipients.
	for _, recipient := range []string{testUserID, subID} {
		active, archived := countInboxByTypeForRecipient(t, recipient, "task_failed")
		if active != 0 {
			t.Fatalf("recipient %s: expected 0 active task_failed rows after terminal status, got %d", recipient, active)
		}
		if archived != 2 {
			t.Fatalf("recipient %s: expected 2 archived task_failed rows after terminal status, got %d", recipient, archived)
		}
	}

	// Sibling notification on the same issue is untouched.
	if active, _ := countInboxByTypeForRecipient(t, testUserID, "new_comment"); active != 1 {
		t.Fatalf("expected sibling new_comment row to remain active, got %d active", active)
	}

	// One inbox:batch-archived event per affected recipient.
	if len(batchArchived) != 2 {
		t.Fatalf("expected 2 inbox:batch-archived events (one per recipient), got %d", len(batchArchived))
	}
	seenRecipients := map[string]bool{}
	for _, e := range batchArchived {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			t.Fatalf("inbox:batch-archived: unexpected payload type %T", e.Payload)
		}
		recipientID, _ := payload["recipient_id"].(string)
		if recipientID == "" {
			t.Fatalf("inbox:batch-archived: missing recipient_id in payload %+v", payload)
		}
		if payload["issue_id"] != issueID {
			t.Fatalf("inbox:batch-archived: expected issue_id %q, got %v", issueID, payload["issue_id"])
		}
		if payload["reason"] != "issue_status_terminal" {
			t.Fatalf("inbox:batch-archived: expected reason 'issue_status_terminal', got %v", payload["reason"])
		}
		if count, _ := payload["count"].(int64); count != 2 {
			t.Fatalf("inbox:batch-archived: expected count=2 for recipient %s, got %v", recipientID, payload["count"])
		}
		seenRecipients[recipientID] = true
	}
	if !seenRecipients[testUserID] || !seenRecipients[subID] {
		t.Fatalf("expected batch-archived events for both creator and sub, got %v", seenRecipients)
	}
}

// TestNotification_StatusChange_NonTerminalKeepsTaskFailed verifies that a
// transition to a non-terminal status (e.g. in_progress) does NOT archive
// existing task_failed inbox rows.
func TestNotification_StatusChange_NonTerminalKeepsTaskFailed(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")

	bus.Publish(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":  "00000000-0000-0000-0000-bbbbbbbbbbbb",
			"agent_id": "00000000-0000-0000-0000-aaaaaaaaaaaa",
			"issue_id": issueID,
		},
	})

	if active, _ := countInboxByTypeForRecipient(t, testUserID, "task_failed"); active != 1 {
		t.Fatalf("precondition: expected 1 active task_failed row, got %d", active)
	}

	publishStatusChange(bus, issueID, "in_progress", "todo")

	// task_failed row stays active because in_progress is not terminal.
	active, archived := countInboxByTypeForRecipient(t, testUserID, "task_failed")
	if active != 1 || archived != 0 {
		t.Fatalf("expected task_failed row to remain active after non-terminal transition, got active=%d archived=%d", active, archived)
	}
}

// TestNotification_StatusChange_ReopenSurfacesNewTaskFailed verifies that
// after a terminal-status auto-archive, a status flip back to in_progress
// followed by a new task failure produces a fresh, visible task_failed row.
// This guards the "reopen and rerun" path described in the design.
func TestNotification_StatusChange_ReopenSurfacesNewTaskFailed(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	addTestSubscriber(t, issueID, "member", testUserID, "creator")

	agentID := "00000000-0000-0000-0000-aaaaaaaaaaaa"

	bus.Publish(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":  "00000000-0000-0000-0000-bbbbbbbbbbbb",
			"agent_id": agentID,
			"issue_id": issueID,
		},
	})

	// First terminal transition archives the original failure.
	publishStatusChange(bus, issueID, "in_review", "in_progress")
	if active, archived := countInboxByTypeForRecipient(t, testUserID, "task_failed"); active != 0 || archived != 1 {
		t.Fatalf("after terminal transition: expected active=0 archived=1, got active=%d archived=%d", active, archived)
	}

	// Reviewer kicks the issue back; a rerun fails again.
	publishStatusChange(bus, issueID, "in_progress", "in_review")
	bus.Publish(events.Event{
		Type:        protocol.EventTaskFailed,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":  "00000000-0000-0000-0000-cccccccccccc",
			"agent_id": agentID,
			"issue_id": issueID,
		},
	})

	// The new failure is visible; the old archived row stays archived.
	active, archived := countInboxByTypeForRecipient(t, testUserID, "task_failed")
	if active != 1 {
		t.Fatalf("expected 1 active task_failed row after reopen+fail, got %d", active)
	}
	if archived != 1 {
		t.Fatalf("expected 1 archived task_failed row preserved from prior cycle, got %d", archived)
	}
}

// TestNotification_ReplyNotifiesParentAuthor verifies that replying to a
// member's comment sends a "mentioned" notification to the parent author,
// even without an explicit @mention. (OPE-856)
func TestNotification_ReplyNotifiesParentAuthor(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	parentAuthorEmail := "notif-parent-author@multica.ai"
	parentAuthorID := createTestUser(t, parentAuthorEmail)
	t.Cleanup(func() { cleanupTestUser(t, parentAuthorEmail) })

	replierEmail := "notif-replier@multica.ai"
	replierID := createTestUser(t, replierEmail)
	t.Cleanup(func() { cleanupTestUser(t, replierEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		testPool.Exec(context.Background(), `DELETE FROM notification_event WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		cleanupTestIssue(t, issueID)
	})

	// Create a parent comment by the parent author.
	parentCommentID := "00000000-0000-0000-0000-ae0100000001"
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, parentCommentID, issueID, testWorkspaceID, "member", parentAuthorID, "parent comment", "comment"); err != nil {
		t.Fatalf("insert parent comment: %v", err)
	}

	// Insert the reply comment into DB (needed for notification_event FK).
	replyCommentID := "00000000-0000-0000-0000-ae0100000002"
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type, parent_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, replyCommentID, issueID, testWorkspaceID, "member", replierID, "thanks, I will check it", "comment", parentCommentID); err != nil {
		t.Fatalf("insert reply comment: %v", err)
	}

	// Publish a reply event (no @mention in content).
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     replierID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         replyCommentID,
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   replierID,
				Content:    "thanks, I will check it",
				Type:       "comment",
				ParentID:   &parentCommentID,
			},
			"issue_title":  "reply test issue",
			"issue_status": "todo",
			"app_origin":   "http://localhost:3000",
		},
	})

	// Parent author should receive a "mentioned" inbox notification.
	items := inboxItemsForRecipient(t, queries, parentAuthorID)
	var mentionedItems []db.ListInboxItemsRow
	for _, item := range items {
		if item.Type == "mentioned" {
			mentionedItems = append(mentionedItems, item)
		}
	}
	if len(mentionedItems) != 1 {
		t.Fatalf("expected 1 'mentioned' inbox item for parent author, got %d (total items: %d)", len(mentionedItems), len(items))
	}

	// Replier should NOT get a reply notification to themselves.
	replierItems := inboxItemsForRecipient(t, queries, replierID)
	for _, item := range replierItems {
		if item.Type == "mentioned" {
			t.Fatalf("replier should not get a mentioned notification from their own reply")
		}
	}
}

// TestNotification_ReplyToSelf_NoNotification verifies that replying to your
// own comment does NOT produce a self-notification. (OPE-856 AC2)
func TestNotification_ReplyToSelf_NoNotification(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	selfEmail := "notif-self-reply@multica.ai"
	selfID := createTestUser(t, selfEmail)
	t.Cleanup(func() { cleanupTestUser(t, selfEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		cleanupTestIssue(t, issueID)
	})

	// Create a parent comment by selfID.
	parentCommentID := "00000000-0000-0000-0000-5e1f0e010001"
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, parentCommentID, issueID, testWorkspaceID, "member", selfID, "my own comment", "comment"); err != nil {
		t.Fatalf("insert parent comment: %v", err)
	}

	// Self-reply.
	replyCommentID := "00000000-0000-0000-0000-5e1f0e010002"
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     selfID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         replyCommentID,
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   selfID,
				Content:    "replying to my own comment",
				Type:       "comment",
				ParentID:   &parentCommentID,
			},
			"issue_title":  "self reply test",
			"issue_status": "todo",
		},
	})

	// Self should NOT get a mentioned notification.
	items := inboxItemsForRecipient(t, queries, selfID)
	for _, item := range items {
		if item.Type == "mentioned" {
			t.Fatalf("self-reply should not produce a mentioned notification")
		}
	}
}

// TestNotification_ReplyWithMention_NoDuplicate verifies that if the reply
// already @mentions the parent author, no duplicate notification is sent.
// (OPE-856 AC3)
func TestNotification_ReplyWithMention_NoDuplicate(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	parentAuthorEmail := "notif-dedup-parent@multica.ai"
	parentAuthorID := createTestUser(t, parentAuthorEmail)
	t.Cleanup(func() { cleanupTestUser(t, parentAuthorEmail) })

	replierEmail := "notif-dedup-replier@multica.ai"
	replierID := createTestUser(t, replierEmail)
	t.Cleanup(func() { cleanupTestUser(t, replierEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		testPool.Exec(context.Background(), `DELETE FROM notification_event WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		cleanupTestIssue(t, issueID)
	})

	// Create parent comment.
	parentCommentID := "00000000-0000-0000-0000-ded000000001"
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, parentCommentID, issueID, testWorkspaceID, "member", parentAuthorID, "original comment", "comment"); err != nil {
		t.Fatalf("insert parent comment: %v", err)
	}

	// Insert the reply comment into DB (needed for notification_event FK).
	replyContent := fmt.Sprintf("[@Parent](mention://member/%s) got it!", parentAuthorID)
	replyCommentID := "00000000-0000-0000-0000-ded000000002"
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type, parent_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, replyCommentID, issueID, testWorkspaceID, "member", replierID, replyContent, "comment", parentCommentID); err != nil {
		t.Fatalf("insert reply comment: %v", err)
	}

	// Reply that also @mentions the parent author explicitly.
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     replierID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         replyCommentID,
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   replierID,
				Content:    replyContent,
				Type:       "comment",
				ParentID:   &parentCommentID,
			},
			"issue_title":  "dedup test issue",
			"issue_status": "todo",
			"app_origin":   "http://localhost:3000",
		},
	})

	// Parent author should receive exactly 1 "mentioned" notification (not 2).
	items := inboxItemsForRecipient(t, queries, parentAuthorID)
	mentionedCount := 0
	for _, item := range items {
		if item.Type == "mentioned" {
			mentionedCount++
		}
	}
	if mentionedCount != 1 {
		t.Fatalf("expected exactly 1 'mentioned' inbox item (dedup), got %d", mentionedCount)
	}
}

// TestNotification_ReplyToAgent_NoNotification verifies that replying to an
// agent's comment does NOT trigger a notification. (OPE-856 AC4)
func TestNotification_ReplyToAgent_NoNotification(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	replierEmail := "notif-agent-reply@multica.ai"
	replierID := createTestUser(t, replierEmail)
	t.Cleanup(func() { cleanupTestUser(t, replierEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		testPool.Exec(context.Background(), `DELETE FROM comment WHERE issue_id = $1`, issueID)
		cleanupTestIssue(t, issueID)
	})

	// Create a parent comment authored by an agent.
	agentID := "00000000-0000-0000-0000-a9e000000001"
	parentCommentID := "00000000-0000-0000-0000-a9e0000e0101"
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, parentCommentID, issueID, testWorkspaceID, "agent", agentID, "agent said something", "comment"); err != nil {
		t.Fatalf("insert agent comment: %v", err)
	}

	// Reply to agent comment.
	replyCommentID := "00000000-0000-0000-0000-a9e0000e0102"
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     replierID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         replyCommentID,
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   replierID,
				Content:    "replying to an agent",
				Type:       "comment",
				ParentID:   &parentCommentID,
			},
			"issue_title":  "agent reply test",
			"issue_status": "todo",
		},
	})

	// No "mentioned" notification should exist for the agent or anyone else
	// (beyond subscriber notifications which go to issue subscribers).
	// Specifically check that no mentioned-type inbox was created for the agent.
	agentItems, _ := queries.ListInboxItems(context.Background(), db.ListInboxItemsParams{
		WorkspaceID:   util.MustParseUUID(testWorkspaceID),
		RecipientType: "agent",
		RecipientID:   util.MustParseUUID(agentID),
	})
	for _, item := range agentItems {
		if item.Type == "mentioned" {
			t.Fatalf("agent should not receive a mentioned notification from reply")
		}
	}
}

// TestNotification_DingTalkTaskCompletedIndependentOfMentioned verifies that
// enabling dingtalk + task_completed does not depend on dingtalk + mentioned,
// and toggling task_completed independently creates a DingTalk delivery
// without affecting the mentioned preference behavior.
func TestNotification_DingTalkTaskCompletedIndependentOfMentioned(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	recipientEmail := "notif-dt-task-completed@multica.ai"
	recipientID := createTestUser(t, recipientEmail)
	t.Cleanup(func() { cleanupTestUser(t, recipientEmail) })

	bindingID := createNotificationBindingForUser(t, recipientID, "dingtalk")
	// Enable only task_completed for dingtalk — NOT mentioned
	enableNotificationPreferenceForUser(t, recipientID, "dingtalk", "task_completed", bindingID)

	issueID := createTestIssue(t, testWorkspaceID, recipientID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	agentID := "00000000-0000-0000-0000-aaaaaaaaaaaa"
	addTestSubscriber(t, issueID, "member", recipientID, "creator")
	addTestSubscriber(t, issueID, "agent", agentID, "assignee")

	// Publish task:completed
	bus.Publish(events.Event{
		Type:        protocol.EventTaskCompleted,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		ActorID:     "",
		Payload: map[string]any{
			"task_id":  "00000000-0000-0000-0000-bbbbbbbbbbbb",
			"agent_id": agentID,
			"issue_id": issueID,
			"status":   "completed",
		},
	})

	// Should have a notification event for the recipient
	nEvents := notificationEventsForRecipient(t, queries, recipientID)
	if len(nEvents) == 0 {
		t.Fatal("expected at least 1 notification event for task_completed dingtalk delivery")
	}

	// Find DingTalk delivery among all events
	var foundDingtalk bool
	for _, ne := range nEvents {
		deliveries := notificationDeliveriesForEvent(t, queries, util.UUIDToString(ne.ID))
		for _, d := range deliveries {
			if d.Channel == "dingtalk" {
				foundDingtalk = true
				if d.Status != "pending" {
					t.Fatalf("expected dingtalk delivery status 'pending', got %q", d.Status)
				}
			}
		}
	}
	if !foundDingtalk {
		t.Fatal("expected a DingTalk delivery record for task_completed, but found none")
	}

	// Now verify that mentioned preference is NOT enabled (no dingtalk delivery
	// should be created for a mention event)
	commentID := "00000000-0000-0000-0000-000000000789"
	commentContent := "hey [@Recipient](mention://member/" + recipientID + ") check this"
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (id, issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, commentID, issueID, testWorkspaceID, "member", testUserID, commentContent, "comment"); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), "DELETE FROM comment WHERE id = $1", commentID)
	})

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         commentID,
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   testUserID,
				Content:    commentContent,
				Type:       "comment",
			},
			"issue_title":  "dingtalk task pref test",
			"issue_status": "todo",
		},
	})

	// Check that no dingtalk delivery was created for the mentioned event
	mentionEvents := notificationEventsForRecipient(t, queries, recipientID)
	for _, ne := range mentionEvents {
		if ne.Type != "mentioned" {
			continue
		}
		deliveries := notificationDeliveriesForEvent(t, queries, util.UUIDToString(ne.ID))
		for _, d := range deliveries {
			if d.Channel == "dingtalk" {
				t.Fatal("dingtalk delivery should NOT be created for mentioned when only task_completed is enabled")
			}
		}
	}
}
