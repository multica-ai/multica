package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestResidentNewAPIClient(t *testing.T) {
	const valid = `{"task_id":"task-file","agent_id":"agent-file","workspace_id":"workspace-file","token":"mat_file"}`
	for _, tc := range []struct {
		name     string
		owner    string
		data     string
		task     string
		token    string
		marker   bool
		wantFile bool
		wantErr  bool
	}{
		{name: "nested", owner: "1", data: valid, wantFile: true},
		{name: "task marker guard satisfied", owner: "1", data: valid, marker: true, wantFile: true},
		{name: "owner member token replaced", owner: "1", data: valid, token: "mul_owner", marker: true, wantFile: true},
		{name: "owner opt-in required", data: valid},
		{name: "explicit task", owner: "1", data: valid, task: "task-explicit", token: "mat_explicit"},
		{name: "explicit task without token fails closed", owner: "1", data: valid, task: "task-explicit", wantErr: true},
		{name: "explicit token", owner: "1", data: valid, token: "mat_explicit"},
		{name: "malformed", owner: "1", data: "{"},
		{name: "member token", owner: "1", data: `{"task_id":"task-file","token":"mul_member"}`},
		{name: "missing identity", owner: "1", data: `{"token":"mat_file"}`},
		{name: "missing file guard unchanged", owner: "1", marker: true, wantErr: true},
		{name: "invalid file guard unchanged", owner: "1", data: "{", marker: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redirectTestHome(t, t.TempDir())
			t.Setenv("MULTICA_RESIDENT_OWNER", tc.owner)
			t.Setenv("MULTICA_TASK_ID", tc.task)
			t.Setenv("MULTICA_TOKEN", tc.token)
			t.Setenv("MULTICA_AGENT_ID", "")
			t.Setenv("MULTICA_DAEMON_PORT", "")
			t.Setenv("MULTICA_WORKSPACE_ID", "workspace-explicit")
			t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:8080")
			root := t.TempDir()
			if tc.data != "" {
				if err := os.WriteFile(filepath.Join(root, ".multica_resident_task.json"), []byte(tc.data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.marker {
				path := filepath.Join(root, "workdir", execenv.TaskContextMarkerRelPath)
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(`{"managed_by":"`+execenv.TaskContextMarkerManagedBy+`","agent_id":"agent-file","issue_id":"issue-1"}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			nested := filepath.Join(root, "workdir", "repo", "pkg")
			if err := os.MkdirAll(nested, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Chdir(nested)
			client, err := newAPIClient(testCmd())
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "mat_ token") {
					t.Fatalf("expected task token guard, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantFile {
				if client.Token != "mat_file" || client.TaskID != "task-file" || client.AgentID != "agent-file" || client.WorkspaceID != "workspace-file" {
					t.Error("client did not use resident task identity and workspace")
				}
			} else if client.Token != tc.token || client.TaskID != tc.task || client.AgentID != "" || client.WorkspaceID != "workspace-explicit" {
				t.Error("client did not preserve explicit identity and workspace")
			}
		})
	}
}
