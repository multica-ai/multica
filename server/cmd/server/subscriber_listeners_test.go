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

// subscriberTest helpers — reuse the integration test fixtures from TestMain
// (testPool, testUserID, testWorkspaceID are set in integration_test.go).

// createTestIssue inserts a minimal issue and returns its UUID string.
// Picks the next per-workspace number to avoid colliding with the
// uq_issue_workspace_number unique constraint when a single test creates
// multiple issues.
func createTestIssue(t *testing.T, workspaceID, creatorID string) string {
	t.Helper()
	ctx := context.Background()
	var issueID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number)
		VALUES ($1, 'subscriber test issue', 'todo', 'medium', 'member', $2, 0,
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, workspaceID, creatorID).Scan(&issueID)
	if err != nil {
		t.Fatalf("createTestIssue: %v", err)
	}
	return issueID
}

// createTestUser inserts a user with the given email and returns the UUID string.
func createTestUser(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Subscriber Test User", email).Scan(&userID)
	if err != nil {
		t.Fatalf("createTestUser: %v", err)
	}
	return userID
}

func cleanupTestIssue(t *testing.T, issueID string) {
	t.Helper()
	testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
}

func cleanupTestUser(t *testing.T, email string) {
	t.Helper()
	testPool.Exec(context.Background(), `DELETE FROM "user" WHERE email = $1`, email)
}

func isSubscribed(t *testing.T, queries *db.Queries, issueID, userType, userID string) bool {
	t.Helper()
	subscribed, err := queries.IsIssueSubscriber(context.Background(), db.IsIssueSubscriberParams{
		IssueID:  util.MustParseUUID(issueID),
		UserType: userType,
		UserID:   util.MustParseUUID(userID),
	})
	if err != nil {
		t.Fatalf("IsIssueSubscriber: %v", err)
	}
	return subscribed
}

func subscriberCount(t *testing.T, queries *db.Queries, issueID string) int {
	t.Helper()
	subs, err := queries.ListIssueSubscribers(context.Background(), util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("ListIssueSubscribers: %v", err)
	}
	return len(subs)
}

func TestSubscriberIssueCreated_CreatorSubscribed(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	// Publish issue:created event with no assignee
	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "test issue",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
		},
	})

	if !isSubscribed(t, queries, issueID, "member", testUserID) {
		t.Fatal("expected creator to be subscribed after issue:created")
	}
	if count := subscriberCount(t, queries, issueID); count != 1 {
		t.Fatalf("expected 1 subscriber, got %d", count)
	}
}

func TestSubscriberIssueCreated_CreatorAndAssignee(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	assigneeEmail := "subscriber-assignee-test@multica.ai"
	assigneeID := createTestUser(t, assigneeEmail)
	t.Cleanup(func() { cleanupTestUser(t, assigneeEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

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
				Title:        "test issue",
				Status:       "todo",
				Priority:     "medium",
				CreatorType:  "member",
				CreatorID:    testUserID,
				AssigneeType: &assigneeType,
				AssigneeID:   &assigneeID,
			},
		},
	})

	if !isSubscribed(t, queries, issueID, "member", testUserID) {
		t.Fatal("expected creator to be subscribed")
	}
	if !isSubscribed(t, queries, issueID, "member", assigneeID) {
		t.Fatal("expected assignee to be subscribed")
	}
	if count := subscriberCount(t, queries, issueID); count != 2 {
		t.Fatalf("expected 2 subscribers, got %d", count)
	}
}

func TestSubscriberIssueCreated_SelfAssign(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	// Creator is also the assignee (self-assign)
	assigneeType := "member"
	assigneeID := testUserID
	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:           issueID,
				WorkspaceID:  testWorkspaceID,
				Title:        "test issue",
				Status:       "todo",
				Priority:     "medium",
				CreatorType:  "member",
				CreatorID:    testUserID,
				AssigneeType: &assigneeType,
				AssigneeID:   &assigneeID,
			},
		},
	})

	// Should only have 1 subscriber record (ON CONFLICT DO NOTHING handles idempotency)
	if count := subscriberCount(t, queries, issueID); count != 1 {
		t.Fatalf("expected 1 subscriber for self-assign, got %d", count)
	}
	if !isSubscribed(t, queries, issueID, "member", testUserID) {
		t.Fatal("expected creator/assignee to be subscribed")
	}
}

func TestSubscriberIssueUpdated_AssigneeChanged(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	assigneeEmail := "subscriber-new-assignee-test@multica.ai"
	assigneeID := createTestUser(t, assigneeEmail)
	t.Cleanup(func() { cleanupTestUser(t, assigneeEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	assigneeType := "member"
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:           issueID,
				WorkspaceID:  testWorkspaceID,
				Title:        "test issue",
				Status:       "todo",
				Priority:     "medium",
				CreatorType:  "member",
				CreatorID:    testUserID,
				AssigneeType: &assigneeType,
				AssigneeID:   &assigneeID,
			},
			"assignee_changed": true,
		},
	})

	if !isSubscribed(t, queries, issueID, "member", assigneeID) {
		t.Fatal("expected new assignee to be subscribed after assignee change")
	}
}

func TestSubscriberIssueUpdated_NoAssigneeChange(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	// Publish issue:updated without assignee_changed flag
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "test issue",
				Status:      "in_progress",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"assignee_changed": false,
			"status_changed":   true,
		},
	})

	// No subscriber should have been added
	if count := subscriberCount(t, queries, issueID); count != 0 {
		t.Fatalf("expected 0 subscribers when assignee not changed, got %d", count)
	}
}

func TestSubscriberCommentCreated_CommenterSubscribed(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	commenterEmail := "subscriber-commenter-test@multica.ai"
	commenterID := createTestUser(t, commenterEmail)
	t.Cleanup(func() { cleanupTestUser(t, commenterEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     commenterID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000000000",
				IssueID:    issueID,
				AuthorType: "member",
				AuthorID:   commenterID,
				Content:    "test comment",
				Type:       "comment",
			},
		},
	})

	if !isSubscribed(t, queries, issueID, "member", commenterID) {
		t.Fatal("expected commenter to be subscribed after comment:created")
	}
}

func TestSubscriberAddedEventPublished(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	// Track subscriber:added events
	var subscriberEvents []events.Event
	bus.Subscribe(protocol.EventSubscriberAdded, func(e events.Event) {
		subscriberEvents = append(subscriberEvents, e)
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
				Title:       "test issue",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
		},
	})

	if len(subscriberEvents) != 1 {
		t.Fatalf("expected 1 subscriber:added event, got %d", len(subscriberEvents))
	}
	evt := subscriberEvents[0]
	if evt.WorkspaceID != testWorkspaceID {
		t.Fatalf("expected workspace_id %s, got %s", testWorkspaceID, evt.WorkspaceID)
	}
	payload, ok := evt.Payload.(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any payload")
	}
	if payload["issue_id"] != issueID {
		t.Fatalf("expected issue_id %s, got %v", issueID, payload["issue_id"])
	}
	if payload["user_id"] != testUserID {
		t.Fatalf("expected user_id %s, got %v", testUserID, payload["user_id"])
	}
}

// Autopilot publishes EventIssueCreated with a map[string]any payload (not handler.IssueResponse).
// The listener must still subscribe the creator.
func TestSubscriberIssueCreated_AutopilotMapPayload(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": map[string]any{
				"id":           issueID,
				"workspace_id": testWorkspaceID,
				"title":        "autopilot test issue",
				"status":       "todo",
				"priority":     "medium",
				"creator_type": "member",
				"creator_id":   testUserID,
			},
		},
	})

	if !isSubscribed(t, queries, issueID, "member", testUserID) {
		t.Fatal("expected creator to be subscribed when autopilot publishes map payload")
	}
}

// Verify parseUUID is consistent — the local helper should agree with util.MustParseUUID
// for valid input, and panic on invalid input (the silent-zero behavior was removed
// after #1661 to prevent silent SQL writes against a zero UUID).
func TestParseUUIDConsistency(t *testing.T) {
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	local := parseUUID(uuid)
	utilResult := util.MustParseUUID(uuid)
	if local != utilResult {
		t.Fatalf("parseUUID inconsistency: local=%v, util=%v", local, utilResult)
	}
	if !local.Valid {
		t.Fatal("expected valid UUID")
	}

	// Invalid input (empty string) must panic now — never silently return a zero UUID.
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected parseUUID(\"\") to panic, but it returned normally")
		}
	}()
	_ = parseUUID("")
}
