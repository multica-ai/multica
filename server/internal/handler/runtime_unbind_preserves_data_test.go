package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// These are the regressions for MUL-5559. Deleting a runtime used to archive its
// agents and then hard-delete the rows, so the agents, their conversations and
// their task history all disappeared — while the confirmation dialog said
// "archive". Each test below pins one thing that must now survive, plus the two
// invariants that keep the new flow safe.

// TestUnbindAgentsAndDeleteRuntime_KeepsChatHistory: the agent's chat sessions
// and messages cascade from the agent row, so hard-deleting it destroyed every
// conversation with it. Unbinding must leave them readable.
func TestUnbindAgentsAndDeleteRuntime_KeepsChatHistory(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Unbind Chat Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Unbind Chat Agent")

	var sessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, runtime_id, title)
		VALUES ($1, $2, $3, $4, 'unbind chat history')
		RETURNING id
	`, testWorkspaceID, agentID, testUserID, runtimeID).Scan(&sessionID); err != nil {
		t.Fatalf("insert chat session: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'does this survive the machine being retired?')
	`, sessionID); err != nil {
		t.Fatalf("insert chat message: %v", err)
	}

	unbindRuntime(t, ctx, runtimeID, agentID)

	var sessions, messages int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM chat_session WHERE id = $1`, sessionID).Scan(&sessions); err != nil {
		t.Fatalf("count chat sessions: %v", err)
	}
	if sessions != 1 {
		t.Fatalf("chat session rows = %d, want 1 (the conversation must survive)", sessions)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM chat_message WHERE chat_session_id = $1`, sessionID).Scan(&messages); err != nil {
		t.Fatalf("count chat messages: %v", err)
	}
	if messages != 1 {
		t.Fatalf("chat message rows = %d, want 1", messages)
	}
	// chat_session.runtime_id is ON DELETE SET NULL, so the session detaches from
	// the deleted runtime and a rebind starts a fresh provider session.
	var sessionBound bool
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id IS NOT NULL FROM chat_session WHERE id = $1`, sessionID).Scan(&sessionBound); err != nil {
		t.Fatalf("read chat session runtime: %v", err)
	}
	if sessionBound {
		t.Fatalf("chat session must no longer point at the deleted runtime")
	}
}

// TestUnbindAgentsAndDeleteRuntime_KeepsTaskHistory is the second half of the
// data loss, and the one that needs agent_task_queue.runtime_id to be nullable:
// that column is NOT NULL with an ON DELETE CASCADE FK, and task_message /
// task_usage cascade from the task in turn. Unbinding only the agent would leave
// it alive with no record of anything it ever did.
func TestUnbindAgentsAndDeleteRuntime_KeepsTaskHistory(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Unbind Task History Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Unbind Task History Agent")

	doneTask := insertFixtureTask(t, ctx, runtimeID, agentID, "completed", true)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_message (task_id, seq, type, content)
		VALUES ($1, 1, 'assistant', 'transcript that must not vanish')
	`, doneTask); err != nil {
		t.Fatalf("insert task message: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens)
		VALUES ($1, 'anthropic', 'claude-test', 10, 20)
	`, doneTask); err != nil {
		t.Fatalf("insert task usage: %v", err)
	}
	runningTask := insertFixtureTask(t, ctx, runtimeID, agentID, "running", false)

	unbindRuntime(t, ctx, runtimeID, agentID)

	var (
		taskRows    int
		taskBound   bool
		msgRows     int
		usageRows   int
		activeState string
	)
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE id = $1`, doneTask).Scan(&taskRows); err != nil {
		t.Fatalf("count task rows: %v", err)
	}
	if taskRows != 1 {
		t.Fatalf("completed task rows = %d, want 1 (history must survive)", taskRows)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT runtime_id IS NOT NULL FROM agent_task_queue WHERE id = $1`, doneTask).Scan(&taskBound); err != nil {
		t.Fatalf("read task binding: %v", err)
	}
	if taskBound {
		t.Fatalf("history task must be detached from the deleted runtime")
	}
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM task_message WHERE task_id = $1`, doneTask).Scan(&msgRows); err != nil {
		t.Fatalf("count task messages: %v", err)
	}
	if msgRows != 1 {
		t.Fatalf("task_message rows = %d, want 1", msgRows)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM task_usage WHERE task_id = $1`, doneTask).Scan(&usageRows); err != nil {
		t.Fatalf("count task usage: %v", err)
	}
	if usageRows != 1 {
		t.Fatalf("task_usage rows = %d, want 1", usageRows)
	}

	// The in-flight task is cancelled rather than deleted, so the record of it
	// having been interrupted survives too.
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM agent_task_queue WHERE id = $1`, runningTask).Scan(&activeState); err != nil {
		t.Fatalf("read running task status: %v", err)
	}
	if activeState != "cancelled" {
		t.Fatalf("in-flight task status = %q, want cancelled", activeState)
	}
}

// TestUnbindAgentsAndDeleteRuntime_CancelsDeferredTasks covers the status the
// cancel query used to miss. 'deferred' arrived with migration 128 and was never
// added to CancelAgentTasksByRuntimeOrAgent; it went unnoticed only because the
// runtime delete cascaded those rows away. With runtime_id nullable and the
// active-requires-runtime CHECK in place, a missed status would abort the whole
// delete — so this test is what keeps the runtime deletable.
func TestUnbindAgentsAndDeleteRuntime_CancelsDeferredTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Unbind Deferred Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Unbind Deferred Agent")
	deferredTask := insertFixtureTask(t, ctx, runtimeID, agentID, "deferred", false)

	unbindRuntime(t, ctx, runtimeID, agentID)

	var status string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM agent_task_queue WHERE id = $1`, deferredTask).Scan(&status); err != nil {
		t.Fatalf("read deferred task status: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("deferred task status = %q, want cancelled", status)
	}
}

func TestCountUndrainedTasksByRuntimeOrAgent_IncludesCrossRuntimeTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Drain Assertion Agent Runtime")
	otherRuntimeID := createCascadeFixtureRuntime(t, ctx, "Drain Assertion Task Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Drain Assertion Agent")
	_ = insertFixtureTask(t, ctx, otherRuntimeID, agentID, "queued", false)

	count, err := testHandler.Queries.CountUndrainedTasksByRuntimeOrAgent(
		ctx,
		db.CountUndrainedTasksByRuntimeOrAgentParams{
			RuntimeIds: []pgtype.UUID{parseUUID(runtimeID)},
			AgentIds:   []pgtype.UUID{parseUUID(agentID)},
		},
	)
	if err != nil {
		t.Fatalf("count undrained tasks: %v", err)
	}
	if count != 1 {
		t.Fatalf("undrained count = %d, want 1 for task pinned to another runtime", count)
	}
}

func TestDeleteStaleOfflineRuntimes_UnboundAgentDoesNotDisableGC(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	boundRuntimeID := createCascadeFixtureRuntime(t, ctx, "GC Unbound Agent Source")
	agentID := createCascadeFixtureAgent(t, ctx, boundRuntimeID, "GC Unbound Agent")
	if _, err := testPool.Exec(ctx, `UPDATE agent SET runtime_id = NULL WHERE id = $1`, agentID); err != nil {
		t.Fatalf("unbind GC fixture agent: %v", err)
	}

	var staleRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, 'GC stale candidate', 'cloud', 'gc-regression', 'offline',
			'GC stale candidate', '{}'::jsonb, $2, now() - interval '200 years')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&staleRuntimeID); err != nil {
		t.Fatalf("seed stale runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, staleRuntimeID)
	})

	deleted, err := testHandler.Queries.DeleteStaleOfflineRuntimes(ctx, 3_000_000_000)
	if err != nil {
		t.Fatalf("delete stale runtimes: %v", err)
	}
	found := false
	for _, row := range deleted {
		if uuidToString(row.ID) == staleRuntimeID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("unbound agent made stale-runtime GC skip an unrelated candidate")
	}
}

// TestUnbindAgentsAndDeleteRuntime_KeepsAutopilotConfig pauses an automation
// whose assignee cannot run after teardown. Leaving it active would append an
// identical skipped run every schedule tick forever. Its assignee and config
// remain intact, and pause_reason tells the user how to recover.
func TestUnbindAgentsAndDeleteRuntime_KeepsAutopilotConfig(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Unbind Autopilot Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Unbind Autopilot Agent")

	var autopilotID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO autopilot (
			workspace_id, title, description, assignee_type, assignee_id,
			created_by_type, created_by_id, status, execution_mode
		)
		VALUES ($1, 'unbind autopilot', 'do the thing', 'agent', $2, 'member', $3, 'active', 'run_only')
		RETURNING id
	`, testWorkspaceID, agentID, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("insert autopilot: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE id = $1`, autopilotID)
	})

	unbindRuntime(t, ctx, runtimeID, agentID)

	var status, pauseReason string
	var assigneeRows int
	if err := testPool.QueryRow(ctx,
		`SELECT status, pause_reason FROM autopilot WHERE id = $1`, autopilotID).Scan(&status, &pauseReason); err != nil {
		t.Fatalf("read autopilot status: %v", err)
	}
	if status != "paused" {
		t.Fatalf("autopilot status = %q, want paused", status)
	}
	if pauseReason != string(ReasonAgentRuntimeRequired) {
		t.Fatalf("autopilot pause_reason = %q, want agent_runtime_required", pauseReason)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM autopilot WHERE id = $1 AND assignee_id = $2`,
		autopilotID, agentID).Scan(&assigneeRows); err != nil {
		t.Fatalf("read autopilot assignee: %v", err)
	}
	if assigneeRows != 1 {
		t.Fatalf("autopilot must still point at the surviving agent")
	}
}

func TestUnbindAgentsAndDeleteRuntime_RedactsAgentBroadcast(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Unbind Broadcast Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Unbind Broadcast Agent")
	if _, err := testPool.Exec(ctx, `
		UPDATE agent
		SET mcp_config = '{"servers":{"private":{"token":"secret"}}}'::jsonb,
		    composio_toolkit_allowlist = ARRAY['github']::text[]
		WHERE id = $1
	`, agentID); err != nil {
		t.Fatalf("seed secret-bearing agent fields: %v", err)
	}

	var broadcast *AgentResponse
	testHandler.Bus.Subscribe(protocol.EventAgentStatus, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		agent, ok := payload["agent"].(AgentResponse)
		if !ok || agent.ID != agentID {
			return
		}
		broadcast = &agent
	})

	unbindRuntime(t, ctx, runtimeID, agentID)

	if broadcast == nil {
		t.Fatal("expected agent:status broadcast for unbound agent")
	}
	if broadcast.McpConfig != nil {
		t.Fatalf("broadcast leaked mcp_config: %s", string(broadcast.McpConfig))
	}
	if !broadcast.McpConfigRedacted {
		t.Fatal("broadcast must mark mcp_config as redacted")
	}
	if broadcast.ComposioToolkitAllowlist != nil {
		t.Fatalf("broadcast leaked Composio allowlist: %v", broadcast.ComposioToolkitAllowlist)
	}
	if !broadcast.ComposioToolkitAllowlistRedacted {
		t.Fatal("broadcast must mark Composio allowlist as redacted")
	}
}

// TestUnbindAgentsAndDeleteRuntime_DeletesSystemAgents: system agents are
// invisible execution infrastructure (the Agent Builder, for example) with no UI
// to rebind them, so leaving them unbound would strand rows no one can repair.
// They are still hard-deleted with their runtime.
func TestUnbindAgentsAndDeleteRuntime_DeletesSystemAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Unbind System Runtime")
	userAgent := createCascadeFixtureAgent(t, ctx, runtimeID, "Unbind System Runtime User Agent")
	systemAgent := createCascadeFixtureAgent(t, ctx, runtimeID, "Unbind System Runtime System Agent")
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET kind = 'system', system_key = 'unbind_probe' WHERE id = $1`,
		systemAgent); err != nil {
		t.Fatalf("make agent a system agent: %v", err)
	}

	// Only the user agent is part of the confirmed plan: system agents never
	// appear in the dialog (ListActiveAgentsByRuntimeForUpdate filters to
	// kind='user').
	unbindRuntime(t, ctx, runtimeID, userAgent)

	var systemRows, userRows int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent WHERE id = $1`, systemAgent).Scan(&systemRows); err != nil {
		t.Fatalf("count system agent: %v", err)
	}
	if systemRows != 0 {
		t.Fatalf("system agent rows = %d, want 0 (still deleted with its runtime)", systemRows)
	}
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent WHERE id = $1`, userAgent).Scan(&userRows); err != nil {
		t.Fatalf("count user agent: %v", err)
	}
	if userRows != 1 {
		t.Fatalf("user agent rows = %d, want 1", userRows)
	}
}

// TestUnbindAgentsAndDeleteRuntime_LegacyRouteBehavesTheSame: installed clients
// still POST to /archive-agents-and-delete with an active-only expected set. That
// request must keep working — a changed contract here would 409 forever and leave
// the runtime undeletable — and it must perform the unbind, not the old archive.
func TestUnbindAgentsAndDeleteRuntime_LegacyRouteBehavesTheSame(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Legacy Route Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Legacy Route Agent")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/archive-agents-and-delete",
		map[string]any{"expected_active_agent_ids": []string{agentID}})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UnbindAgentsAndDeleteRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("legacy route: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		AgentsUnbound  int `json:"agents_unbound"`
		AgentsArchived int `json:"agents_archived"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.AgentsUnbound != 1 || body.AgentsArchived != 1 {
		t.Fatalf("counts = unbound %d / archived-mirror %d, want 1/1", body.AgentsUnbound, body.AgentsArchived)
	}

	var archived, bound bool
	if err := testPool.QueryRow(ctx,
		`SELECT archived_at IS NOT NULL, runtime_id IS NOT NULL FROM agent WHERE id = $1`,
		agentID).Scan(&archived, &bound); err != nil {
		t.Fatalf("read agent state: %v", err)
	}
	if archived {
		t.Fatalf("legacy route must unbind, not archive")
	}
	if bound {
		t.Fatalf("legacy route must leave the agent unbound")
	}
}

// TestAgentResponse_RuntimeBoundSignal pins the wire contract an unbound agent is
// served with: runtime_id stays a string (empty), so installed clients keep
// parsing, and runtime_bound is the explicit signal the UI branches on.
func TestAgentResponse_RuntimeBoundSignal(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createCascadeFixtureRuntime(t, ctx, "Runtime Bound Signal Runtime")
	agentID := createCascadeFixtureAgent(t, ctx, runtimeID, "Runtime Bound Signal Agent")

	bound := getAgentResponse(t, ctx, agentID)
	if !bound.RuntimeBound {
		t.Fatalf("bound agent: runtime_bound = false, want true")
	}
	if bound.RuntimeID == "" {
		t.Fatalf("bound agent: runtime_id must be set")
	}

	unbindRuntime(t, ctx, runtimeID, agentID)

	unboundResp := getAgentResponse(t, ctx, agentID)
	if unboundResp.RuntimeBound {
		t.Fatalf("unbound agent: runtime_bound = true, want false")
	}
	if unboundResp.RuntimeID != "" {
		t.Fatalf("unbound agent: runtime_id = %q, want empty string", unboundResp.RuntimeID)
	}
	if unboundResp.ArchivedAt != nil {
		t.Fatalf("unbound agent must not be reported as archived")
	}
}

// unbindRuntime drives the confirmed delete endpoint for one runtime whose
// confirmed active set is exactly the given agents, and fails the test if the
// runtime does not go away.
func unbindRuntime(t *testing.T, ctx context.Context, runtimeID string, activeAgentIDs ...string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/runtimes/"+runtimeID+"/unbind-agents-and-delete",
		map[string]any{"expected_active_agent_ids": activeAgentIDs})
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.UnbindAgentsAndDeleteRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UnbindAgentsAndDeleteRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var rtRows int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&rtRows); err != nil {
		t.Fatalf("count runtime rows: %v", err)
	}
	if rtRows != 0 {
		t.Fatalf("runtime row survived the delete")
	}
}

// insertFixtureTask seeds one agent_task_queue row in the given state. Terminal
// rows get completed_at so they satisfy agent_task_queue_active_requires_runtime
// once the runtime is detached.
func insertFixtureTask(t *testing.T, ctx context.Context, runtimeID, agentID, status string, terminal bool) string {
	t.Helper()
	var taskID string
	completedAt := "NULL"
	if terminal {
		completedAt = "now()"
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, context, completed_at)
		VALUES ($1, $2, $3, '{"fixture":true}'::jsonb, `+completedAt+`)
		RETURNING id
	`, agentID, runtimeID, status).Scan(&taskID); err != nil {
		t.Fatalf("insert fixture task (%s): %v", status, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

// getAgentResponse reads one agent through the handler's own serializer so the
// test asserts the wire shape, not the row.
func getAgentResponse(t *testing.T, ctx context.Context, agentID string) AgentResponse {
	t.Helper()
	agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("load agent %s: %v", agentID, err)
	}
	return testHandler.agentToResponse(agent)
}
