package main

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func createAssignmentListenerTestSquad(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	var leaderID string
	if err := testPool.QueryRow(ctx, `
		SELECT id::text FROM agent
		WHERE workspace_id = $1
		ORDER BY created_at ASC
		LIMIT 1
	`, testWorkspaceID).Scan(&leaderID); err != nil {
		t.Fatalf("load squad leader: %v", err)
	}

	var squadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, 'Squad assignee listener test', '', $2, $3)
		RETURNING id
	`, testWorkspaceID, leaderID, testUserID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM squad WHERE id = $1`, squadID); err != nil {
			t.Errorf("cleanup squad: %v", err)
		}
	})
	return squadID
}

// A squad is a routing object, not a subscriber or inbox recipient. Both
// issue event paths must stop before attempting writes that the database
// constrains to member/agent identities. The log assertion is load-bearing:
// checking only for zero squad rows would also pass in the broken version
// because PostgreSQL rejects those rows before the listeners log and return.
func TestSquadAssigneeListenersSkipUnsupportedRecipientWrites(t *testing.T) {
	queries := db.New(testPool)
	bus := events.New()
	registerSubscriberListeners(bus, testPool)
	registerNotificationListeners(bus, queries)

	squadID := createAssignmentListenerTestSquad(t)
	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		cleanupInboxForIssue(t, issueID)
		cleanupTestIssue(t, issueID)
	})

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelError})))
	defer slog.SetDefault(previousLogger)

	assigneeType := "squad"
	issue := handler.IssueResponse{
		ID:           issueID,
		WorkspaceID:  testWorkspaceID,
		Title:        "squad-assigned issue",
		Status:       "todo",
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    testUserID,
		AssigneeType: &assigneeType,
		AssigneeID:   &squadID,
	}

	bus.Publish(events.Event{
		Type:        protocol.EventIssueCreated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload:     map[string]any{"issue": issue},
	})
	bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: testWorkspaceID,
		ActorType:   "member",
		ActorID:     testUserID,
		Payload: map[string]any{
			"issue":            issue,
			"assignee_changed": true,
		},
	})

	// slog's default logger is process-global. Scope the captured records to
	// this test's unique issue so unrelated background errors cannot fail it.
	var issueLogLines []string
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, "issue_id="+issueID) {
			issueLogLines = append(issueLogLines, line)
		}
	}
	issueLogs := strings.Join(issueLogLines, "\n")
	for _, unexpected := range []string{
		"failed to add issue subscriber",
		"direct notification creation failed",
		"SQLSTATE 23514",
	} {
		if strings.Contains(issueLogs, unexpected) {
			t.Fatalf("squad assignment attempted an unsupported recipient write (%q):\n%s", unexpected, issueLogs)
		}
	}

	if count := subscriberCount(t, queries, issueID); count != 1 {
		t.Fatalf("subscriber count = %d, want creator only", count)
	}
}
