package main

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// countInboxByTypeForIssue counts a member's rows of one type on one issue.
func countInboxByTypeForIssue(t *testing.T, recipientID, issueID, notifType string) (active, archived int) {
	t.Helper()
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FILTER (WHERE NOT archived), count(*) FILTER (WHERE archived)
		FROM inbox_item
		WHERE recipient_type = 'member' AND recipient_id = $1 AND issue_id = $2 AND type = $3
	`, recipientID, issueID, notifType).Scan(&active, &archived); err != nil {
		t.Fatalf("countInboxByTypeForIssue: %v", err)
	}
	return active, archived
}

// seedFinishedTask records a run whose originator was resolved by source.
func seedFinishedTask(t *testing.T, issueID, originatorID, source string) string {
	t.Helper()
	agentID := getAgentID(t)
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id, accountable_user_id, originator_source)
		SELECT id, runtime_id, $2, 'completed', 0, $3, $3, $4 FROM agent WHERE id = $1
		RETURNING id
	`, agentID, issueID, originatorID, source).Scan(&taskID); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func publishTaskCompleted(bus *events.Bus, issueID, taskID string) {
	bus.Publish(events.Event{
		Type:        protocol.EventTaskCompleted,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":  taskID,
			"agent_id": "00000000-0000-0000-0000-aaaaaaaaaaaa",
			"issue_id": issueID,
			"status":   "completed",
		},
	})
}

// A run started by a member's own action comes back to that member; a run
// one agent started for another does not, even with a human accountable.
func TestNotification_TaskCompleted_HandsBackToDirectOriginator(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	directEmail := "notif-turn-direct@multica.ai"
	directID := createNotificationMember(t, directEmail)
	t.Cleanup(func() { cleanupTestUser(t, directEmail) })
	delegatedEmail := "notif-turn-delegated@multica.ai"
	delegatedID := createNotificationMember(t, delegatedEmail)
	t.Cleanup(func() { cleanupTestUser(t, delegatedEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	var inboxEvents []events.Event
	bus.Subscribe(protocol.EventInboxNew, func(e events.Event) {
		inboxEvents = append(inboxEvents, e)
	})

	publishTaskCompleted(bus, issueID, seedFinishedTask(t, issueID, directID, "direct_human"))
	publishTaskCompleted(bus, issueID, seedFinishedTask(t, issueID, delegatedID, "delegation"))

	if active, _ := countInboxByTypeForIssue(t, directID, issueID, "agent_completed"); active != 1 {
		t.Fatalf("direct originator: expected 1 agent_completed row, got %d", active)
	}
	if active, _ := countInboxByTypeForIssue(t, delegatedID, issueID, "agent_completed"); active != 0 {
		t.Fatalf("delegated originator: expected no agent_completed row, got %d", active)
	}
	// The creator hears once per run; the direct originator once.
	if active, _ := countInboxByTypeForIssue(t, testUserID, issueID, "agent_completed"); active != 2 {
		t.Fatalf("creator: expected 2 agent_completed rows, got %d", active)
	}
	if len(inboxEvents) != 3 {
		t.Fatalf("expected 3 inbox:new events, got %d", len(inboxEvents))
	}
}

// A failed attempt that will be retried has not ended the run.
func TestNotification_TaskFailed_RetryPendingIsSilent(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})
	addTestSubscriber(t, issueID, "member", testUserID, "creator")

	for _, retryPending := range []bool{true, false} {
		bus.Publish(events.Event{
			Type:        protocol.EventTaskFailed,
			WorkspaceID: testWorkspaceID,
			ActorType:   "system",
			Payload: map[string]any{
				"task_id":       "00000000-0000-0000-0000-bbbbbbbbbbbb",
				"agent_id":      "00000000-0000-0000-0000-aaaaaaaaaaaa",
				"issue_id":      issueID,
				"retry_pending": retryPending,
			},
		})
	}

	if active, _ := countInboxByTypeForIssue(t, testUserID, issueID, "task_failed"); active != 1 {
		t.Fatalf("expected only the final failure to reach the Inbox, got %d rows", active)
	}
}

func publishComment(bus *events.Bus, issueID, authorType, authorID string) {
	bus.Publish(events.Event{
		Type:        protocol.EventCommentCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   authorType,
		ActorID:     authorID,
		Payload: map[string]any{
			"comment": handler.CommentResponse{
				ID:         "00000000-0000-0000-0000-000000000000",
				IssueID:    issueID,
				AuthorType: authorType,
				AuthorID:   authorID,
				Content:    "reply",
				Type:       "comment",
			},
			"issue_title":  "turn test issue",
			"issue_status": "in_progress",
		},
	})
}

// Replying moves the issue on, so it leaves the replier's Inbox and no one
// else's; an agent's comment leaves every Inbox alone.
func TestNotification_MemberCommentClearsOwnInbox(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	otherEmail := "notif-turn-other@multica.ai"
	otherID := createNotificationMember(t, otherEmail)
	t.Cleanup(func() { cleanupTestUser(t, otherEmail) })

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})
	for _, recipient := range []string{testUserID, otherID} {
		if _, err := testPool.Exec(context.Background(), `
			INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, details)
			VALUES ($1, 'member', $2, 'agent_completed', 'action_required', $3, 'turn', '{}')
		`, testWorkspaceID, recipient, issueID); err != nil {
			t.Fatalf("seed inbox row: %v", err)
		}
	}

	var batchArchived []events.Event
	bus.Subscribe(protocol.EventInboxBatchArchived, func(e events.Event) {
		batchArchived = append(batchArchived, e)
	})

	publishComment(bus, issueID, "agent", "00000000-0000-0000-0000-aaaaaaaaaaaa")
	if active, _ := countInboxByTypeForIssue(t, testUserID, issueID, "agent_completed"); active != 1 {
		t.Fatalf("an agent comment must not clear the Inbox, got %d active", active)
	}

	publishComment(bus, issueID, "member", testUserID)
	if active, archived := countInboxByTypeForIssue(t, testUserID, issueID, "agent_completed"); active != 0 || archived != 1 {
		t.Fatalf("replier: expected the row archived, got active=%d archived=%d", active, archived)
	}
	if active, _ := countInboxByTypeForIssue(t, otherID, issueID, "agent_completed"); active != 1 {
		t.Fatalf("another member's row must stay, got %d active", active)
	}
	if len(batchArchived) != 1 {
		t.Fatalf("expected 1 inbox:batch-archived event, got %d", len(batchArchived))
	}
	payload, _ := batchArchived[0].Payload.(map[string]any)
	if payload["recipient_id"] != testUserID || payload["reason"] != "acted_on_issue" {
		t.Fatalf("unexpected inbox:batch-archived payload %+v", payload)
	}
}

// Changing the status is acting on the issue too.
func TestNotification_MemberStatusChangeClearsOwnInbox(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, details)
		VALUES ($1, 'member', $2, 'mentioned', 'info', $3, 'turn', '{}')
	`, testWorkspaceID, testUserID, issueID); err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}

	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue": handler.IssueResponse{
				ID:          issueID,
				WorkspaceID: testWorkspaceID,
				Title:       "turn test issue",
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

	if active, archived := countInboxByTypeForIssue(t, testUserID, issueID, "mentioned"); active != 0 || archived != 1 {
		t.Fatalf("expected the row archived, got active=%d archived=%d", active, archived)
	}
}
