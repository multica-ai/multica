package handler

// FIR-4002 — the "Always on" checkbox on an agent's Skills tab always read back
// unchecked, no matter how the flag was set.
//
// FIR-3805 wired the checkbox to `agent.skills[].always_on`, and FIR-3881 fixed
// GET /api/agents/{id}/skills to send that field. But the agent detail page does
// not read either of those: it takes its agent straight out of the workspace
// list (agentListOptions → GET /api/agents), and ListAgents built its skill rows
// from ListAgentSkillsByWorkspace, which never selected always_on. So the one
// response the checkbox is rendered from was the one response missing the field.
//
// Two failures followed from that, and the second is the damaging one:
//
//  1. A skill that IS always on rendered unchecked — "the checkbox doesn't save".
//  2. The tab's draft is seeded from the same list, so its always-on base was
//     permanently empty. Proposing ANY skills change therefore carried
//     always_on_skill_ids: [], and approving it silently cleared every flag the
//     agent had.
//
// This test pins the field to the list response. It fails before the fix (the
// always-on skill reports false) and passes after.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListAgents_ReportsSkillAlwaysOn(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Always-On Agent List Test", nil)
	onID := insertHandlerTestSkill(t, "agent-list-always-on", "on")
	offID := insertHandlerTestSkill(t, "agent-list-load-on-demand", "off")
	for _, s := range []struct {
		id       string
		alwaysOn bool
	}{{onID, true}, {offID, false}} {
		if _, err := testPool.Exec(context.Background(),
			`INSERT INTO agent_skill (agent_id, skill_id, always_on) VALUES ($1, $2, $3)`,
			agentID, s.id, s.alwaysOn,
		); err != nil {
			t.Fatalf("attach skill to agent: %v", err)
		}
	}

	w := httptest.NewRecorder()
	testHandler.ListAgents(w, newRequest(http.MethodGet, "/api/agents", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var agents []AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&agents); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var skills []AgentSkillSummary
	for _, a := range agents {
		if a.ID == agentID {
			skills = a.Skills
		}
	}
	if skills == nil {
		t.Fatalf("ListAgents: test agent %s missing from the response", agentID)
	}

	var sawOn, sawOff bool
	for _, s := range skills {
		switch s.ID {
		case onID:
			sawOn = true
			if !s.AlwaysOn {
				t.Fatalf("ListAgents: always-on skill must report always_on true — the Skills tab renders its checkbox from this response")
			}
		case offID:
			sawOff = true
			if s.AlwaysOn {
				t.Fatalf("ListAgents: load-on-demand skill must not report always_on true")
			}
		}
	}
	if !sawOn || !sawOff {
		t.Fatalf("ListAgents: expected both test skills on the agent, got %d", len(skills))
	}
}
