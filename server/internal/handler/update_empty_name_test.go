package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Create rejects an empty name or title; update must reject it too, so an
// API or CLI caller cannot blank one out after the fact.
func TestUpdateRejectsEmptyNameOrTitle(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	t.Run("issue", func(t *testing.T) {
		id := dbfx.Issue(t, "keep this issue title")
		w := httptest.NewRecorder()
		testHandler.UpdateIssue(w, withURLParam(newRequest(http.MethodPut, "/api/issues/"+id, map[string]any{"title": ""}), "id", id))
		assertEmptyNameRejected(t, w, "title is required")
		assertStoredText(t, `SELECT title FROM issue WHERE id = $1`, id, "keep this issue title")
	})

	t.Run("issue batch", func(t *testing.T) {
		firstID := dbfx.Issue(t, "first batch issue title")
		secondID := dbfx.Issue(t, "second batch issue title")
		req := newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
			"issue_ids": []string{firstID, secondID},
			"updates":   map[string]any{"title": "", "priority": "high"},
		})
		response := testutil.Call(t, testHandler.BatchUpdateIssues, req)
		assertEmptyNameRejected(t, response.ResponseRecorder, "title is required")
		assertStoredText(t, `SELECT title FROM issue WHERE id = $1`, firstID, "first batch issue title")
		assertStoredText(t, `SELECT title FROM issue WHERE id = $1`, secondID, "second batch issue title")
		assertStoredText(t, `SELECT priority FROM issue WHERE id = $1`, firstID, "none")
		assertStoredText(t, `SELECT priority FROM issue WHERE id = $1`, secondID, "none")
	})

	t.Run("project", func(t *testing.T) {
		project := createTestProjectForResources(t, "keep this project title")
		w := httptest.NewRecorder()
		testHandler.UpdateProject(w, withURLParam(newRequest(http.MethodPut, "/api/projects/"+project.ID, map[string]any{"title": ""}), "id", project.ID))
		assertEmptyNameRejected(t, w, "title is required")
		assertStoredText(t, `SELECT title FROM project WHERE id = $1`, project.ID, "keep this project title")
	})

	t.Run("agent", func(t *testing.T) {
		id := createHandlerTestAgent(t, "keep-this-agent-name", nil)
		w := httptest.NewRecorder()
		testHandler.UpdateAgent(w, withURLParam(newRequest(http.MethodPut, "/api/agents/"+id, map[string]any{"name": ""}), "id", id))
		assertEmptyNameRejected(t, w, "name is required")
		assertStoredText(t, `SELECT name FROM agent WHERE id = $1`, id, "keep-this-agent-name")
	})

	t.Run("squad", func(t *testing.T) {
		leaderID := createHandlerTestAgent(t, "empty-name-squad-leader", nil)
		id := dbfx.Squad(t, "keep this squad name", leaderID)
		w := httptest.NewRecorder()
		testHandler.UpdateSquad(w, squadScopeReq("", http.MethodPatch, "/api/squads/"+id, map[string]any{"name": ""}, map[string]string{"id": id}))
		assertEmptyNameRejected(t, w, "name is required")
		assertStoredText(t, `SELECT name FROM squad WHERE id = $1`, id, "keep this squad name")
	})

	t.Run("skill", func(t *testing.T) {
		rec := httptest.NewRecorder()
		testHandler.CreateSkill(rec, newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/skills", CreateSkillRequest{
			Name:    "keep-this-skill-name",
			Content: "# skill",
		}))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create skill: expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var created SkillWithFilesResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode create skill: %v", err)
		}
		dbfx.Cleanup(t, `DELETE FROM skill WHERE id = $1`, created.ID)

		w := httptest.NewRecorder()
		testHandler.UpdateSkill(w, withURLParam(newRequest(http.MethodPut, "/api/skills/"+created.ID, map[string]any{"name": ""}), "id", created.ID))
		assertEmptyNameRejected(t, w, "name is required")
		assertStoredText(t, `SELECT name FROM skill WHERE id = $1`, created.ID, "keep-this-skill-name")
	})
}

func assertEmptyNameRejected(t *testing.T, w *httptest.ResponseRecorder, wantError string) {
	t.Helper()
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v (%s)", err, w.Body.String())
	}
	if body.Error != wantError {
		t.Fatalf("expected error %q, got %q", wantError, body.Error)
	}
}

func assertStoredText(t *testing.T, query, id, want string) {
	t.Helper()
	var got string
	dbfx.QueryRow(t, query, id).Scan(&got)
	if got != want {
		t.Fatalf("stored value changed: got %q, want %q", got, want)
	}
}
