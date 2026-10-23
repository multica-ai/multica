package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// TestSEInfraHealthIncludesAgentFleet verifies the SE-37663 fleet section:
// failures recorded in agent_task_queue surface as failing agents with
// failure-class counts, and an active quota streak flips the fleet status
// to warn alongside the infra snapshot payload.
func TestSEInfraHealthIncludesAgentFleet(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	if testRuntimeID == "" {
		t.Skip("runtime fixture not available")
	}
	ctx := context.Background()

	agentID := uuid.NewString()
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent (id, workspace_id, name, description, runtime_mode, runtime_config,
		                   runtime_id, visibility, max_concurrent_tasks, owner_id,
		                   instructions, custom_env, custom_args)
		VALUES ($1, $2, 'fleet-health-test-agent', '', 'cloud', '{}'::jsonb,
		        $3, 'workspace', 1, $4, '', '{}'::jsonb, '[]'::jsonb)
	`, agentID, testWorkspaceID, testRuntimeID, testUserID); err != nil {
		t.Fatalf("create agent: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agentID)
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	for i := 0; i < 2; i++ {
		if _, err := testPool.Exec(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, failure_reason)
			VALUES ($1, $2, 'failed', 0, 'agent_error.provider_quota_limit')
		`, agentID, testRuntimeID); err != nil {
			t.Fatalf("insert failed task: %v", err)
		}
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
		VALUES ($1, $2, 'completed', 0)
	`, agentID, testRuntimeID); err != nil {
		t.Fatalf("insert completed task: %v", err)
	}

	snapshotPath := filepath.Join(t.TempDir(), "infra-health-snapshot.json")
	const snapshot = `{"generated_at":"2026-09-18T10:00:00Z","overall":"ok","targets":[],"nodes":{},"history":{}}`
	if err := os.WriteFile(snapshotPath, []byte(snapshot), 0o600); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	t.Setenv("MULTICA_HEALTH_SNAPSHOT_PATH", snapshotPath)

	w := httptest.NewRecorder()
	testHandler.GetSEInfraHealth(w, newRequestAs(testUserID, http.MethodGet, "/api/se/health?workspace_id="+testWorkspaceID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GetSEInfraHealth: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Overall    string `json:"overall"`
		AgentFleet *struct {
			Status            string           `json:"status"`
			QuotaStreakActive bool             `json:"quota_streak_active"`
			AuthStreakActive  bool             `json:"auth_streak_active"`
			Runtimes          []map[string]any `json:"runtimes"`
			FailingAgents     []struct {
				AgentID     string `json:"agent_id"`
				AgentName   string `json:"agent_name"`
				Completed   int64  `json:"completed"`
				Failed      int64  `json:"failed"`
				QuotaFailed int64  `json:"quota_failed"`
				AuthFailed  int64  `json:"auth_failed"`
			} `json:"failing_agents"`
		} `json:"agent_fleet"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AgentFleet == nil {
		t.Fatalf("agent_fleet section missing from response")
	}
	fleet := resp.AgentFleet
	if fleet.Runtimes == nil || fleet.FailingAgents == nil {
		t.Fatalf("fleet arrays must be non-nil, got runtimes=%v failing_agents=%v", fleet.Runtimes, fleet.FailingAgents)
	}
	if !fleet.QuotaStreakActive {
		t.Fatalf("expected quota_streak_active=true for fresh quota failures")
	}
	if fleet.Status != "warn" {
		t.Fatalf("expected fleet status warn, got %q", fleet.Status)
	}
	var found bool
	for _, a := range fleet.FailingAgents {
		if a.AgentID == agentID {
			found = true
			if a.Failed != 2 || a.QuotaFailed != 2 || a.AuthFailed != 0 || a.Completed != 1 {
				t.Fatalf("unexpected stats for seeded agent: %+v", a)
			}
		}
	}
	if !found {
		t.Fatalf("seeded agent %s missing from failing_agents: %+v", agentID, fleet.FailingAgents)
	}
}
