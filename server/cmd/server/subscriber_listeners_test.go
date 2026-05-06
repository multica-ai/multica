package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// setUserAutoSubscribePrefs replaces the user's preferences.auto_subscribe
// block wholesale. Existing keys outside auto_subscribe are preserved.
func setUserAutoSubscribePrefs(t *testing.T, userID string, autoSub map[string]bool) {
	t.Helper()
	ctx := context.Background()

	var existingRaw []byte
	if err := testPool.QueryRow(ctx, `SELECT preferences FROM "user" WHERE id = $1`, userID).Scan(&existingRaw); err != nil {
		t.Fatalf("read prefs: %v", err)
	}
	blob := map[string]any{}
	if len(existingRaw) > 0 {
		_ = json.Unmarshal(existingRaw, &blob)
	}
	asBlock := map[string]any{}
	for k, v := range autoSub {
		asBlock[k] = v
	}
	blob["auto_subscribe"] = asBlock

	raw, err := json.Marshal(blob)
	if err != nil {
		t.Fatalf("marshal prefs: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE "user" SET preferences = $1::jsonb WHERE id = $2`, raw, userID); err != nil {
		t.Fatalf("set prefs: %v", err)
	}
}

// subscriberTest helpers — reuse the integration test fixtures from TestMain
// (testPool, testUserID, testWorkspaceID are set in integration_test.go).

// createTestIssue inserts a minimal issue and returns its UUID string.
// Bumps the workspace's issue_counter atomically so the per-workspace
// (workspace_id, number) unique constraint holds when a single test
// creates several issues in the same workspace.
func createTestIssue(t *testing.T, workspaceID, creatorID string) string {
	t.Helper()
	ctx := context.Background()
	var issueID string
	err := testPool.QueryRow(ctx, `
		WITH bump AS (
			UPDATE workspace SET issue_counter = issue_counter + 1
			WHERE id = $1 RETURNING issue_counter
		)
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number)
		SELECT $1, 'subscriber test issue', 'todo', 'medium', 'member', $2, 0, issue_counter FROM bump
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
		IssueID:  util.ParseUUID(issueID),
		UserType: userType,
		UserID:   util.ParseUUID(userID),
	})
	if err != nil {
		t.Fatalf("IsIssueSubscriber: %v", err)
	}
	return subscribed
}

func subscriberCount(t *testing.T, queries *db.Queries, issueID string) int {
	t.Helper()
	subs, err := queries.ListIssueSubscribers(context.Background(), util.ParseUUID(issueID))
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

// commentEvent builds a comment:created event for a member commenter.
func commentEvent(issueID, commenterID string) events.Event {
	return events.Event{
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
	}
}

// Default for "commenter" is false — opt-in. Without an explicit override the
// commenter must NOT be auto-subscribed.
func TestSubscriberCommentCreated_CommenterNotSubscribedByDefault(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	commenterEmail := "subscriber-commenter-default-test@multica.ai"
	commenterID := createTestUser(t, commenterEmail)
	t.Cleanup(func() { cleanupTestUser(t, commenterEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	bus.Publish(commentEvent(issueID, commenterID))

	if isSubscribed(t, queries, issueID, "member", commenterID) {
		t.Fatal("expected commenter to NOT be subscribed by default")
	}
}

// When the user opts in to auto_subscribe.commenter, the listener must
// subscribe them.
func TestSubscriberCommentCreated_CommenterSubscribesWhenOptIn(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	commenterEmail := "subscriber-commenter-optin-test@multica.ai"
	commenterID := createTestUser(t, commenterEmail)
	t.Cleanup(func() { cleanupTestUser(t, commenterEmail) })

	setUserAutoSubscribePrefs(t, commenterID, map[string]bool{"commenter": true})

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	bus.Publish(commentEvent(issueID, commenterID))

	if !isSubscribed(t, queries, issueID, "member", commenterID) {
		t.Fatal("expected commenter to be subscribed when auto_subscribe.commenter=true")
	}
}

// Default for "mentioned" is false — opt-in. A user mentioned in the
// description must NOT be auto-subscribed unless they opt in.
func TestSubscriberIssueCreated_MentionedNotSubscribedByDefault(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	mentionedEmail := "subscriber-mentioned-default-test@multica.ai"
	mentionedID := createTestUser(t, mentionedEmail)
	t.Cleanup(func() { cleanupTestUser(t, mentionedEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	desc := "Hey [@Mentioned](mention://member/" + mentionedID + ") please look"
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
				Description: &desc,
			},
		},
	})

	if isSubscribed(t, queries, issueID, "member", mentionedID) {
		t.Fatal("expected mentioned user NOT to be subscribed by default")
	}
}

// When the mentioned user has auto_subscribe.mentioned=true, the listener
// must subscribe them.
func TestSubscriberIssueCreated_MentionedSubscribesWhenOptIn(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	mentionedEmail := "subscriber-mentioned-optin-test@multica.ai"
	mentionedID := createTestUser(t, mentionedEmail)
	t.Cleanup(func() { cleanupTestUser(t, mentionedEmail) })

	setUserAutoSubscribePrefs(t, mentionedID, map[string]bool{"mentioned": true})

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	desc := "Hey [@Mentioned](mention://member/" + mentionedID + ") please look"
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
				Description: &desc,
			},
		},
	})

	if !isSubscribed(t, queries, issueID, "member", mentionedID) {
		t.Fatal("expected mentioned user to be subscribed when auto_subscribe.mentioned=true")
	}
}

// A user can opt OUT of being subscribed when assigned by setting
// auto_subscribe.assignee=false.
func TestSubscriberIssueUpdated_AssigneeOptOut(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	assigneeEmail := "subscriber-assignee-optout-test@multica.ai"
	assigneeID := createTestUser(t, assigneeEmail)
	t.Cleanup(func() { cleanupTestUser(t, assigneeEmail) })

	setUserAutoSubscribePrefs(t, assigneeID, map[string]bool{"assignee": false})

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

	if isSubscribed(t, queries, issueID, "member", assigneeID) {
		t.Fatal("expected assignee NOT to be subscribed when auto_subscribe.assignee=false")
	}
}

// Agents have no preferences UI — they must always auto-subscribe so their
// task pickup keeps working.
func TestSubscriberIssueCreated_AgentAlwaysSubscribesAsAssignee(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	// Pick an existing agent in the test workspace (any agent ID works for
	// subscription — we don't FK-validate agent existence here).
	var agentID string
	if err := testPool.QueryRow(
		context.Background(),
		`SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID); err != nil {
		t.Skipf("no agent in test workspace: %v", err)
	}

	assigneeType := "agent"
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
				AssigneeID:   &agentID,
			},
		},
	})

	if !isSubscribed(t, queries, issueID, "agent", agentID) {
		t.Fatal("expected agent assignee to always be subscribed")
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

// Verify parseUUID is consistent — pgtype.UUID from our local helper should match util.ParseUUID
func TestParseUUIDConsistency(t *testing.T) {
	uuid := "550e8400-e29b-41d4-a716-446655440000"
	local := parseUUID(uuid)
	utilResult := util.ParseUUID(uuid)
	if local != utilResult {
		t.Fatalf("parseUUID inconsistency: local=%v, util=%v", local, utilResult)
	}
	if !local.Valid {
		t.Fatal("expected valid UUID")
	}

	// Empty string should produce invalid UUID
	empty := parseUUID("")
	if empty != (pgtype.UUID{}) {
		t.Fatalf("expected zero UUID for empty string, got %v", empty)
	}
}
