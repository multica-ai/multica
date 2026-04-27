package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentDuplicateBaseName(t *testing.T) {
	t.Parallel()
	if got, want := agentDuplicateBaseName("Agent"), "Agent"; got != want {
		t.Fatalf("agentDuplicateBaseName(%q) = %q, want %q", "Agent", got, want)
	}
	if got, want := agentDuplicateBaseName("Agent (1)"), "Agent"; got != want {
		t.Fatalf("agentDuplicateBaseName(%q) = %q, want %q", "Agent (1)", got, want)
	}
	if got, want := agentDuplicateBaseName("My Bot (99)"), "My Bot"; got != want {
		t.Fatalf("agentDuplicateBaseName(%q) = %q, want %q", "My Bot (99)", got, want)
	}
	if got, want := agentDuplicateBaseName("x (notnum)"), "x (notnum)"; got != want {
		t.Fatalf("agentDuplicateBaseName(%q) = %q, want %q", "x (notnum)", got, want)
	}
}

func TestNextDuplicateAgentName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		existing []string
		source   string
		want     string
	}{
		{[]string{"Other"}, "Agent", "Agent (1)"},
		{[]string{"Agent"}, "Agent", "Agent (1)"},
		{[]string{"Agent", "Agent (1)"}, "Agent", "Agent (2)"},
		{[]string{"Agent (1)", "Agent (2)"}, "Agent (1)", "Agent (3)"},
		{[]string{"Agent (1)", "Agent (3)"}, "Agent (1)", "Agent (4)"},
		{[]string{"Agent", "Agent (1)", "Agent (2)"}, "Agent (1)", "Agent (3)"},
		{[]string{"Alpha", "Alpha (1)"}, "Alpha (1)", "Alpha (2)"},
	}
	for _, tc := range cases {
		if got := nextDuplicateAgentName(tc.existing, tc.source); got != tc.want {
			t.Fatalf("nextDuplicateAgentName(%v, %q) = %q, want %q", tc.existing, tc.source, got, tc.want)
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
