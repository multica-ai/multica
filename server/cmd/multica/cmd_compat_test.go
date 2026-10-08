package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestRunConfigSetPersistsValues(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := testCmd()

	if err := runConfigSet(cmd, []string{"server_url", "http://example.com"}); err != nil {
		t.Fatalf("runConfigSet(server_url) error = %v", err)
	}
	if err := runConfigSet(cmd, []string{"workspace_id", "ws-123"}); err != nil {
		t.Fatalf("runConfigSet(workspace_id) error = %v", err)
	}

	cfg, err := cli.LoadCLIConfig()
	if err != nil {
		t.Fatalf("LoadCLIConfig() error = %v", err)
	}
	if cfg.ServerURL != "http://example.com" {
		t.Fatalf("ServerURL = %q, want %q", cfg.ServerURL, "http://example.com")
	}
	if cfg.WorkspaceID != "ws-123" {
		t.Fatalf("WorkspaceID = %q, want %q", cfg.WorkspaceID, "ws-123")
	}
}
