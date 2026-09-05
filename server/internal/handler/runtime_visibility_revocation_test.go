package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// privacyBeginHook changes committed state after the HTTP preflight but before
// the mutation transaction. No sleeps or scheduling assumptions are needed to
// reproduce a stale authorization decision.
type privacyBeginHook func(context.Context) (pgx.Tx, error)

func (hook privacyBeginHook) Begin(ctx context.Context) (pgx.Tx, error) { return hook(ctx) }

func TestRuntimePrivacyRechecksOwnerInTransaction(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler fixture is required")
	}
	for _, confirmed := range []bool{false, true} {
		t.Run(map[bool]string{false: "patch", true: "confirmed"}[confirmed], func(t *testing.T) {
			runtimeID, oldOwner, newOwner := runtimeVisibilityFixture(t)
			dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public' WHERE id = $1`, runtimeID)
			h := *testHandler
			h.TxStarter = privacyBeginHook(func(ctx context.Context) (pgx.Tx, error) {
				dbfx.Exec(t, `UPDATE agent_runtime SET owner_id = $2 WHERE id = $1`, runtimeID, newOwner)
				return testPool.Begin(ctx)
			})
			method, path, handler := http.MethodPatch, "/api/runtimes/"+runtimeID, h.UpdateAgentRuntime
			body := map[string]any{"visibility": "private"}
			if confirmed {
				method, path, handler = http.MethodPost, path+"/revoke-workspace-access", h.RevokeRuntimeWorkspaceAccess
				body = map[string]any{"expected_nonowner_agent_ids": []string{}, "expected_task_ids": []string{}}
			}
			req := newRequestAs(oldOwner, method, path, body)
			testutil.Call(t, handler, testutil.WithURLParams(req, "runtimeId", runtimeID)).Want(http.StatusForbidden)
			runtime, err := testHandler.Queries.GetAgentRuntime(context.Background(), mustTestUUID(t, runtimeID))
			if err != nil || runtime.Visibility != "public" {
				t.Fatalf("stale owner changed visibility: runtime=%+v err=%v", runtime, err)
			}
		})
	}
}

func TestAgentBindingRechecksRuntimeVisibilityInTransaction(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler fixture is required")
	}
	for _, create := range []bool{true, false} {
		t.Run(map[bool]string{true: "create", false: "rebind"}[create], func(t *testing.T) {
			runtimeID, _, memberID := runtimeVisibilityFixture(t)
			dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public' WHERE id = $1`, runtimeID)
			h := *testHandler
			h.TxStarter = privacyBeginHook(func(ctx context.Context) (pgx.Tx, error) {
				dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'private' WHERE id = $1`, runtimeID)
				return testPool.Begin(ctx)
			})
			body := map[string]any{"name": "Delayed Runtime Binding", "runtime_id": runtimeID}
			method, path, param, id, handler := http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/agents", "workspaceId", testWorkspaceID, h.CreateAgent
			if !create {
				id = createRuntimePrivacyAgent(t, runtimeID, memberID, "Delayed Runtime Rebind")
				dbfx.Exec(t, `UPDATE agent SET runtime_id = NULL WHERE id = $1`, id)
				method, path, param, handler = http.MethodPatch, "/api/agents/"+id, "id", h.UpdateAgent
			}
			req := newRequestAs(memberID, method, path, body)
			testutil.Call(t, handler, testutil.WithURLParams(req, param, id)).Want(http.StatusForbidden)
			var count int
			if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent WHERE runtime_id = $1 AND owner_id = $2`, runtimeID, memberID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("delayed binding persisted: count=%d err=%v", count, err)
			}
		})
	}
}

func TestRuntimeRevocationSettlesChats(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler fixture is required")
	}
	for _, tc := range []struct {
		name, status             string
		started, output, channel bool
	}{
		{name: "queued", status: "queued"},
		{name: "dispatched", status: "dispatched"},
		{name: "running output", status: "running", started: true, output: true},
		{name: "running empty offline", status: "running", started: true},
		{name: "channel empty", status: "running", started: true, channel: true},
		{name: "local directory wait", status: "waiting_local_directory", started: true},
		{name: "deferred", status: "deferred"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			runtimeID, ownerID, memberID := runtimeVisibilityFixture(t)
			dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public' WHERE id = $1`, runtimeID)
			agentID := createRuntimePrivacyAgent(t, runtimeID, memberID, "Revoked Chat Agent")
			sessionID := dbfx.ChatSession(t, agentID, testutil.Cols{"creator_id": memberID})
			cols := testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID, "status": tc.status, "initiator_user_id": memberID}
			if tc.started {
				cols["started_at"], cols["session_id"] = time.Now().Add(-time.Minute), "revoked-provider-session"
			}
			taskID := dbfx.Task(t, agentID, cols)
			dbfx.Exec(t, `UPDATE agent_task_queue SET chat_input_task_id = id WHERE id = $1`, taskID)
			messageID := dbfx.Insert(t, "chat_message", testutil.Cols{"chat_session_id": sessionID, "role": "user", "content": "keep this prompt", "task_id": taskID, "channel_ingested": tc.channel})
			attachmentID := dbfx.Insert(t, "attachment", testutil.Cols{
				"workspace_id": testWorkspaceID, "uploader_type": "member", "uploader_id": memberID,
				"filename": "input.txt", "url": "https://example.com/input.txt", "content_type": "text/plain", "size_bytes": 1,
				"chat_session_id": sessionID, "chat_message_id": messageID,
			})
			t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM chat_draft_restore WHERE task_id = $1`, taskID) })
			if tc.output {
				dbfx.Insert(t, "task_message", testutil.Cols{"task_id": taskID, "seq": 0, "type": "text", "content": "partial answer"})
			}
			runtimePrivacyRequest(t, runtimeID, ownerID, map[string]any{
				"expected_nonowner_agent_ids": []string{agentID}, "expected_task_ids": []string{taskID},
			}).Want(http.StatusOK)
			task, err := testHandler.Queries.GetAgentTask(ctx, mustTestUUID(t, taskID))
			if err != nil || task.Status != "cancelled" || task.FailureReason.String != "runtime_access_revoked" {
				t.Fatalf("revoked task=%+v err=%v", task, err)
			}
			deferred := tc.started && !tc.output && !tc.channel
			if task.ChatFinalizeDeferredAt.Valid != deferred {
				t.Fatalf("deferred marker=%v want %v", task.ChatFinalizeDeferredAt.Valid, deferred)
			}
			if tc.started {
				session, err := testHandler.Queries.GetChatSession(ctx, mustTestUUID(t, sessionID))
				if err != nil || session.SessionID.String != "revoked-provider-session" {
					t.Fatalf("cancelled resume pointer=%q err=%v", session.SessionID.String, err)
				}
			}
			if deferred && !testHandler.TaskService.FinalizeDeferredCancelledChat(ctx, task.ID) {
				t.Fatal("cancel ack did not settle the durable marker")
			}
			if testHandler.TaskService.FinalizeDeferredCancelledChat(ctx, task.ID) {
				t.Fatal("duplicate ack settled the same turn twice")
			}
			var restores, outcomes int
			dbfx.QueryRow(t, `SELECT count(*) FROM chat_draft_restore WHERE task_id = $1 AND content = 'keep this prompt' AND $2::uuid = ANY(attachment_ids)`, taskID, attachmentID).Scan(&restores)
			dbfx.QueryRow(t, `SELECT count(*) FROM chat_message WHERE task_id = $1 AND role = 'assistant' AND failure_reason = 'runtime_access_revoked'`, taskID).Scan(&outcomes)
			if tc.output || tc.channel {
				if restores != 0 || outcomes != 1 {
					t.Fatalf("restores=%d outcomes=%d, want 0/1", restores, outcomes)
				}
			} else if restores != 1 || outcomes != 0 {
				t.Fatalf("restores=%d outcomes=%d, want 1/0", restores, outcomes)
			}
		})
	}
}

func TestRuntimeRevocationRollsBackChatAndBindings(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler fixture is required")
	}
	ctx := context.Background()
	runtimeID, ownerID, memberID := runtimeVisibilityFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public' WHERE id = $1`, runtimeID)
	agentID := createRuntimePrivacyAgent(t, runtimeID, memberID, "Rollback Chat Agent")
	sessionID := dbfx.ChatSession(t, agentID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID, "status": "running", "session_id": "must-not-advance"})
	h := *testHandler
	h.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
	req := newRequestAs(ownerID, http.MethodPost, "/api/runtimes/"+runtimeID+"/revoke-workspace-access", map[string]any{
		"expected_nonowner_agent_ids": []string{agentID}, "expected_task_ids": []string{taskID},
	})
	testutil.Call(t, h.RevokeRuntimeWorkspaceAccess, testutil.WithURLParams(req, "runtimeId", runtimeID)).Want(http.StatusInternalServerError)
	var unchanged bool
	dbfx.QueryRow(t, `SELECT r.visibility = 'public' AND a.runtime_id = r.id AND t.status = 'running' AND t.chat_finalize_deferred_at IS NULL AND cs.session_id IS NULL
		FROM agent_runtime r JOIN agent a ON a.id = $2 JOIN agent_task_queue t ON t.id = $3 JOIN chat_session cs ON cs.id = t.chat_session_id WHERE r.id = $1`, runtimeID, agentID, taskID).Scan(&unchanged)
	if !unchanged {
		t.Fatal("failed commit partially revoked runtime access or chat state")
	}
	if testHandler.TaskService.FinalizeDeferredCancelledChat(ctx, mustTestUUID(t, taskID)) {
		t.Fatal("rolled-back marker was finalized")
	}
}

func TestRuntimeRevocationDoesNotDeadlockChatWriter(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler fixture is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runtimeID, ownerID, memberID := runtimeVisibilityFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public' WHERE id = $1`, runtimeID)
	agentID := createRuntimePrivacyAgent(t, runtimeID, memberID, "Busy Chat Agent")
	sessionID := dbfx.ChatSession(t, agentID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "chat_session_id": sessionID, "status": "running"})
	writer, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(context.Background())
	if _, err := db.New(writer).LockChatSessionForDelete(ctx, mustTestUUID(t, sessionID)); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"expected_nonowner_agent_ids": []string{agentID}, "expected_task_ids": []string{taskID}}
	req := newRequestAs(ownerID, http.MethodPost, "/api/runtimes/"+runtimeID+"/revoke-workspace-access", body).WithContext(ctx)
	var response struct {
		Code string `json:"code"`
	}
	testutil.Call(t, testHandler.RevokeRuntimeWorkspaceAccess, testutil.WithURLParams(req, "runtimeId", runtimeID)).Want(http.StatusConflict).JSON(&response)
	if response.Code != "runtime_access_revocation_busy" {
		t.Fatalf("busy response=%+v", response)
	}
	// A chat writer can finish in its normal session -> agent order after the
	// revocation has released its provisional agent lock.
	if _, err := db.New(writer).GetAgentForClaimUpdate(ctx, mustTestUUID(t, agentID)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	runtimePrivacyRequest(t, runtimeID, ownerID, body).Want(http.StatusOK)
}

func TestRuntimeRevocationRejectsDelayedEnqueue(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database-backed handler fixture is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runtimeID, _, memberID := runtimeVisibilityFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public' WHERE id = $1`, runtimeID)
	agentID := createRuntimePrivacyAgent(t, runtimeID, memberID, "Delayed Enqueue Agent")
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	qtx := db.New(tx)
	if _, err := qtx.LockAgentRuntimeForAccessChange(ctx, mustTestUUID(t, runtimeID)); err != nil {
		t.Fatal(err)
	}
	if _, err := qtx.GetAgentForClaimUpdate(ctx, mustTestUUID(t, agentID)); err != nil {
		t.Fatal(err)
	}
	var pid int
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := testHandler.Queries.CreateAgentTask(ctx, db.CreateAgentTaskParams{AgentID: mustTestUUID(t, agentID), RuntimeID: mustTestUUID(t, runtimeID)})
		done <- err
	}()
	if !waitForWaiterBlockedBy(t, pid, 5*time.Second) {
		t.Fatal("enqueue did not wait for the revocation's agent lock")
	}
	if _, err := qtx.UpdateAgentRuntimeVisibility(ctx, db.UpdateAgentRuntimeVisibilityParams{ID: mustTestUUID(t, runtimeID), Visibility: "private"}); err != nil {
		t.Fatal(err)
	}
	if _, err := qtx.UnbindNonOwnerUserAgentsFromRuntime(ctx, db.UnbindNonOwnerUserAgentsFromRuntimeParams{RuntimeID: mustTestUUID(t, runtimeID), RuntimeOwnerID: pgtype.UUID{}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("delayed enqueue error=%v, want no row", err)
		}
	case <-ctx.Done():
		t.Fatal("delayed enqueue never completed")
	}
}

func createRuntimePrivacyAgent(t *testing.T, runtimeID, ownerID, name string) string {
	t.Helper()
	return dbfx.Agent(t, name, runtimeID, testutil.Cols{
		"owner_id": ownerID,
	})
}

func runtimePrivacyRequest(t *testing.T, runtimeID, actorID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequestAs(actorID, http.MethodPost, "/api/runtimes/"+runtimeID+"/revoke-workspace-access", body)
	return testutil.Call(t, testHandler.RevokeRuntimeWorkspaceAccess, testutil.WithURLParams(req, "runtimeId", runtimeID))
}

// TestRevokeRuntimeWorkspaceAccess verifies the full public-to-private
// contract: a teammate's user agent is unbound and its running work becomes a
// durable cancellation, while the machine owner's agent and work continue.
func TestRevokeRuntimeWorkspaceAccess(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID, runtimeOwnerID, memberID := runtimeVisibilityFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public' WHERE id = $1`, runtimeID)

	ownerAgentID := createRuntimePrivacyAgent(t, runtimeID, runtimeOwnerID, "Runtime Privacy Owner Agent")
	memberAgentID := createRuntimePrivacyAgent(t, runtimeID, memberID, "Runtime Privacy Member Agent")
	memberAutopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           "Runtime Privacy Member Autopilot",
		"assignee_type":   "agent",
		"assignee_id":     memberAgentID,
		"status":          "active",
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	ownerTaskID := dbfx.Task(t, ownerAgentID, testutil.Cols{
		"runtime_id": runtimeID,
		"status":     "running",
	})
	memberTaskID := dbfx.Task(t, memberAgentID, testutil.Cols{
		"runtime_id": runtimeID,
		"status":     "running",
	})

	// A regular PATCH cannot silently revoke a teammate's execution. It returns
	// the plan the owner must explicitly confirm instead.
	patch := newRequestAs(runtimeOwnerID, http.MethodPatch, "/api/runtimes/"+runtimeID, map[string]any{"visibility": "private"})
	var plan struct {
		Code           string          `json:"code"`
		NonownerAgents []AgentResponse `json:"nonowner_agents"`
		ActiveTaskIDs  []string        `json:"active_task_ids"`
	}
	testutil.Call(t, testHandler.UpdateAgentRuntime, testutil.WithURLParams(patch, "runtimeId", runtimeID)).Want(http.StatusConflict).JSON(&plan)
	if plan.Code != "runtime_has_nonowner_dependents" || len(plan.NonownerAgents) != 1 || plan.NonownerAgents[0].ID != memberAgentID {
		t.Fatalf("unexpected privacy plan: %+v", plan)
	}
	if len(plan.ActiveTaskIDs) != 1 || plan.ActiveTaskIDs[0] != memberTaskID {
		t.Fatalf("unexpected active task plan: %+v", plan.ActiveTaskIDs)
	}

	// A stale/empty confirmation is rejected rather than applying a partial
	// privacy transition.
	var stale struct {
		Code string `json:"code"`
	}
	runtimePrivacyRequest(t, runtimeID, runtimeOwnerID, map[string]any{
		"expected_nonowner_agent_ids": []string{},
		"expected_task_ids":           []string{},
	}).Want(http.StatusConflict).JSON(&stale)
	if stale.Code != "runtime_access_revocation_plan_changed" {
		t.Fatalf("stale plan code = %q, want runtime_access_revocation_plan_changed", stale.Code)
	}

	runtimePrivacyRequest(t, runtimeID, runtimeOwnerID, map[string]any{
		"expected_nonowner_agent_ids": []string{memberAgentID},
		"expected_task_ids":           []string{memberTaskID},
	}).Want(http.StatusOK)

	var visibility string
	if err := testPool.QueryRow(ctx, `SELECT visibility FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&visibility); err != nil {
		t.Fatalf("read runtime visibility: %v", err)
	}
	if visibility != "private" {
		t.Fatalf("runtime visibility = %q, want private", visibility)
	}
	var memberBound, ownerBound bool
	if err := testPool.QueryRow(ctx, `SELECT runtime_id IS NOT NULL FROM agent WHERE id = $1`, memberAgentID).Scan(&memberBound); err != nil {
		t.Fatalf("read teammate binding: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT runtime_id IS NOT NULL FROM agent WHERE id = $1`, ownerAgentID).Scan(&ownerBound); err != nil {
		t.Fatalf("read owner binding: %v", err)
	}
	if memberBound || !ownerBound {
		t.Fatalf("bindings after revocation: member=%v owner=%v; want false/true", memberBound, ownerBound)
	}
	var autopilotStatus, autopilotPauseReason string
	if err := testPool.QueryRow(ctx, `SELECT status, COALESCE(pause_reason, '') FROM autopilot WHERE id = $1`, memberAutopilotID).Scan(&autopilotStatus, &autopilotPauseReason); err != nil {
		t.Fatalf("read teammate autopilot: %v", err)
	}
	if autopilotStatus != "paused" || autopilotPauseReason != string(ReasonAgentRuntimeRequired) {
		t.Fatalf("teammate autopilot = %s/%s, want paused/%s", autopilotStatus, autopilotPauseReason, ReasonAgentRuntimeRequired)
	}

	var memberStatus, memberReason, ownerStatus string
	if err := testPool.QueryRow(ctx, `SELECT status, COALESCE(failure_reason, '') FROM agent_task_queue WHERE id = $1`, memberTaskID).Scan(&memberStatus, &memberReason); err != nil {
		t.Fatalf("read teammate task: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, ownerTaskID).Scan(&ownerStatus); err != nil {
		t.Fatalf("read owner task: %v", err)
	}
	if memberStatus != "cancelled" || memberReason != "runtime_access_revoked" || ownerStatus != "running" {
		t.Fatalf("task states after revocation: member=%s/%s owner=%s", memberStatus, memberReason, ownerStatus)
	}

	// A daemon that completed just after its cancellation poll cannot revive the
	// task: CompleteAgentTask only transitions a still-running row.
	_, err := testHandler.Queries.CompleteAgentTask(ctx, db.CompleteAgentTaskParams{
		ID:                    mustTestUUID(t, memberTaskID),
		Result:                []byte(`{"late":true}`),
		SessionRolloutMissing: false,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("late completion error = %v, want pgx.ErrNoRows", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, memberTaskID).Scan(&memberStatus); err != nil {
		t.Fatalf("re-read teammate task: %v", err)
	}
	if memberStatus != "cancelled" {
		t.Fatalf("late completion revived task to %q", memberStatus)
	}
}

// TestUpdateAgentRuntimePrivateWithoutTeammateDependencies exercises the
// direct path. It must use the same locked snapshot as the confirmed path,
// even though it has nothing disruptive to revoke.
func TestUpdateAgentRuntimePrivateWithoutTeammateDependencies(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID, runtimeOwnerID, _ := runtimeVisibilityFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET visibility = 'public' WHERE id = $1`, runtimeID)

	req := newRequestAs(runtimeOwnerID, http.MethodPatch, "/api/runtimes/"+runtimeID, map[string]any{"visibility": "private"})
	testutil.Call(t, testHandler.UpdateAgentRuntime, testutil.WithURLParams(req, "runtimeId", runtimeID)).Want(http.StatusOK)
	var visibility string
	if err := testPool.QueryRow(ctx, `SELECT visibility FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&visibility); err != nil {
		t.Fatalf("read runtime visibility: %v", err)
	}
	if visibility != "private" {
		t.Fatalf("runtime visibility = %q, want private", visibility)
	}
}

func mustTestUUID(t *testing.T, raw string) pgtype.UUID {
	t.Helper()
	value, err := uuidFromString(raw)
	if err != nil {
		t.Fatalf("parse test UUID %q: %v", raw, err)
	}
	return value
}
