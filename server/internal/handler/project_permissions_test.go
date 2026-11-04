package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestCreateProjectRequiresAdminOrOwner(t *testing.T) {
	for _, role := range []string{"member", "admin", "owner"} {
		for _, credential := range []string{"user", "pat", "task_token"} {
			for _, resources := range []bool{false, true} {
				name := role + "/" + credential + "/without_resources"
				if resources {
					name = role + "/" + credential + "/with_resources"
				}
				t.Run(name, func(t *testing.T) {
					userID := dbfx.User(t, name, name+"@project-permissions.test")
					dbfx.Member(t, testWorkspaceID, userID, role)
					// The agent owner is a workspace owner even when the token's
					// bound user is a member: only the bound user's role counts.
					token := projectPermissionToken(t, credential, userID)
					title := "create permission " + name
					body := map[string]any{"title": title}
					if resources {
						body["resources"] = []map[string]any{{
							"resource_type": "github_repo",
							"resource_ref":  map[string]any{"url": "https://github.com/multica-ai/multica"},
						}}
					}
					dbfx.Cleanup(t, `DELETE FROM project WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title)
					dbfx.Cleanup(t, `DELETE FROM project_resource WHERE project_id IN (SELECT id FROM project WHERE workspace_id = $1 AND title = $2)`, testWorkspaceID, title)
					req := newRequestAs(userID, "POST", "/api/projects", body)
					create := http.HandlerFunc(testHandler.CreateProject)
					if token != "" {
						req.Header.Set("Authorization", "Bearer "+token)
						create = middleware.Auth(testHandler.Queries, nil, nil, nil)(create).ServeHTTP
					}
					response := testutil.Call(t, create, req)
					count := dbfx.Count(t, `SELECT count(*) FROM project WHERE workspace_id = $1 AND title = $2`, testWorkspaceID, title)
					if role == "member" {
						if response.Code != http.StatusForbidden {
							t.Errorf("member create: want 403, got %d: %s", response.Code, response.Text())
						}
						if count != 0 {
							t.Errorf("member create wrote %d project rows, want 0", count)
						}
					} else {
						var project ProjectResponse
						response.Want(http.StatusCreated).JSON(&project)
						if count != 1 {
							t.Errorf("%s create wrote %d project rows, want 1", role, count)
						}
						wantResources := 0
						if resources {
							wantResources = 1
						}
						if got := dbfx.Count(t, `SELECT count(*) FROM project_resource WHERE project_id = $1`, project.ID); got != wantResources {
							t.Errorf("resource rows = %d, want %d", got, wantResources)
						}
					}

					// Exercise delete with the same credential so the shared role
					// gate cannot silently diverge for agent tokens or PATs.
					projectID := dbfx.Project(t, "delete permission "+name)
					deleteReq := withURLParam(newRequestAs(userID, "DELETE", "/api/projects/"+projectID, nil), "id", projectID)
					deleteHandler := http.HandlerFunc(testHandler.DeleteProject)
					if token != "" {
						deleteReq.Header.Set("Authorization", "Bearer "+token)
						deleteHandler = middleware.Auth(testHandler.Queries, nil, nil, nil)(deleteHandler).ServeHTTP
					}
					wantDelete := http.StatusNoContent
					if role == "member" {
						wantDelete = http.StatusForbidden
					}
					testutil.Call(t, deleteHandler, deleteReq).Want(wantDelete)
				})
			}
		}
	}
}

func TestCreateProjectChecksRoleBeforeValidation(t *testing.T) {
	userID := dbfx.User(t, "Project Member", "validation@project-permissions.test")
	dbfx.Member(t, testWorkspaceID, userID, "member")
	for _, body := range []string{`{`, `{}`, `{"title":"denied","resources":[{"resource_type":"invalid"}]}`} {
		req := testutil.WithHeaders(testutil.JSONRequest("POST", "/api/projects", body), "X-User-ID", userID, "X-Workspace-ID", testWorkspaceID)
		testutil.Call(t, testHandler.CreateProject, req).Want(http.StatusForbidden)
	}
}

func TestUpdateProjectStillAllowsMember(t *testing.T) {
	userID := dbfx.User(t, "Project Editor", "editor@project-permissions.test")
	dbfx.Member(t, testWorkspaceID, userID, "member")
	projectID := dbfx.Project(t, "member editable project")
	req := withURLParam(newRequestAs(userID, "PUT", "/api/projects/"+projectID, map[string]any{"title": "edited by member"}), "id", projectID)
	var project ProjectResponse
	testutil.Call(t, testHandler.UpdateProject, req).Want(http.StatusOK).JSON(&project)
	if project.Title != "edited by member" {
		t.Errorf("updated title = %q", project.Title)
	}
}

func projectPermissionToken(t *testing.T, credential, userID string) string {
	t.Helper()
	if credential == "user" {
		return ""
	}
	if credential == "pat" {
		token := "mul_project_permissions_" + userID
		dbfx.Insert(t, "personal_access_token", testutil.Cols{
			"user_id": userID, "name": "project permissions", "token_hash": auth.HashToken(token), "token_prefix": "mul_project",
		})
		return token
	}
	agentID := dbfx.Agent(t, "Project permission agent", testRuntimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": testRuntimeID})
	token := "mat_project_permissions_" + taskID
	dbfx.Insert(t, "task_token", testutil.Cols{
		"token_hash": auth.HashToken(token), "task_id": taskID, "agent_id": agentID,
		"workspace_id": testWorkspaceID, "user_id": userID, "expires_at": testutil.Raw("now() + interval '1 hour'"),
	})
	return token
}
