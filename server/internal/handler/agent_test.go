package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type txStarterFunc func(ctx context.Context) (pgx.Tx, error)

func (f txStarterFunc) Begin(ctx context.Context) (pgx.Tx, error) {
	return f(ctx)
}

// TestListWorkspaceAgentTaskSnapshot covers the agent presence snapshot endpoint:
// every active task (queued/dispatched/running) PLUS each agent's most recent
// OUTCOME task (completed/failed only). Cancelled tasks are excluded by design
// from the outcome half — they're a procedural signal, not an outcome, and
// must NOT mask a prior failure.
//
// The fixtures cover every branch the SQL must classify:
//   - actives are always returned, no dedup
//   - outcomes are deduped to "latest per agent" by completed_at
//   - the OLD 2-minute window must be irrelevant (a 5-minute-old failure is
//     still returned if it's the latest outcome)
//   - cancelled rows are NEVER returned, even when they are temporally newer
//     than a failure — this is what keeps the failed signal sticky after the
//     user cancels their queued retry
func TestListWorkspaceAgentTaskSnapshot(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	// Three agents so we can verify per-agent semantics independently.
	agentA := createHandlerTestAgent(t, "snapshot-agent-a", []byte(`{}`))
	agentB := createHandlerTestAgent(t, "snapshot-agent-b", []byte(`{}`))
	agentC := createHandlerTestAgent(t, "snapshot-agent-c", []byte(`{}`))

	type taskFixture struct {
		agentID     string
		status      string
		completedAt string // SQL expression; "" for NULL
		label       string
	}
	fixtures := []taskFixture{
		// Agent A — actives + a newer completed supersedes an older failed.
		{agentA, "queued", "", "A.queued"},
		{agentA, "dispatched", "", "A.dispatched"},
		{agentA, "running", "", "A.running"},
		{agentA, "failed", "now() - interval '10 minutes'", "A.old_failed"},
		{agentA, "completed", "now() - interval '30 seconds'", "A.latest_completed"},

		// Agent B — old failure with no later outcome stays visible (no
		// time window).
		{agentB, "failed", "now() - interval '5 minutes'", "B.stale_failed_kept"},

		// Agent C — failure followed by a NEWER cancelled. The cancelled
		// must be skipped by the SQL filter so the failure remains visible.
		// This is the scenario where a user fails, then cancels their
		// queued retry to debug.
		{agentC, "failed", "now() - interval '5 minutes'", "C.failure"},
		{agentC, "cancelled", "now() - interval '30 seconds'", "C.newer_cancelled_must_be_ignored"},
	}

	insertedIDs := make([]string, 0, len(fixtures))
	for _, f := range fixtures {
		var id string
		var query string
		if f.completedAt == "" {
			query = `INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
			         VALUES ($1, $2, $3, 0) RETURNING id`
		} else {
			query = `INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, completed_at)
			         VALUES ($1, $2, $3, 0, ` + f.completedAt + `) RETURNING id`
		}
		if err := testPool.QueryRow(ctx, query, f.agentID, testRuntimeID, f.status).Scan(&id); err != nil {
			t.Fatalf("insert %s: %v", f.label, err)
		}
		insertedIDs = append(insertedIDs, id)
	}
	t.Cleanup(func() {
		for _, id := range insertedIDs {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, id)
		}
	})

	w := httptest.NewRecorder()
	req := newRequest(http.MethodGet, "/api/agent-task-snapshot", nil)
	testHandler.ListWorkspaceAgentTaskSnapshot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListWorkspaceAgentTaskSnapshot: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var tasks []AgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Per-agent breakdown so leftover tasks from other tests in this package
	// don't pollute the assertions.
	type key struct{ agent, status string }
	counts := map[key]int{}
	for _, task := range tasks {
		if task.AgentID != agentA && task.AgentID != agentB && task.AgentID != agentC {
			continue
		}
		counts[key{task.AgentID, task.Status}]++
	}

	wantCounts := map[key]int{
		// Agent A: 3 actives + the latest outcome (completed). The older
		// failed must be excluded by DISTINCT ON.
		{agentA, "queued"}:     1,
		{agentA, "dispatched"}: 1,
		{agentA, "running"}:    1,
		{agentA, "completed"}:  1,
		// Agent B: just the failed outcome.
		{agentB, "failed"}: 1,
		// Agent C: the failed outcome must survive the temporally newer
		// cancellation — that's the whole point of excluding cancelled
		// from the outcome half.
		{agentC, "failed"}: 1,
	}
	for k, expected := range wantCounts {
		if got := counts[k]; got != expected {
			t.Errorf("agent=%s status=%s: expected %d, got %d", k.agent, k.status, expected, got)
		}
	}

	// The OLD failed terminal on agent A must be excluded.
	if counts[key{agentA, "failed"}] != 0 {
		t.Errorf("agent A old failed must be superseded by newer completed; got %d", counts[key{agentA, "failed"}])
	}

	// No cancelled row may ever appear in the snapshot — they're filtered at
	// SQL level so the front-end's "cancel doesn't mask failure" rule lands
	// without any front-end logic.
	for _, agentID := range []string{agentA, agentB, agentC} {
		if counts[key{agentID, "cancelled"}] != 0 {
			t.Errorf("agent %s: cancelled rows must be excluded from snapshot; got %d",
				agentID, counts[key{agentID, "cancelled"}])
		}
	}
}

func TestCreateAgent_RejectsDuplicateName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Clean up any agents created by this test.
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, "duplicate-name-test-agent",
		)
	})

	body := map[string]any{
		"name":                 "duplicate-name-test-agent",
		"description":          "first description",
		"runtime_id":           testRuntimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}

	// First call — creates the agent.
	w1 := httptest.NewRecorder()
	testHandler.CreateAgent(w1, newRequest(http.MethodPost, "/api/agents", body))
	if w1.Code != http.StatusCreated {
		t.Fatalf("first CreateAgent: expected 201, got %d: %s", w1.Code, w1.Body.String())
	}
	var resp1 map[string]any
	if err := json.NewDecoder(w1.Body).Decode(&resp1); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	agentID1, _ := resp1["id"].(string)
	if agentID1 == "" {
		t.Fatalf("first CreateAgent: no id in response: %v", resp1)
	}

	// Second call — same name must be rejected with 409 Conflict.
	// The unique constraint prevents silent duplicates; the UI shows a clear error.
	body["description"] = "updated description"
	w2 := httptest.NewRecorder()
	testHandler.CreateAgent(w2, newRequest(http.MethodPost, "/api/agents", body))
	if w2.Code != http.StatusConflict {
		t.Fatalf("second CreateAgent with duplicate name: expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestBulkUpdateAgents_SelectedAgentsAndFields(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID, sourceRuntimeID, targetRuntimeID := createSetRuntimeWorkspace(t)
	activeA := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-selected-a", false)
	activeB := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-selected-b", false)
	activeC := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-selected-c", false)
	archived := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-selected-archived", true)

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/agents/bulk-update", map[string]any{
		"agent_ids":            []string{activeA, activeC},
		"runtime_id":           targetRuntimeID,
		"model":                " gpt-5.5 ",
		"max_concurrent_tasks": 7,
		"custom_args_patch": []map[string]string{
			{"action": "add", "value": "--foo"},
			{"action": "add", "value": "bar"},
		},
	}), "id", workspaceID)
	testHandler.BulkUpdateAgents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BulkUpdateAgents selected: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp BulkUpdateAgentsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Updated != 2 {
		t.Fatalf("updated = %d, want 2", resp.Updated)
	}

	for _, agentID := range []string{activeA, activeC} {
		var runtimeID, model, customArgs string
		var maxConcurrent int
		if err := testPool.QueryRow(context.Background(), `SELECT runtime_id::text, model, max_concurrent_tasks, custom_args::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID, &model, &maxConcurrent, &customArgs); err != nil {
			t.Fatalf("read selected agent %s: %v", agentID, err)
		}
		if runtimeID != targetRuntimeID || model != "gpt-5.5" || maxConcurrent != 7 || customArgs != `["--foo", "bar"]` {
			t.Fatalf("selected agent %s changed incorrectly: runtime=%s model=%q max=%d args=%s", agentID, runtimeID, model, maxConcurrent, customArgs)
		}
	}

	for _, agentID := range []string{activeB, archived} {
		var runtimeID, model string
		var maxConcurrent int
		if err := testPool.QueryRow(context.Background(), `SELECT runtime_id::text, model, max_concurrent_tasks FROM agent WHERE id = $1`, agentID).Scan(&runtimeID, &model, &maxConcurrent); err != nil {
			t.Fatalf("read untouched agent %s: %v", agentID, err)
		}
		if runtimeID != sourceRuntimeID || model != "old-model" || maxConcurrent != 1 {
			t.Fatalf("untouched agent %s changed: runtime=%s model=%q max=%d", agentID, runtimeID, model, maxConcurrent)
		}
	}
}

func TestBulkUpdateAgents_ClearModelForSelectedAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID, sourceRuntimeID, _ := createSetRuntimeWorkspace(t)
	activeA := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-clear-model-a", false)
	activeB := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-clear-model-b", false)

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/agents/bulk-update", map[string]any{
		"agent_ids": []string{activeA},
		"model":     "",
	}), "id", workspaceID)
	testHandler.BulkUpdateAgents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BulkUpdateAgents clear model: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var cleared sql.NullString
	if err := testPool.QueryRow(context.Background(), `SELECT model FROM agent WHERE id = $1`, activeA).Scan(&cleared); err != nil {
		t.Fatalf("read cleared model: %v", err)
	}
	if cleared.Valid {
		t.Fatalf("selected agent model = %q, want NULL", cleared.String)
	}

	var untouched string
	if err := testPool.QueryRow(context.Background(), `SELECT model FROM agent WHERE id = $1`, activeB).Scan(&untouched); err != nil {
		t.Fatalf("read untouched model: %v", err)
	}
	if untouched != "old-model" {
		t.Fatalf("untouched agent model = %q, want old-model", untouched)
	}
}

func TestBulkUpdateAgents_RejectsAgentArchivedBeforeBulkTransaction(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID, sourceRuntimeID, targetRuntimeID := createSetRuntimeWorkspace(t)
	agentID := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-race-archived", false)

	origTxStarter := testHandler.TxStarter
	t.Cleanup(func() { testHandler.TxStarter = origTxStarter })
	archived := false
	testHandler.TxStarter = txStarterFunc(func(ctx context.Context) (pgx.Tx, error) {
		if !archived {
			archived = true
			if _, err := testPool.Exec(ctx, `UPDATE agent SET archived_at = now(), archived_by = $2 WHERE id = $1`, agentID, testUserID); err != nil {
				t.Fatalf("archive agent before bulk transaction: %v", err)
			}
		}
		return origTxStarter.Begin(ctx)
	})

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/agents/bulk-update", map[string]any{
		"agent_ids":  []string{agentID},
		"runtime_id": targetRuntimeID,
	}), "id", workspaceID)
	testHandler.BulkUpdateAgents(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("BulkUpdateAgents raced archive: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "is archived") {
		t.Fatalf("BulkUpdateAgents raced archive: expected archived error, got %s", w.Body.String())
	}

	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read raced archived agent: %v", err)
	}
	if runtimeID != sourceRuntimeID {
		t.Fatalf("archived agent runtime changed to %s, want %s", runtimeID, sourceRuntimeID)
	}
}

func TestBulkUpdateAgents_DeduplicatesSelectedAgentIDsAfterUUIDNormalization(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID, sourceRuntimeID, targetRuntimeID := createSetRuntimeWorkspace(t)
	agentID := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-normalized-id", false)

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/agents/bulk-update", map[string]any{
		"agent_ids":  []string{strings.ToUpper(agentID), agentID},
		"runtime_id": targetRuntimeID,
	}), "id", workspaceID)
	testHandler.BulkUpdateAgents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BulkUpdateAgents normalized ids: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp BulkUpdateAgentsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Updated != 1 {
		t.Fatalf("updated = %d, want 1", resp.Updated)
	}

	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("read normalized-id agent: %v", err)
	}
	if runtimeID != targetRuntimeID {
		t.Fatalf("agent runtime = %s, want %s (source was %s)", runtimeID, targetRuntimeID, sourceRuntimeID)
	}
}

func TestBulkUpdateAgents_PatchesCustomArgsForSelectedAgents(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID, sourceRuntimeID, _ := createSetRuntimeWorkspace(t)
	activeA := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-args-a", false)
	activeB := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-args-b", false)
	archived := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-args-archived", true)
	seeds := map[string]string{
		activeA:  `["--permission-mode", "acceptEdits", "--remove-me"]`,
		activeB:  `["--permission-mode", "--keep"]`,
		archived: `["--permission-mode", "--remove-me"]`,
	}
	for agentID, args := range seeds {
		if _, err := testPool.Exec(context.Background(), `UPDATE agent SET custom_args = $1::jsonb WHERE id = $2`, args, agentID); err != nil {
			t.Fatalf("seed custom_args for %s: %v", agentID, err)
		}
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/agents/bulk-update", map[string]any{
		"agent_ids": []string{activeA, activeB},
		"custom_args_patch": []map[string]string{
			{"action": "replace", "value": "--permission-mode", "replacement": "--mode"},
			{"action": "remove", "value": "--remove-me"},
			{"action": "add", "value": "--new"},
		},
	}), "id", workspaceID)
	testHandler.BulkUpdateAgents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BulkUpdateAgents custom_args patch: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cases := map[string]string{
		activeA:  `["--mode", "acceptEdits", "--new"]`,
		activeB:  `["--mode", "--keep", "--new"]`,
		archived: `["--permission-mode", "--remove-me"]`,
	}
	for agentID, want := range cases {
		var got string
		if err := testPool.QueryRow(context.Background(), `SELECT custom_args::text FROM agent WHERE id = $1`, agentID).Scan(&got); err != nil {
			t.Fatalf("read custom_args for %s: %v", agentID, err)
		}
		if got != want {
			t.Fatalf("custom_args for %s = %s, want %s", agentID, got, want)
		}
	}
}

func TestBulkUpdateAgents_RejectsInvalidCustomArgsPatchPayloads(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID, _, _ := createSetRuntimeWorkspace(t)
	cases := []struct {
		name      string
		body      map[string]any
		wantError string
	}{
		{
			name: "empty patch",
			body: map[string]any{
				"custom_args_patch": []map[string]string{},
			},
			wantError: "custom_args_patch must not be empty",
		},
		{
			name: "empty value",
			body: map[string]any{
				"custom_args_patch": []map[string]string{
					{"action": "add", "value": " "},
				},
			},
			wantError: "custom_args_patch contains an empty value",
		},
		{
			name: "replace missing replacement",
			body: map[string]any{
				"custom_args_patch": []map[string]string{
					{"action": "replace", "value": "--old"},
				},
			},
			wantError: "custom_args_patch replace operation requires replacement",
		},
		{
			name: "unknown action",
			body: map[string]any{
				"custom_args_patch": []map[string]string{
					{"action": "rename", "value": "--old", "replacement": "--new"},
				},
			},
			wantError: `custom_args_patch contains unknown action "rename"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/agents/bulk-update", tc.body), "id", workspaceID)
			testHandler.BulkUpdateAgents(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("BulkUpdateAgents invalid patch: expected 400, got %d: %s", w.Code, w.Body.String())
			}
			var resp map[string]string
			if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp["error"] != tc.wantError {
				t.Fatalf("error = %q, want %q", resp["error"], tc.wantError)
			}
		})
	}
}

func TestBulkUpdateAgents_PatchesEnvForAllActiveWithoutLeakingValues(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID, sourceRuntimeID, _ := createSetRuntimeWorkspace(t)
	activeA := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-env-a", false)
	activeB := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-env-b", false)
	archived := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-env-archived", true)
	for _, agentID := range []string{activeA, activeB, archived} {
		if _, err := testPool.Exec(context.Background(), `UPDATE agent SET custom_env = '{"KEEP":"old-secret","REMOVE":"remove-secret"}' WHERE id = $1`, agentID); err != nil {
			t.Fatalf("seed custom_env for %s: %v", agentID, err)
		}
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/agents/bulk-update", map[string]any{
		"env_set":    map[string]string{"KEEP": "rotated-secret", "NEW": "fresh-secret"},
		"env_remove": []string{"REMOVE"},
	}), "id", workspaceID)
	testHandler.BulkUpdateAgents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BulkUpdateAgents env patch: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "rotated-secret") || strings.Contains(w.Body.String(), "fresh-secret") {
		t.Fatalf("bulk response leaked env values: %s", w.Body.String())
	}
	var resp BulkUpdateAgentsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Updated != 2 {
		t.Fatalf("updated = %d, want 2", resp.Updated)
	}

	for _, agentID := range []string{activeA, activeB} {
		var stored string
		if err := testPool.QueryRow(context.Background(), `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
			t.Fatalf("read custom_env for %s: %v", agentID, err)
		}
		var got map[string]string
		if err := json.Unmarshal([]byte(stored), &got); err != nil {
			t.Fatalf("decode custom_env for %s: %v", agentID, err)
		}
		want := map[string]string{"KEEP": "rotated-secret", "NEW": "fresh-secret"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("custom_env for %s = %v, want %v", agentID, got, want)
		}

		var details string
		if err := testPool.QueryRow(context.Background(), `
			SELECT details::text FROM activity_log
			WHERE workspace_id = $1 AND action = 'agent_env_updated' AND details->>'agent_id' = $2
			ORDER BY created_at DESC LIMIT 1
		`, workspaceID, agentID).Scan(&details); err != nil {
			t.Fatalf("expected agent_env_updated activity row for %s: %v", agentID, err)
		}
		for _, leak := range []string{"old-secret", "remove-secret", "rotated-secret", "fresh-secret"} {
			if strings.Contains(details, leak) {
				t.Fatalf("audit details leaked value %q: %s", leak, details)
			}
		}
		for _, key := range []string{"KEEP", "NEW", "REMOVE"} {
			if !strings.Contains(details, key) {
				t.Fatalf("audit details missing key %q: %s", key, details)
			}
		}
	}

	var archivedEnv string
	if err := testPool.QueryRow(context.Background(), `SELECT custom_env::text FROM agent WHERE id = $1`, archived).Scan(&archivedEnv); err != nil {
		t.Fatalf("read archived custom_env: %v", err)
	}
	if !strings.Contains(archivedEnv, "old-secret") || !strings.Contains(archivedEnv, "remove-secret") {
		t.Fatalf("archived agent env changed: %s", archivedEnv)
	}
}

func TestBulkUpdateAgents_PatchesEnvForSelectedAgentsOnly(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID, sourceRuntimeID, _ := createSetRuntimeWorkspace(t)
	activeA := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-env-selected-a", false)
	activeB := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-env-selected-b", false)
	activeC := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-env-selected-c", false)
	archived := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-env-selected-archived", true)
	for _, agentID := range []string{activeA, activeB, activeC, archived} {
		if _, err := testPool.Exec(context.Background(), `UPDATE agent SET custom_env = '{"KEEP":"old-secret","REMOVE":"remove-secret"}' WHERE id = $1`, agentID); err != nil {
			t.Fatalf("seed custom_env for %s: %v", agentID, err)
		}
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/agents/bulk-update", map[string]any{
		"agent_ids":  []string{activeA, activeC},
		"env_set":    map[string]string{"KEEP": "rotated-secret", "NEW": "fresh-secret"},
		"env_remove": []string{"REMOVE"},
	}), "id", workspaceID)
	testHandler.BulkUpdateAgents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BulkUpdateAgents selected env patch: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp BulkUpdateAgentsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Updated != 2 {
		t.Fatalf("updated = %d, want 2", resp.Updated)
	}

	for _, agentID := range []string{activeA, activeC} {
		var stored string
		if err := testPool.QueryRow(context.Background(), `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
			t.Fatalf("read selected custom_env for %s: %v", agentID, err)
		}
		var got map[string]string
		if err := json.Unmarshal([]byte(stored), &got); err != nil {
			t.Fatalf("decode selected custom_env for %s: %v", agentID, err)
		}
		want := map[string]string{"KEEP": "rotated-secret", "NEW": "fresh-secret"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("selected custom_env for %s = %v, want %v", agentID, got, want)
		}
	}

	for _, agentID := range []string{activeB, archived} {
		var stored string
		if err := testPool.QueryRow(context.Background(), `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
			t.Fatalf("read untouched custom_env for %s: %v", agentID, err)
		}
		var got map[string]string
		if err := json.Unmarshal([]byte(stored), &got); err != nil {
			t.Fatalf("decode untouched custom_env for %s: %v", agentID, err)
		}
		want := map[string]string{"KEEP": "old-secret", "REMOVE": "remove-secret"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("untouched custom_env for %s = %v, want %v", agentID, got, want)
		}
	}
}

func TestListBulkAgentEnvKeys_ReturnsKeysOnlyForTargets(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID, sourceRuntimeID, _ := createSetRuntimeWorkspace(t)
	activeA := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-env-keys-a", false)
	activeB := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-env-keys-b", false)
	activeC := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-env-keys-c", false)
	archived := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-env-keys-archived", true)

	seeds := map[string]string{
		activeA:  `{"API_KEY":"secret-a"," API_KEY ":"secret-a-duplicate","SHARED":"shared-a"}`,
		activeB:  `{"API_KEY":"secret-b","ONLY_B":"secret-b"}`,
		activeC:  `{"ONLY_C":"secret-c"}`,
		archived: `{"ARCHIVED_ONLY":"archived-secret"}`,
	}
	for agentID, env := range seeds {
		if _, err := testPool.Exec(context.Background(), `UPDATE agent SET custom_env = $1::jsonb WHERE id = $2`, env, agentID); err != nil {
			t.Fatalf("seed custom_env for %s: %v", agentID, err)
		}
	}

	var revealCountBefore int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM activity_log WHERE workspace_id = $1 AND action = 'agent_env_revealed'`, workspaceID).Scan(&revealCountBefore); err != nil {
		t.Fatalf("count reveal activity before: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/agents/env-keys", map[string]any{
		"agent_ids": []string{activeA, activeB},
	}), "id", workspaceID)
	testHandler.ListBulkAgentEnvKeys(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListBulkAgentEnvKeys selected: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("env key summary leaked values: %s", w.Body.String())
	}
	var selectedResp BulkAgentEnvKeysResponse
	if err := json.NewDecoder(w.Body).Decode(&selectedResp); err != nil {
		t.Fatalf("decode selected response: %v", err)
	}
	selectedCounts := map[string]int{}
	for _, item := range selectedResp.Keys {
		selectedCounts[item.Key] = item.AgentCount
	}
	wantSelected := map[string]int{"API_KEY": 2, "ONLY_B": 1, "SHARED": 1}
	if !reflect.DeepEqual(selectedCounts, wantSelected) {
		t.Fatalf("selected key counts = %v, want %v", selectedCounts, wantSelected)
	}

	w = httptest.NewRecorder()
	req = withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/agents/env-keys", map[string]any{}), "id", workspaceID)
	testHandler.ListBulkAgentEnvKeys(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListBulkAgentEnvKeys all active: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var allResp BulkAgentEnvKeysResponse
	if err := json.NewDecoder(w.Body).Decode(&allResp); err != nil {
		t.Fatalf("decode all response: %v", err)
	}
	allCounts := map[string]int{}
	for _, item := range allResp.Keys {
		allCounts[item.Key] = item.AgentCount
	}
	if allCounts["ONLY_C"] != 1 {
		t.Fatalf("all active key counts missing ONLY_C: %v", allCounts)
	}
	if _, ok := allCounts["ARCHIVED_ONLY"]; ok {
		t.Fatalf("archived key leaked into active summary: %v", allCounts)
	}

	var revealCountAfter int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM activity_log WHERE workspace_id = $1 AND action = 'agent_env_revealed'`, workspaceID).Scan(&revealCountAfter); err != nil {
		t.Fatalf("count reveal activity after: %v", err)
	}
	if revealCountAfter != revealCountBefore {
		t.Fatalf("keys-only summary wrote reveal audit rows: before=%d after=%d", revealCountBefore, revealCountAfter)
	}
}

func TestListBulkAgentEnvKeys_RejectsEmptySelectedList(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID, _, _ := createSetRuntimeWorkspace(t)
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+"/agents/env-keys", map[string]any{
		"agent_ids": []string{},
	}), "id", workspaceID)
	testHandler.ListBulkAgentEnvKeys(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("ListBulkAgentEnvKeys empty selected list: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "agent_ids must not be empty") {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
}

// TestBulkUpdateAgents_RejectsAgentActor extends the MUL-2600 lateral-movement
// guard to the bulk surfaces: an authenticated agent process
// (X-Actor-Source=task_token) must not bulk-edit agent configuration or
// enumerate env keys, even though the host member is a workspace owner. A
// rejected bulk-update must leave every agent untouched.
func TestBulkUpdateAgents_RejectsAgentActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	workspaceID, sourceRuntimeID, _ := createSetRuntimeWorkspace(t)
	agentID := createSetRuntimeAgent(t, workspaceID, sourceRuntimeID, "bulk-actor-guard", false)
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET model = 'old-model' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("seed agent model: %v", err)
	}

	cases := []struct {
		name string
		path string
		fn   func(http.ResponseWriter, *http.Request)
		body any
	}{
		{"bulk-update", "/agents/bulk-update", testHandler.BulkUpdateAgents, map[string]any{"model": "new-model"}},
		{"env-keys", "/agents/env-keys", testHandler.ListBulkAgentEnvKeys, map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+workspaceID+tc.path, tc.body), "id", workspaceID)
			req.Header.Set("X-Actor-Source", "task_token")
			req.Header.Del("X-Agent-ID")
			req.Header.Del("X-Task-ID")
			w := httptest.NewRecorder()
			tc.fn(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("expected 403 from agent actor, got %d: %s", w.Code, w.Body.String())
			}
		})
	}

	var model string
	if err := testPool.QueryRow(context.Background(), `SELECT coalesce(model, '') FROM agent WHERE id = $1`, agentID).Scan(&model); err != nil {
		t.Fatalf("read agent model: %v", err)
	}
	if model != "old-model" {
		t.Fatalf("rejected bulk-update mutated agent model: got %q, want \"old-model\"", model)
	}
}

func TestWorkspaceAlwaysRedactSecrets(t *testing.T) {
	tests := []struct {
		name     string
		settings []byte
		want     bool
	}{
		{"nil settings", nil, false},
		{"empty settings", []byte(`{}`), false},
		{"false", []byte(`{"always_redact_env": false}`), false},
		{"true", []byte(`{"always_redact_env": true}`), true},
		{"invalid json", []byte(`not json`), false},
		{"other fields only", []byte(`{"theme": "dark"}`), false},
		{"true among other fields", []byte(`{"theme": "dark", "always_redact_env": true}`), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := workspaceAlwaysRedactSecrets(tt.settings); got != tt.want {
				t.Errorf("workspaceAlwaysRedactSecrets(%q) = %v, want %v", tt.settings, got, tt.want)
			}
		})
	}
}

func TestUpdateAgent_EmptyModelClearsToRuntimeDefault(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "single-clear-model", nil)
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET model = 'old-model' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("seed agent model: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"model": "",
	}), "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent clear model: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var modelIsNull bool
	if err := testPool.QueryRow(context.Background(), `SELECT model IS NULL FROM agent WHERE id = $1`, agentID).Scan(&modelIsNull); err != nil {
		t.Fatalf("read agent model: %v", err)
	}
	if !modelIsNull {
		t.Fatalf("model was not cleared to NULL")
	}
}

func createSetRuntimeWorkspace(t *testing.T) (workspaceID, sourceRuntimeID, targetRuntimeID string) {
	t.Helper()
	ctx := context.Background()
	slug := fmt.Sprintf("set-runtime-%d", time.Now().UnixNano())
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'BULK')
		RETURNING id
	`, "Set Runtime Test", slug).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, testUserID); err != nil {
		t.Fatalf("create workspace member: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, 'Source Runtime', 'cloud', 'claude', 'online', 'source', '{}'::jsonb, now())
		RETURNING id
	`, workspaceID).Scan(&sourceRuntimeID); err != nil {
		t.Fatalf("create source runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at)
		VALUES ($1, 'Target Runtime', 'cloud', 'codex', 'online', 'target', '{}'::jsonb, now())
		RETURNING id
	`, workspaceID).Scan(&targetRuntimeID); err != nil {
		t.Fatalf("create target runtime: %v", err)
	}
	return workspaceID, sourceRuntimeID, targetRuntimeID
}

func createSetRuntimeAgent(t *testing.T, workspaceID, runtimeID, name string, archived bool) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id, model, archived_at
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4, 'old-model', CASE WHEN $5 THEN now() ELSE NULL END)
		RETURNING id
	`, workspaceID, name, runtimeID, testUserID, archived).Scan(&agentID); err != nil {
		t.Fatalf("create agent %s: %v", name, err)
	}
	return agentID
}

// rawJSONResponse decodes the raw map so we can assert the literal
// JSON shape — `custom_env` MUST be absent from the wire output, not
// merely empty, otherwise a future caller decoding into a wider struct
// could still see masked or partial values.
func rawJSONResponse(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return out
}

// TestGetAgent_ResponseHasNoCustomEnv guards the core invariant from
// MUL-2600: the generic agent resource response NEVER carries the
// custom_env field, even for the agent's owner. Only the dedicated
// env endpoint exposes secret values.
func TestGetAgent_ResponseHasNoCustomEnv(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "noenv-get-agent", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET custom_env = '{"SECRET_KEY": "super-secret"}' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to set custom_env: %v", err)
	}

	req := newRequest("GET", "/agents/"+agentID, nil)
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.GetAgent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	raw := rawJSONResponse(t, w.Body.Bytes())
	if _, ok := raw["custom_env"]; ok {
		t.Errorf("custom_env field must not appear in agent response, got %v", raw["custom_env"])
	}
	if _, ok := raw["custom_env_redacted"]; ok {
		t.Errorf("custom_env_redacted field must not appear in agent response (use has_custom_env)")
	}
	if got, _ := raw["has_custom_env"].(bool); !got {
		t.Errorf("has_custom_env expected true, got %v", raw["has_custom_env"])
	}
	if got, _ := raw["custom_env_key_count"].(float64); got != 1 {
		t.Errorf("custom_env_key_count expected 1, got %v", raw["custom_env_key_count"])
	}

	// Sanity-check the typed shape too — the struct must not have
	// rehydrated the masked map.
	var typed AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &typed); err != nil {
		t.Fatalf("typed decode failed: %v", err)
	}
	if typed.HasCustomEnv != true {
		t.Errorf("typed.HasCustomEnv expected true")
	}
	if typed.CustomEnvKeyCount != 1 {
		t.Errorf("typed.CustomEnvKeyCount expected 1, got %d", typed.CustomEnvKeyCount)
	}
}

// TestListAgents_ResponseHasNoCustomEnv mirrors the GetAgent guard for
// the list endpoint. Same invariant: no custom_env field on the wire,
// only coarse metadata.
func TestListAgents_ResponseHasNoCustomEnv(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentName := "noenv-list-agent"
	agentID := createHandlerTestAgent(t, agentName, nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET custom_env = '{"SECRET_KEY": "super-secret", "OTHER": "y"}' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to set custom_env: %v", err)
	}

	req := newRequest("GET", "/agents", nil)
	w := httptest.NewRecorder()
	testHandler.ListAgents(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var rawAgents []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rawAgents); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	var found map[string]any
	for _, a := range rawAgents {
		if name, _ := a["name"].(string); name == agentName {
			found = a
			break
		}
	}
	if found == nil {
		t.Fatal("agent not found in list response")
	}
	if _, ok := found["custom_env"]; ok {
		t.Errorf("custom_env must not appear in list response")
	}
	if got, _ := found["custom_env_key_count"].(float64); got != 2 {
		t.Errorf("custom_env_key_count expected 2, got %v", found["custom_env_key_count"])
	}
	if got, _ := found["has_custom_env"].(bool); !got {
		t.Errorf("has_custom_env expected true")
	}
}

// TestGetAgentEnv_OwnerSucceedsAndAudits exercises the happy path: an
// agent owner reveals env, and the response carries the plaintext map.
// The activity_log row is checked at the end so the audit trail is
// proven to land in the same transaction window.
func TestGetAgentEnv_OwnerSucceedsAndAudits(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "env-reveal-owner-agent", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET custom_env = '{"KEY_ONE": "v1", "KEY_TWO": "v2"}' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to set custom_env: %v", err)
	}

	req := newRequest("GET", "/api/agents/"+agentID+"/env", nil)
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.GetAgentEnv(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GetAgentEnv: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AgentEnvResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AgentID != agentID {
		t.Errorf("agent_id mismatch: got %q", resp.AgentID)
	}
	expected := map[string]string{"KEY_ONE": "v1", "KEY_TWO": "v2"}
	if !reflect.DeepEqual(resp.CustomEnv, expected) {
		t.Errorf("CustomEnv mismatch: got %v, want %v", resp.CustomEnv, expected)
	}

	// Audit row must exist; keys but not values must be recorded.
	var revealedKeysJSON string
	if err := testPool.QueryRow(ctx, `
		SELECT details::text FROM activity_log
		WHERE workspace_id = $1 AND action = 'agent_env_revealed'
		  AND details->>'agent_id' = $2
		ORDER BY created_at DESC LIMIT 1
	`, testWorkspaceID, agentID).Scan(&revealedKeysJSON); err != nil {
		t.Fatalf("no agent_env_revealed activity row found: %v", err)
	}
	if !strings.Contains(revealedKeysJSON, `"KEY_ONE"`) || !strings.Contains(revealedKeysJSON, `"KEY_TWO"`) {
		t.Errorf("expected revealed_keys to contain KEY_ONE and KEY_TWO, got: %s", revealedKeysJSON)
	}
	if strings.Contains(revealedKeysJSON, `"v1"`) || strings.Contains(revealedKeysJSON, `"v2"`) {
		t.Errorf("activity details must NOT contain env values, got: %s", revealedKeysJSON)
	}
}

// TestAgentEnv_AgentActorRejected proves the security-critical actor
// guard: even when the underlying user is a workspace owner, a request
// arriving from inside a running agent task is denied 403. This is
// the lateral-movement fix — an agent running with its owner's token
// cannot reveal a sibling agent's secrets.
func TestAgentEnv_AgentActorRejected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	targetID := createHandlerTestAgent(t, "env-target-agent", nil)
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET custom_env = '{"K":"v"}' WHERE id = $1`, targetID); err != nil {
		t.Fatalf("failed to set custom_env: %v", err)
	}

	// Spin up a separate agent + task that authorises the X-Agent-ID /
	// X-Task-ID header pair resolveActor checks. The owning member of
	// the host agent is the same testUserID (workspace owner), which is
	// the exact lateral-movement shape we want to block.
	hostAgentID := createHandlerTestAgent(t, "env-host-agent", nil)
	hostTaskID := createHandlerTestTaskForAgent(t, hostAgentID)

	cases := []struct {
		name string
		fn   func(http.ResponseWriter, *http.Request)
		body any
	}{
		{"reveal", testHandler.GetAgentEnv, nil},
		{"update", testHandler.UpdateAgentEnv, map[string]any{"custom_env": map[string]string{"K": "v2"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method := http.MethodGet
			if tc.body != nil {
				method = http.MethodPut
			}
			req := newRequest(method, "/api/agents/"+targetID+"/env", tc.body)
			req = withURLParam(req, "id", targetID)
			req.Header.Set("X-Agent-ID", hostAgentID)
			req.Header.Set("X-Task-ID", hostTaskID)
			w := httptest.NewRecorder()
			tc.fn(w, req)
			if w.Code != http.StatusForbidden {
				t.Fatalf("expected 403 from agent actor, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// TestAgentEnv_TaskTokenActorSource locks in the post-MUL-2600 attack
// model: an agent process that strips its identifying headers
// (X-Agent-ID / X-Task-ID) but is still authenticated by an `mat_`
// task token MUST be recognized as actor=agent and rejected on the
// env endpoint. The auth middleware sets X-Actor-Source=task_token
// from the token row; resolveActor honors that header before the
// header-pair fallback. Without this guard the lateral-movement fix
// would only block "honest" CLIs that voluntarily set both headers.
func TestAgentEnv_TaskTokenActorSource(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	targetID := createHandlerTestAgent(t, "env-tt-target-agent", nil)
	if _, err := testPool.Exec(context.Background(), `UPDATE agent SET custom_env = '{"K":"v"}' WHERE id = $1`, targetID); err != nil {
		t.Fatalf("failed to set custom_env: %v", err)
	}

	req := newRequest(http.MethodGet, "/api/agents/"+targetID+"/env", nil)
	req = withURLParam(req, "id", targetID)
	// Simulate the auth middleware's post-mat_-resolution state: the
	// only header touching actor identity is X-Actor-Source. The agent
	// process stripped X-Agent-ID and X-Task-ID, hoping to fall back
	// to the member auth path — the server-set X-Actor-Source must
	// short-circuit that escape.
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Del("X-Agent-ID")
	req.Header.Del("X-Task-ID")
	w := httptest.NewRecorder()
	testHandler.GetAgentEnv(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when X-Actor-Source=task_token, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateAgentEnv_PreservesSentinelValues verifies the **** guard.
// A naive write would clobber real secrets with the masked
// placeholder; we want any key whose value comes in as **** to keep
// its stored value.
func TestUpdateAgentEnv_PreservesSentinelValues(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "env-sentinel-agent", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET custom_env = '{"KEEP_ME":"real-secret","ALSO":"another-secret"}' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to seed custom_env: %v", err)
	}

	// Client sends one key with a real new value, one with **** (should
	// be preserved), and one new key that isn't in the existing map but
	// arrives as **** (must be dropped, never written as literal).
	body := map[string]any{
		"custom_env": map[string]string{
			"KEEP_ME":   "****",
			"ALSO":      "rotated",
			"PHANTOM":   "****",
			"BRAND_NEW": "fresh",
		},
	}
	req := newRequest(http.MethodPut, "/api/agents/"+agentID+"/env", body)
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgentEnv(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgentEnv: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Refetch from DB so we don't rely on the response body alone.
	var stored string
	if err := testPool.QueryRow(ctx, `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("failed to read back custom_env: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(stored), &got); err != nil {
		t.Fatalf("failed to decode stored custom_env: %v", err)
	}
	want := map[string]string{
		"KEEP_ME":   "real-secret", // **** must preserve the existing value
		"ALSO":      "rotated",     // explicit overwrite
		"BRAND_NEW": "fresh",       // new addition
		// PHANTOM is intentionally absent — **** for a non-existent key
		// is dropped, never persisted as literal `****`.
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stored custom_env mismatch:\n got:  %v\n want: %v", got, want)
	}

	// Audit row should reflect the diff. We decode the jsonb back into a
	// typed map and compare semantically — postgres serializes jsonb with
	// canonicalised whitespace (`"added_keys": ["BRAND_NEW"]`), so a raw
	// substring match on the dense form silently fails on real database
	// output.
	var details string
	if err := testPool.QueryRow(ctx, `
		SELECT details::text FROM activity_log
		WHERE workspace_id = $1 AND action = 'agent_env_updated' AND details->>'agent_id' = $2
		ORDER BY created_at DESC LIMIT 1
	`, testWorkspaceID, agentID).Scan(&details); err != nil {
		t.Fatalf("expected agent_env_updated activity row: %v", err)
	}
	var auditFields struct {
		AddedKeys     []string `json:"added_keys"`
		ChangedKeys   []string `json:"changed_keys"`
		PreservedKeys []string `json:"preserved_keys"`
	}
	if err := json.Unmarshal([]byte(details), &auditFields); err != nil {
		t.Fatalf("failed to decode audit details: %v (raw=%s)", err, details)
	}
	if !reflect.DeepEqual(auditFields.AddedKeys, []string{"BRAND_NEW"}) {
		t.Errorf("added_keys: got %v, want [BRAND_NEW]; raw=%s", auditFields.AddedKeys, details)
	}
	if !reflect.DeepEqual(auditFields.ChangedKeys, []string{"ALSO"}) {
		t.Errorf("changed_keys: got %v, want [ALSO]; raw=%s", auditFields.ChangedKeys, details)
	}
	if !reflect.DeepEqual(auditFields.PreservedKeys, []string{"KEEP_ME"}) {
		t.Errorf("preserved_keys: got %v, want [KEEP_ME]; raw=%s", auditFields.PreservedKeys, details)
	}
	// Audit must never contain values.
	for _, leak := range []string{"real-secret", "another-secret", "rotated", "fresh"} {
		if strings.Contains(details, leak) {
			t.Errorf("audit details leaked value %q: %s", leak, details)
		}
	}
}

func TestUpdateAgent_RejectsCustomEnvInBody(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "update-no-env-agent", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET custom_env = '{"PRE":"existing"}' WHERE id = $1`, agentID); err != nil {
		t.Fatalf("failed to seed custom_env: %v", err)
	}

	// Sending custom_env via the generic PUT /api/agents/{id} must fail
	// loudly with a 400 — see the comment on the rejection in agent.go.
	// Silently dropping the field used to make scripted clients believe
	// they had rotated a secret when nothing actually happened.
	body := map[string]any{
		"description": "still updating description",
		"custom_env":  map[string]string{"INJECTED": "should-not-stick"},
	}
	req := newRequest(http.MethodPut, "/api/agents/"+agentID, body)
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateAgent: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "custom_env") || !strings.Contains(w.Body.String(), "/env") {
		t.Errorf("error body should mention custom_env and the env endpoint; got %s", w.Body.String())
	}

	// The stored env must be untouched by the rejected request.
	var stored string
	if err := testPool.QueryRow(ctx, `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&stored); err != nil {
		t.Fatalf("failed to read custom_env: %v", err)
	}
	if !strings.Contains(stored, `"PRE": "existing"`) && !strings.Contains(stored, `"PRE":"existing"`) {
		t.Errorf("UpdateAgent must NOT touch custom_env; got %q", stored)
	}
	if strings.Contains(stored, "INJECTED") {
		t.Errorf("UpdateAgent should have rejected custom_env in body; got %q", stored)
	}
}

// TestMergeAgentEnv_PureFunction exercises the diff/sentinel logic
// without the DB round-trip — keeps the contract front-and-centre in
// case someone refactors the handler later.
func TestMergeAgentEnv_PureFunction(t *testing.T) {
	cases := []struct {
		name     string
		existing map[string]string
		request  map[string]string
		want     map[string]string
		audit    envAudit
	}{
		{
			name:     "preserve sentinel",
			existing: map[string]string{"A": "real"},
			request:  map[string]string{"A": "****"},
			want:     map[string]string{"A": "real"},
			audit:    envAudit{preserved: []string{"A"}},
		},
		{
			name:     "drop sentinel for missing key",
			existing: map[string]string{},
			request:  map[string]string{"A": "****"},
			want:     map[string]string{},
			audit:    envAudit{},
		},
		{
			name:     "add new key",
			existing: map[string]string{},
			request:  map[string]string{"B": "v"},
			want:     map[string]string{"B": "v"},
			audit:    envAudit{added: []string{"B"}},
		},
		{
			name:     "change existing value",
			existing: map[string]string{"B": "old"},
			request:  map[string]string{"B": "new"},
			want:     map[string]string{"B": "new"},
			audit:    envAudit{changed: []string{"B"}},
		},
		{
			name:     "remove key absent from request",
			existing: map[string]string{"B": "v"},
			request:  map[string]string{},
			want:     map[string]string{},
			audit:    envAudit{removed: []string{"B"}},
		},
		{
			name:     "noop when value unchanged",
			existing: map[string]string{"B": "same"},
			request:  map[string]string{"B": "same"},
			want:     map[string]string{"B": "same"},
			audit:    envAudit{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, audit := mergeAgentEnv(tc.existing, tc.request)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("merged map: got %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(audit, tc.audit) {
				t.Errorf("audit: got %+v, want %+v", audit, tc.audit)
			}
		})
	}
}

func TestPatchAgentEnv_PureFunction(t *testing.T) {
	got, audit := patchAgentEnv(
		map[string]string{
			"CHANGE":   "old",
			"KEEP":     "same",
			"PRESERVE": "real-secret",
			"REMOVE":   "gone",
		},
		map[string]string{
			"ADD":      "new",
			"CHANGE":   "new",
			"KEEP":     "same",
			"PRESERVE": envSentinel,
		},
		[]string{"REMOVE", "MISSING"},
	)

	want := map[string]string{
		"ADD":      "new",
		"CHANGE":   "new",
		"KEEP":     "same",
		"PRESERVE": "real-secret",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("patched env = %v, want %v", got, want)
	}
	wantAudit := envAudit{
		added:     []string{"ADD"},
		removed:   []string{"REMOVE"},
		changed:   []string{"CHANGE"},
		preserved: []string{"PRESERVE"},
	}
	if !reflect.DeepEqual(audit, wantAudit) {
		t.Fatalf("audit = %+v, want %+v", audit, wantAudit)
	}
}

// Compile-time guard: AgentResponse must NOT carry the legacy env
// fields. Reintroducing them is a security regression — this test
// fails to compile rather than fails at runtime so reviewers see the
// breakage in the diff. Kept as a runtime test because the package
// boundary makes a struct-tag introspection cheap and obvious.
func TestAgentResponseShape_HasNoLegacyEnvFields(t *testing.T) {
	typ := reflect.TypeOf(AgentResponse{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		switch tag {
		case "custom_env", "custom_env_redacted", "custom_env_redacted_reason":
			t.Errorf("AgentResponse must not carry %q field (MUL-2600)", tag)
		}
	}
}

// TestUpdateAgent_RedactsMcpConfigForAgentActor closes the second leg
// of MUL-2600 review #2: an agent process with a task token (or with
// the X-Actor-Source server marker) must not be able to scrape another
// agent's mcp_config via an unrelated mutation response. Even when the
// host PAT would otherwise satisfy canManageAgent, the response body
// must come back with mcp_config redacted.
func TestUpdateAgent_RedactsMcpConfigForAgentActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// The target agent has a populated mcp_config that historically would
	// be leaked back via the UpdateAgent / ArchiveAgent / RestoreAgent
	// HTTP response.
	target := createHandlerTestAgent(t, "mut-mcp-target", []byte(`{"server":"secret-config"}`))

	// A second agent acts as the "calling" agent process whose task
	// token authenticated the request. It is registered in the same
	// workspace so resolveActor recognises X-Agent-ID as valid.
	caller := createHandlerTestAgent(t, "mut-mcp-caller", nil)
	taskID := insertHandlerTestTask(t, caller)

	desc := "trivial mutation that should NOT leak target mcp_config"
	req := newRequest(http.MethodPut, "/api/agents/"+target, map[string]any{
		"description": desc,
	})
	req = withURLParam(req, "id", target)
	// Simulate a task-token-authenticated agent request. The auth
	// middleware would normally set these; we mimic both the modern
	// path (X-Actor-Source) and the legacy header pair so the test is
	// resilient to either resolveActor branch.
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", caller)
	req.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// The response contract keeps `mcp_config` always-present so clients
	// can distinguish "no config" vs "redacted" via the companion flag.
	// `json.RawMessage` of a JSON null decodes to the literal bytes
	// `null`, not Go nil — so check for "no secret-bearing content"
	// rather than `!= nil`.
	if len(resp.McpConfig) > 0 && !bytes.Equal(bytes.TrimSpace(resp.McpConfig), []byte("null")) {
		t.Errorf("UpdateAgent response leaked mcp_config to agent actor: %s", string(resp.McpConfig))
	}
	if !resp.McpConfigRedacted {
		t.Errorf("UpdateAgent response should set mcp_config_redacted=true for agent actor")
	}
}

// TestUpdateAgent_KeepsMcpConfigForMemberActor is the matching positive
// test — a normal member request (owner/admin) still receives the full
// mcp_config in the mutation response, so the redaction does not
// accidentally regress the legitimate Web admin flow.
func TestUpdateAgent_KeepsMcpConfigForMemberActor(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	target := createHandlerTestAgent(t, "mut-mcp-member", []byte(`{"server":"member-visible"}`))

	req := newRequest(http.MethodPut, "/api/agents/"+target, map[string]any{
		"description": "owner-visible mutation",
	})
	req = withURLParam(req, "id", target)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.McpConfig == nil {
		t.Errorf("UpdateAgent response should keep mcp_config for member actor; got nil")
	}
	if resp.McpConfigRedacted {
		t.Errorf("UpdateAgent response should NOT mark mcp_config redacted for member actor")
	}
}

// TestUpdateAgent_PreservesSkillsInResponse is the regression for #3459:
// updating only description/instructions used to return "skills": []
// because the handler skipped the skill reload that GetAgent does. The
// DB row was always preserved; the response just lied about it, which
// scared users into manually re-running `agent skills set` and risked
// scripted clients writing the empty set back. We assert (a) the
// response carries the bound skills, (b) the DB row is unchanged, and
// (c) GetAgent reports the same shape so the two endpoints don't drift.
func TestUpdateAgent_PreservesSkillsInResponse(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "update-preserves-skills-agent", nil)
	skillA := insertHandlerTestSkill(t, "update-preserve-a", "alpha body")
	skillB := insertHandlerTestSkill(t, "update-preserve-b", "beta body")
	for _, sid := range []string{skillA, skillB} {
		if _, err := testPool.Exec(ctx,
			`INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`,
			agentID, sid,
		); err != nil {
			t.Fatalf("attach skill %s: %v", sid, err)
		}
	}

	req := newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"description": "metadata-only update",
	})
	req = withURLParam(req, "id", agentID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	gotIDs := map[string]bool{}
	for _, s := range resp.Skills {
		gotIDs[s.ID] = true
	}
	for _, want := range []string{skillA, skillB} {
		if !gotIDs[want] {
			t.Errorf("UpdateAgent response missing skill %s; got %+v", want, resp.Skills)
		}
	}

	// Defence in depth: the junction table must be untouched too. Without
	// this check a future regression that DOES wipe agent_skill rows but
	// reloads them into the response would silently pass.
	var rowCount int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*) FROM agent_skill WHERE agent_id = $1`,
		agentID,
	).Scan(&rowCount); err != nil {
		t.Fatalf("count agent_skill: %v", err)
	}
	if rowCount != 2 {
		t.Errorf("agent_skill row count: expected 2, got %d", rowCount)
	}

	// GetAgent must agree with UpdateAgent on the skill list — otherwise
	// CLI users will see one shape from the mutation and a different one
	// on the very next read.
	getReq := newRequest(http.MethodGet, "/api/agents/"+agentID, nil)
	getReq = withURLParam(getReq, "id", agentID)
	getW := httptest.NewRecorder()
	testHandler.GetAgent(getW, getReq)
	if getW.Code != http.StatusOK {
		t.Fatalf("GetAgent: expected 200, got %d: %s", getW.Code, getW.Body.String())
	}
	var getResp AgentResponse
	if err := json.NewDecoder(getW.Body).Decode(&getResp); err != nil {
		t.Fatalf("decode GetAgent: %v", err)
	}
	if len(getResp.Skills) != len(resp.Skills) {
		t.Errorf("GetAgent skill count %d != UpdateAgent skill count %d",
			len(getResp.Skills), len(resp.Skills))
	}
}

// TestArchiveRestoreAgent_PreservesSkillsInResponse is the sister
// regression for #3459: ArchiveAgent / RestoreAgent share the same
// agentToResponse path as UpdateAgent and previously also returned
// "skills": [] regardless of what was in the junction table. The
// archive/restore broadcasts are the only place where mobile clients
// learn about state flips, so an empty skills array there would propagate
// to every connected client until the next refetch.
func TestArchiveRestoreAgent_PreservesSkillsInResponse(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "archive-preserves-skills-agent", nil)
	skillID := insertHandlerTestSkill(t, "archive-preserve", "body")
	if _, err := testPool.Exec(ctx,
		`INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`,
		agentID, skillID,
	); err != nil {
		t.Fatalf("attach skill: %v", err)
	}

	archiveReq := newRequest(http.MethodPost, "/api/agents/"+agentID+"/archive", nil)
	archiveReq = withURLParam(archiveReq, "id", agentID)
	archiveW := httptest.NewRecorder()
	testHandler.ArchiveAgent(archiveW, archiveReq)
	if archiveW.Code != http.StatusOK {
		t.Fatalf("ArchiveAgent: expected 200, got %d: %s", archiveW.Code, archiveW.Body.String())
	}
	var archived AgentResponse
	if err := json.NewDecoder(archiveW.Body).Decode(&archived); err != nil {
		t.Fatalf("decode archive: %v", err)
	}
	if len(archived.Skills) != 1 || archived.Skills[0].ID != skillID {
		t.Errorf("ArchiveAgent: expected 1 skill %s, got %+v", skillID, archived.Skills)
	}

	restoreReq := newRequest(http.MethodPost, "/api/agents/"+agentID+"/restore", nil)
	restoreReq = withURLParam(restoreReq, "id", agentID)
	restoreW := httptest.NewRecorder()
	testHandler.RestoreAgent(restoreW, restoreReq)
	if restoreW.Code != http.StatusOK {
		t.Fatalf("RestoreAgent: expected 200, got %d: %s", restoreW.Code, restoreW.Body.String())
	}
	var restored AgentResponse
	if err := json.NewDecoder(restoreW.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if len(restored.Skills) != 1 || restored.Skills[0].ID != skillID {
		t.Errorf("RestoreAgent: expected 1 skill %s, got %+v", skillID, restored.Skills)
	}
}

// insertHandlerTestTask creates an in_progress task for the given
// agent so resolveActor's GetAgentTask lookup succeeds without
// dragging the full TaskService into the test.
func insertHandlerTestTask(t *testing.T, agentID string) string {
	t.Helper()
	ctx := context.Background()
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		VALUES ($1, $2, 'running', 0)
		RETURNING id
	`, agentID, handlerTestRuntimeID(t)).Scan(&taskID); err != nil {
		t.Fatalf("insert test task: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	return taskID
}

// Defence-in-depth: spot-check that the package compiles a small
// fmt.Sprintf so accidental imports stay tidy.
var _ = fmt.Sprintf
