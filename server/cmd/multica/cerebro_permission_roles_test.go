package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestPermissionRolesCommandsAreRegistered(t *testing.T) {
	roles, _, err := permissionsCmd.Find([]string{"roles"})
	if err != nil || roles == nil {
		t.Fatalf("permissions roles command not found: %v", err)
	}
	for _, name := range []string{"list", "show", "create", "update", "archive", "assign", "unassign"} {
		if cmd, _, findErr := roles.Find([]string{name}); findErr != nil || cmd == nil {
			t.Errorf("permissions roles %s not found: %v", name, findErr)
		}
	}
}

func TestListPermissionRolesUsesWorkspaceRolesEndpoint(t *testing.T) {
	var path, query string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, query = r.URL.Path, r.URL.RawQuery
		_ = json.NewEncoder(w).Encode([]permissionRole{})
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, permTestWorkspaceID, "test-token")
	if err := listPermissionRoles(context.Background(), client, true, "json"); err != nil {
		t.Fatalf("listPermissionRoles: %v", err)
	}
	if path != "/api/workspaces/"+permTestWorkspaceID+"/roles" {
		t.Fatalf("path = %q, want workspace Roles endpoint", path)
	}
	if query != "include_archived=true" {
		t.Fatalf("query = %q, want include_archived=true", query)
	}
}

func TestShowPermissionRoleReadsRoleAndAssignments(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/roles/role-1/assignments" {
			_ = json.NewEncoder(w).Encode([]permissionRoleAssignment{})
			return
		}
		_ = json.NewEncoder(w).Encode(permissionRole{ID: "role-1", Name: "Reviewer", Version: 2})
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, permTestWorkspaceID, "test-token")
	if err := showPermissionRole(context.Background(), client, "role-1", "json"); err != nil {
		t.Fatalf("showPermissionRole: %v", err)
	}
	if len(paths) != 2 || paths[0] != "/api/roles/role-1" || paths[1] != "/api/roles/role-1/assignments" {
		t.Fatalf("paths = %#v, want Role then assignments", paths)
	}
}
