package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAgentBuilderInstructionsConstrainModelsToRuntimeCatalog(t *testing.T) {
	for _, requirement := range []string{
		"AVAILABLE RUNTIME MODELS",
		"Never use a model label as the id",
		"never invent a model id",
	} {
		if !strings.Contains(agentBuilderInstructions, requirement) {
			t.Fatalf("agent builder instructions missing model constraint %q", requirement)
		}
	}
}

func TestCreateAgentBuilderSessionCreatesIsolatedHiddenBuilder(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM agent
			WHERE workspace_id = $1 AND kind = 'system' AND system_key LIKE 'agent_builder:%'
		`, testWorkspaceID)
	})

	create := func(model string) CreateAgentBuilderSessionResponse {
		w := httptest.NewRecorder()
		testHandler.CreateAgentBuilderSession(w, newRequest(http.MethodPost, "/api/agent-builder/sessions", map[string]any{
			"runtime_id": testRuntimeID,
			"model":      model,
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateAgentBuilderSession: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var response CreateAgentBuilderSessionResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if response.SessionID == "" || response.BuilderAgentID == "" {
			t.Fatalf("missing builder identifiers: %+v", response)
		}
		return response
	}

	first := create("builder-model-a")
	second := create("builder-model-b")
	if first.BuilderAgentID == second.BuilderAgentID {
		t.Fatalf("builder sessions unexpectedly shared an agent: %s", first.BuilderAgentID)
	}
	if first.SessionID == second.SessionID {
		t.Fatalf("each creation flow must receive a fresh chat session")
	}

	var kind, systemKey, firstModel string
	if err := testPool.QueryRow(context.Background(), `
		SELECT kind, system_key, model FROM agent WHERE id = $1
	`, first.BuilderAgentID).Scan(&kind, &systemKey, &firstModel); err != nil {
		t.Fatalf("load builder agent: %v", err)
	}
	if kind != "system" || !strings.HasPrefix(systemKey, "agent_builder:") {
		t.Fatalf("unexpected builder identity kind=%q system_key=%q", kind, systemKey)
	}
	if firstModel != "builder-model-a" {
		t.Fatalf("first builder model was mutated: got %q", firstModel)
	}

	w := httptest.NewRecorder()
	testHandler.ListAgents(w, newRequest(http.MethodGet, "/api/agents", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents: %d: %s", w.Code, w.Body.String())
	}
	var listed []AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode agent list: %v", err)
	}
	for _, agent := range listed {
		if agent.ID == first.BuilderAgentID {
			t.Fatalf("system builder leaked into the user-facing agent list")
		}
	}

	// Knowing the ID must not expose system infrastructure through the public
	// Agent detail/update/archive loaders.
	w = httptest.NewRecorder()
	req := withURLParams(newRequest(http.MethodGet, "/api/agents/"+first.BuilderAgentID, nil), "id", first.BuilderAgentID)
	testHandler.GetAgent(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetAgent(system): expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Deleting the private Builder chat also removes its session-scoped hidden
	// Agent, so completed/cancelled flows do not accumulate infrastructure rows.
	w = httptest.NewRecorder()
	req = withURLParams(newRequest(http.MethodDelete, "/api/chat/sessions/"+first.SessionID, nil), "sessionId", first.SessionID)
	req = withChatTestWorkspaceCtx(t, req)
	testHandler.DeleteChatSession(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteChatSession(builder): expected 204, got %d: %s", w.Code, w.Body.String())
	}
	var remaining int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent WHERE id = $1`, first.BuilderAgentID).Scan(&remaining); err != nil {
		t.Fatalf("count deleted builder: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("builder agent survived chat deletion")
	}
}

func TestCreateAgentAttachesSkillsInCreateTransaction(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	var skillID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, 'Atomic Create Skill', '', '# Atomic', '{}'::jsonb, $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&skillID); err != nil {
		t.Fatalf("create skill fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE workspace_id = $1 AND name = 'Atomic Skill Agent'`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, skillID)
	})

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", map[string]any{
		"name":       "Atomic Skill Agent",
		"runtime_id": testRuntimeID,
		"skill_ids":  []string{skillID},
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var response AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Skills) != 1 || response.Skills[0].ID != skillID {
		t.Fatalf("create response did not include attached skill: %+v", response.Skills)
	}
	var introSessions int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM chat_session WHERE agent_id = $1 AND is_agent_intro = true
	`, response.ID).Scan(&introSessions); err != nil {
		t.Fatalf("count welcome chat sessions: %v", err)
	}
	if introSessions != 1 {
		t.Fatalf("welcome chat sessions = %d, want 1", introSessions)
	}
}

// newBuilderSession starts a builder conversation on testRuntimeID and registers
// cleanup for the carrier agents the flow creates.
func newBuilderSession(t *testing.T) CreateAgentBuilderSessionResponse {
	t.Helper()
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM agent
			WHERE workspace_id = $1 AND kind = 'system' AND system_key LIKE 'agent_builder:%'
		`, testWorkspaceID)
	})

	w := httptest.NewRecorder()
	testHandler.CreateAgentBuilderSession(w, newRequest(http.MethodPost, "/api/agent-builder/sessions", map[string]any{
		"runtime_id": testRuntimeID,
		"model":      "model-pinned-to-runtime-a",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgentBuilderSession: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var session CreateAgentBuilderSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return session
}

// newTestRuntime inserts an extra runtime in the fixture workspace so switch
// tests have somewhere to move to.
func newTestRuntime(t *testing.T, name, status string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, $4, 'switch test runtime', '{}'::jsonb, $5, now())
		RETURNING id
	`, testWorkspaceID, name, strings.ToLower(strings.ReplaceAll(name, " ", "_")), status, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime %q: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func switchBuilderRuntime(t *testing.T, sessionID, runtimeID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParams(
		newRequest(http.MethodPatch, "/api/agent-builder/sessions/"+sessionID+"/runtime", map[string]any{
			"runtime_id": runtimeID,
		}),
		"sessionId", sessionID,
	)
	testHandler.SwitchAgentBuilderRuntime(w, req)
	return w
}

func TestSwitchAgentBuilderRuntimeRebindsCarrier(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	session := newBuilderSession(t)
	target := newTestRuntime(t, "Builder Switch Target", "online")

	w := switchBuilderRuntime(t, session.SessionID, target)
	if w.Code != http.StatusOK {
		t.Fatalf("SwitchAgentBuilderRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response SwitchAgentBuilderRuntimeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode switch response: %v", err)
	}
	if response.RuntimeID != target {
		t.Fatalf("response runtime = %q, want %q", response.RuntimeID, target)
	}

	// The carrier is what stamps a chat task's runtime, so this row — not the
	// client's local selection — is the fix.
	var boundRuntimeID, boundRuntimeMode string
	var boundModel pgtype.Text
	if err := testPool.QueryRow(ctx, `
		SELECT runtime_id::text, runtime_mode, model FROM agent WHERE id = $1
	`, session.BuilderAgentID).Scan(&boundRuntimeID, &boundRuntimeMode, &boundModel); err != nil {
		t.Fatalf("load builder carrier: %v", err)
	}
	if boundRuntimeID != target {
		t.Fatalf("carrier runtime = %q, want %q", boundRuntimeID, target)
	}
	if boundRuntimeMode != "cloud" {
		t.Fatalf("carrier runtime_mode = %q, want cloud", boundRuntimeMode)
	}
	if boundModel.Valid {
		t.Fatalf("carrier model should be cleared on rebind, got %q", boundModel.String)
	}

	// Left deliberately stale: the daemon only resumes a stored provider session
	// when this pointer matches the claiming task's runtime, so keeping the old
	// value is what makes the new runtime start a fresh session.
	var sessionRuntimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT runtime_id::text FROM chat_session WHERE id = $1
	`, session.SessionID).Scan(&sessionRuntimeID); err != nil {
		t.Fatalf("load chat session: %v", err)
	}
	if sessionRuntimeID != testRuntimeID {
		t.Fatalf("chat_session.runtime_id = %q, want the original %q", sessionRuntimeID, testRuntimeID)
	}
}

func TestSwitchAgentBuilderRuntimeRejectsOfflineTarget(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	session := newBuilderSession(t)
	offline := newTestRuntime(t, "Builder Switch Offline", "offline")

	if w := switchBuilderRuntime(t, session.SessionID, offline); w.Code != http.StatusConflict {
		t.Fatalf("offline target: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSwitchAgentBuilderRuntimeRejectsWhileReplyPending(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	session := newBuilderSession(t)
	target := newTestRuntime(t, "Builder Switch Pending Target", "online")

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, chat_session_id, status, priority, context, runtime_id)
		VALUES ($1, $2, 'running', 2, '{}'::jsonb, $3)
	`, session.BuilderAgentID, session.SessionID, testRuntimeID); err != nil {
		t.Fatalf("insert pending task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, session.SessionID)
	})

	if w := switchBuilderRuntime(t, session.SessionID, target); w.Code != http.StatusConflict {
		t.Fatalf("pending reply: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSwitchAgentBuilderRuntimeRejectsNonBuilderSession(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	target := newTestRuntime(t, "Builder Switch Foreign Target", "online")

	// A user-authored agent changes runtime through the agent update path; this
	// endpoint must not be a second way in.
	var userAgentID, userSessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Builder Switch User Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 'public_to', 1, $3)
		RETURNING id
	`, testWorkspaceID, testRuntimeID, testUserID).Scan(&userAgentID); err != nil {
		t.Fatalf("create user agent: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id)
		VALUES ($1, $2, $3, 'Not a builder', $4)
		RETURNING id
	`, testWorkspaceID, userAgentID, testUserID, testRuntimeID).Scan(&userSessionID); err != nil {
		t.Fatalf("create user chat session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, userSessionID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, userAgentID)
	})

	if w := switchBuilderRuntime(t, userSessionID, target); w.Code != http.StatusNotFound {
		t.Fatalf("user agent session: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	var stillBound string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, userAgentID).Scan(&stillBound); err != nil {
		t.Fatalf("reload user agent: %v", err)
	}
	if stillBound != testRuntimeID {
		t.Fatalf("user agent runtime changed to %q; the builder endpoint must never touch it", stillBound)
	}
}

// The regression this whole change exists for: a send that loaded the agent
// before a rebind committed must still enqueue on the runtime the session is
// bound to NOW. SendDirectChatMessage is handed the stale agent on purpose here
// — that is exactly what its caller does when the two requests interleave.
func TestSendDirectChatMessageUsesCurrentlyBoundRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	created := newBuilderSession(t)
	target := newTestRuntime(t, "Builder Send Rebind Target", "online")

	sessionUUID := parseUUID(created.SessionID)
	session, err := testHandler.Queries.GetChatSession(ctx, sessionUUID)
	if err != nil {
		t.Fatalf("load chat session: %v", err)
	}
	staleAgent, err := testHandler.Queries.GetAgent(ctx, parseUUID(created.BuilderAgentID))
	if err != nil {
		t.Fatalf("load builder carrier: %v", err)
	}
	if uuidToString(staleAgent.RuntimeID) != testRuntimeID {
		t.Fatalf("carrier should start on %q, got %q", testRuntimeID, uuidToString(staleAgent.RuntimeID))
	}

	if w := switchBuilderRuntime(t, created.SessionID, target); w.Code != http.StatusOK {
		t.Fatalf("SwitchAgentBuilderRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, created.SessionID)
	})

	sent, err := testHandler.TaskService.SendDirectChatMessage(
		ctx, session, staleAgent, parseUUID(testUserID), "hello after the switch", nil, "member", parseUUID(testUserID),
	)
	if err != nil {
		t.Fatalf("SendDirectChatMessage: %v", err)
	}
	if got := uuidToString(sent.Task.RuntimeID); got != target {
		t.Fatalf("task runtime = %q, want the rebound runtime %q — a stale in-flight send must not resurrect the old runtime", got, target)
	}
}

// holderBackendPID returns the server-side PID of the backend serving tx, so a
// waiter can later be attributed to this specific lock holder.
func holderBackendPID(t *testing.T, ctx context.Context, tx pgx.Tx) int {
	t.Helper()
	var pid int
	if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("read holder backend pid: %v", err)
	}
	return pid
}

// waitForWaiterBlockedBy blocks until some backend is waiting on a lock held by
// holderPID, which is how the interleaving tests below observe "the other side
// is parked on our chat_session row" without guessing at timings.
//
// pg_blocking_pids is what makes this specific. `go test ./...` runs package
// test binaries in parallel against one DATABASE_URL, so a probe for "any
// Lock-waiting backend" could match an unrelated package and let the holder
// commit early — after which the path under test would start clean, read the
// committed state, and pass even with its lock removed. Attributing the waiter
// to this transaction's PID removes that false-green path.
//
// Returns false only after the deadline with no attributable waiter, which is
// the signal that the path under test never took the lock. A probe error is
// fatal rather than swallowed: a permissions or connectivity failure must not
// be reported as "that path is not locking".
func waitForWaiterBlockedBy(t *testing.T, holderPID int, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var waiting int
		if err := testPool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database()
			  AND state = 'active'
			  AND wait_event_type = 'Lock'
			  AND $1::int = ANY(pg_blocking_pids(pid))
		`, holderPID).Scan(&waiting); err != nil {
			t.Fatalf("probe pg_stat_activity for waiters blocked by pid %d: %v", holderPID, err)
		}
		if waiting > 0 {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A rebind that has not committed yet must hold off a concurrent send, and the
// send must then observe the NEW runtime. This is the half of the protocol the
// lock owns: with the lock removed the send reads straight through the
// uncommitted rebind under READ COMMITTED, sees the pre-switch runtime, and
// enqueues the reply on the runtime the user just switched away from.
func TestSendDirectChatMessageWaitsForUncommittedRebind(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	created := newBuilderSession(t)
	target := newTestRuntime(t, "Builder Interleave Send Target", "online")
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, created.SessionID)
	})

	session, err := testHandler.Queries.GetChatSession(ctx, parseUUID(created.SessionID))
	if err != nil {
		t.Fatalf("load chat session: %v", err)
	}
	// The agent as the send handler would have loaded it: still on runtime A.
	staleAgent, err := testHandler.Queries.GetAgent(ctx, parseUUID(created.BuilderAgentID))
	if err != nil {
		t.Fatalf("load builder carrier: %v", err)
	}

	// Hold an uncommitted rebind, exactly as SwitchAgentBuilderRuntime does.
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rebind tx: %v", err)
	}
	defer tx.Rollback(context.Background())
	holderPID := holderBackendPID(t, ctx, tx)
	qtx := testHandler.Queries.WithTx(tx)
	if _, err := qtx.LockChatSessionForRuntimeBind(ctx, session.ID); err != nil {
		t.Fatalf("lock chat session: %v", err)
	}
	targetRuntime, err := testHandler.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
		ID:          parseUUID(target),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load target runtime: %v", err)
	}
	if _, err := qtx.RebindAgentBuilderRuntime(ctx, db.RebindAgentBuilderRuntimeParams{
		ID:          staleAgent.ID,
		RuntimeID:   targetRuntime.ID,
		RuntimeMode: targetRuntime.RuntimeMode,
	}); err != nil {
		t.Fatalf("rebind carrier: %v", err)
	}

	type sendResult struct {
		task db.AgentTaskQueue
		err  error
	}
	results := make(chan sendResult, 1)
	go func() {
		sent, err := testHandler.TaskService.SendDirectChatMessage(
			context.Background(), session, staleAgent, parseUUID(testUserID),
			"sent while the rebind was still open", nil, "member", parseUUID(testUserID),
		)
		if err != nil {
			results <- sendResult{err: err}
			return
		}
		results <- sendResult{task: sent.Task}
	}()

	if !waitForWaiterBlockedBy(t, holderPID, 10*time.Second) {
		select {
		case got := <-results:
			t.Fatalf("send completed (err=%v, runtime=%q) while an uncommitted rebind held the chat_session lock; the send path is not taking the lock",
				got.err, uuidToString(got.task.RuntimeID))
		default:
			t.Fatalf("send never blocked on the chat_session lock held by pid %d; the send path is not taking the lock", holderPID)
		}
	}
	select {
	case got := <-results:
		t.Fatalf("send returned (err=%v) before the rebind committed", got.err)
	default:
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit rebind: %v", err)
	}

	select {
	case got := <-results:
		if got.err != nil {
			t.Fatalf("SendDirectChatMessage after commit: %v", got.err)
		}
		if runtimeID := uuidToString(got.task.RuntimeID); runtimeID != target {
			t.Fatalf("task runtime = %q, want the rebound runtime %q", runtimeID, target)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("send did not complete after the rebind committed")
	}
}

// The mirror image: a send that has not committed yet must hold off a rebind,
// and the rebind must then see the now-visible pending task and refuse. Without
// the lock on the switch side, GetPendingChatTask cannot see the uncommitted
// task, so the switch reports success while a reply is already in flight on the
// old runtime — the same "UI says B, execution is A" split, one turn later.
func TestSwitchAgentBuilderRuntimeWaitsForUncommittedSend(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	created := newBuilderSession(t)
	target := newTestRuntime(t, "Builder Interleave Switch Target", "online")
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, created.SessionID)
	})

	// Hold an uncommitted send: the chat_session lock plus its task row, in the
	// order SendDirectChatMessage takes them.
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin send tx: %v", err)
	}
	defer tx.Rollback(context.Background())
	holderPID := holderBackendPID(t, ctx, tx)
	qtx := testHandler.Queries.WithTx(tx)
	if _, err := qtx.LockChatSessionForRuntimeBind(ctx, parseUUID(created.SessionID)); err != nil {
		t.Fatalf("lock chat session: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, chat_session_id, status, priority, context, runtime_id)
		VALUES ($1, $2, 'queued', 2, '{}'::jsonb, $3)
	`, created.BuilderAgentID, created.SessionID, testRuntimeID); err != nil {
		t.Fatalf("insert in-flight task: %v", err)
	}

	codes := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		req := withURLParams(
			newRequest(http.MethodPatch, "/api/agent-builder/sessions/"+created.SessionID+"/runtime", map[string]any{
				"runtime_id": target,
			}),
			"sessionId", created.SessionID,
		)
		testHandler.SwitchAgentBuilderRuntime(w, req)
		codes <- w.Code
	}()

	if !waitForWaiterBlockedBy(t, holderPID, 10*time.Second) {
		select {
		case code := <-codes:
			t.Fatalf("switch returned %d while an uncommitted send held the chat_session lock; the switch path is not taking the lock", code)
		default:
			t.Fatalf("switch never blocked on the chat_session lock held by pid %d; the switch path is not taking the lock", holderPID)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit send: %v", err)
	}

	select {
	case code := <-codes:
		if code != http.StatusConflict {
			t.Fatalf("switch returned %d after the send committed, want 409 — a reply is in flight", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("switch did not complete after the send committed")
	}

	// And the carrier must be untouched: a refused switch cannot half-apply.
	var boundRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, created.BuilderAgentID).Scan(&boundRuntimeID); err != nil {
		t.Fatalf("reload builder carrier: %v", err)
	}
	if boundRuntimeID != testRuntimeID {
		t.Fatalf("carrier runtime = %q after a refused switch, want the original %q", boundRuntimeID, testRuntimeID)
	}
}

func TestSwitchAgentBuilderRuntimeEnforcesRuntimeAndSessionOwnership(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// A plain member, so canUseRuntimeForAgent's owner/admin bypass does not
	// apply — the fixture user is the workspace owner and may legitimately use
	// anyone's private runtime.
	var plainMemberID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Builder Switch Plain Member', 'builder-switch-plain@multica.ai')
		RETURNING id
	`).Scan(&plainMemberID); err != nil {
		t.Fatalf("create plain member user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, plainMemberID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'member')
	`, testWorkspaceID, plainMemberID); err != nil {
		t.Fatalf("add plain member: %v", err)
	}

	// The fixture runtime is private to the workspace owner, so start this
	// member's session on a public one.
	var publicRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, NULL, 'Builder Switch Public', 'cloud', 'builder_switch_public', 'online', 'public', '{}'::jsonb, $2, 'public', now())
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&publicRuntimeID); err != nil {
		t.Fatalf("create public runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, publicRuntimeID)
	})

	// That member's own builder session, so the creator gate passes and the
	// runtime gate is what we are actually testing.
	createW := httptest.NewRecorder()
	testHandler.CreateAgentBuilderSession(createW, newRequestAs(plainMemberID, http.MethodPost, "/api/agent-builder/sessions", map[string]any{
		"runtime_id": publicRuntimeID,
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateAgentBuilderSession as plain member: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var created CreateAgentBuilderSessionResponse
	if err := json.Unmarshal(createW.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM agent
			WHERE workspace_id = $1 AND kind = 'system' AND system_key LIKE 'agent_builder:%'
		`, testWorkspaceID)
	})

	// The workspace owner's private runtime is not a legal target for them.
	var privateRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, NULL, 'Builder Switch Private', 'cloud', 'builder_switch_private', 'online', 'private', '{}'::jsonb, $2, 'private', now())
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&privateRuntimeID); err != nil {
		t.Fatalf("create private runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, privateRuntimeID)
	})

	forbiddenW := httptest.NewRecorder()
	testHandler.SwitchAgentBuilderRuntime(forbiddenW, withURLParams(
		newRequestAs(plainMemberID, http.MethodPatch, "/api/agent-builder/sessions/"+created.SessionID+"/runtime", map[string]any{
			"runtime_id": privateRuntimeID,
		}),
		"sessionId", created.SessionID,
	))
	if forbiddenW.Code != http.StatusForbidden {
		t.Fatalf("someone else's private runtime: expected 403, got %d: %s", forbiddenW.Code, forbiddenW.Body.String())
	}

	// And a session the caller does not own is not theirs to rebind, whatever
	// their workspace role — this one is issued by the workspace owner.
	target := newTestRuntime(t, "Builder Switch Ownership Target", "online")
	if w := switchBuilderRuntime(t, created.SessionID, target); w.Code != http.StatusForbidden {
		t.Fatalf("someone else's session: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	var boundRuntimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, created.BuilderAgentID).Scan(&boundRuntimeID); err != nil {
		t.Fatalf("reload builder carrier: %v", err)
	}
	if boundRuntimeID != publicRuntimeID {
		t.Fatalf("carrier runtime = %q after refused switches, want the original %q", boundRuntimeID, publicRuntimeID)
	}
}

// The probe above is only as good as its attribution: a database-wide "is
// anything waiting on a lock?" check would match another package's test binary
// on the shared DATABASE_URL and let the interleaving tests commit their holder
// early, turning them green even with the lock under test removed. This pins the
// property directly — a waiter blocked by a DIFFERENT backend must not count.
func TestWaitForWaiterBlockedByIgnoresUnrelatedWaiters(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	mine := newBuilderSession(t)
	theirs := newBuilderSession(t)

	// Our holder: locks our own session and blocks nobody.
	holderTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder tx: %v", err)
	}
	defer holderTx.Rollback(context.Background())
	holderPID := holderBackendPID(t, ctx, holderTx)
	if _, err := holderTx.Exec(ctx, `SELECT id FROM chat_session WHERE id = $1 FOR UPDATE`, mine.SessionID); err != nil {
		t.Fatalf("hold our own session lock: %v", err)
	}

	// An unrelated holder on a different row, plus a backend parked behind it —
	// the shape of another package's test running against the same database.
	otherTx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin unrelated holder tx: %v", err)
	}
	defer otherTx.Rollback(context.Background())
	otherPID := holderBackendPID(t, ctx, otherTx)
	if _, err := otherTx.Exec(ctx, `SELECT id FROM chat_session WHERE id = $1 FOR UPDATE`, theirs.SessionID); err != nil {
		t.Fatalf("hold unrelated session lock: %v", err)
	}

	blocked := make(chan struct{})
	go func() {
		defer close(blocked)
		waiterTx, err := testPool.Begin(context.Background())
		if err != nil {
			return
		}
		defer waiterTx.Rollback(context.Background())
		_, _ = waiterTx.Exec(context.Background(), `SELECT id FROM chat_session WHERE id = $1 FOR UPDATE`, theirs.SessionID)
	}()

	// The unrelated waiter is genuinely parked, so a database-wide probe would
	// fire here.
	if !waitForWaiterBlockedBy(t, otherPID, 10*time.Second) {
		t.Fatal("the unrelated waiter never blocked; this test cannot prove anything")
	}
	// Attributed to our holder, it must not.
	if waitForWaiterBlockedBy(t, holderPID, 500*time.Millisecond) {
		t.Fatal("probe matched a waiter blocked by another backend; the interleaving tests could commit their holder early and pass with the lock removed")
	}

	if err := otherTx.Rollback(ctx); err != nil {
		t.Fatalf("release unrelated lock: %v", err)
	}
	select {
	case <-blocked:
	case <-time.After(10 * time.Second):
		t.Fatal("unrelated waiter did not finish after its blocker released")
	}
}
