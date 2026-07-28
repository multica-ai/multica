package handler

// FIR-3881 — `multica agent skills list` printed ALWAYS_ON = "no" for a skill
// that was in fact always on.
//
// The CLI renders that column from the `always_on` field of each row
// (cerebroAlwaysOnCell), but GET /api/agents/{id}/skills never sent the field,
// so every row read as "no". `multica agent get` was right and the list was
// wrong — the same misleading shape as the always-on write bug itself, one
// command over.

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestListAgentSkills_ReportsAlwaysOn(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Always-On List Test", nil)
	onID := insertHandlerTestSkill(t, "agent-skill-always-on", "on")
	offID := insertHandlerTestSkill(t, "agent-skill-load-on-demand", "off")
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
	req := newRequest("GET", "/api/agents/"+agentID+"/skills", nil)
	req = withURLParam(req, "id", agentID)
	testHandler.ListAgentSkills(w, req)
	if w.Code != 200 {
		t.Fatalf("ListAgentSkills: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var rows []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("ListAgentSkills: failed to decode body: %v", err)
	}

	var sawOn, sawOff bool
	for _, row := range rows {
		switch row["id"] {
		case onID:
			sawOn = true
			if on, _ := row["always_on"].(bool); !on {
				t.Fatalf("ListAgentSkills: always-on skill must report always_on true, got: %v", row["always_on"])
			}
		case offID:
			sawOff = true
			// A load-on-demand binding may omit the field entirely; what it must
			// never do is claim true.
			if on, _ := row["always_on"].(bool); on {
				t.Fatalf("ListAgentSkills: load-on-demand skill must not report always_on true")
			}
		}
	}
	if !sawOn || !sawOff {
		t.Fatalf("ListAgentSkills: expected both test skills in response, got %d rows", len(rows))
	}
}
