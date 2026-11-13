package main

import (
	"context"
	"slices"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func issueUnreadRows(t *testing.T, issueID string) map[string][2]string {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT user_id::text, outcome, COALESCE(task_id::text, '') FROM issue_unread WHERE issue_id = $1`, issueID)
	if err != nil {
		t.Fatalf("query issue_unread: %v", err)
	}
	defer rows.Close()
	got := map[string][2]string{}
	for rows.Next() {
		var userID, outcome, taskID string
		if err := rows.Scan(&userID, &outcome, &taskID); err != nil {
			t.Fatalf("scan issue_unread: %v", err)
		}
		got[userID] = [2]string{outcome, taskID}
	}
	return got
}

func TestIssueUnread_RunEndMarksSubscribedMembers(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerIssueUnreadListeners(bus, queries)

	emails := []string{
		"unread-subscriber@multica.ai", "unread-delegated@multica.ai",
		"unread-opted-out@multica.ai", "unread-outsider@multica.ai",
	}
	t.Cleanup(func() {
		for _, email := range emails {
			cleanupTestUser(t, email)
		}
	})
	subscriber := createNotificationMember(t, emails[0])
	delegated := createNotificationMember(t, emails[1])
	optedOut := createNotificationMember(t, emails[2])
	outsider := createTestUser(t, emails[3])

	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue_unread WHERE issue_id = $1`, issueID)
		cleanupTestIssue(t, issueID)
	})
	const agentID = "00000000-0000-4000-8000-0000000000a1"
	addTestSubscriber(t, issueID, "member", subscriber, "manual")
	addTestSubscriber(t, issueID, "member", delegated, "delegated")
	addTestSubscriber(t, issueID, "member", optedOut, "manual")
	addTestSubscriber(t, issueID, "member", outsider, "manual")
	addTestSubscriber(t, issueID, "agent", agentID, "manual")
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue_subscriber SET unsubscribed_at = now() WHERE issue_id = $1 AND user_id = $2`, issueID, optedOut); err != nil {
		t.Fatalf("opt out: %v", err)
	}

	var notified []string
	bus.Subscribe(protocol.EventIssueUnreadChanged, func(e events.Event) {
		payload := e.Payload.(map[string]any)
		notified = append(notified, payload["recipient_id"].(string))
	})
	runEnded := func(eventType, taskID string, extra map[string]any) {
		payload := map[string]any{"issue_id": issueID, "agent_id": agentID, "task_id": taskID}
		for k, v := range extra {
			payload[k] = v
		}
		bus.Publish(events.Event{Type: eventType, WorkspaceID: testWorkspaceID, Payload: payload})
	}

	runEnded(protocol.EventTaskFailed, "00000000-0000-4000-8000-0000000000b1", map[string]any{"retry_pending": true})
	if got := issueUnreadRows(t, issueID); len(got) != 0 {
		t.Fatalf("a failed attempt with a retry pending marked the issue unread: %v", got)
	}

	runEnded(protocol.EventTaskCompleted, "00000000-0000-4000-8000-0000000000b2", nil)
	got := issueUnreadRows(t, issueID)
	if len(got) != 1 || got[subscriber][0] != "completed" {
		t.Fatalf("want only the active member subscriber marked completed, got %v", got)
	}
	if !slices.Equal(notified, []string{subscriber}) {
		t.Fatalf("want issue_unread:changed for the subscriber only, got %v", notified)
	}

	runEnded(protocol.EventTaskFailed, "00000000-0000-4000-8000-0000000000b3", map[string]any{"retry_pending": false})
	if row := issueUnreadRows(t, issueID)[subscriber]; row != [2]string{"failed", "00000000-0000-4000-8000-0000000000b3"} {
		t.Fatalf("a later run should replace the unread row's run details, got %v", row)
	}
}
