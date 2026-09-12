package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// stubWakeup records every call so the test can assert that notify
// reaches the daemon hub and carries the right runtime / task IDs.
type stubWakeup struct {
	calls []struct{ runtimeID, taskID string }
}

func (s *stubWakeup) NotifyTaskAvailable(runtimeID, taskID string) {
	s.calls = append(s.calls, struct{ runtimeID, taskID string }{runtimeID, taskID})
}

func createQuickCreateNotifyFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) (workspaceID, userID, agentID, runtimeID string) {
	t.Helper()
	suffix := time.Now().UnixNano()

	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Quick Create Notify Test", fmt.Sprintf("quick-create-notify-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Quick Create Notify Test", fmt.Sprintf("quick-create-notify-%d", suffix), "temporary quick-create notify test workspace", "QCN").Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider,
			status, device_info, metadata, last_seen_at, visibility, owner_id
		)
		VALUES ($1, NULL, $2, 'cloud', 'quick_create_notify_test', 'online', 'test runtime', '{}'::jsonb, now(), 'private', $3)
		RETURNING id
	`, workspaceID, "Quick Create Notify Runtime", userID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, workspaceID, "Quick Create Notify Agent", runtimeID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create agent: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx := context.Background()
		pool.Exec(cleanupCtx, `DELETE FROM inbox_item WHERE workspace_id = $1`, workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		pool.Exec(cleanupCtx, `DELETE FROM agent WHERE id = $1`, agentID)
		pool.Exec(cleanupCtx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
		pool.Exec(cleanupCtx, `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
		pool.Exec(cleanupCtx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(cleanupCtx, `DELETE FROM "user" WHERE id = $1`, userID)
	})

	return workspaceID, userID, agentID, runtimeID
}

// TestNotifyTaskAvailable_BumpsBeforeWakeup pins the contract noted in
// the EmptyClaimCache docs: the version Bump MUST run before the
// daemon WS wakeup, otherwise the wakeup-driven claim could read a
// still-current empty verdict and return null while the freshly
// queued task sits idle. The test (1) marks the runtime empty under
// the current version, (2) fires notifyTaskAvailable, then (3)
// asserts the prior verdict is rejected AND the wakeup hook saw the
// new task — proving every enqueue path (issue / mention /
// quick-create / chat / autopilot / retry) gets the same
// bump-then-notify behaviour for free.
func TestNotifyTaskAvailable_BumpsBeforeWakeup(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewEmptyClaimCache(rdb)
	wakeup := &stubWakeup{}

	svc := &TaskService{
		EmptyClaim: cache,
		Wakeup:     wakeup,
	}

	runtimeID := testUUID(7)
	taskID := testUUID(8)
	runtimeKey := util.UUIDToString(runtimeID)

	ctx := context.Background()
	v0 := cache.CurrentVersion(ctx, runtimeKey)
	cache.MarkEmpty(ctx, runtimeKey, v0)
	if !cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("precondition: cache should report empty after MarkEmpty under current version")
	}

	svc.notifyTaskAvailable(db.AgentTaskQueue{
		ID:        taskID,
		RuntimeID: runtimeID,
	})

	if cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("notifyTaskAvailable must Bump the version so the prior empty verdict is rejected")
	}
	if got := len(wakeup.calls); got != 1 {
		t.Fatalf("expected 1 wakeup call, got %d", got)
	}
	if wakeup.calls[0].runtimeID != runtimeKey {
		t.Fatalf("wakeup runtime mismatch: got %q want %q", wakeup.calls[0].runtimeID, runtimeKey)
	}
	if wakeup.calls[0].taskID != util.UUIDToString(taskID) {
		t.Fatalf("wakeup task mismatch: got %q want %q", wakeup.calls[0].taskID, util.UUIDToString(taskID))
	}
}

// TestNotifyTaskAvailable_InvalidWithoutRuntimeIsNoOp guards the
// no-RuntimeID early return — chat / quick-create / autopilot all set
// it on insert, but a buggy caller that forgot must not silently bump
// every workspace's version. The cache treats Bump("") as a no-op,
// but this test pins that the RuntimeID guard sits above the Bump
// call so a future refactor cannot drop the guard without test
// coverage.
func TestNotifyTaskAvailable_InvalidWithoutRuntimeIsNoOp(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewEmptyClaimCache(rdb)
	wakeup := &stubWakeup{}

	svc := &TaskService{
		EmptyClaim: cache,
		Wakeup:     wakeup,
	}

	ctx := context.Background()
	v0 := cache.CurrentVersion(ctx, "rt-stays")
	cache.MarkEmpty(ctx, "rt-stays", v0)

	svc.notifyTaskAvailable(db.AgentTaskQueue{
		// RuntimeID intentionally invalid (zero value, Valid=false).
		ID: testUUID(9),
	})

	if !cache.IsEmpty(ctx, "rt-stays") {
		t.Fatal("notifyTaskAvailable with invalid RuntimeID must not touch cache")
	}
	if got := len(wakeup.calls); got != 0 {
		t.Fatalf("expected 0 wakeup calls when RuntimeID is invalid, got %d", got)
	}
}

func TestCompleteQuickCreateWithoutCreatedIssueWritesSuccessWithOutput(t *testing.T) {
	pool := newTaskClaimRacePool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, runtimeID := createQuickCreateNotifyFixture(t, ctx, pool)
	qc, err := json.Marshal(QuickCreateContext{
		Type:        QuickCreateContextType,
		WorkspaceID: workspaceID,
		RequesterID: userID,
		Prompt:      "Draft an issue, but do not create it.",
	})
	if err != nil {
		t.Fatalf("marshal quick-create context: %v", err)
	}
	task, err := q.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		AgentID:   util.MustParseUUID(agentID),
		RuntimeID: util.MustParseUUID(runtimeID),
		Priority:  2,
		Context:   qc,
	})
	if err != nil {
		t.Fatalf("create quick-create task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM inbox_item WHERE details->>'task_id' = $1`, util.UUIDToString(task.ID))
		_, _ = pool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
	})

	svc := NewTaskService(q, pool, nil, events.New())
	claimed, err := svc.ClaimTask(ctx, task.AgentID)
	if err != nil {
		t.Fatalf("ClaimTask: %v", err)
	}
	if claimed == nil || claimed.ID != task.ID {
		t.Fatalf("claimed task = %v, want %s", claimed, util.UUIDToString(task.ID))
	}
	if _, err := svc.StartTask(ctx, task.ID); err != nil {
		t.Fatalf("StartTask: %v", err)
	}

	draft := "### Proposed issue\n\nThe sync job should expose a retry button."
	result, err := json.Marshal(map[string]string{"output": draft})
	if err != nil {
		t.Fatalf("marshal task result: %v", err)
	}
	if _, err := svc.CompleteTask(ctx, task.ID, result, "", ""); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	var inboxType, title string
	var issueID pgtype.UUID
	var body pgtype.Text
	var details []byte
	if err := pool.QueryRow(ctx, `
		SELECT type, title, issue_id, body, details
		FROM inbox_item
		WHERE details->>'task_id' = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, util.UUIDToString(task.ID)).Scan(&inboxType, &title, &issueID, &body, &details); err != nil {
		t.Fatalf("load quick-create inbox item: %v", err)
	}
	if inboxType != "quick_create_done" {
		t.Fatalf("quick-create no-issue inbox type = %q, want quick_create_done", inboxType)
	}
	if issueID.Valid {
		t.Fatalf("quick-create preview-only inbox issue_id valid = true, want false")
	}
	if title != "Quick create completed" {
		t.Fatalf("quick-create no-issue title = %q", title)
	}
	if !body.Valid || body.String != draft {
		t.Fatalf("quick-create no-issue body = %#v, want draft output", body)
	}
	var detail map[string]any
	if err := json.Unmarshal(details, &detail); err != nil {
		t.Fatalf("unmarshal inbox details: %v", err)
	}
	if detail["output"] != draft {
		t.Fatalf("quick-create no-issue detail output = %#v, want draft", detail["output"])
	}
	if detail["original_prompt"] != "Draft an issue, but do not create it." {
		t.Fatalf("quick-create no-issue original prompt = %#v", detail["original_prompt"])
	}

	var failedCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM inbox_item
		WHERE details->>'task_id' = $1 AND type = 'quick_create_failed'
	`, util.UUIDToString(task.ID)).Scan(&failedCount); err != nil {
		t.Fatalf("count failed inbox rows: %v", err)
	}
	if failedCount != 0 {
		t.Fatalf("quick-create preview-only wrote %d failed inbox rows, want 0", failedCount)
	}
}
