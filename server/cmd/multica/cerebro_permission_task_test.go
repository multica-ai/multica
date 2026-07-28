package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestPermissionTaskCommandUsesTaskAccessEndpoint(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewEncoder(w).Encode(permissionTaskAccess{
			TaskID: "task-1", AgentID: "agent-1", AllowedTools: []string{"tools:Read"}, Status: "active",
		})
	}))
	defer srv.Close()

	client := cli.NewAPIClient(srv.URL, permTestWorkspaceID, "test-token")
	if err := showPermissionTask(context.Background(), client, "task-1", "json"); err != nil {
		t.Fatalf("showPermissionTask: %v", err)
	}
	if path != "/api/tasks/task-1/access" {
		t.Fatalf("path = %q, want task access endpoint", path)
	}
}
