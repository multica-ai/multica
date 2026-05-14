package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestListSkills_OmitsContent guards the fix for GH multica-ai/multica#2174:
// the workspace skill list endpoint must not ship the SKILL.md `content`
// blob, which used to bloat the payload past CLI timeouts on workspaces with
// many large skills. The detail endpoint still returns content (covered by
// TestGetSkill_IncludesContent below).
func TestListSkills_OmitsContent(t *testing.T) {
	skillID := insertHandlerTestSkill(t, "list-omits-content", strings.Repeat("a", 4096))

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/skills?workspace_id="+testWorkspaceID, nil)
	testHandler.ListSkills(w, req)
	if w.Code != 200 {
		t.Fatalf("ListSkills: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Decode into a generic shape so we can prove the wire format has no
	// `content` field at all — not "content present but empty", which would
	// still leave the bytes on the wire.
	var rows []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("ListSkills: failed to decode body: %v", err)
	}

	var found bool
	for _, row := range rows {
		if row["id"] != skillID {
			continue
		}
		found = true
		if _, ok := row["content"]; ok {
			t.Fatalf("ListSkills: response must not include `content` field, got: %v", row)
		}
		// Other expected list fields should still be present.
		for _, key := range []string{"id", "name", "description", "config", "created_at", "updated_at", "workspace_id"} {
			if _, ok := row[key]; !ok {
				t.Fatalf("ListSkills: missing expected field %q in response: %v", key, row)
			}
		}
	}
	if !found {
		t.Fatalf("ListSkills: inserted skill %s not in response", skillID)
	}
}

// TestGetSkill_IncludesContent confirms the detail endpoint still ships the
// full SKILL.md body — the list-summary change must not regress single-skill
// reads.
func TestGetSkill_IncludesContent(t *testing.T) {
	body := "# detail body\nstill served on /api/skills/{id}"
	skillID := insertHandlerTestSkill(t, "detail-includes-content", body)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/skills/"+skillID, nil)
	req = withURLParam(req, "id", skillID)
	testHandler.GetSkill(w, req)
	if w.Code != 200 {
		t.Fatalf("GetSkill: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("GetSkill: failed to decode body: %v", err)
	}
	if got, _ := resp["content"].(string); got != body {
		t.Fatalf("GetSkill: expected content %q, got %q", body, got)
	}
}

// TestListAgentSkills_OmitsContent: same constraint for the agent-scoped
// listing — gpt-boy review of the original fix flagged this as a sister case
// because `multica agent skills list` follows the same shape rules.
func TestListAgentSkills_OmitsContent(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Handler Skill Summary Test", nil)
	skillID := insertHandlerTestSkill(t, "agent-skill-omits-content", strings.Repeat("b", 1024))
	if _, err := testPool.Exec(context.Background(),
		`INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`,
		agentID, skillID,
	); err != nil {
		t.Fatalf("attach skill to agent: %v", err)
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
	if len(rows) == 0 {
		t.Fatalf("ListAgentSkills: expected at least 1 skill")
	}
	for _, row := range rows {
		if _, ok := row["content"]; ok {
			t.Fatalf("ListAgentSkills: response must not include `content` field, got: %v", row)
		}
	}
}

// insertHandlerTestSkill writes a skill row directly via SQL and registers a
// cleanup hook. We bypass the create handler to keep the test focused on the
// list/detail wire shape and to make it easy to inject a large body.
func insertHandlerTestSkill(t *testing.T, namePrefix, content string) string {
	t.Helper()
	name := namePrefix + "-" + t.Name()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, $2, $3, $4, '{}'::jsonb, $5)
		RETURNING id
	`, testWorkspaceID, name, "fixture", content, testUserID).Scan(&id); err != nil {
		t.Fatalf("insert skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, id)
	})
	return id
}
