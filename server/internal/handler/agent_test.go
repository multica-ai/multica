package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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

func TestDeriveOpencodeGoModel(t *testing.T) {
	tests := []struct {
		name      string
		agentName string
		want      string
		wantOK    bool
	}{
		{
			name:      "deepseek flash",
			agentName: OpencodeAgentPrefix + "deepseek-v4-flash",
			want:      OpencodeModelPrefix + "deepseek-v4-flash",
			wantOK:    true,
		},
		{
			name:      "deepseek pro",
			agentName: OpencodeAgentPrefix + "deepseek-v4-pro",
			want:      OpencodeModelPrefix + "deepseek-v4-pro",
			wantOK:    true,
		},
		{
			name:      "kimi decimal version",
			agentName: OpencodeAgentPrefix + "kimi-k2-6",
			want:      OpencodeModelPrefix + "kimi-k2.6",
			wantOK:    true,
		},
		{
			name:      "qwen decimal version",
			agentName: OpencodeAgentPrefix + "qwen3-5-plus",
			want:      OpencodeModelPrefix + "qwen3.5-plus",
			wantOK:    true,
		},
		{
			name:      "qwen next decimal version",
			agentName: OpencodeAgentPrefix + "qwen3-6-plus",
			want:      OpencodeModelPrefix + "qwen3.6-plus",
			wantOK:    true,
		},
		{
			name:      "mimo decimal version",
			agentName: OpencodeAgentPrefix + "mimo-v2-5",
			want:      OpencodeModelPrefix + "mimo-v2.5",
			wantOK:    true,
		},
		{
			name:      "glm major version",
			agentName: OpencodeAgentPrefix + "glm-5",
			want:      OpencodeModelPrefix + "glm-5",
			wantOK:    true,
		},
		{
			name:      "glm minor version",
			agentName: OpencodeAgentPrefix + "glm-5-1",
			want:      OpencodeModelPrefix + "glm-5.1",
			wantOK:    true,
		},
		{
			name:      "multiple version groups",
			agentName: OpencodeAgentPrefix + "model-v1-2-3",
			want:      OpencodeModelPrefix + "model-v1.2.3",
			wantOK:    true,
		},
		{
			name:      "custom name",
			agentName: "custom-name",
			wantOK:    false,
		},
		{
			name:      "empty opencode suffix",
			agentName: OpencodeAgentPrefix,
			wantOK:    false,
		},
		{
			name:      "opencode seul sans tiret",
			agentName: ProviderOpencode,
			wantOK:    false,
		},
		{
			name:      "casing Opencode-glm-5",
			agentName: "Opencode-glm-5",
			wantOK:    false,
		},
		{
			name:      "trailing dash opencode-foo-",
			agentName: OpencodeAgentPrefix + "foo-",
			want:      OpencodeModelPrefix + "foo-",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := deriveOpencodeGoModel(tt.agentName)
			if ok != tt.wantOK {
				t.Fatalf("deriveOpencodeGoModel(%q) ok = %v, want %v", tt.agentName, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("deriveOpencodeGoModel(%q) = %q, want %q", tt.agentName, got, tt.want)
			}
		})
	}
}

func TestCreateAgent_OpencodeProviderModelDefaults(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	tests := []struct {
		name      string
		provider  string
		agentName string
		model     string
		wantModel string
	}{
		{
			name:      "auto fills derived model",
			provider:  ProviderOpencode,
			agentName: OpencodeAgentPrefix + "qwen3-5-plus",
			wantModel: OpencodeModelPrefix + "qwen3.5-plus",
		},
		{
			name:      "keeps empty model for custom opencode name",
			provider:  ProviderOpencode,
			agentName: "custom-name-auto-model-test",
		},
		{
			name:      "preserves explicit model",
			provider:  ProviderOpencode,
			agentName: OpencodeAgentPrefix + "glm-5-1",
			model:     "custom/provider",
			wantModel: "custom/provider",
		},
		{
			name:      "does not fill for other provider",
			provider:  "handler_test_runtime",
			agentName: OpencodeAgentPrefix + "mimo-v2-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtimeID := createHandlerTestRuntime(t, tt.provider)
			body := map[string]any{
				"name":                 tt.agentName,
				"runtime_id":           runtimeID,
				"visibility":           "private",
				"max_concurrent_tasks": 1,
			}
			if tt.model != "" {
				body["model"] = tt.model
			}

			created := createAgentForHandlerTest(t, body)
			if created.Model != tt.wantModel {
				t.Fatalf("CreateAgent model = %q, want %q", created.Model, tt.wantModel)
			}
		})
	}
}

func createHandlerTestRuntime(t *testing.T, provider string) string {
	t.Helper()

	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, 'offline', $4, '{}'::jsonb, now())
		RETURNING id
	`, testWorkspaceID, "Handler Test Runtime "+t.Name(), provider, "Handler test runtime").Scan(&runtimeID); err != nil {
		t.Fatalf("create handler test runtime: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	return runtimeID
}

func createAgentForHandlerTest(t *testing.T, body map[string]any) AgentResponse {
	t.Helper()

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("CreateAgent: decode response: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, created.ID)
	})

	return created
}
