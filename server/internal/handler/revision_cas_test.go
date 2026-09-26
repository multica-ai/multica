package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateAgentInstructionsRevisionCAS(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "revision-cas-agent", nil)
	baseline, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
	if err != nil {
		t.Fatalf("load baseline agent: %v", err)
	}

	first := httptest.NewRecorder()
	testHandler.UpdateAgent(first, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"instructions":      "writer A",
		"expected_revision": baseline.Revision,
	}), "id", agentID))
	if first.Code != http.StatusOK {
		t.Fatalf("writer A: expected 200, got %d: %s", first.Code, first.Body.String())
	}

	stale := httptest.NewRecorder()
	testHandler.UpdateAgent(stale, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"instructions":      "writer B",
		"expected_revision": baseline.Revision,
	}), "id", agentID))
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "revision_conflict") {
		t.Fatalf("stale writer: expected revision_conflict 409, got %d: %s", stale.Code, stale.Body.String())
	}

	afterStale, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(agentID))
	if err != nil {
		t.Fatalf("reload after stale writer: %v", err)
	}
	if afterStale.Revision != baseline.Revision+1 || afterStale.Instructions != "writer A" {
		t.Fatalf("stale writer changed state: revision=%d instructions=%q", afterStale.Revision, afterStale.Instructions)
	}

	fresh := httptest.NewRecorder()
	testHandler.UpdateAgent(fresh, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"instructions":      "rebuilt from writer A",
		"expected_revision": afterStale.Revision,
	}), "id", agentID))
	if fresh.Code != http.StatusOK {
		t.Fatalf("rebuilt writer: expected 200, got %d: %s", fresh.Code, fresh.Body.String())
	}
}

func TestUpdateAgentRejectsMixedCaseInstructionsKey(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "revision-cas-case-agent", nil)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"Instructions":      "bypass",
		"expected_revision": 1,
	}), "id", agentID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("mixed-case instructions: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAutopilotDescriptionRevisionCAS(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createWebhookTestAgent(t, "revision-cas-autopilot-agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	baseline, err := testHandler.Queries.GetAutopilot(context.Background(), parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("load baseline autopilot: %v", err)
	}

	first := httptest.NewRecorder()
	testHandler.UpdateAutopilot(first, withURLParam(newRequest(http.MethodPatch, "/api/autopilots/"+autopilotID, map[string]any{
		"description":       "writer A",
		"expected_revision": baseline.Revision,
	}), "id", autopilotID))
	if first.Code != http.StatusOK {
		t.Fatalf("writer A: expected 200, got %d: %s", first.Code, first.Body.String())
	}

	stale := httptest.NewRecorder()
	testHandler.UpdateAutopilot(stale, withURLParam(newRequest(http.MethodPatch, "/api/autopilots/"+autopilotID, map[string]any{
		"description":       "writer B",
		"expected_revision": baseline.Revision,
	}), "id", autopilotID))
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "revision_conflict") {
		t.Fatalf("stale writer: expected revision_conflict 409, got %d: %s", stale.Code, stale.Body.String())
	}

	afterStale, err := testHandler.Queries.GetAutopilot(context.Background(), parseUUID(autopilotID))
	if err != nil {
		t.Fatalf("reload after stale writer: %v", err)
	}
	if afterStale.Revision != baseline.Revision+1 || afterStale.Description.String != "writer A" {
		t.Fatalf("stale writer changed state: revision=%d description=%q", afterStale.Revision, afterStale.Description.String)
	}

	fresh := httptest.NewRecorder()
	testHandler.UpdateAutopilot(fresh, withURLParam(newRequest(http.MethodPatch, "/api/autopilots/"+autopilotID, map[string]any{
		"description":       "rebuilt from writer A",
		"expected_revision": afterStale.Revision,
	}), "id", autopilotID))
	if fresh.Code != http.StatusOK {
		t.Fatalf("rebuilt writer: expected 200, got %d: %s", fresh.Code, fresh.Body.String())
	}
}

func TestUpdateAutopilotRejectsMixedCaseDescriptionKey(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createWebhookTestAgent(t, "revision-cas-description-case-agent")
	autopilotID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	w := httptest.NewRecorder()
	testHandler.UpdateAutopilot(w, withURLParam(newRequest(http.MethodPatch, "/api/autopilots/"+autopilotID, map[string]any{
		"Description":       "bypass",
		"expected_revision": 1,
	}), "id", autopilotID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("mixed-case description: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
