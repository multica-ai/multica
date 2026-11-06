package handler

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestProjectDeletionPermissions(t *testing.T) {
	for _, tc := range []struct {
		name, role, actor, creator string
		allowed                    bool
	}{
		{"member deletes own", "member", "member", "self", true},
		{"member denied others", "member", "member", "other", false},
		{"member deletes own agent project", "member", "member", "owned-agent", true},
		{"member denied other agent project", "member", "member", "other-agent", false},
		{"owner deletes others", "owner", "member", "other", true},
		{"admin deletes others", "admin", "member", "other", true},
		{"agent deletes own", "member", "agent", "owned-agent", true},
		{"agent denied human project", "owner", "agent", "self", false},
		{"agent denied sibling project", "owner", "agent", "other-agent", false},
		{"cloud PAT denied admin rights", "admin", "cloud_pat", "other", false},
		{"cloud PAT denied owner rights", "owner", "cloud_pat", "other", false},
		{"cloud PAT denied creator rights", "member", "cloud_pat", "self", false},
		{"member denied unknown creator", "member", "member", "unknown", false},
		{"admin deletes unknown creator", "admin", "member", "unknown", true},
		{"agent denied unknown creator", "owner", "agent", "unknown", false},
		{"member denied missing agent", "member", "member", "missing-agent", false},
		{"admin deletes missing agent project", "admin", "member", "missing-agent", true},
		{"member denied former agent", "member", "member", "former-agent", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := dbfx.Workspace(t, "project deletion", "delete-"+uuid.NewString())
			dbfx.Member(t, ws, testUserID, tc.role)
			agent := dbfx.Agent(t, "own agent", "", testutil.Cols{"workspace_id": ws})
			other := dbfx.User(t, "Other", "delete-"+uuid.NewString()+"@example.test")
			dbfx.Member(t, ws, other, "member")
			otherAgent := dbfx.Agent(t, "other agent", "", testutil.Cols{"workspace_id": ws, "owner_id": other})
			id := ""
			if tc.creator == "unknown" {
				id = dbfx.Project(t, "legacy project", testutil.Cols{"workspace_id": ws})
			} else {
				creatorUser, creatorAgent := testUserID, ""
				switch tc.creator {
				case "other":
					creatorUser = other
				case "owned-agent", "missing-agent", "former-agent":
					creatorAgent = agent
				case "other-agent":
					creatorUser, creatorAgent = other, otherAgent
				}
				req := newRequestAs(creatorUser, http.MethodPost, "/api/projects", map[string]any{"title": "delete target"})
				req.Header.Set("X-Workspace-ID", ws)
				if creatorAgent != "" {
					req.Header.Set("X-Actor-Source", "task_token")
					req.Header.Set("X-Agent-ID", creatorAgent)
				}
				var created ProjectResponse
				testutil.Call(t, testHandler.CreateProject, req).Want(http.StatusCreated).JSON(&created)
				id = created.ID
				t.Cleanup(func() { dbfx.Exec(t, "DELETE FROM project WHERE id=$1", id) })
				if tc.creator == "missing-agent" {
					dbfx.Exec(t, "DELETE FROM agent WHERE id=$1", agent)
				} else if tc.creator == "former-agent" {
					dbfx.Exec(t, "UPDATE agent SET owner_id=$1 WHERE id=$2", other, agent)
				}
			}
			req := withURLParam(newRequest(http.MethodDelete, "/api/projects/"+id, nil), "id", id)
			req.Header.Set("X-Workspace-ID", ws)
			if tc.actor == "agent" {
				req.Header.Set("X-Actor-Source", "task_token")
				req.Header.Set("X-Agent-ID", agent)
			} else if tc.actor == "cloud_pat" {
				req.Header.Set("X-Actor-Source", tc.actor)
			}
			want := http.StatusForbidden
			if tc.allowed {
				want = http.StatusNoContent
			}
			testutil.Call(t, testHandler.DeleteProject, req).Want(want)
			if count := dbfx.Count(t, "SELECT count(*) FROM project WHERE id=$1", id); (count == 0) != tc.allowed {
				t.Fatalf("remaining projects = %d, allowed = %v", count, tc.allowed)
			}
		})
	}
}

func TestCreateProjectRecordsCreator(t *testing.T) {
	for _, actor := range []string{"member", "agent"} {
		for _, resources := range []bool{false, true} {
			t.Run(actor+"/resources="+strconv.FormatBool(resources), func(t *testing.T) {
				ws := dbfx.Workspace(t, "project creator", "creator-"+uuid.NewString())
				dbfx.Member(t, ws, testUserID, "member")
				agent := dbfx.Agent(t, "creator agent", "", testutil.Cols{"workspace_id": ws})
				body := map[string]any{"title": "created project", "creator_type": "agent", "creator_id": uuid.NewString()}
				if resources {
					body["resources"] = []map[string]any{{"resource_type": "github_repo", "resource_ref": map[string]any{"url": "https://github.com/multica-ai/multica"}}}
				}
				req := newRequest(http.MethodPost, "/api/projects", body)
				req.Header.Set("X-Workspace-ID", ws)
				creatorID := testUserID
				if actor == "agent" {
					req.Header.Set("X-Actor-Source", "task_token")
					req.Header.Set("X-Agent-ID", agent)
					creatorID = agent
				}
				var out map[string]any
				testutil.Call(t, testHandler.CreateProject, req).Want(http.StatusCreated).JSON(&out)
				id := out["id"].(string)
				t.Cleanup(func() {
					dbfx.Exec(t, "DELETE FROM project_resource WHERE project_id=$1", id)
					dbfx.Exec(t, "DELETE FROM project WHERE id=$1", id)
				})
				if out["creator_type"] != actor || out["creator_id"] != creatorID {
					t.Fatalf("creator = %v:%v, want %s:%s", out["creator_type"], out["creator_id"], actor, creatorID)
				}
				var storedType, storedID string
				dbfx.QueryRow(t, "SELECT creator_type, creator_id::text FROM project WHERE id=$1", id).Scan(&storedType, &storedID)
				if storedType != actor || storedID != creatorID {
					t.Fatalf("stored creator = %s:%s", storedType, storedID)
				}
				// Creator is immutable: changing the lead or submitting creator fields cannot transfer deletion rights.
				req = withURLParam(newRequest(http.MethodPut, "/api/projects/"+id, map[string]any{"lead_type": "agent", "lead_id": agent, "creator_type": "member", "creator_id": uuid.NewString()}), "id", id)
				req.Header.Set("X-Workspace-ID", ws)
				testutil.Call(t, testHandler.UpdateProject, req).Want(http.StatusOK).JSON(&out)
				if out["creator_type"] != actor || out["creator_id"] != creatorID {
					t.Fatal("update changed creator")
				}
				for _, read := range []string{"detail", "list", "search"} {
					url := "/api/projects/" + id
					handler := testHandler.GetProject
					switch read {
					case "list":
						url, handler = "/api/projects", testHandler.ListProjects
					case "search":
						url, handler = "/api/projects/search?q=created", testHandler.SearchProjects
					}
					req = withURLParam(newRequest(http.MethodGet, url, nil), "id", id)
					req.Header.Set("X-Workspace-ID", ws)
					testutil.Call(t, handler, req).Want(http.StatusOK).JSON(&out)
					if read != "detail" {
						out = out["projects"].([]any)[0].(map[string]any)
					}
					if out["creator_type"] != actor || out["creator_id"] != creatorID {
						t.Fatalf("read omitted creator: %v", out)
					}
				}
			})
		}
	}
}
