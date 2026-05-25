package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestAgentRunDashboardOwnerFilter(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := handlerTestRuntimeID(t)

	var otherOwnerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ($1, $2)
		RETURNING id
	`, "Agent Dashboard Owner", "agent-dashboard-owner-"+strconv.FormatInt(time.Now().UnixNano(), 10)+"@multica.ai").Scan(&otherOwnerID); err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, otherOwnerID)
		testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, otherOwnerID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
	`, testWorkspaceID, otherOwnerID); err != nil {
		t.Fatalf("add other owner membership: %v", err)
	}

	createAgent := func(name string, ownerID string) string {
		t.Helper()
		var agentID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent (
				workspace_id, name, description, runtime_mode, runtime_config,
				runtime_id, visibility, max_concurrent_tasks, owner_id,
				instructions, custom_env, custom_args
			)
			VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4, '', '{}'::jsonb, '[]'::jsonb)
			RETURNING id
		`, testWorkspaceID, name, runtimeID, ownerID).Scan(&agentID); err != nil {
			t.Fatalf("create agent %q: %v", name, err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
		})
		return agentID
	}

	otherAgentID := createAgent("Owner Filter Other Agent", otherOwnerID)
	myAgentID := createAgent("Owner Filter My Agent", testUserID)
	started := time.Now().UTC().Add(-5 * time.Minute)
	completed := started.Add(2 * time.Minute)

	createRun := func(agentID string) string {
		t.Helper()
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, status, priority, started_at, completed_at, created_at
			)
			VALUES ($1, $2, 'completed', 0, $3, $4, $3)
			RETURNING id
		`, agentID, runtimeID, started, completed).Scan(&taskID); err != nil {
			t.Fatalf("create run for agent %s: %v", agentID, err)
		}
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
		})
		return taskID
	}
	createRun(otherAgentID)
	createRun(myAgentID)

	t.Run("owner_id narrows all dashboard rollups to that member's agents", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetAgentRunDashboard(w, newRequest("GET", "/api/dashboard/agent-runs?days=1&owner_id="+otherOwnerID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp AgentRunDashboardResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Summary.TotalRuns != 1 {
			t.Fatalf("expected only the selected owner's run, got %d", resp.Summary.TotalRuns)
		}
		if len(resp.Agents) != 1 || resp.Agents[0].AgentID != otherAgentID {
			t.Fatalf("expected only agent %s, got %#v", otherAgentID, resp.Agents)
		}
	})

	t.Run("owner=me intersects with explicit agent filters", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetAgentRunDashboard(w, newRequest("GET", "/api/dashboard/agent-runs?days=1&owner=me&agent_id="+otherAgentID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var resp AgentRunDashboardResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Summary.TotalRuns != 0 || len(resp.Agents) != 0 {
			t.Fatalf("expected owner=me to exclude another owner's agent, got summary=%#v agents=%#v", resp.Summary, resp.Agents)
		}
	})

	t.Run("invalid owner_id is rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetAgentRunDashboard(w, newRequest("GET", "/api/dashboard/agent-runs?owner_id=not-a-uuid", nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
		}
	})
}
