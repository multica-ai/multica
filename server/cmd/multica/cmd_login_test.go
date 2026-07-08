package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func newLoginTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "login"}
	cmd.Flags().String("token", "", "")
	cmd.Flags().String("profile", "", "")
	return cmd
}

func TestRunLoginTokenAutoWatchesDiscoveredWorkspaces(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "")
	t.Setenv("MULTICA_WORKSPACE_ID", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mul_test_token" {
			t.Fatalf("Authorization = %q, want bearer token", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/me":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":  "Ada",
				"email": "ada@example.com",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/workspaces":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "ws-1", "name": "Alpha"},
				{"id": "ws-2", "name": "Beta"},
			})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newLoginTestCmd()
	_ = cmd.Flags().Set("token", "mul_test_token")

	stderr := captureStderr(t)
	err := runLogin(cmd, nil)
	errOut := stderr.read()
	if err != nil {
		t.Fatalf("runLogin: %v", err)
	}
	if !strings.Contains(errOut, "Found 2 workspace(s):") || !strings.Contains(errOut, "daemon start") {
		t.Fatalf("stderr = %q, want workspace discovery and daemon hint", errOut)
	}

	cfg, err := cli.LoadCLIConfig()
	if err != nil {
		t.Fatalf("LoadCLIConfig: %v", err)
	}
	if cfg.Token != "mul_test_token" || cfg.ServerURL != srv.URL || cfg.WorkspaceID != "ws-1" {
		t.Fatalf("config = %#v, want token, server URL, and first workspace", cfg)
	}
}
