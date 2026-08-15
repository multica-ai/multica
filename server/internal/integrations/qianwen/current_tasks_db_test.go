package qianwen

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

const qianwenCurrentTasksRawSecret = "QIANWEN_CURRENT_TASK_RAW_DATA_MUST_NOT_LEAK"

// TestListCurrentTasksUsesBoundMemberViewAndSafeProjection is the PostgreSQL
// tracer for the caller-relative task list. The installation belongs to A, but
// the signed Qianwen identity is bound to regular member B. B must therefore see
// current work through B's VIEW permissions, never through installer A's owner
// privileges, and a chat task remains private to its chat-session creator even
// when the underlying agent is shared.
func TestListCurrentTasksUsesBoundMemberViewAndSafeProjection(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Date(2026, time.August, 15, 9, 30, 0, 0, time.UTC)
	fixture.service.now = func() time.Time { return now }
	boundOpenUserID := "opaque-current-tasks-bound-b"
	boundOpenUUID := "opaque-current-tasks-device-b"
	boundUserID := util.MustParseUUID(uuid.NewString())
	otherUserID := util.MustParseUUID(uuid.NewString())
	registerQianwenCurrentTasksCleanup(t, fixture, boundUserID, otherUserID)

	for _, user := range []struct {
		id    pgtype.UUID
		label string
	}{
		{id: boundUserID, label: "Bound B"},
		{id: otherUserID, label: "Other C"},
	} {
		if _, err := fixture.pool.Exec(ctx, `
			INSERT INTO "user" (id, name, email)
			VALUES ($1, $2, $3)
		`, user.id, "Qianwen Current Tasks "+user.label, "qianwen-current-tasks-"+uuid.NewString()+"@multica.test"); err != nil {
			t.Fatalf("seed %s user: %v", user.label, err)
		}
		if _, err := fixture.pool.Exec(ctx, `
			INSERT INTO member (workspace_id, user_id, role)
			VALUES ($1, $2, 'member')
		`, fixture.workspaceID, user.id); err != nil {
			t.Fatalf("seed %s membership: %v", user.label, err)
		}
	}
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO channel_user_binding (
			workspace_id, multica_user_id, installation_id,
			channel_type, channel_user_id, config
		) VALUES (
			$1, $2, $3, 'qianwen', $4,
			jsonb_build_object('open_uuid', $5::text, 'identity_scope', 'skill')
		)
	`, fixture.workspaceID, boundUserID, fixture.installation.Installation.ID, boundOpenUserID, boundOpenUUID); err != nil {
		t.Fatalf("bind Qianwen identity to member B: %v", err)
	}

	if _, err := fixture.pool.Exec(ctx, `
		UPDATE agent
		SET name = 'Shared Qianwen Agent', permission_mode = 'public_to', visibility = 'workspace'
		WHERE id = $1
	`, fixture.agentID); err != nil {
		t.Fatalf("make installed agent shared: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
		VALUES ($1, 'workspace', $2, $3)
		ON CONFLICT (agent_id, target_type, target_id) DO NOTHING
	`, fixture.agentID, fixture.workspaceID, fixture.userID); err != nil {
		t.Fatalf("grant workspace access to shared agent: %v", err)
	}

	privateAgentID := uuid.NewString()
	otherTargetAgentID := uuid.NewString()
	teamTargetAgentID := uuid.NewString()
	systemAgentID := uuid.NewString()
	archivedAgentID := uuid.NewString()
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO agent (
			id, workspace_id, name, runtime_mode, runtime_config, runtime_id,
			visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args, kind, system_key
		) VALUES
			($1, $5, 'Private A Agent', 'cloud', '{}'::jsonb, $6,
			 'private', 'private', 1, $7, '', '{}'::jsonb, '[]'::jsonb, 'user', NULL),
			($2, $5, 'Only C Agent', 'cloud', '{}'::jsonb, $6,
			 'private', 'public_to', 1, $7, '', '{}'::jsonb, '[]'::jsonb, 'user', NULL),
			($3, $5, 'Team Target Agent', 'cloud', '{}'::jsonb, $6,
			 'private', 'public_to', 1, $7, '', '{}'::jsonb, '[]'::jsonb, 'user', NULL),
			($4, $5, 'Hidden System Agent', 'cloud', '{}'::jsonb, $6,
			 'workspace', 'public_to', 1, $7, '', '{}'::jsonb, '[]'::jsonb, 'system', $8),
			($9, $5, 'Archived Shared Agent', 'cloud', '{}'::jsonb, $6,
			 'workspace', 'public_to', 1, $7, '', '{}'::jsonb, '[]'::jsonb, 'user', NULL)
	`, privateAgentID, otherTargetAgentID, teamTargetAgentID, systemAgentID,
		fixture.workspaceID, fixture.runtimeID, fixture.userID, "qianwen-current-tasks:"+uuid.NewString(), archivedAgentID); err != nil {
		t.Fatalf("seed visibility-matrix agents: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `UPDATE agent SET archived_at = now(), archived_by = $2 WHERE id = $1`, archivedAgentID, fixture.userID); err != nil {
		t.Fatalf("archive shared test agent: %v", err)
	}
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id, created_by)
		VALUES
			($1, 'member', $2, $4),
			($3, 'team', $5, $4),
			($6, 'workspace', $7, $4),
			($8, 'workspace', $7, $4)
	`, otherTargetAgentID, otherUserID, teamTargetAgentID, fixture.userID,
		uuid.NewString(), systemAgentID, fixture.workspaceID, archivedAgentID); err != nil {
		t.Fatalf("seed non-viewable invocation targets: %v", err)
	}

	issueID := uuid.NewString()
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO issue (
			id, workspace_id, title, status, priority,
			creator_type, creator_id, number, position
		) VALUES ($1, $2, 'Installer A workspace issue', 'in_progress', 'medium', 'member', $3, 1, 0)
	`, issueID, fixture.workspaceID, fixture.userID); err != nil {
		t.Fatalf("seed shared issue: %v", err)
	}

	boundChatID := uuid.NewString()
	otherChatID := uuid.NewString()
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO chat_session (id, workspace_id, agent_id, creator_id, title, runtime_id)
		VALUES
			($1, $3, $4, $5, 'Bound B private chat', $7),
			($2, $3, $4, $6, 'Other C private chat', $7)
	`, boundChatID, otherChatID, fixture.workspaceID, fixture.agentID,
		boundUserID, otherUserID, fixture.runtimeID); err != nil {
		t.Fatalf("seed shared-agent chat sessions: %v", err)
	}

	visibleIssueTaskID := uuid.NewString()
	visibleBoundChatTaskID := uuid.NewString()
	hiddenOtherChatTaskID := uuid.NewString()
	hiddenPrivateTaskID := uuid.NewString()
	hiddenOtherTargetTaskID := uuid.NewString()
	hiddenTeamTaskID := uuid.NewString()
	hiddenSystemTaskID := uuid.NewString()
	hiddenArchivedTaskID := uuid.NewString()
	hiddenTerminalTaskID := uuid.NewString()
	hiddenQuickActionTaskID := uuid.NewString()
	seedQianwenCurrentTask(t, ctx, fixture, visibleIssueTaskID, fixture.agentID, issueID, nil,
		"deferred", fixture.userID, nil, nil, now.Add(-10*time.Minute))
	startedAt := now.Add(-4 * time.Minute)
	seedQianwenCurrentTask(t, ctx, fixture, visibleBoundChatTaskID, fixture.agentID, nil, boundChatID,
		"waiting_local_directory", boundUserID, nil, &startedAt, now.Add(-5*time.Minute))
	seedQianwenCurrentTask(t, ctx, fixture, hiddenOtherChatTaskID, fixture.agentID, nil, otherChatID,
		"running", otherUserID, nil, &startedAt, now.Add(-4*time.Minute))
	seedQianwenCurrentTask(t, ctx, fixture, hiddenPrivateTaskID, privateAgentID, issueID, nil,
		"queued", fixture.userID, nil, nil, now.Add(-3*time.Minute))
	seedQianwenCurrentTask(t, ctx, fixture, hiddenOtherTargetTaskID, otherTargetAgentID, issueID, nil,
		"dispatched", fixture.userID, nil, nil, now.Add(-2*time.Minute))
	seedQianwenCurrentTask(t, ctx, fixture, hiddenTeamTaskID, teamTargetAgentID, issueID, nil,
		"running", fixture.userID, nil, &startedAt, now.Add(-90*time.Second))
	seedQianwenCurrentTask(t, ctx, fixture, hiddenSystemTaskID, systemAgentID, issueID, nil,
		"running", fixture.userID, nil, &startedAt, now.Add(-80*time.Second))
	seedQianwenCurrentTask(t, ctx, fixture, hiddenArchivedTaskID, archivedAgentID, issueID, nil,
		"running", fixture.userID, nil, &startedAt, now.Add(-75*time.Second))
	seedQianwenCurrentTask(t, ctx, fixture, hiddenTerminalTaskID, fixture.agentID, issueID, nil,
		"completed", boundUserID, nil, &startedAt, now.Add(-70*time.Second))
	seedQianwenCurrentTask(t, ctx, fixture, hiddenQuickActionTaskID, fixture.agentID, nil, nil,
		"queued", boundUserID, uuid.NewString(), nil, now.Add(-60*time.Second))

	invocation := independentlySignedCurrentTaskListInvocation(
		fixture.installation.AccessToken,
		boundOpenUserID,
		boundOpenUUID,
		now,
	)
	var got CurrentTaskList
	var err error
	got, err = fixture.service.ListCurrentTasks(
		ctx,
		fixture.installation.ConnectionID,
		fixture.installation.AccessToken,
		invocation,
	)
	if err != nil {
		t.Fatalf("ListCurrentTasks() error = %v", err)
	}
	var _ []CurrentTaskSummary = got.Tasks
	if got.HasMore || got.NextCursor != "" {
		t.Fatalf("single-page result has_more=%v next_cursor=%q, want false/empty", got.HasMore, got.NextCursor)
	}
	if len(got.Tasks) != 2 {
		t.Fatalf("visible current tasks = %d, want exactly B's chat plus A's VIEW-visible issue", len(got.Tasks))
	}

	payload, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal safe task-list projection: %v", err)
	}
	if strings.Contains(string(payload), qianwenCurrentTasksRawSecret) {
		t.Fatalf("task list leaked raw task data: %s", payload)
	}
	assertQianwenCurrentTaskProjection(t, payload, map[string]struct {
		title     string
		source    string
		agentName string
		status    string
		started   bool
	}{
		visibleIssueTaskID: {
			title:     "Installer A workspace issue",
			source:    "issue",
			agentName: "Shared Qianwen Agent",
			status:    "queued",
		},
		visibleBoundChatTaskID: {
			title:     "Bound B private chat",
			source:    "chat",
			agentName: "Shared Qianwen Agent",
			status:    "running",
			started:   true,
		},
	})
}

func TestListCurrentTasksPaginatesQianwenRequestsWithoutLosingRequestIDs(t *testing.T) {
	fixture := newQianwenServiceDBFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	now := time.Date(2026, time.August, 15, 10, 0, 0, 0, time.UTC)
	fixture.service.now = func() time.Time { return now }
	wantRequestIDs := make(map[string]bool, 3)
	for i := 0; i < 3; i++ {
		requestID := uuid.NewString()
		wantRequestIDs[requestID] = true
		if _, err := fixture.submit(ctx, requestID, fmt.Sprintf("inspect build shard %d", i+1)); err != nil {
			t.Fatalf("Submit(request %d): %v", i+1, err)
		}
	}

	pageOneInvocation := independentlySignedCurrentTaskListInvocationWithRequest(
		fixture.installation.AccessToken,
		qianwenDBOpenUserID,
		qianwenDBOpenUUID,
		now,
		TaskListRequest{Limit: 2},
	)
	pageOne, err := fixture.service.ListCurrentTasks(
		ctx,
		fixture.installation.ConnectionID,
		fixture.installation.AccessToken,
		pageOneInvocation,
	)
	if err != nil {
		t.Fatalf("ListCurrentTasks(first page): %v", err)
	}
	if len(pageOne.Tasks) != 2 || !pageOne.HasMore || pageOne.NextCursor == "" {
		t.Fatalf("first page = %+v, want 2 tasks, has_more, and cursor", pageOne)
	}

	pageTwoInvocation := independentlySignedCurrentTaskListInvocationWithRequest(
		fixture.installation.AccessToken,
		qianwenDBOpenUserID,
		qianwenDBOpenUUID,
		now,
		TaskListRequest{Limit: 2, Cursor: pageOne.NextCursor},
	)
	pageTwo, err := fixture.service.ListCurrentTasks(
		ctx,
		fixture.installation.ConnectionID,
		fixture.installation.AccessToken,
		pageTwoInvocation,
	)
	if err != nil {
		t.Fatalf("ListCurrentTasks(second page): %v", err)
	}
	if len(pageTwo.Tasks) != 1 || pageTwo.HasMore || pageTwo.NextCursor != "" {
		t.Fatalf("second page = %+v, want final single task", pageTwo)
	}

	seenTaskIDs := make(map[string]bool, 3)
	for _, task := range append(pageOne.Tasks, pageTwo.Tasks...) {
		if seenTaskIDs[task.TaskID] {
			t.Fatalf("task %s repeated across cursor pages", task.TaskID)
		}
		seenTaskIDs[task.TaskID] = true
		if !wantRequestIDs[task.RequestID] {
			t.Fatalf("task %s request_id=%q is absent or outside this installation", task.TaskID, task.RequestID)
		}
		delete(wantRequestIDs, task.RequestID)
	}
	if len(wantRequestIDs) != 0 {
		t.Fatalf("request ids missing across pages: %v", wantRequestIDs)
	}
}

func independentlySignedCurrentTaskListInvocation(token, openUserID, openUUID string, now time.Time) TaskListInvocation {
	return independentlySignedCurrentTaskListInvocationWithRequest(token, openUserID, openUUID, now, TaskListRequest{Limit: 20})
}

func independentlySignedCurrentTaskListInvocationWithRequest(token, openUserID, openUUID string, now time.Time, request TaskListRequest) TaskListInvocation {
	invocation := TaskListInvocation{
		Request: request,
		Identity: InvocationMetadata{
			OpenUserID: openUserID,
			OpenUUID:   openUUID,
			Timestamp:  fmt.Sprint(now.UnixMilli()),
			Nonce:      "0123456789abcdef0123456789abcdef",
		},
	}
	// This is deliberately independent of CanonicalTaskListInvocation: a
	// production canonicalization regression must not make its own DB tracer pass.
	cursor := invocation.Request.Cursor
	if cursor == "" {
		cursor = "-"
	}
	canonical := strings.Join([]string{
		"QIANWEN-HMAC-SHA256-V1",
		"task_list",
		invocation.Identity.Timestamp,
		invocation.Identity.Nonce,
		invocation.Identity.OpenUserID,
		invocation.Identity.OpenUUID,
		fmt.Sprint(invocation.Request.Limit),
		cursor,
	}, "\n")
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = mac.Write([]byte(canonical))
	invocation.Identity.Signature = hex.EncodeToString(mac.Sum(nil))
	return invocation
}

func seedQianwenCurrentTask(
	t *testing.T,
	ctx context.Context,
	fixture *qianwenServiceDBFixture,
	taskID string,
	agentID any,
	issueID any,
	chatSessionID any,
	status string,
	originatorID any,
	regenerateFor any,
	startedAt *time.Time,
	createdAt time.Time,
) {
	t.Helper()
	if _, err := fixture.pool.Exec(ctx, `
		INSERT INTO agent_task_queue (
			id, agent_id, runtime_id, issue_id, chat_session_id,
			status, priority, context, result, error, work_dir,
			originator_user_id, accountable_user_id, originator_source,
			regenerate_quick_actions_for, created_at, started_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, 0,
			jsonb_build_object('private_prompt', $7::text),
			jsonb_build_object('private_result', $7::text),
			$7, $7,
			$8, $8, 'direct_human',
			$9, $10, $11
		)
	`, taskID, agentID, fixture.runtimeID, issueID, chatSessionID, status,
		qianwenCurrentTasksRawSecret, originatorID, regenerateFor, createdAt, startedAt); err != nil {
		t.Fatalf("seed current task %s: %v", taskID, err)
	}
}

func registerQianwenCurrentTasksCleanup(t *testing.T, fixture *qianwenServiceDBFixture, userIDs ...pgtype.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		statements := []struct {
			name  string
			query string
			args  []any
		}{
			{"tasks", `DELETE FROM agent_task_queue WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = $1)`, []any{fixture.workspaceID}},
			{"chat sessions", `DELETE FROM chat_session WHERE workspace_id = $1`, []any{fixture.workspaceID}},
			{"issues", `DELETE FROM issue WHERE workspace_id = $1`, []any{fixture.workspaceID}},
			{"invocation targets", `DELETE FROM agent_invocation_target WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = $1)`, []any{fixture.workspaceID}},
			{"matrix agents", `DELETE FROM agent WHERE workspace_id = $1 AND id <> $2`, []any{fixture.workspaceID, fixture.agentID}},
			{"bound identities", `DELETE FROM channel_user_binding WHERE installation_id = $1 AND multica_user_id = ANY($2::uuid[])`, []any{fixture.installation.Installation.ID, userIDs}},
			{"members", `DELETE FROM member WHERE workspace_id = $1 AND user_id = ANY($2::uuid[])`, []any{fixture.workspaceID, userIDs}},
			{"users", `DELETE FROM "user" WHERE id = ANY($1::uuid[])`, []any{userIDs}},
		}
		for _, statement := range statements {
			if _, err := fixture.pool.Exec(ctx, statement.query, statement.args...); err != nil {
				t.Errorf("cleanup Qianwen current-tasks %s: %v", statement.name, err)
			}
		}
	})
}

func assertQianwenCurrentTaskProjection(
	t *testing.T,
	payload []byte,
	want map[string]struct {
		title     string
		source    string
		agentName string
		status    string
		started   bool
	},
) {
	t.Helper()
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatalf("decode task-list envelope: %v", err)
	}
	for key := range envelope {
		if key != "tasks" && key != "has_more" && key != "next_cursor" {
			t.Fatalf("unsafe top-level task-list field %q in %s", key, payload)
		}
	}
	var tasks []map[string]json.RawMessage
	if err := json.Unmarshal(envelope["tasks"], &tasks); err != nil {
		t.Fatalf("decode task-list summaries: %v", err)
	}
	allowed := map[string]bool{
		"task_id": true, "request_id": true, "display_title": true,
		"source": true, "agent_name": true, "status": true,
		"created_at": true, "started_at": true,
	}
	seen := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		for key := range task {
			if !allowed[key] {
				t.Fatalf("unsafe task-summary field %q in %s", key, payload)
			}
		}
		taskID := qianwenCurrentTaskJSONText(t, task, "task_id")
		expected, ok := want[taskID]
		if !ok {
			t.Fatalf("unexpected or unauthorized task %q in %s", taskID, payload)
		}
		seen[taskID] = true
		if got := qianwenCurrentTaskJSONText(t, task, "display_title"); got != expected.title {
			t.Errorf("task %s display_title=%q, want %q", taskID, got, expected.title)
		}
		if got := qianwenCurrentTaskJSONText(t, task, "source"); got != expected.source {
			t.Errorf("task %s source=%q, want %q", taskID, got, expected.source)
		}
		if got := qianwenCurrentTaskJSONText(t, task, "agent_name"); got != expected.agentName {
			t.Errorf("task %s agent_name=%q, want %q", taskID, got, expected.agentName)
		}
		if got := qianwenCurrentTaskJSONText(t, task, "status"); got != expected.status {
			t.Errorf("task %s status=%q, want %q", taskID, got, expected.status)
		}
		if qianwenCurrentTaskJSONText(t, task, "created_at") == "" {
			t.Errorf("task %s has empty created_at", taskID)
		}
		startedRaw, hasStarted := task["started_at"]
		started := hasStarted && string(startedRaw) != "null" && qianwenCurrentTaskJSONText(t, task, "started_at") != ""
		if started != expected.started {
			t.Errorf("task %s started_at presence=%v, want %v", taskID, started, expected.started)
		}
		if requestRaw, ok := task["request_id"]; ok && string(requestRaw) != "null" && string(requestRaw) != `""` {
			t.Errorf("direct DB task %s unexpectedly acquired request_id=%s", taskID, requestRaw)
		}
	}
	for taskID := range want {
		if !seen[taskID] {
			t.Errorf("expected visible task %s is absent from %s", taskID, payload)
		}
	}
}

func qianwenCurrentTaskJSONText(t *testing.T, task map[string]json.RawMessage, key string) string {
	t.Helper()
	raw, ok := task[key]
	if !ok {
		t.Fatalf("task summary missing required safe field %q", key)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode task-summary field %q: %v", key, err)
	}
	return value
}
