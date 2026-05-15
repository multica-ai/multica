package handler

// CEREBRO-PATCH(agent-tools-handler-test): JEH-1359 — response metadata regression coverage.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListAgentTools_ReturnsRegisteredMetadataWithEnabledStatus(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "agent-tools-metadata", []byte(`{}`))
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_tool_grant WHERE agent_id = $1`, agentID)
		testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID)
	})

	prevItems := testHandler.cerebroToolItems
	prevDesc := testHandler.cerebroToolDesc
	testHandler.SetCerebroToolMeta([]CerebroToolItem{
		{Name: "list_issues", Description: "List workspace issues."},
		{Name: "get_issue", Description: "Read one workspace issue."},
	})
	t.Cleanup(func() {
		testHandler.cerebroToolItems = prevItems
		testHandler.cerebroToolDesc = prevDesc
	})

	if _, err := testPool.Exec(ctx,
		`INSERT INTO agent_tool_grant (agent_id, tool_name, enabled, config_json)
		 VALUES ($1, 'list_issues', true, '{"row_limit": 25}'::jsonb)`,
		agentID,
	); err != nil {
		t.Fatalf("insert grant: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tools", nil), "id", agentID)
	testHandler.ListAgentTools(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgentTools: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var tools []AgentToolResponse
	if err := json.NewDecoder(w.Body).Decode(&tools); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 registered tools, got %d: %+v", len(tools), tools)
	}
	if tools[0].Name != "list_issues" || tools[0].Description != "List workspace issues." || !tools[0].Enabled {
		t.Fatalf("enabled tool mismatch: %+v", tools[0])
	}
	var cfg map[string]int
	if err := json.Unmarshal(tools[0].ConfigJSON, &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg["row_limit"] != 25 {
		t.Fatalf("config mismatch: %s", string(tools[0].ConfigJSON))
	}
	if tools[1].Name != "get_issue" || tools[1].Description != "Read one workspace issue." || tools[1].Enabled {
		t.Fatalf("disabled registered tool mismatch: %+v", tools[1])
	}
}
