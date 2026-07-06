package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

func newConfigTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config"}
	cmd.Flags().String("profile", "", "")
	return cmd
}

func TestRunConfigSetPersistsSupportedKeysInProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newConfigTestCmd()
	_ = cmd.Flags().Set("profile", "dev")

	stderr := captureStderr(t)
	defer stderr.restore()
	if err := runConfigSet(cmd, []string{"server_url", "http://127.0.0.1:8080"}); err != nil {
		t.Fatalf("runConfigSet server_url: %v", err)
	}
	if err := runConfigSet(cmd, []string{"app_url", "http://127.0.0.1:3000"}); err != nil {
		t.Fatalf("runConfigSet app_url: %v", err)
	}
	if err := runConfigSet(cmd, []string{"workspace_id", "ws-123"}); err != nil {
		t.Fatalf("runConfigSet workspace_id: %v", err)
	}
	_ = stderr.read()

	cfg, err := cli.LoadCLIConfigForProfile("dev")
	if err != nil {
		t.Fatalf("LoadCLIConfigForProfile: %v", err)
	}
	if cfg.ServerURL != "http://127.0.0.1:8080" || cfg.AppURL != "http://127.0.0.1:3000" || cfg.WorkspaceID != "ws-123" {
		t.Fatalf("config = %#v, want persisted supported keys", cfg)
	}
}

func TestRunConfigShowIncludesProfileAndDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newConfigTestCmd()
	_ = cmd.Flags().Set("profile", "empty")

	out, err := captureStdout(t, func() error { return runConfigShow(cmd, nil) })
	if err != nil {
		t.Fatalf("runConfigShow: %v", err)
	}
	for _, want := range []string{
		"Profile:      empty",
		"server_url:   (not set)",
		"app_url:      (not set)",
		"workspace_id: (not set)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("runConfigShow output missing %q:\n%s", want, out)
		}
	}
}

func TestRunConfigSetRejectsUnknownKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cmd := newConfigTestCmd()
	err := runConfigSet(cmd, []string{"token", "secret"})
	if err == nil || !strings.Contains(err.Error(), "unknown config key") {
		t.Fatalf("runConfigSet error = %v, want unknown key", err)
	}
}
