package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestProjectCreatorDeletionPreservesWorkspaceData(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"creator", http.StatusNoContent},
		{"other member", http.StatusForbidden},
		{"wrong workspace", http.StatusNotFound},
		{"former member", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := dbfx.Workspace(t, "project cleanup", "cleanup-"+uuid.NewString())
			dbfx.Member(t, ws, testUserID, "member")
			other := dbfx.User(t, "Other", uuid.NewString()+"@example.test")
			dbfx.Member(t, ws, other, "member")
			req := newRequest(http.MethodPost, "/api/projects", map[string]any{
				"title": "Shared project", "lead_type": "member", "lead_id": other,
				"resources": []map[string]any{{
					"resource_type": "github_repo",
					"resource_ref":  map[string]any{"url": "https://github.com/multica-ai/multica"},
				}},
			})
			req.Header.Set("X-Workspace-ID", ws)
			var project ProjectResponse
			testutil.Call(t, testHandler.CreateProject, req).Want(http.StatusCreated).JSON(&project)
			t.Cleanup(func() {
				dbfx.Exec(t, "DELETE FROM project_resource WHERE project_id=$1", project.ID)
				dbfx.Exec(t, "DELETE FROM project WHERE id=$1", project.ID)
			})
			issue := dbfx.Issue(t, "Another member's issue", testutil.Cols{
				"workspace_id": ws, "project_id": project.ID, "creator_id": other,
			})
			agent := dbfx.Agent(t, "Project agent", "", testutil.Cols{"workspace_id": ws})
			session := dbfx.ChatSession(t, agent, testutil.Cols{"workspace_id": ws, "project_id": project.ID})

			user, requestWS := testUserID, ws
			switch tc.name {
			case "other member":
				user = other
			case "wrong workspace":
				requestWS = testWorkspaceID
			case "former member":
				dbfx.Exec(t, "DELETE FROM member WHERE workspace_id=$1 AND user_id=$2", ws, testUserID)
			}
			req = withURLParam(newRequestAs(user, http.MethodDelete, "/api/projects/"+project.ID, nil), "id", project.ID)
			req.Header.Set("X-Workspace-ID", requestWS)
			testutil.Call(t, testHandler.DeleteProject, req).Want(tc.status)

			deleted := tc.status == http.StatusNoContent
			remaining := 1
			if deleted {
				remaining = 0
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM project WHERE id=$1", project.ID); n != remaining {
				t.Fatalf("remaining projects = %d, want %d", n, remaining)
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM project_resource WHERE project_id=$1", project.ID); n != remaining {
				t.Fatalf("remaining resources = %d, want %d", n, remaining)
			}
			var issueProject, sessionProject *string
			dbfx.QueryRow(t, "SELECT project_id::text FROM issue WHERE id=$1", issue).Scan(&issueProject)
			dbfx.QueryRow(t, "SELECT project_id::text FROM chat_session WHERE id=$1", session).Scan(&sessionProject)
			for _, ref := range []*string{issueProject, sessionProject} {
				if deleted && ref != nil || !deleted && (ref == nil || *ref != project.ID) {
					t.Fatalf("project reference = %v, deleted = %v", ref, deleted)
				}
			}
		})
	}
}
