package lark

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestProjectSyncActivityNotificationsAreCompleteAndOrdered(t *testing.T) {
	pool := channelScopeTestDB(t)
	ctx := context.Background()

	var eventOrderPresent bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'channel_notification_outbox'
			  AND column_name = 'event_order'
		)`).Scan(&eventOrderPresent); err != nil {
		t.Fatalf("check activity notification schema: %v", err)
	}
	if !eventOrderPresent {
		t.Skip("activity notification migration not applied")
	}

	const (
		workspaceID    = "25310000-0000-4000-8000-000000000001"
		userID         = "25310000-0000-4000-8000-000000000002"
		runtimeID      = "25310000-0000-4000-8000-000000000003"
		agentID        = "25310000-0000-4000-8000-000000000004"
		installationID = "25310000-0000-4000-8000-000000000005"
		issueID        = "25310000-0000-4000-8000-000000000006"
		topicID        = "25310000-0000-4000-8000-000000000007"
		commentID      = "25310000-0000-4000-8000-000000000008"
		taskID         = "25310000-0000-4000-8000-000000000009"
	)

	clean := func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_notification_outbox WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_issue_topic_binding WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM channel_installation WHERE workspace_id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	}
	clean()
	t.Cleanup(clean)

	seed := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatalf("seed activity notification test: %v", err)
		}
	}

	seed(`INSERT INTO workspace (id, name, slug) VALUES ($1, 'activity notification test', 'activity-notification-test')`, workspaceID)
	seed(`INSERT INTO "user" (id, name, email) VALUES ($1, 'Activity User', 'activity-notification@example.test')`, userID)
	seed(`
		INSERT INTO agent_runtime (id, workspace_id, name, runtime_mode, provider, owner_id)
		VALUES ($1, $2, 'Activity Runtime', 'local', 'codex', $3)`,
		runtimeID, workspaceID, userID,
	)
	seed(`
		INSERT INTO agent (id, workspace_id, name, runtime_mode, runtime_id, owner_id)
		VALUES ($1, $2, 'Activity Agent', 'local', $3, $4)`,
		agentID, workspaceID, runtimeID, userID,
	)
	seed(`
		INSERT INTO channel_installation (
			id, workspace_id, agent_id, channel_type, config, installer_user_id
		) VALUES ($1, $2, $3, 'feishu', '{"app_id":"activity-notification-test"}', $4)`,
		installationID, workspaceID, agentID, userID,
	)
	seed(`
		INSERT INTO issue (
			id, workspace_id, number, title, status, priority, creator_type, creator_id
		) VALUES ($1, $2, 7, 'Activity events', 'todo', 'none', 'member', $3)`,
		issueID, workspaceID, userID,
	)
	seed(`
		INSERT INTO channel_issue_topic_binding (
			id, workspace_id, installation_id, issue_id, channel_chat_id,
			topic_root_message_id, binding_source, created_by_user_id
		) VALUES ($1, $2, $3, $4, 'oc_activity', 'om_activity', 'manual_topic_bind', $5)`,
		topicID, workspaceID, installationID, issueID, userID,
	)

	seed(`
		UPDATE issue
		SET status = 'in_progress',
		    assignee_type = 'agent',
		    assignee_id = $1,
		    priority = 'high',
		    metadata = '{"blocked_reason":"Waiting for review"}'
		WHERE id = $2`,
		agentID, issueID,
	)
	seed(`
		INSERT INTO comment (
			id, workspace_id, issue_id, author_type, author_id, content
		) VALUES ($1, $2, $3, 'agent', $4, 'Initial result')`,
		commentID, workspaceID, issueID, agentID,
	)
	seed(`UPDATE comment SET content = 'Updated result' WHERE id = $1`, commentID)
	seed(`
		INSERT INTO agent_task_queue (
			id, agent_id, runtime_id, issue_id, status,
			originator_user_id, accountable_user_id
		) VALUES ($1, $2, $3, $4, 'queued', $5, $5)`,
		taskID, agentID, runtimeID, issueID, userID,
	)
	seed(`UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, taskID)
	seed(`
		UPDATE agent_task_queue
		SET status = 'completed',
		    completed_at = now(),
		    result = '{"task_id":"25310000-0000-4000-8000-000000000009","output":"All checks passed","pr_url":"https://example.test/pr/1"}'
		WHERE id = $1`,
		taskID,
	)

	expected := []string{
		"issue_status_changed",
		"assignee_changed",
		"priority_changed",
		"blocked_reason_changed",
		"comment_created",
		"comment_updated",
		"task_started",
		"task_completed",
		"task_result",
	}

	rows, err := pool.Query(ctx, `
		SELECT event_type, project_id, project_binding_id,
		       issue_topic_binding_id, payload
		FROM channel_notification_outbox
		WHERE workspace_id = $1 AND issue_id = $2
		ORDER BY event_order`,
		workspaceID, issueID,
	)
	if err != nil {
		t.Fatalf("list activity notifications: %v", err)
	}
	defer rows.Close()

	var got []string
	payloads := make(map[string]json.RawMessage, len(expected))
	for rows.Next() {
		var (
			eventType        string
			projectID        pgtype.UUID
			projectBindingID pgtype.UUID
			topicBindingID   pgtype.UUID
			payload          []byte
		)
		if err := rows.Scan(&eventType, &projectID, &projectBindingID, &topicBindingID, &payload); err != nil {
			t.Fatalf("scan activity notification: %v", err)
		}
		if projectID.Valid || projectBindingID.Valid {
			t.Fatalf("direct activity notification unexpectedly carries Project route")
		}
		if topicBindingID != util.MustParseUUID(topicID) {
			t.Fatalf("activity notification topic = %v, want %s", topicBindingID, topicID)
		}
		var decoded map[string]any
		if err := json.Unmarshal(payload, &decoded); err != nil {
			t.Fatalf("decode %s payload: %v", eventType, err)
		}
		if decoded["issue_id"] != issueID {
			t.Fatalf("%s payload issue_id = %#v, want %s", eventType, decoded["issue_id"], issueID)
		}
		payloads[eventType] = append(json.RawMessage(nil), payload...)
		got = append(got, eventType)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate activity notifications: %v", err)
	}
	if len(got) != len(expected) {
		t.Fatalf("activity notification count = %d (%v), want %d (%v)", len(got), got, len(expected), expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("activity notification order[%d] = %q, want %q (all=%v)", i, got[i], expected[i], got)
		}
	}

	queries := db.New(pool)
	issue, err := queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
		ID:          util.MustParseUUID(issueID),
		WorkspaceID: util.MustParseUUID(workspaceID),
	})
	if err != nil {
		t.Fatalf("load activity Issue for rendering: %v", err)
	}
	store := newProjectSyncStore(pool)
	worker := NewProjectIssueSyncWorker(&ProjectSyncService{
		queries: queries,
		store:   store,
	}, "activity-render-test")
	rendered := make(map[string]string, len(expected))
	for _, eventType := range expected {
		var payload projectNotificationPayload
		if err := json.Unmarshal(payloads[eventType], &payload); err != nil {
			t.Fatalf("decode %s payload for rendering: %v", eventType, err)
		}
		text, err := worker.renderNotification(ctx, ChannelNotificationOutbox{
			EventType: eventType,
			TaskID:    util.MustParseUUID(taskID),
		}, issue, payload)
		if err != nil {
			t.Fatalf("render %s notification: %v", eventType, err)
		}
		if strings.TrimSpace(text) == "" {
			t.Fatalf("render %s notification returned empty text", eventType)
		}
		rendered[eventType] = text
	}
	if !strings.Contains(rendered["comment_updated"], "Updated result") {
		t.Fatalf("comment_updated text = %q, want updated content", rendered["comment_updated"])
	}
	if !strings.Contains(rendered["task_result"], "All checks passed") {
		t.Fatalf("task_result text = %q, want task output", rendered["task_result"])
	}
	if !strings.Contains(rendered["assignee_changed"], "Activity Agent") {
		t.Fatalf("assignee_changed text = %q, want resolved Agent name", rendered["assignee_changed"])
	}

	for i, want := range expected {
		items, err := store.claimNotifications(ctx, "activity-order-test", time.Now().Add(-time.Minute), 25)
		if err != nil {
			t.Fatalf("claim activity notification %d: %v", i, err)
		}
		if len(items) != 1 {
			t.Fatalf("claim activity notification %d returned %d items, want 1", i, len(items))
		}
		if items[0].EventType != want {
			t.Fatalf("claimed activity notification %d = %q, want %q", i, items[0].EventType, want)
		}
		if err := store.markNotificationSent(ctx, items[0].ID, "activity-order-test"); err != nil {
			t.Fatalf("mark activity notification %d sent: %v", i, err)
		}
	}

	items, err := store.claimNotifications(ctx, "activity-order-test", time.Now().Add(-time.Minute), 25)
	if err != nil {
		t.Fatalf("claim after activity notifications drained: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("claim after activity notifications drained returned %d items", len(items))
	}
}
