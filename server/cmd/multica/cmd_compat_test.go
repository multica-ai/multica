package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
)

func TestLegacyCompatibilityCommandsRemainAvailable(t *testing.T) {
	t.Run("auth login remains available", func(t *testing.T) {
		if _, _, err := authCmd.Find([]string{"login"}); err != nil {
			t.Fatalf("expected auth login command to exist: %v", err)
		}
	})

	t.Run("workspace get remains available", func(t *testing.T) {
		if _, _, err := workspaceCmd.Find([]string{"get"}); err != nil {
			t.Fatalf("expected workspace get command to exist: %v", err)
		}
	})

	t.Run("workspace members remains available", func(t *testing.T) {
		if _, _, err := workspaceCmd.Find([]string{"members"}); err != nil {
			t.Fatalf("expected workspace members command to exist: %v", err)
		}
	})

	t.Run("config show and set remain available", func(t *testing.T) {
		if _, _, err := configCmd.Find([]string{"show"}); err != nil {
			t.Fatalf("expected config show command to exist: %v", err)
		}
		if _, _, err := configCmd.Find([]string{"set"}); err != nil {
			t.Fatalf("expected config set command to exist: %v", err)
		}
	})
}

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

func TestRunConfigSetPersistsAutoPublishSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := testCmd()

	if err := runConfigSet(cmd, []string{"auto_publish", "true"}); err != nil {
		t.Fatalf("runConfigSet(auto_publish) error = %v", err)
	}
	if err := runConfigSet(cmd, []string{"publish_remote", "fork"}); err != nil {
		t.Fatalf("runConfigSet(publish_remote) error = %v", err)
	}

	cfg, err := cli.LoadCLIConfig()
	if err != nil {
		t.Fatalf("LoadCLIConfig() error = %v", err)
	}
	if !cfg.AutoPublish {
		t.Fatalf("AutoPublish = false, want true")
	}
	if cfg.PublishRemote != "fork" {
		t.Fatalf("PublishRemote = %q, want %q", cfg.PublishRemote, "fork")
	}
}
