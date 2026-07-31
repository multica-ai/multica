package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// insertReimportSkill seeds a skill owned by testUserID with a clawhub origin
// pointing at the given slug, plus an outdated body, and returns its id.
func insertReimportSkill(t *testing.T, slug, content string) string {
	t.Helper()
	name := "reimport-" + t.Name()
	config := `{"origin":{"type":"clawhub","source_url":"https://clawhub.ai/acme/` + slug + `","slug":"` + slug + `"}}`
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6)
		RETURNING id
	`, testWorkspaceID, name, "old description", content, config, testUserID).Scan(&id); err != nil {
		t.Fatalf("insert skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill_file WHERE skill_id = $1`, id)
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, id)
	})
	return id
}

// withMockClawHubReimport stands up a mock ClawHub API for the given slug and
// points clawHubAPIBase at it for the duration of the test.
func withMockClawHubReimport(t *testing.T, slug, displayName, body string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/skills/" + slug:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"skill": map[string]any{
					"slug":        slug,
					"displayName": displayName,
					"summary":     "fresh summary",
					"tags":        map[string]string{"latest": "2.0.0"},
				},
			})
		case "/api/v1/skills/" + slug + "/versions/2.0.0":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"version": map[string]any{
					"version": "2.0.0",
					"files":   []map[string]any{{"path": "SKILL.md", "size": len(body)}},
				},
			})
		case "/api/v1/skills/" + slug + "/file":
			_, _ = w.Write([]byte(body))
		default:
			t.Fatalf("unexpected ClawHub path: %s", r.URL.String())
		}
	}))
	prev := clawHubAPIBase
	clawHubAPIBase = srv.URL + "/api/v1"
	t.Cleanup(func() {
		clawHubAPIBase = prev
		srv.Close()
	})
}

func callReimport(t *testing.T, userID, skillID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequestAsUser(userID, http.MethodPost, "/api/skills/"+skillID+"/reimport", nil)
	req = withURLParam(req, "id", skillID)
	testHandler.ReimportSkill(w, req)
	return w
}

func TestReimportSkill_OverwritesFromSource(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}
	slug := "review-helper"
	id := insertReimportSkill(t, slug, "# Old body\n")
	// The overwrite guard requires the target's name to equal the imported name,
	// so the mock's displayName must match the seeded skill name.
	var name string
	if err := testPool.QueryRow(context.Background(), `SELECT name FROM skill WHERE id = $1`, id).Scan(&name); err != nil {
		t.Fatalf("read name: %v", err)
	}
	withMockClawHubReimport(t, slug, name, "# Fresh body\n")

	w := callReimport(t, testUserID, id)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp SkillWithFilesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID != id {
		t.Fatalf("id changed: got %s want %s", resp.ID, id)
	}
	if resp.Content != "# Fresh body\n" {
		t.Fatalf("content = %q, want fresh body", resp.Content)
	}
	if resp.Name != name {
		t.Fatalf("name = %q, want preserved %q", resp.Name, name)
	}
}

func TestReimportSkill_NonCreatorForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}
	id := insertReimportSkill(t, "review-helper", "# Old\n")
	// A different (non-creator) user in the same workspace.
	w := callReimport(t, "00000000-0000-0000-0000-0000000000ff", id)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", w.Code, w.Body.String())
	}
}

func TestReimportSkill_ManualSkillNotUpdatable(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler test DB not configured")
	}
	// insertHandlerTestSkill seeds config '{}' (manual, no origin), created by testUserID.
	id := insertHandlerTestSkill(t, "manual", "# Manual\n")
	w := callReimport(t, testUserID, id)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", w.Code, w.Body.String())
	}
}
