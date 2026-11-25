package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
)

// TestPrivateProjectOwnerRecoveryRejectsMachineCredentials runs the real
// Auth -> router -> handler chain for the workspace owner's way into private
// projects (MUL-9284). A task token (mat_) and a cloud node PAT (mcn_) both
// authenticate as the owner, but neither may list private projects or add
// the owner to one; the owner's own JWT and PAT keep both abilities.
func TestPrivateProjectOwnerRecoveryRejectsMachineCredentials(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	f := dbfx.New(testPool, testWorkspaceID, testUserID)
	f.WorkspaceID = f.Workspace(t, "Private project recovery", "private-recovery-"+uuid.NewString())
	f.Member(t, f.WorkspaceID, f.UserID, "owner")
	insider := f.User(t, "Private project insider", uuid.NewString()+"@example.test")
	f.Member(t, f.WorkspaceID, insider, "member")
	projectID := f.Project(t, "Recovery target", dbfx.Cols{"visibility": "private"})
	f.InsertNoID(t, "project_member", dbfx.Cols{
		"workspace_id": f.WorkspaceID, "project_id": projectID, "user_id": insider,
	}, "project_id = $1", projectID)
	f.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, f.WorkspaceID)

	runtimeID := f.Runtime(t, "Private project recovery")
	agentID := f.Agent(t, "Private project recovery", runtimeID)
	taskID := f.Task(t, agentID, dbfx.Cols{"runtime_id": runtimeID})
	task := "mat_" + uuid.NewString()
	f.InsertNoID(t, "task_token", dbfx.Cols{
		"token_hash": auth.HashToken(task), "task_id": taskID, "agent_id": agentID,
		"workspace_id": f.WorkspaceID, "user_id": f.UserID,
		"expires_at": dbfx.Raw("now() + interval '1 hour'"),
	}, "token_hash = $1", auth.HashToken(task))

	pat, err := auth.GeneratePATToken()
	if err != nil {
		t.Fatal(err)
	}
	f.Insert(t, "personal_access_token", dbfx.Cols{
		"user_id": f.UserID, "name": "private project recovery", "token_hash": auth.HashToken(pat), "token_prefix": pat[:12],
	})

	cloud := "mcn_" + uuid.NewString()
	fleet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Token string `json:"token"`
		}
		if r.URL.Path != "/api/v1/pat/verify" || json.NewDecoder(r.Body).Decode(&request) != nil || request.Token != cloud {
			http.Error(w, "invalid verification request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"valid": true, "owner_id": f.UserID})
	}))
	t.Cleanup(fleet.Close)
	t.Setenv("MULTICA_CLOUD_URL", fleet.URL)
	router, _ := NewRouterWithOptions(testPool, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil, RouterOptions{})

	call := func(t *testing.T, method, path, token string) *dbfx.Response {
		t.Helper()
		req := dbfx.JSONRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Workspace-ID", f.WorkspaceID)
		// Auth must discard this client assertion and stamp its own.
		req.Header.Set("X-Actor-Source", "member")
		return dbfx.Call(t, router.ServeHTTP, req)
	}
	ownerIsMember := func() bool {
		return f.Count(t, `SELECT count(*) FROM project_member WHERE project_id = $1 AND user_id = $2`, projectID, f.UserID) == 1
	}
	listPath, joinPath := "/api/projects/private", "/api/projects/"+projectID+"/join"

	for name, token := range map[string]string{"task token": task, "cloud PAT": cloud} {
		t.Run(name, func(t *testing.T) {
			call(t, http.MethodGet, listPath, token).Want(http.StatusForbidden)
			call(t, http.MethodPost, joinPath, token).Want(http.StatusForbidden)
			if ownerIsMember() {
				t.Fatal("a rejected join added the owner to the project")
			}
		})
	}

	t.Run("owner JWT lists private projects", func(t *testing.T) {
		var resp struct {
			Projects []struct {
				ID string `json:"id"`
			} `json:"projects"`
		}
		call(t, http.MethodGet, listPath, testToken).Want(http.StatusOK).JSON(&resp)
		if len(resp.Projects) != 1 || resp.Projects[0].ID != projectID {
			t.Fatalf("projects = %+v, want the one private project", resp.Projects)
		}
	})
	t.Run("owner PAT joins", func(t *testing.T) {
		call(t, http.MethodPost, joinPath, pat).Want(http.StatusOK)
		if !ownerIsMember() {
			t.Fatal("the owner's PAT join did not add them to the project")
		}
	})
}
