package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// TestCanonicalUsageReadPaths pins FIR-3940: a run whose usage exists ONLY in
// the canonical `model_usage_event` ledger — no legacy `task_usage` row, no
// `task_usage_hourly` rollup tick — must still surface on all four usage read
// paths. Before the canonical repoint every one of these returned an empty
// list, which is what made `multica agent usage` and the workspace dashboard's
// cost numbers read as "nothing spent".
func TestCanonicalUsageReadPaths(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title)
		VALUES ($1, 'canonical usage test project')
		RETURNING id
	`, testWorkspaceID).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, projectID) })

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, project_id, number)
		VALUES (
			$1, 'canonical usage test', $2, 'member', $3,
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
		)
		RETURNING id
	`, testWorkspaceID, testUserID, projectID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	now := time.Now().UTC()
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, started_at, completed_at, created_at)
		VALUES ($1, $2, $3, 'completed', $4, $5, now())
		RETURNING id
	`, agentID, issueID, runtimeID, now.Add(-20*time.Minute), now.Add(-10*time.Minute)).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	// Canonical ledger only — deliberately NO task_usage row, so this run is
	// invisible to the legacy task_usage_hourly rollup by construction.
	const (
		wantModel  = "claude-opus-5-canonical-test"
		wantInput  = 4242
		wantOutput = 111
		wantCents  = 777
	)
	if _, err := testPool.Exec(ctx, `
		INSERT INTO model_usage_event (
			event_id, task_id, workspace_id, issue_id, agent_id, runtime_id,
			sequence, observed_at, provider, model,
			input_tokens, output_tokens, cost_cents,
			source, completeness, counter_semantics
		)
		VALUES (
			'fir-3940-test-1', $1, $2, $3, $4, $5,
			1, now(), 'claude', $6,
			$7, $8, $9,
			'final_response', 'complete', 'delta'
		)
	`, taskID, testWorkspaceID, issueID, agentID, runtimeID,
		wantModel, wantInput, wantOutput, wantCents); err != nil {
		t.Fatalf("insert model_usage_event: %v", err)
	}

	type byAgentRow struct {
		AgentID      string `json:"agent_id"`
		Model        string `json:"model"`
		InputTokens  int64  `json:"input_tokens"`
		OutputTokens int64  `json:"output_tokens"`
		CostCents    int64  `json:"cost_cents"`
	}
	type dailyRow struct {
		Date        string `json:"date"`
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
		CostCents   int64  `json:"cost_cents"`
	}
	type runtimeDailyRow struct {
		Date        string `json:"date"`
		Provider    string `json:"provider"`
		Model       string `json:"model"`
		InputTokens int64  `json:"input_tokens"`
		CostCents   int64  `json:"cost_cents"`
	}

	decode := func(t *testing.T, label string, w *httptest.ResponseRecorder, out any) {
		t.Helper()
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", label, w.Code, w.Body.String())
		}
		if err := json.NewDecoder(w.Body).Decode(out); err != nil {
			t.Fatalf("%s: decode: %v", label, err)
		}
	}

	t.Run("dashboard by-agent", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageByAgent(w, newRequest("GET", "/api/dashboard/usage/by-agent?days=1", nil))
		var rows []byAgentRow
		decode(t, "by-agent", w, &rows)
		for _, r := range rows {
			if r.Model != wantModel {
				continue
			}
			if r.AgentID != agentID {
				t.Fatalf("by-agent: model attributed to agent %s, want %s", r.AgentID, agentID)
			}
			if r.InputTokens != wantInput || r.OutputTokens != wantOutput || r.CostCents != wantCents {
				t.Fatalf("by-agent: got in=%d out=%d cents=%d, want in=%d out=%d cents=%d",
					r.InputTokens, r.OutputTokens, r.CostCents, wantInput, wantOutput, wantCents)
			}
			return
		}
		t.Fatalf("by-agent: canonical-ledger run missing from response: %v", rows)
	})

	t.Run("dashboard by-agent project filter", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageByAgent(w, newRequest("GET", "/api/dashboard/usage/by-agent?days=1&project_id="+projectID, nil))
		var rows []byAgentRow
		decode(t, "by-agent project", w, &rows)
		for _, r := range rows {
			if r.Model == wantModel && r.InputTokens == wantInput {
				return
			}
		}
		t.Fatalf("by-agent project: canonical-ledger run missing under its own project: %v", rows)
	})

	t.Run("dashboard daily", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.GetDashboardUsageDaily(w, newRequest("GET", "/api/dashboard/usage/daily?days=1", nil))
		var rows []dailyRow
		decode(t, "daily", w, &rows)
		for _, r := range rows {
			if r.Model == wantModel && r.InputTokens == wantInput && r.CostCents == wantCents {
				return
			}
		}
		t.Fatalf("daily: canonical-ledger run missing from response: %v", rows)
	})

	withRuntimeParam := func(req *http.Request) *http.Request {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("runtimeId", runtimeID)
		return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	}

	t.Run("runtime daily", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := withRuntimeParam(newRequest("GET", "/api/runtimes/"+runtimeID+"/usage?days=1", nil))
		testHandler.GetRuntimeUsage(w, req)
		var rows []runtimeDailyRow
		decode(t, "runtime daily", w, &rows)
		for _, r := range rows {
			if r.Model == wantModel && r.InputTokens == wantInput && r.CostCents == wantCents {
				return
			}
		}
		t.Fatalf("runtime daily: canonical-ledger run missing from response: %v", rows)
	})

	t.Run("runtime by-agent", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := withRuntimeParam(newRequest("GET", "/api/runtimes/"+runtimeID+"/usage/by-agent?days=1", nil))
		testHandler.GetRuntimeUsageByAgent(w, req)
		var rows []byAgentRow
		decode(t, "runtime by-agent", w, &rows)
		for _, r := range rows {
			if r.Model == wantModel && r.AgentID == agentID && r.InputTokens == wantInput {
				return
			}
		}
		t.Fatalf("runtime by-agent: canonical-ledger run missing from response: %v", rows)
	})
}
