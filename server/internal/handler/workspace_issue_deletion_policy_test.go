package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestWorkspaceIssueDeletionPolicyAuthorization(t *testing.T) {
	for _, tc := range []struct {
		name, role, source string
		want               int
	}{
		{"owner", "owner", "", 200},
		{"admin", "admin", "", 200},
		{"member", "member", "", 403},
		{"owner task token", "owner", "task_token", 403},
		{"admin cloud PAT", "admin", "cloud_pat", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ws := dbfx.Workspace(t, "policy endpoint", "policy-"+uuid.NewString(), testutil.Cols{"restrict_issue_deletion": true, "settings": testutil.Raw(`'{"github_enabled":false}'::jsonb`)})
			dbfx.Member(t, ws, testUserID, tc.role)
			req := withURLParam(newRequest(http.MethodPatch, "/api/workspaces/"+ws+"/issue-deletion-policy", map[string]any{"restrict_issue_deletion": false}), "id", ws)
			req.Header.Set("X-Workspace-ID", ws)
			if tc.source != "" {
				req.Header.Set("X-Actor-Source", tc.source)
			}
			result := testutil.Call(t, testHandler.UpdateWorkspaceIssueDeletionPolicy, req).Want(tc.want)
			workspace, err := testHandler.Queries.GetWorkspace(context.Background(), parseUUID(ws))
			if err != nil {
				t.Fatal(err)
			}
			if workspace.RestrictIssueDeletion != (tc.want != 200) {
				t.Fatalf("saved policy=%v, status=%d", workspace.RestrictIssueDeletion, tc.want)
			}
			if tc.want == 200 {
				var response WorkspaceResponse
				result.JSON(&response)
				if !response.IssueDeletionPolicySupported || response.RestrictIssueDeletion {
					t.Fatalf("incorrect saved policy response: %+v", response)
				}
			}
			var settings map[string]any
			if err := json.Unmarshal(workspace.Settings, &settings); err != nil {
				t.Fatal(err)
			}
			if settings["github_enabled"] != false {
				t.Fatalf("changed unrelated settings: %v", settings)
			}
		})
	}
}

func TestWorkspaceIssueDeletionPolicyRejectsInvalidInput(t *testing.T) {
	ws := dbfx.Workspace(t, "policy validation", "policy-"+uuid.NewString(), testutil.Cols{"restrict_issue_deletion": true})
	dbfx.Member(t, ws, testUserID, "owner")
	for _, body := range []any{nil, map[string]any{}, map[string]any{"restrict_issue_deletion": nil}, map[string]any{"restrict_issue_deletion": "false"}, map[string]any{"restrict_issue_deletion": false, "settings": map[string]any{}}, []bool{false}} {
		req := withURLParam(newRequest(http.MethodPatch, "/api/workspaces/"+ws+"/issue-deletion-policy", body), "id", ws)
		req.Header.Set("X-Workspace-ID", ws)
		testutil.Call(t, testHandler.UpdateWorkspaceIssueDeletionPolicy, req).Want(http.StatusBadRequest)
	}
	var restricted bool
	dbfx.QueryRow(t, "SELECT restrict_issue_deletion FROM workspace WHERE id=$1", ws).Scan(&restricted)
	if !restricted {
		t.Fatal("invalid request changed the policy")
	}
}

func TestWorkspaceIssueDeletionPolicyIsIndependentOfSettings(t *testing.T) {
	ws := dbfx.Workspace(t, "independent policy", "policy-"+uuid.NewString(), testutil.Cols{"settings": testutil.Raw(`'{"github_enabled":true,"old_key":"old"}'::jsonb`)})
	dbfx.Member(t, ws, testUserID, "owner")
	admin := dbfx.User(t, "Admin", "policy-"+uuid.NewString()+"@example.test")
	dbfx.Member(t, ws, admin, "admin")
	setPolicy := func(value bool) {
		t.Helper()
		req := withURLParam(newRequest(http.MethodPatch, "/api/workspaces/"+ws+"/issue-deletion-policy", map[string]any{"restrict_issue_deletion": value}), "id", ws)
		req.Header.Set("X-User-ID", admin)
		req.Header.Set("X-Workspace-ID", ws)
		testutil.Call(t, testHandler.UpdateWorkspaceIssueDeletionPolicy, req).Want(http.StatusOK)
	}
	setPolicy(true)
	// An older client's full snapshot includes a stale JSON key and even a
	// top-level policy field; neither belongs to the generic update contract.
	req := withURLParam(newRequest(http.MethodPatch, "/api/workspaces/"+ws, map[string]any{
		"restrict_issue_deletion": false,
		"settings":                map[string]any{"github_enabled": false, "restrict_issue_deletion": false},
	}), "id", ws)
	req.Header.Set("X-Workspace-ID", ws)
	testutil.Call(t, testHandler.UpdateWorkspace, req).Want(http.StatusOK)
	workspace, err := testHandler.Queries.GetWorkspace(context.Background(), parseUUID(ws))
	if err != nil {
		t.Fatal(err)
	}
	if !workspace.RestrictIssueDeletion {
		t.Fatal("a generic settings snapshot disabled the policy")
	}
	var settings map[string]any
	if err := json.Unmarshal(workspace.Settings, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["github_enabled"] != false {
		t.Fatalf("settings were not saved: %v", settings)
	}
	if _, exists := settings["old_key"]; exists {
		t.Fatal("generic settings no longer use their original replacement semantics")
	}
	setPolicy(false)
	workspace, err = testHandler.Queries.GetWorkspace(context.Background(), parseUUID(ws))
	if err != nil {
		t.Fatal(err)
	}
	if workspace.RestrictIssueDeletion {
		t.Fatal("explicit policy update did not take effect")
	}
	if err := json.Unmarshal(workspace.Settings, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["github_enabled"] != false {
		t.Fatal("policy update overwrote the other administrator's setting")
	}
	// List responses power the shared clients and must carry support and value.
	var response []WorkspaceResponse
	testutil.Call(t, testHandler.ListWorkspaces, newRequest(http.MethodGet, "/api/workspaces", nil)).Want(http.StatusOK).JSON(&response)
	for _, item := range response {
		if item.ID == ws {
			if !item.IssueDeletionPolicySupported || item.RestrictIssueDeletion {
				t.Fatal("list returned an incorrect policy")
			}
			return
		}
	}
	t.Fatal("workspace missing from list response")
}
