package service

import (
	"context"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestTaskFailedFieldsTerminalFailure(t *testing.T) {
	secret := "sk-" + strings.Repeat("a", 24)
	fields := taskFailedFields("provider rejected "+secret, "agent_error", false)

	if fields["failure_reason"] != "agent_error" {
		t.Fatalf("failure_reason = %v, want agent_error", fields["failure_reason"])
	}
	if fields["retry_pending"] != false {
		t.Fatalf("retry_pending = %v, want false", fields["retry_pending"])
	}
	errorText, ok := fields["error"].(string)
	if !ok || errorText == "" {
		t.Fatal("terminal failure must include deliverable error text")
	}
	if strings.Contains(errorText, secret) {
		t.Fatal("terminal failure payload leaked an API key")
	}
	if !strings.Contains(errorText, "[REDACTED API KEY]") {
		t.Fatalf("terminal failure payload was not redacted: %q", errorText)
	}
}

func TestTaskFailedFieldsRetryPendingOmitsError(t *testing.T) {
	fields := taskFailedFields("task timed out", "timeout", true)

	if fields["retry_pending"] != true {
		t.Fatalf("retry_pending = %v, want true", fields["retry_pending"])
	}
	if _, present := fields["error"]; present {
		t.Fatal("retry-pending failure must not expose a terminal error")
	}
}

func TestTaskEventCarriesPayloadAndScopeHints(t *testing.T) {
	task := db.AgentTaskQueue{
		ID:            testUUID(41),
		AgentID:       testUUID(42),
		IssueID:       testUUID(43),
		ChatSessionID: testUUID(44),
		Status:        "failed",
	}
	e := taskEvent(protocol.EventTaskFailed, "workspace-1", task, map[string]any{
		"failure_reason": "timeout",
		"retry_pending":  false,
	})

	if e.TaskID != util.UUIDToString(task.ID) {
		t.Fatalf("event TaskID = %q, want %q", e.TaskID, util.UUIDToString(task.ID))
	}
	if e.ChatSessionID != util.UUIDToString(task.ChatSessionID) {
		t.Fatalf("event ChatSessionID = %q, want %q", e.ChatSessionID, util.UUIDToString(task.ChatSessionID))
	}
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", e.Payload)
	}
	for key, want := range map[string]any{
		"task_id":         util.UUIDToString(task.ID),
		"agent_id":        util.UUIDToString(task.AgentID),
		"issue_id":        util.UUIDToString(task.IssueID),
		"chat_session_id": util.UUIDToString(task.ChatSessionID),
		"status":          "failed",
		"failure_reason":  "timeout",
		"retry_pending":   false,
	} {
		if got := payload[key]; got != want {
			t.Errorf("payload[%q] = %v, want %v", key, got, want)
		}
	}
}

// TestFailTaskPublishesDeliverableChannelFailure walks the real persistence
// path that previously emitted only the base task fields. The DingTalk
// subscriber needs the redacted error and chat scope on this exact event to
// replace its initial acknowledgement with a terminal failure notice.
func TestFailTaskPublishesDeliverableChannelFailure(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)

	var runtimeID string
	if err := pool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load agent runtime: %v", err)
	}
	var chatSessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id)
		VALUES ($1, $2, $3) RETURNING id`, workspaceID, agentID, userID).Scan(&chatSessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	var taskID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue
			(agent_id, runtime_id, chat_session_id, status, priority, attempt, max_attempts)
		VALUES ($1, $2, $3, 'running', 0, 1, 1)
		RETURNING id`, agentID, runtimeID, chatSessionID).Scan(&taskID); err != nil {
		t.Fatalf("seed running chat task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, chatSessionID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, chatSessionID)
	})

	bus := events.New()
	var got events.Event
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) { got = e })
	svc := &TaskService{Queries: q, TxStarter: pool, Bus: bus}
	if _, err := svc.FailTask(ctx, util.MustParseUUID(taskID), "provider failed", "", "", "agent_error", false, ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	if got.Type != protocol.EventTaskFailed {
		t.Fatalf("event type = %q, want %q", got.Type, protocol.EventTaskFailed)
	}
	if got.WorkspaceID != workspaceID || got.TaskID != taskID || got.ChatSessionID != chatSessionID {
		t.Fatalf("event scope = workspace %q task %q chat %q", got.WorkspaceID, got.TaskID, got.ChatSessionID)
	}
	payload, ok := got.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", got.Payload)
	}
	if payload["error"] != "provider failed" {
		t.Fatalf("payload error = %v, want provider failed", payload["error"])
	}
	if payload["failure_reason"] != "agent_error" || payload["retry_pending"] != false {
		t.Fatalf("failure metadata = reason %v retry %v", payload["failure_reason"], payload["retry_pending"])
	}
}
