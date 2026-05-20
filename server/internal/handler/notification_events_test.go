package handler

// CEREBRO-PATCH(notification-events): verifies durable notification rows for JEH-1805 triggers.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestCreateCommentCreatesMentionNotification(t *testing.T) {
	ctx := context.Background()
	recipientID := createNotificationTestMember(t, "mention-recipient@multica.test")
	issueID := createNotificationTestIssue(t, "Mention notification")
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)

	content := "hello [@Mention Recipient](mention://member/" + recipientID + ") " + longText(260)
	req := withURLParam(newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": content,
	}), "id", issueID)
	w := httptest.NewRecorder()
	testHandler.CreateComment(w, req)
	if w.Code != 201 {
		t.Fatalf("CreateComment: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var row struct {
		Type     string         `json:"type"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := testPool.QueryRow(ctx, `
		SELECT type, metadata
		FROM notifications
		WHERE recipient_type = 'member' AND recipient_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, recipientID).Scan(&row.Type, &row.Metadata); err != nil {
		t.Fatalf("read notification: %v", err)
	}
	if row.Type != "mention" {
		t.Fatalf("type = %q, want mention", row.Type)
	}
	if got := row.Metadata["issue_title"]; got != "Mention notification" {
		t.Fatalf("issue_title = %v", got)
	}
	if excerpt, _ := row.Metadata["comment_excerpt"].(string); len([]rune(excerpt)) > 200 {
		t.Fatalf("comment_excerpt has %d runes, want <= 200", len([]rune(excerpt)))
	}
}

func TestUpdateIssueCreatesStatusAndAssignmentNotifications(t *testing.T) {
	ctx := context.Background()
	subscriberID := createNotificationTestMember(t, "status-subscriber@multica.test")
	assigneeID := createNotificationTestMember(t, "assignee-recipient@multica.test")
	issueID := createNotificationTestIssue(t, "Issue update notifications")
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason)
		VALUES ($1, 'member', $2, 'manual')
		ON CONFLICT DO NOTHING
	`, issueID, subscriberID); err != nil {
		t.Fatalf("seed subscriber: %v", err)
	}

	req := withURLParam(newRequest("PATCH", "/api/issues/"+issueID, map[string]any{
		"status":        "in_progress",
		"assignee_type": "member",
		"assignee_id":   assigneeID,
	}), "id", issueID)
	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, req)
	if w.Code != 200 {
		t.Fatalf("UpdateIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var statusMeta []byte
	if err := testPool.QueryRow(ctx, `
		SELECT metadata
		FROM notifications
		WHERE recipient_id = $1 AND type = 'issue.status_changed'
		ORDER BY created_at DESC
		LIMIT 1
	`, subscriberID).Scan(&statusMeta); err != nil {
		t.Fatalf("read status notification: %v", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(statusMeta, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata["status_fra"] != "todo" || metadata["status_til"] != "in_progress" {
		t.Fatalf("status metadata = %+v", metadata)
	}
	var assignedCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM notifications
		WHERE recipient_id = $1 AND recipient_type = 'member' AND type = 'issue.assigned'
	`, assigneeID).Scan(&assignedCount); err != nil {
		t.Fatalf("count assignment notifications: %v", err)
	}
	if assignedCount != 1 {
		t.Fatalf("assignment notifications = %d, want 1", assignedCount)
	}
}

func TestCompleteTaskCreatesRunNotificationForAgentAndSubscribers(t *testing.T) {
	ctx := context.Background()
	subscriberID := createNotificationTestMember(t, "run-subscriber@multica.test")
	issueID := createNotificationTestIssue(t, "Run notification")
	defer testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)

	var agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("find agent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO issue_subscriber (issue_id, user_type, user_id, reason)
		VALUES ($1, 'member', $2, 'manual')
		ON CONFLICT DO NOTHING
	`, issueID, subscriberID); err != nil {
		t.Fatalf("seed subscriber: %v", err)
	}
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
		VALUES ($1, $2, $3, 'running', 0, now())
		RETURNING id
	`, agentID, testRuntimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("seed task: %v", err)
	}

	_, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(taskID), []byte(`{"output":"done"}`), "", "")
	if err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	var count int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM notifications
		WHERE reference_id = $1 AND type = 'agent.run_completed'
	`, taskID).Scan(&count); err != nil {
		t.Fatalf("count run notifications: %v", err)
	}
	if count != 2 {
		t.Fatalf("run notifications = %d, want 2", count)
	}
}

func createNotificationTestIssue(t *testing.T, title string) string {
	t.Helper()
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, position, number)
		VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, 0)
		RETURNING id
	`, testWorkspaceID, title, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	return issueID
}

func createNotificationTestMember(t *testing.T, email string) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Notification Recipient', $1)
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, email).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
		ON CONFLICT (workspace_id, user_id) DO NOTHING
	`, testWorkspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	return userID
}

func longText(n int) string {
	runes := make([]rune, n)
	for i := range runes {
		runes[i] = 'x'
	}
	return string(runes)
}
