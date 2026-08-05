package handler

// FIR-4359 — GET /api/agents answers a much broader question than most callers
// ask. Every issue page, picker and mention list needs an agent's name and
// avatar, but the response also carries each agent's full instructions and the
// description of every skill bound to it: 845 KB across 58 agents in production,
// of which instructions + skills are 93%.
//
// ?slim=true drops both fields. It is opt-in on purpose: an older desktop build
// never sends it and keeps the exact response it was built against.
//
// This test pins both directions — slim omits the two fields and nothing else,
// and the default response still carries them.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func listAgentsForTest(t *testing.T, url string) []AgentResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListAgents(w, newRequest(http.MethodGet, url, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents %s: expected 200, got %d: %s", url, w.Code, w.Body.String())
	}
	var agents []AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&agents); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return agents
}

func findAgentForTest(t *testing.T, agents []AgentResponse, id string) AgentResponse {
	t.Helper()
	for _, a := range agents {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("ListAgents: test agent %s missing from the response", id)
	return AgentResponse{}
}

func TestListAgents_SlimOmitsInstructionsAndSkills(t *testing.T) {
	instructions := "Slim-mode test instructions — long enough to notice."
	agentID := createHandlerTestAgent(t, "Handler Slim List Test", nil)
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent SET instructions = $2 WHERE id = $1`, agentID, instructions,
	); err != nil {
		t.Fatalf("set agent instructions: %v", err)
	}
	skillID := insertHandlerTestSkill(t, "agent-list-slim-skill", "on")
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO agent_skill (agent_id, skill_id, always_on) VALUES ($1, $2, true)`,
		agentID, skillID,
	); err != nil {
		t.Fatalf("attach skill to agent: %v", err)
	}

	// Default response: the contract an older client was built against.
	full := findAgentForTest(t, listAgentsForTest(t, "/api/agents"), agentID)
	if full.Instructions != instructions {
		t.Fatalf("ListAgents: default response must keep instructions, got %q", full.Instructions)
	}
	if len(full.Skills) != 1 || full.Skills[0].ID != skillID {
		t.Fatalf("ListAgents: default response must keep skills, got %+v", full.Skills)
	}

	// Slim response: the two heavy fields are gone, identity is untouched.
	slim := findAgentForTest(t, listAgentsForTest(t, "/api/agents?slim=true"), agentID)
	if slim.Instructions != "" {
		t.Fatalf("ListAgents?slim=true: instructions must be empty, got %q", slim.Instructions)
	}
	if len(slim.Skills) != 0 {
		t.Fatalf("ListAgents?slim=true: skills must be empty, got %+v", slim.Skills)
	}
	if slim.Skills == nil {
		t.Fatalf("ListAgents?slim=true: skills must serialize as [] not null — picker code reads skills.length")
	}
	if slim.Name != full.Name || slim.ID != full.ID {
		t.Fatalf("ListAgents?slim=true: identity fields must be unchanged, got %+v", slim)
	}
}
