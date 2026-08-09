package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// createMigrationTestRuntime seeds an extra runtime in the handler test
// workspace. Migration always needs at least two runtimes, and the private
// variant backs the target-permission test.
func createMigrationTestRuntime(t *testing.T, name, visibility, ownerID string) string {
	t.Helper()

	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', 'handler_test_runtime', 'online', $2, '{}'::jsonb, $3, $4, now())
		RETURNING id
	`, testWorkspaceID, name, ownerID, visibility).Scan(&runtimeID); err != nil {
		t.Fatalf("create migration test runtime %s: %v", name, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

// seedTaskWithStatus creates one agent_task_queue row in an explicit status.
// completed_at is stamped for terminal statuses so the
// agent_task_queue_active_requires_runtime check stays satisfied either way.
func seedTaskWithStatus(t *testing.T, agentID, runtimeID, status string) string {
	t.Helper()

	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, fire_at)
		VALUES ($1, $2, $3, 0, CASE WHEN $3 = 'deferred' THEN now() + interval '1 hour' ELSE NULL END)
		RETURNING id
	`, agentID, runtimeID, status).Scan(&taskID); err != nil {
		t.Fatalf("seed %s task: %v", status, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

func taskRuntimeID(t *testing.T, taskID string) string {
	t.Helper()

	var runtimeID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT runtime_id FROM agent_task_queue WHERE id = $1`, taskID).Scan(&runtimeID); err != nil {
		t.Fatalf("read task runtime: %v", err)
	}
	return runtimeID
}

func agentRuntimeIDOf(t *testing.T, agentID string) string {
	t.Helper()

	var runtimeID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT COALESCE(runtime_id::text, '') FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read agent runtime: %v", err)
	}
	return runtimeID
}

func migrateRequest(t *testing.T, userID, targetRuntimeID string, body any) *httptest.ResponseRecorder {
	t.Helper()

	req := withURLParam(
		newRequestAs(userID, http.MethodPost, "/api/runtimes/"+targetRuntimeID+"/migrate-agents", body),
		"runtimeId", targetRuntimeID,
	)
	w := httptest.NewRecorder()
	testHandler.MigrateAgentsToRuntime(w, req)
	return w
}

func decodeMigrateResponse(t *testing.T, w *httptest.ResponseRecorder) migrateAgentsToRuntimeResponse {
	t.Helper()

	var resp migrateAgentsToRuntimeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode migrate response: %v (body: %s)", err, w.Body.String())
	}
	return resp
}

// TestMigrateAgentsToRuntime_MovesUnclaimedTasksOnly is the core of MUL-5758.
//
// Daemons list claim candidates by agent_task_queue.runtime_id, so a task
// queued before a runtime switch stays visible only to the runtime the agent
// left — stranded for good when that machine is the failing one being
// evacuated. Migration must therefore carry 'queued' and 'deferred' rows onto
// the new runtime while leaving 'dispatched' / 'running' /
// 'waiting_local_directory' where they are: those are already claimed and
// executing, and re-pointing them would desync the owning daemon.
func TestMigrateAgentsToRuntime_MovesUnclaimedTasksOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	source := handlerTestRuntimeID(t)
	target := createMigrationTestRuntime(t, "migrate-target", "public", testUserID)
	agentID := createHandlerTestAgent(t, "migrate-tasks-agent", nil)
	if _, err := testPool.Exec(ctx, `
		UPDATE agent SET model = 'claude-opus-4', thinking_level = 'high', service_tier = 'priority'
		WHERE id = $1
	`, agentID); err != nil {
		t.Fatalf("seed model settings: %v", err)
	}

	queued := seedTaskWithStatus(t, agentID, source, "queued")
	deferredTask := seedTaskWithStatus(t, agentID, source, "deferred")
	dispatched := seedTaskWithStatus(t, agentID, source, "dispatched")
	running := seedTaskWithStatus(t, agentID, source, "running")
	waiting := seedTaskWithStatus(t, agentID, source, "waiting_local_directory")

	w := migrateRequest(t, testUserID, target, map[string]any{"agent_ids": []string{agentID}})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeMigrateResponse(t, w)

	if len(resp.Migrated) != 1 || resp.Migrated[0].AgentID != agentID {
		t.Fatalf("expected the agent in migrated, got %+v", resp.Migrated)
	}
	if resp.TasksMigrated != 2 {
		t.Errorf("expected 2 unclaimed tasks moved (queued + deferred), got %d", resp.TasksMigrated)
	}
	if resp.TasksStayingActive != 3 {
		t.Errorf("expected 3 claimed tasks left in place, got %d", resp.TasksStayingActive)
	}

	if got := agentRuntimeIDOf(t, agentID); got != target {
		t.Errorf("agent runtime = %s, want %s", got, target)
	}
	for _, tc := range []struct {
		name   string
		taskID string
		want   string
	}{
		{"queued", queued, target},
		{"deferred", deferredTask, target},
		{"dispatched", dispatched, source},
		{"running", running, source},
		{"waiting_local_directory", waiting, source},
	} {
		if got := taskRuntimeID(t, tc.taskID); got != tc.want {
			t.Errorf("%s task runtime = %s, want %s", tc.name, got, tc.want)
		}
	}

	// Runtime-native settings are cleared so the new runtime resolves its own
	// defaults, and the response names what was discarded so the confirmation
	// dialog can show it instead of clearing silently.
	var model, thinking, tier string
	if err := testPool.QueryRow(ctx, `
		SELECT model, COALESCE(thinking_level, ''), COALESCE(service_tier, '') FROM agent WHERE id = $1
	`, agentID).Scan(&model, &thinking, &tier); err != nil {
		t.Fatalf("read model settings: %v", err)
	}
	if model != "" || thinking != "" || tier != "" {
		t.Errorf("expected model settings cleared, got model=%q thinking=%q tier=%q", model, thinking, tier)
	}
	if resp.Migrated[0].ClearedModel != "claude-opus-4" ||
		resp.Migrated[0].ClearedThinkingLevel != "high" ||
		resp.Migrated[0].ClearedServiceTier != "priority" {
		t.Errorf("response must report what it cleared, got %+v", resp.Migrated[0])
	}
}

// TestMigrateAgentsToRuntime_DryRunReportsSplitWithoutWriting covers the
// confirmation dialog's data source. No client-side projection can produce this
// split: derive-presence folds 'dispatched' and 'waiting_local_directory' into
// "queued" and ignores 'deferred' entirely.
func TestMigrateAgentsToRuntime_DryRunReportsSplitWithoutWriting(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	source := handlerTestRuntimeID(t)
	target := createMigrationTestRuntime(t, "migrate-dryrun-target", "public", testUserID)
	agentID := createHandlerTestAgent(t, "migrate-dryrun-agent", nil)

	queued := seedTaskWithStatus(t, agentID, source, "queued")
	seedTaskWithStatus(t, agentID, source, "deferred")
	seedTaskWithStatus(t, agentID, source, "running")

	w := migrateRequest(t, testUserID, target, map[string]any{
		"agent_ids": []string{agentID},
		"dry_run":   true,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeMigrateResponse(t, w)

	if !resp.DryRun {
		t.Error("expected dry_run=true in the response")
	}
	if resp.TasksMigrated != 2 || resp.TasksStayingActive != 1 {
		t.Errorf("expected 2 to move / 1 to stay, got %d / %d", resp.TasksMigrated, resp.TasksStayingActive)
	}
	if len(resp.Migrated) != 1 {
		t.Fatalf("expected 1 agent projected, got %+v", resp.Migrated)
	}

	if got := agentRuntimeIDOf(t, agentID); got != source {
		t.Errorf("dry run must not move the agent; runtime = %s, want %s", got, source)
	}
	if got := taskRuntimeID(t, queued); got != source {
		t.Errorf("dry run must not move tasks; queued task runtime = %s, want %s", got, source)
	}
}

// TestMigrateAgentsToRuntime_SkipsInsteadOfFailing pins the bulk contract
// (amended 2026-08-06 to declarative-overwrite semantics): agents the caller
// cannot write and ids that do not resolve are reported per agent, not
// errors, so "select all, apply what you may" works. An agent already on the
// target is NOT a skip — the request declares the desired state, so it is
// updated in place.
func TestMigrateAgentsToRuntime_SkipsInsteadOfFailing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	target := createMigrationTestRuntime(t, "migrate-skip-target", "public", testUserID)
	callerID := createPermissionTestMember(t, "migrate-skip-caller@multica.test")
	otherOwnerID := createPermissionTestMember(t, "migrate-skip-other@multica.test")

	movable := createHandlerTestAgent(t, "migrate-skip-movable", nil)
	foreign := createHandlerTestAgent(t, "migrate-skip-foreign", nil)
	alreadyThere := createHandlerTestAgent(t, "migrate-skip-already", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET owner_id = $1 WHERE id = $2`, callerID, movable); err != nil {
		t.Fatalf("assign movable owner: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET owner_id = $1 WHERE id = $2`, otherOwnerID, foreign); err != nil {
		t.Fatalf("assign foreign owner: %v", err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent SET owner_id = $1, runtime_id = $2 WHERE id = $3`,
		callerID, target, alreadyThere); err != nil {
		t.Fatalf("assign already-on-target agent: %v", err)
	}
	missing := uuid.NewString()

	w := migrateRequest(t, callerID, target, map[string]any{
		"agent_ids": []string{movable, foreign, alreadyThere, missing},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeMigrateResponse(t, w)

	migrated := map[string]bool{}
	for _, m := range resp.Migrated {
		migrated[m.AgentID] = true
	}
	if len(resp.Migrated) != 2 || !migrated[movable] || !migrated[alreadyThere] {
		t.Fatalf("expected the caller's own agent AND the already-on-target agent applied, got %+v", resp.Migrated)
	}
	reasons := map[string]string{}
	for _, s := range resp.Skipped {
		reasons[s.AgentID] = s.Reason
	}
	if len(resp.Skipped) != 2 {
		t.Fatalf("expected exactly 2 skips, got %+v", resp.Skipped)
	}
	if reasons[foreign] != migrateSkipForbidden {
		t.Errorf("expected %q for another member's agent, got %q", migrateSkipForbidden, reasons[foreign])
	}
	if reasons[missing] != migrateSkipNotFound {
		t.Errorf("expected %q for an unknown id, got %q", migrateSkipNotFound, reasons[missing])
	}

	// The already-on-target agent stays on the target (idempotent), and the
	// skipped agent must be untouched, not half-migrated.
	if got := agentRuntimeIDOf(t, alreadyThere); got != target {
		t.Errorf("in-place agent runtime = %s, want %s", got, target)
	}
	if got := agentRuntimeIDOf(t, foreign); got == target {
		t.Error("a forbidden agent must not be moved")
	}
}

// TestMigrateAgentsToRuntime_InPlaceModelUpdate covers the bulk-model-change
// use of the endpoint: an agent already on the target runtime is updated in
// place with the request's replacement model, and its queued tasks are NOT
// re-pointed (they already carry the target) — neither in the real run's
// tasks_migrated nor in the dry run's projection, which regressed once when
// CountAgentTasksByMigrationGroup lacked the IS DISTINCT FROM guard.
func TestMigrateAgentsToRuntime_InPlaceModelUpdate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	target := createMigrationTestRuntime(t, "migrate-inplace-target", "public", testUserID)
	agentID := createHandlerTestAgent(t, "migrate-inplace-agent", nil)
	if _, err := testPool.Exec(ctx, `
		UPDATE agent SET runtime_id = $1, model = 'old-model' WHERE id = $2
	`, target, agentID); err != nil {
		t.Fatalf("bind agent to target: %v", err)
	}
	queued := seedTaskWithStatus(t, agentID, target, "queued")

	// Dry run first: the queued task already lives on the target, so the
	// projection must not promise to move it.
	w := migrateRequest(t, testUserID, target, map[string]any{
		"agent_ids": []string{agentID},
		"dry_run":   true,
		"model":     "new-model",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("dry run: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if resp := decodeMigrateResponse(t, w); resp.TasksMigrated != 0 {
		t.Errorf("dry run promised %d task moves for tasks already on the target, want 0", resp.TasksMigrated)
	}

	w = migrateRequest(t, testUserID, target, map[string]any{
		"agent_ids": []string{agentID},
		"model":     "new-model",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeMigrateResponse(t, w)

	if len(resp.Migrated) != 1 || resp.Migrated[0].AgentID != agentID {
		t.Fatalf("expected the in-place agent applied, got %+v", resp.Migrated)
	}
	if resp.TasksMigrated != 0 {
		t.Errorf("tasks already on the target must not count as migrated, got %d", resp.TasksMigrated)
	}

	var model string
	if err := testPool.QueryRow(ctx, `SELECT model FROM agent WHERE id = $1`, agentID).Scan(&model); err != nil {
		t.Fatalf("read model: %v", err)
	}
	if model != "new-model" {
		t.Errorf("model = %q, want %q", model, "new-model")
	}
	if got := agentRuntimeIDOf(t, agentID); got != target {
		t.Errorf("agent runtime = %s, want %s (unchanged)", got, target)
	}
	if got := taskRuntimeID(t, queued); got != target {
		t.Errorf("queued task runtime = %s, want %s (untouched)", got, target)
	}
}

// TestMigrateAgentsToRuntime_ReplacementValidation pins the 400 contract for
// the optional replacement settings: thinking/tier are model-native, the
// combination with clear_model_settings=false is contradictory, and non-empty
// enum values are checked against the target provider (the test provider has
// no thinking enum, so any value is literal-invalid).
func TestMigrateAgentsToRuntime_ReplacementValidation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	target := createMigrationTestRuntime(t, "migrate-validate-target", "public", testUserID)
	agentID := createHandlerTestAgent(t, "migrate-validate-agent", nil)

	for name, body := range map[string]map[string]any{
		"thinking without model": {
			"agent_ids":      []string{agentID},
			"thinking_level": "high",
		},
		"model with clear_model_settings=false": {
			"agent_ids":            []string{agentID},
			"model":                "some-model",
			"clear_model_settings": false,
		},
		"thinking invalid for provider": {
			"agent_ids":      []string{agentID},
			"model":          "some-model",
			"thinking_level": "high",
		},
		"service tier on non-codex provider": {
			"agent_ids":    []string{agentID},
			"model":        "some-model",
			"service_tier": "priority",
		},
	} {
		if w := migrateRequest(t, testUserID, target, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", name, w.Code, w.Body.String())
		}
	}

	if got := agentRuntimeIDOf(t, agentID); got == target {
		t.Error("a rejected request must not move the agent")
	}
}

// TestMigrateAgentsToRuntime_CrossWorkspaceAgentIsNotFound checks the
// non-disclosure rule: an agent in a workspace the caller is not acting in is
// reported exactly like an id that never existed, and is never written.
func TestMigrateAgentsToRuntime_CrossWorkspaceAgentIsNotFound(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	target := createMigrationTestRuntime(t, "migrate-xws-target", "public", testUserID)

	var otherWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Other WS', 'migrate-xws', '', 'XWS')
		RETURNING id
	`).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})

	var otherRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, NULL, 'other-ws-runtime', 'cloud', 'handler_test_runtime', 'online', 'other', '{}'::jsonb, now())
		RETURNING id
	`, otherWorkspaceID).Scan(&otherRuntimeID); err != nil {
		t.Fatalf("create other workspace runtime: %v", err)
	}
	var otherAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
			visibility, permission_mode, max_concurrent_tasks, owner_id)
		VALUES ($1, 'other-ws-agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 'public_to', 1, $3)
		RETURNING id
	`, otherWorkspaceID, otherRuntimeID, testUserID).Scan(&otherAgentID); err != nil {
		t.Fatalf("create other workspace agent: %v", err)
	}

	w := migrateRequest(t, testUserID, target, map[string]any{"agent_ids": []string{otherAgentID}})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeMigrateResponse(t, w)

	if len(resp.Migrated) != 0 {
		t.Fatalf("a cross-workspace agent must never be migrated, got %+v", resp.Migrated)
	}
	if len(resp.Skipped) != 1 || resp.Skipped[0].Reason != migrateSkipNotFound {
		t.Fatalf("expected a not_found skip, got %+v", resp.Skipped)
	}
	if resp.Skipped[0].Name != "" {
		t.Error("a not_found skip must not leak the agent name")
	}
	if got := agentRuntimeIDOf(t, otherAgentID); got != otherRuntimeID {
		t.Errorf("cross-workspace agent runtime changed: %s", got)
	}
}

// TestMigrateAgentsToRuntime_PrivateTargetForbidden mirrors the single-agent
// gate in UpdateAgent: a private runtime accepts agents only from its owner or
// a workspace admin, and the refusal is a hard 403 rather than a per-agent skip
// because the target — not the selection — is the problem.
func TestMigrateAgentsToRuntime_PrivateTargetForbidden(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeOwnerID := createPermissionTestMember(t, "migrate-private-owner@multica.test")
	callerID := createPermissionTestMember(t, "migrate-private-caller@multica.test")
	target := createMigrationTestRuntime(t, "migrate-private-target", "private", runtimeOwnerID)
	agentID := createHandlerTestAgent(t, "migrate-private-agent", nil)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET owner_id = $1 WHERE id = $2`, callerID, agentID); err != nil {
		t.Fatalf("assign agent owner: %v", err)
	}

	w := migrateRequest(t, callerID, target, map[string]any{"agent_ids": []string{agentID}})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a private target runtime, got %d: %s", w.Code, w.Body.String())
	}
	if got := agentRuntimeIDOf(t, agentID); got == target {
		t.Error("agent must not be moved onto a private runtime the caller may not use")
	}
}

// TestMigrateAgentsToRuntime_StalePlanConflict covers the Runtime detail entry
// point, where the user confirms a set the page rendered earlier. If that set
// moved in the meantime the server refuses with the latest snapshot instead of
// migrating agents the user never saw — same contract as the runtime cascade
// delete's expected_active_agent_ids.
func TestMigrateAgentsToRuntime_StalePlanConflict(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	source := createMigrationTestRuntime(t, "migrate-stale-source", "public", testUserID)
	target := createMigrationTestRuntime(t, "migrate-stale-target", "public", testUserID)
	confirmed := createHandlerTestAgent(t, "migrate-stale-confirmed", nil)
	appeared := createHandlerTestAgent(t, "migrate-stale-appeared", nil)
	for _, id := range []string{confirmed, appeared} {
		if _, err := testPool.Exec(context.Background(),
			`UPDATE agent SET runtime_id = $1 WHERE id = $2`, source, id); err != nil {
			t.Fatalf("bind agent to source runtime: %v", err)
		}
	}

	// The dialog only saw `confirmed`; `appeared` landed on the runtime after.
	w := migrateRequest(t, testUserID, target, map[string]any{
		"agent_ids":                  []string{confirmed},
		"expected_source_runtime_id": source,
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for a changed plan, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode conflict body: %v", err)
	}
	if body["code"] != "runtime_migration_plan_changed" {
		t.Errorf("expected runtime_migration_plan_changed, got %v", body["code"])
	}
	active, _ := body["active_agents"].([]any)
	if len(active) != 2 {
		t.Errorf("conflict must carry the latest agent set, got %d entries", len(active))
	}
	if got := agentRuntimeIDOf(t, confirmed); got != source {
		t.Error("a refused migration must not move anything")
	}

	// Confirming the current set succeeds.
	w = migrateRequest(t, testUserID, target, map[string]any{
		"agent_ids":                  []string{confirmed, appeared},
		"expected_source_runtime_id": source,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after confirming the live set, got %d: %s", w.Code, w.Body.String())
	}
	if got := agentRuntimeIDOf(t, confirmed); got != target {
		t.Errorf("confirmed agent runtime = %s, want %s", got, target)
	}
}

// TestMigrateAgentsToRuntime_ForeignSourceRuntimeLeaksNothing is the MUL-5758
// review regression: expected_source_runtime_id arrives in the REQUEST BODY,
// so it has had none of the authorization the path runtime got.
//
// Before the fix the source was locked and its agents listed by runtime id
// alone, so a caller legitimately migrating inside their own workspace could
// name a runtime belonging to a different workspace and read that runtime's
// active agents — ids and names — straight out of the stale-plan 409 body.
// The explicit-agent-id path never leaked existence like that, and this one
// must not either.
func TestMigrateAgentsToRuntime_ForeignSourceRuntimeLeaksNothing(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	target := createMigrationTestRuntime(t, "migrate-foreign-target", "public", testUserID)
	ownAgent := createHandlerTestAgent(t, "migrate-foreign-own", nil)

	// A separate workspace the caller is not acting in, holding a runtime with
	// an active agent whose name is the thing that must not come back.
	var otherWorkspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Foreign WS', 'migrate-foreign-ws', '', 'FGN')
		RETURNING id
	`).Scan(&otherWorkspaceID); err != nil {
		t.Fatalf("create foreign workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, otherWorkspaceID)
	})

	var foreignRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, NULL, 'foreign-runtime', 'cloud', 'handler_test_runtime', 'online', 'foreign', '{}'::jsonb, now())
		RETURNING id
	`, otherWorkspaceID).Scan(&foreignRuntimeID); err != nil {
		t.Fatalf("create foreign runtime: %v", err)
	}
	const secretAgentName = "top-secret-foreign-agent"
	var foreignAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
			visibility, permission_mode, max_concurrent_tasks, owner_id)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 'public_to', 1, $4)
		RETURNING id
	`, otherWorkspaceID, secretAgentName, foreignRuntimeID, testUserID).Scan(&foreignAgentID); err != nil {
		t.Fatalf("create foreign agent: %v", err)
	}

	w := migrateRequest(t, testUserID, target, map[string]any{
		"agent_ids":                  []string{ownAgent},
		"expected_source_runtime_id": foreignRuntimeID,
	})

	// Rejected as bad input, not as a conflict: a 409 would carry the foreign
	// agent set, and any wording that distinguished "another workspace's
	// runtime" from "no such runtime" would itself confirm the id exists.
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a foreign expected_source_runtime_id, got %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, secretAgentName) || strings.Contains(body, foreignAgentID) {
		t.Fatalf("response leaked the foreign workspace's agents: %s", body)
	}

	// And nothing was written on either side.
	if got := agentRuntimeIDOf(t, foreignAgentID); got != foreignRuntimeID {
		t.Errorf("foreign agent runtime changed: %s", got)
	}
	if got := agentRuntimeIDOf(t, ownAgent); got == target {
		t.Error("a rejected request must not migrate anything")
	}

	// The same wording must come back for an id that exists nowhere, so the
	// two cases stay indistinguishable.
	unknown := migrateRequest(t, testUserID, target, map[string]any{
		"agent_ids":                  []string{ownAgent},
		"expected_source_runtime_id": uuid.NewString(),
	})
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown expected_source_runtime_id, got %d: %s", unknown.Code, unknown.Body.String())
	}
	if unknown.Body.String() != body {
		t.Errorf("a foreign runtime and a nonexistent one must be indistinguishable:\n foreign: %s\n unknown: %s", body, unknown.Body.String())
	}
}

// TestParseBulkAgentIDs covers the shared input guard both bulk endpoints use:
// non-empty, all-UUID, de-duplicated, bounded.
func TestParseBulkAgentIDs(t *testing.T) {
	a := uuid.NewString()
	b := uuid.NewString()

	if _, ok := parseBulkAgentIDs(httptest.NewRecorder(), nil, 10); ok {
		t.Error("an empty id list must be rejected")
	}
	if _, ok := parseBulkAgentIDs(httptest.NewRecorder(), []string{a, b, a}, 2); ok {
		t.Error("the limit applies to the raw list, before de-duplication")
	}
	if _, ok := parseBulkAgentIDs(httptest.NewRecorder(), []string{"not-a-uuid"}, 10); ok {
		t.Error("a malformed id must be rejected")
	}
	got, ok := parseBulkAgentIDs(httptest.NewRecorder(), []string{a, b, a}, 10)
	if !ok {
		t.Fatal("a valid list must be accepted")
	}
	if len(got) != 2 || uuidToString(got[0]) != a || uuidToString(got[1]) != b {
		t.Fatalf("expected de-duplication preserving order, got %v", got)
	}
}

// createHiddenAgentOnRuntime seeds a private agent owned by someone other than
// the caller — invisible in that caller's agent list — bound to `runtimeID`
// and carrying an mcp_config secret.
func createHiddenAgentOnRuntime(t *testing.T, name, runtimeID, ownerID string) string {
	t.Helper()

	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks,
			owner_id, instructions, custom_env, custom_args, mcp_config
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 'private', 1, $4,
			'secret instructions', '{}'::jsonb, '[]'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, name, runtimeID, ownerID,
		[]byte(`{"servers":{"vault":{"token":"super-secret-token"}}}`)).Scan(&agentID); err != nil {
		t.Fatalf("create hidden agent %s: %v", name, err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})
	return agentID
}

// TestMigrateAgentsToRuntime_StalePlanDoesNotLeakHiddenAgents is the
// regression for the MUL-5758 security review.
//
// The stale-plan 409 used to echo `agentToResponse` for every active agent on
// the source runtime, bypassing both of ListAgents' guards: the per-member
// visibility filter and the mcp_config redaction. Any member who could use a
// public target runtime could therefore submit a deliberately mismatched
// agent_ids, trigger the conflict, and read other members' private agents
// including their MCP credentials.
//
// Two properties are pinned here: the echoed payload never names an agent the
// caller cannot see, and it never carries agent secrets in any form.
func TestMigrateAgentsToRuntime_StalePlanDoesNotLeakHiddenAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	source := createMigrationTestRuntime(t, "leak-source", "public", testUserID)
	target := createMigrationTestRuntime(t, "leak-target", "public", testUserID)
	callerID := createPermissionTestMember(t, "migrate-leak-caller@multica.test")
	otherID := createPermissionTestMember(t, "migrate-leak-other@multica.test")

	// The caller's own agent, plus a private agent belonging to someone else —
	// both on the shared source runtime.
	mine := createHandlerTestAgent(t, "leak-visible-agent", nil)
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET owner_id = $1, runtime_id = $2 WHERE id = $3`,
		callerID, source, mine); err != nil {
		t.Fatalf("bind caller agent: %v", err)
	}
	hidden := createHiddenAgentOnRuntime(t, "leak-hidden-agent", source, otherID)

	// Force the conflict first, while the source runtime still holds both
	// agents, and inspect exactly what the 409 discloses.
	stranger := createHandlerTestAgent(t, "leak-stranger-agent", nil)
	if _, err := testPool.Exec(ctx,
		`UPDATE agent SET owner_id = $1 WHERE id = $2`, callerID, stranger); err != nil {
		t.Fatalf("assign stranger owner: %v", err)
	}
	w := migrateRequest(t, callerID, target, map[string]any{
		"agent_ids":                  []string{stranger},
		"expected_source_runtime_id": source,
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if strings.Contains(body, hidden) || strings.Contains(body, "leak-hidden-agent") {
		t.Errorf("409 must not disclose an agent the caller cannot see; body: %s", body)
	}
	for _, secret := range []string{"super-secret-token", "mcp_config", "instructions", "custom_env"} {
		if strings.Contains(body, secret) {
			t.Errorf("409 must not carry %q; body: %s", secret, body)
		}
	}

	var payload struct {
		Code         string `json:"code"`
		ActiveAgents []struct {
			AgentID string `json:"agent_id"`
			Name    string `json:"name"`
		} `json:"active_agents"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode 409: %v", err)
	}
	if payload.Code != "runtime_migration_plan_changed" {
		t.Errorf("code = %q", payload.Code)
	}
	if len(payload.ActiveAgents) != 1 || payload.ActiveAgents[0].AgentID != mine {
		t.Fatalf("409 must list only the caller-visible agent, got %+v", payload.ActiveAgents)
	}

	// And the flip side of the same filter: confirming exactly the visible set
	// must succeed. Comparing against the raw runtime set instead would leave
	// this member unable to ever migrate off a runtime that also hosts an
	// agent hidden from them.
	w = migrateRequest(t, callerID, target, map[string]any{
		"agent_ids":                  []string{mine},
		"expected_source_runtime_id": source,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("visible-set confirmation must succeed, got %d: %s", w.Code, w.Body.String())
	}
	if got := agentRuntimeIDOf(t, hidden); got != source {
		t.Errorf("hidden agent must stay on the source runtime, got %s", got)
	}
}

// TestMigrateAgentsToRuntime_HiddenAgentIsNotFound pins the non-disclosure
// rule for the bulk skip list: an agent inside the workspace but invisible to
// this caller is reported exactly like an id that never existed — no name, no
// `forbidden` reason that would confirm it exists.
func TestMigrateAgentsToRuntime_HiddenAgentIsNotFound(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	source := createMigrationTestRuntime(t, "hidden-skip-source", "public", testUserID)
	target := createMigrationTestRuntime(t, "hidden-skip-target", "public", testUserID)
	callerID := createPermissionTestMember(t, "migrate-hidden-caller@multica.test")
	otherID := createPermissionTestMember(t, "migrate-hidden-other@multica.test")
	hidden := createHiddenAgentOnRuntime(t, "hidden-skip-agent", source, otherID)

	w := migrateRequest(t, callerID, target, map[string]any{"agent_ids": []string{hidden}})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeMigrateResponse(t, w)

	if len(resp.Skipped) != 1 {
		t.Fatalf("expected 1 skip, got %+v", resp.Skipped)
	}
	if resp.Skipped[0].Reason != migrateSkipNotFound {
		t.Errorf("reason = %q, want %q", resp.Skipped[0].Reason, migrateSkipNotFound)
	}
	if resp.Skipped[0].Name != "" {
		t.Errorf("a hidden agent's name must not be disclosed, got %q", resp.Skipped[0].Name)
	}
	if got := agentRuntimeIDOf(t, hidden); got != source {
		t.Errorf("hidden agent must not be written, runtime = %s", got)
	}
}
