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

// inboxTaskIDs lists the runs a member's rows of one type on one issue name.
func inboxTaskIDs(t *testing.T, recipientID, issueID, notifType string) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `
		SELECT coalesce(details->>'task_id', '') FROM inbox_item
		WHERE recipient_type = 'member' AND recipient_id = $1 AND issue_id = $2 AND type = $3
		ORDER BY created_at
	`, recipientID, issueID, notifType)
	if err != nil {
		t.Fatalf("inboxTaskIDs: %v", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("inboxTaskIDs scan: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
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

	directTask := seedFinishedTask(t, issueID, directID, "direct_human")
	publishTaskCompleted(bus, issueID, directTask)
	publishTaskCompleted(bus, issueID, seedFinishedTask(t, issueID, delegatedID, "delegation"))

	if active, _ := countInboxByTypeForIssue(t, directID, issueID, "agent_completed"); active != 1 {
		t.Fatalf("direct originator: expected 1 agent_completed row, got %d", active)
	}
	if ids := inboxTaskIDs(t, directID, issueID, "agent_completed"); len(ids) != 1 || ids[0] != directTask {
		t.Fatalf("direct originator: expected the row to name run %s, got %v", directTask, ids)
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
	if ids := inboxTaskIDs(t, testUserID, issueID, "task_failed"); len(ids) != 1 || ids[0] != "00000000-0000-0000-0000-bbbbbbbbbbbb" {
		t.Fatalf("expected the failure row to name its run, got %v", ids)
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

// seedInboxRow puts one active Inbox row on the issue for the member.
func seedInboxRow(t *testing.T, recipientID, issueID, notifType string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO inbox_item (workspace_id, recipient_type, recipient_id, type, severity, issue_id, title, details)
		VALUES ($1, 'member', $2, $4, 'info', $3, 'turn', '{}')
	`, testWorkspaceID, recipientID, issueID, notifType); err != nil {
		t.Fatalf("seed inbox row: %v", err)
	}
}

// Replying is not handling: a member's comment leaves their Inbox alone, and
// so does an agent's. The member archives the issue when they are done.
func TestNotification_MemberCommentKeepsInbox(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})
	seedInboxRow(t, testUserID, issueID, "agent_completed")

	var batchArchived []events.Event
	bus.Subscribe(protocol.EventInboxBatchArchived, func(e events.Event) {
		batchArchived = append(batchArchived, e)
	})

	publishComment(bus, issueID, "agent", "00000000-0000-0000-0000-aaaaaaaaaaaa")
	publishComment(bus, issueID, "member", testUserID)
	if active, _ := countInboxByTypeForIssue(t, testUserID, issueID, "agent_completed"); active != 1 {
		t.Fatalf("a comment must not clear the Inbox, got %d active", active)
	}
	if len(batchArchived) != 0 {
		t.Fatalf("expected no inbox:batch-archived event, got %d", len(batchArchived))
	}
}

// seedStartedTask records a run that just started, with its originator
// resolved by source. Only one task per issue and agent can be pending, so
// the row is stored as already finished.
func seedStartedTask(t *testing.T, issueID, originatorID, source string, retryOf *string) string {
	t.Helper()
	agentID := getAgentID(t)
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, originator_user_id, accountable_user_id, originator_source, retry_of_task_id)
		SELECT id, runtime_id, $2, 'completed', 0, $3, $3, $4, $5 FROM agent WHERE id = $1
		RETURNING id
	`, agentID, issueID, originatorID, source, retryOf).Scan(&taskID); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func publishTaskQueued(bus *events.Bus, issueID, taskID string) {
	bus.Publish(events.Event{
		Type:        protocol.EventTaskQueued,
		WorkspaceID: testWorkspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":  taskID,
			"agent_id": "00000000-0000-0000-0000-aaaaaaaaaaaa",
			"issue_id": issueID,
			"status":   "queued",
		},
	})
}

// Starting an agent run hands the issue over, so it leaves the Inbox of the
// member whose own action started it and no one else's. A delegated run or a
// retry is not the member handing it over.
func TestNotification_RunStartClearsOriginatorInbox(t *testing.T) {
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
	seedInboxRow(t, testUserID, issueID, "mentioned")
	seedInboxRow(t, otherID, issueID, "mentioned")

	var batchArchived []events.Event
	bus.Subscribe(protocol.EventInboxBatchArchived, func(e events.Event) {
		batchArchived = append(batchArchived, e)
	})

	delegated := seedStartedTask(t, issueID, testUserID, "delegation", nil)
	publishTaskQueued(bus, issueID, delegated)
	first := seedStartedTask(t, issueID, otherID, "direct_human", nil)
	publishTaskQueued(bus, issueID, seedStartedTask(t, issueID, testUserID, "direct_human", &first))
	if active, _ := countInboxByTypeForIssue(t, testUserID, issueID, "mentioned"); active != 1 {
		t.Fatalf("a delegated run or a retry must not clear the Inbox, got %d active", active)
	}

	publishTaskQueued(bus, issueID, seedStartedTask(t, issueID, testUserID, "direct_human", nil))
	if active, archived := countInboxByTypeForIssue(t, testUserID, issueID, "mentioned"); active != 0 || archived != 1 {
		t.Fatalf("originator: expected the row archived, got active=%d archived=%d", active, archived)
	}
	if active, _ := countInboxByTypeForIssue(t, otherID, issueID, "mentioned"); active != 1 {
		t.Fatalf("another member's row must stay, got %d active", active)
	}
	if len(batchArchived) != 1 {
		t.Fatalf("expected 1 inbox:batch-archived event, got %d", len(batchArchived))
	}
	payload, _ := batchArchived[0].Payload.(map[string]any)
	if payload["recipient_id"] != testUserID || payload["reason"] != "handed_to_agent" {
		t.Fatalf("unexpected inbox:batch-archived payload %+v", payload)
	}
}

// A done or cancelled issue waits on no one, so it leaves every Inbox; any
// other status change leaves the Inbox to its owner.
func TestNotification_ClosedIssueLeavesEveryInbox(t *testing.T) {
	queries := db.New(testPool)
	bus := newNotificationBus(t, queries)

	otherEmail := "notif-turn-closed@multica.ai"
	otherID := createNotificationMember(t, otherEmail)
	t.Cleanup(func() { cleanupTestUser(t, otherEmail) })

	for _, status := range []string{"done", "cancelled"} {
		issueID := createTestIssue(t, testWorkspaceID, testUserID)
		t.Cleanup(func() {
			cleanupInboxForIssue(t, issueID)
			cleanupTestIssue(t, issueID)
		})
		seedInboxRow(t, testUserID, issueID, "mentioned")
		seedInboxRow(t, otherID, issueID, "mentioned")

		publishStatusChange(bus, issueID, "in_review", "in_progress")
		if active, _ := countInboxByTypeForIssue(t, testUserID, issueID, "mentioned"); active != 1 {
			t.Fatalf("%s: a status change that does not close the issue must keep the row, got %d active", status, active)
		}

		publishStatusChange(bus, issueID, status, "in_review")
		for _, recipient := range []string{testUserID, otherID} {
			if active, archived := countInboxByTypeForIssue(t, recipient, issueID, "mentioned"); active != 0 || archived != 1 {
				t.Fatalf("%s: expected the row archived for %s, got active=%d archived=%d", status, recipient, active, archived)
			}
		}
	}
}
