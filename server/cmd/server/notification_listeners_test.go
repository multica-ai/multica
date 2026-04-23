package main

import (
	"context"
	"encoding/json"
	"strings"
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
		WorkspaceID:   util.ParseUUID(testWorkspaceID),
		RecipientType: "member",
		RecipientID:   util.ParseUUID(recipientID),
	})
	if err != nil {
		t.Fatalf("ListInboxItems: %v", err)
	}
	return items
}

func notificationEventsForRecipient(t *testing.T, queries *db.Queries, recipientID string) []db.NotificationEvent {
	t.Helper()
	items, err := queries.ListNotificationEventsByRecipient(context.Background(), db.ListNotificationEventsByRecipientParams{
		WorkspaceID:     util.ParseUUID(testWorkspaceID),
		RecipientUserID: util.ParseUUID(recipientID),
	})
	if err != nil {
		t.Fatalf("ListNotificationEventsByRecipient: %v", err)
	}
	return items
}

func notificationDeliveriesForEvent(t *testing.T, queries *db.Queries, eventID string) []db.NotificationDelivery {
	t.Helper()
	items, err := queries.ListNotificationDeliveriesByEvent(context.Background(), util.ParseUUID(eventID))
	if err != nil {
		t.Fatalf("ListNotificationDeliveriesByEvent: %v", err)
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

	workspace, err := queries.GetWorkspace(context.Background(), util.ParseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	expectedLinkSuffix := "/" + workspace.Slug + "/issues/" + issueID + "?comment=" + commentID
	if !events[0].Link.Valid || !strings.Contains(events[0].Link.String, expectedLinkSuffix) {
		t.Fatalf("expected notification link to contain %q, got %#v", expectedLinkSuffix, events[0].Link)
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
}
