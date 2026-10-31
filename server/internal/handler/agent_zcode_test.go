package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateAgentZcodeAcceptsDiscoveredThinkingLevel(t *testing.T) {
	if testHandler == nil {
		t.Skip("requires isolated test database")
	}
	runtimeID := createProviderRuntime(t, "zcode")
	response := httptest.NewRecorder()
	testHandler.CreateAgent(response, newRequest(http.MethodPost, "/api/agents", map[string]any{
		"name":                 "zcode-thinking-regression",
		"runtime_id":           runtimeID,
		"model":                "multica-desktop:example/glm",
		"visibility":           "private",
		"thinking_level":       "max",
		"max_concurrent_tasks": 1,
	}))
	if response.Code != http.StatusCreated {
		t.Fatalf("create ZCode agent: status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		ID            string `json:"id"`
		ThinkingLevel string `json:"thinking_level"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), "DELETE FROM agent WHERE id=$1", result.ID) })
	if result.ThinkingLevel != "max" {
		t.Fatalf("stored reasoning level = %q", result.ThinkingLevel)
	}
}
