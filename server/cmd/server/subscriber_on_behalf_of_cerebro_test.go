package main

// FIR-4930 — the explicit "on behalf of" stamp decides who gets the issue in
// their inbox. These tests pin the two halves of that promise:
//
//  1. on create, the stamped human is subscribed and the human derived from the
//     agent's task chain (for an autopilot: whoever created the autopilot) is NOT;
//  2. on correction, the previously attributed human loses the subscription.

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/pkg/protocol"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// seedAgentTaskWithOriginalUser returns an agent id plus a task whose
// original_user_id is the given human — the shape an autopilot run produces.
func seedAgentTaskWithOriginalUser(t *testing.T, originalUserID string) (agentID, taskID string) {
	t.Helper()
	ctx := context.Background()

	var runtimeID string
	if err := testPool.QueryRow(ctx,
		`SELECT a.id::text, a.runtime_id::text FROM agent a WHERE a.workspace_id = $1 AND a.runtime_id IS NOT NULL LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID); err != nil {
		t.Skipf("no agent with runtime in test workspace: %v", err)
	}

	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, original_user_id)
		VALUES ($1, $2, 'queued', 0, $3)
		RETURNING id::text
	`, agentID, runtimeID, originalUserID).Scan(&taskID); err != nil {
		t.Fatalf("insert agent task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return agentID, taskID
}

// The reported bug: an agent running under an autopilot files issues for many
// different owners, but every task in the chain carries the AUTOPILOT CREATOR as
// original_user_id — so without an explicit stamp all of them land in that one
// person's inbox. With the stamp, only the stamped owner is subscribed.
func TestSubscriberIssueCreated_ExplicitOnBehalfOfReplacesDerivedHuman(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	autopilotCreatorEmail := "on-behalf-of-autopilot-creator@multica.ai"
	autopilotCreatorID := createTestUser(t, autopilotCreatorEmail)
	t.Cleanup(func() { cleanupTestUser(t, autopilotCreatorEmail) })

	appOwnerEmail := "on-behalf-of-app-owner@multica.ai"
	appOwnerID := createTestUser(t, appOwnerEmail)
	t.Cleanup(func() { cleanupTestUser(t, appOwnerEmail) })

	agentID, taskID := seedAgentTaskWithOriginalUser(t, autopilotCreatorID)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "agent",
		ActorID:     agentID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "Deploy review: invoice-warnings",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "agent",
				CreatorID:   agentID,
			},
			"triggering_task_id":   taskID,
			"on_behalf_of_user_id": appOwnerID,
		},
	})

	if !isSubscribed(t, queries, issueID, "member", appOwnerID) {
		t.Fatal("expected the stamped app owner to be auto-subscribed")
	}
	if isSubscribed(t, queries, issueID, "member", autopilotCreatorID) {
		t.Fatal("the autopilot creator must NOT be subscribed once an explicit human is stamped")
	}

	var reason string
	if err := testPool.QueryRow(context.Background(),
		`SELECT reason FROM issue_subscriber WHERE issue_id = $1 AND user_type = 'member' AND user_id = $2`,
		issueID, appOwnerID,
	).Scan(&reason); err != nil {
		t.Fatalf("read subscriber reason: %v", err)
	}
	if reason != "triggered_agent" {
		t.Fatalf("expected reason 'triggered_agent', got %q", reason)
	}
}

// Correcting a wrong stamp has to move the inbox, not just add a second person:
// the previously attributed human keeps getting notified otherwise, which is the
// symptom the whole feature removes.
func TestSubscriberIssueUpdated_CorrectedOnBehalfOfMovesTheSubscription(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	wrongEmail := "on-behalf-of-wrong-human@multica.ai"
	wrongID := createTestUser(t, wrongEmail)
	t.Cleanup(func() { cleanupTestUser(t, wrongEmail) })

	rightEmail := "on-behalf-of-right-human@multica.ai"
	rightID := createTestUser(t, rightEmail)
	t.Cleanup(func() { cleanupTestUser(t, rightEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	// The wrong human is already subscribed the way the platform would have
	// added them.
	addSubscriber(bus, queries, testWorkspaceID, issueID, "member", wrongID, "triggered_agent")
	if !isSubscribed(t, queries, issueID, "member", wrongID) {
		t.Fatal("fixture failed: wrong human should start out subscribed")
	}

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "Deploy review: invoice-warnings",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"on_behalf_of_changed": true,
			"on_behalf_of_user_id": rightID,
		},
	})

	if isSubscribed(t, queries, issueID, "member", wrongID) {
		t.Fatal("the previously stamped human must lose the triggered_agent subscription")
	}
	if !isSubscribed(t, queries, issueID, "member", rightID) {
		t.Fatal("the corrected human must be subscribed")
	}
}

// Clearing the stamp removes the auto-added row and adds nothing, so the issue
// falls back to whatever the derived chain says.
func TestSubscriberIssueUpdated_ClearedOnBehalfOfDropsTheSubscription(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, queries)

	email := "on-behalf-of-cleared-human@multica.ai"
	userID := createTestUser(t, email)
	t.Cleanup(func() { cleanupTestUser(t, email) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() { cleanupTestIssue(t, issueID) })

	addSubscriber(bus, queries, testWorkspaceID, issueID, "member", userID, "triggered_agent")

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "Deploy review: invoice-warnings",
				Status:      "todo",
				Priority:    "medium",
				CreatorType: "member",
				CreatorID:   testUserID,
			},
			"on_behalf_of_changed": true,
			"on_behalf_of_user_id": "",
		},
	})

	if isSubscribed(t, queries, issueID, "member", userID) {
		t.Fatal("clearing the stamp must remove the auto-added triggered_agent subscription")
	}
}
