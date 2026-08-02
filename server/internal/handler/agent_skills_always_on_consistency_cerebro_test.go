package handler

// FIR-4282 — two surfaces report the always-on flag for the same binding:
// GET /api/agents/{id} (the agent page checkbox) and GET /api/agents/{id}/skills
// (the `multica agent skills list` ALWAYS_ON column). FIR-3881 was exactly one
// of them going wrong while the other stayed right, and no test compared them,
// so the disagreement was only visible to a human running both commands.
//
// This test reads both surfaces for the same agent and requires the same
// answer per skill. The JSON shapes differ on purpose — the list omits
// `always_on` when false, the agent response always emits it — so the
// comparison is on the decoded truth value, not on key presence.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestAlwaysOnAgreesBetweenAgentAndSkillsList(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Always-On Consistency Test", nil)
	onID := insertHandlerTestSkill(t, "consistency-always-on", "on")
	offID := insertHandlerTestSkill(t, "consistency-load-on-demand", "off")
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

	fromList := decodeAlwaysOnBySkillID(t, func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest("GET", "/api/agents/"+agentID+"/skills", nil), "id", agentID)
		testHandler.ListAgentSkills(w, req)
		return w
	}(), "ListAgentSkills", func(body []byte) ([]map[string]any, error) {
		var rows []map[string]any
		err := json.Unmarshal(body, &rows)
		return rows, err
	})

	fromAgent := decodeAlwaysOnBySkillID(t, func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest("GET", "/api/agents/"+agentID, nil), "id", agentID)
		testHandler.GetAgent(w, req)
		return w
	}(), "GetAgent", func(body []byte) ([]map[string]any, error) {
		var resp struct {
			Skills []map[string]any `json:"skills"`
		}
		err := json.Unmarshal(body, &resp)
		return resp.Skills, err
	})

	for _, want := range []struct {
		id       string
		alwaysOn bool
	}{{onID, true}, {offID, false}} {
		list, okList := fromList[want.id]
		agent, okAgent := fromAgent[want.id]
		if !okList || !okAgent {
			t.Fatalf("skill %s missing: in list=%v, in agent=%v", want.id, okList, okAgent)
		}
		if list != want.alwaysOn || agent != want.alwaysOn {
			t.Fatalf("skill %s: want always_on=%v, list said %v, agent said %v",
				want.id, want.alwaysOn, list, agent)
		}
	}
}

// decodeAlwaysOnBySkillID maps skill id to the decoded always_on truth value.
// A missing key decodes to false, which is the same reading the CLI and the web
// client apply to an older server that omits the field.
func decodeAlwaysOnBySkillID(
	t *testing.T,
	w *httptest.ResponseRecorder,
	surface string,
	rows func([]byte) ([]map[string]any, error),
) map[string]bool {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("%s: expected 200, got %d: %s", surface, w.Code, w.Body.String())
	}
	decoded, err := rows(w.Body.Bytes())
	if err != nil {
		t.Fatalf("%s: failed to decode body: %v", surface, err)
	}
	out := map[string]bool{}
	for _, row := range decoded {
		id, _ := row["id"].(string)
		on, _ := row["always_on"].(bool)
		out[id] = on
	}
	return out
}
