package main

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestChatIssueListener_PostsStatusMovesIntoThread(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	bus := events.New()
	registerChatIssueListeners(bus, queries)

	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 ORDER BY created_at LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load fixture agent: %v", err)
	}
	var parentID, rootID, threadID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'chat issue listener') RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&parentID); err != nil {
		t.Fatalf("create parent session: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'user', 'do it') RETURNING id
	`, parentID).Scan(&rootID); err != nil {
		t.Fatalf("create root message: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, parent_chat_session_id, root_chat_message_id)
		VALUES ($1, $2, $3, 'do it', $4, $5) RETURNING id
	`, testWorkspaceID, agentID, testUserID, parentID, rootID).Scan(&threadID); err != nil {
		t.Fatalf("create thread session: %v", err)
	}
	issueID := createTestIssue(t, testWorkspaceID, testUserID)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message_issue (issue_id, workspace_id, chat_session_id, chat_message_id, thread_session_id, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, issueID, testWorkspaceID, parentID, rootID, threadID, testUserID); err != nil {
		t.Fatalf("link issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM chat_message_issue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM chat_message WHERE chat_session_id IN ($1, $2)`, parentID, threadID)
		testPool.Exec(ctx, `DELETE FROM chat_session WHERE id IN ($1, $2)`, threadID, parentID)
	})

	publish := func(changed bool) {
		bus.Publish(events.Event{
			Type:        protocol.EventIssueUpdated,
			WorkspaceID: testWorkspaceID,
			ActorType:   "agent",
			ActorID:     agentID,
			Payload: map[string]any{
				"issue": handler.IssueResponse{
					ID:          issueID,
					WorkspaceID: testWorkspaceID,
					Identifier:  "TEST-1",
					Status:      "in_review",
				},
				"status_changed": changed,
				"prev_status":    "in_progress",
			},
		})
	}
	publish(false)
	publish(true)

	var kinds, contents []string
	rows, err := testPool.Query(ctx, `SELECT message_kind, content FROM chat_message WHERE chat_session_id = $1 ORDER BY created_at`, threadID)
	if err != nil {
		t.Fatalf("list thread messages: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind, content string
		if err := rows.Scan(&kind, &content); err != nil {
			t.Fatalf("scan: %v", err)
		}
		kinds = append(kinds, kind)
		contents = append(contents, content)
	}
	if len(contents) != 1 {
		t.Fatalf("expected exactly one event for the one status move, got %v", contents)
	}
	if kinds[0] != protocol.ChatMessageKindIssueEvent || !strings.Contains(contents[0], "from In Progress to **In Review**") {
		t.Fatalf("unexpected event %q (%s)", contents[0], kinds[0])
	}
}
