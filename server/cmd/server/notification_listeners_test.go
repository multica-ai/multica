package main

import (
	"context"
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
			"assignee_changed":  true,
			"status_changed":    false,
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
