package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

func TestWorkspaceNotFoundInvocationContext(t *testing.T) {
	for _, mode := range []string{"default", "profile", "env", "flag", "workspace-env", "workspace-flag", "task", "task-config"} {
		t.Run(mode, func(t *testing.T) {
			t.Chdir(t.TempDir())
			redirectTestHome(t, t.TempDir())
			for _, key := range []string{"MULTICA_AGENT_ID", "MULTICA_TASK_ID", "MULTICA_DAEMON_PORT", "MULTICA_TOKEN", "MULTICA_WORKSPACE_ID", "MULTICA_SERVER_URL", cli.TaskConfigRootEnv, "MULTICA_DEBUG"} {
				t.Setenv(key, "")
			}
			t.Setenv("LC_ALL", "en_US.UTF-8")
			var selected, other atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				selected.Add(1)
				if r.URL.Path != "/api/issues/00000000-0000-0000-0000-000000000001" || r.Header.Get("X-Workspace-ID") != "synthetic-workspace" {
					t.Errorf("unexpected fixture request")
				}
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":"synthetic resource not found"}`))
			}))
			defer srv.Close()
			unused := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { other.Add(1); w.WriteHeader(500) }))
			defer unused.Close()
			cfg := cli.CLIConfig{ServerURL: unused.URL, WorkspaceID: "synthetic-workspace", Token: "synthetic-member"}
			if err := cli.SaveCLIConfig(cfg); err != nil {
				t.Fatal(err)
			}
			if err := cli.SaveCLIConfigForProfile(cfg, "other"); err != nil {
				t.Fatal(err)
			}
			cmd := &cobra.Command{}
			cmd.Flags().String("profile", "", "")
			cmd.Flags().String("server-url", "", "")
			cmd.Flags().String("workspace-id", "", "")
			cmd.Flags().String("output", "json", "")
			profile := "default"
			switch mode {
			case "default":
				cfg.ServerURL = srv.URL
				if err := cli.SaveCLIConfig(cfg); err != nil {
					t.Fatal(err)
				}
			case "profile":
				profile = "staging"
				cfg.ServerURL = srv.URL
				if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
					t.Fatal(err)
				}
				_ = cmd.Flags().Set("profile", profile)
			case "env":
				_ = cmd.Flags().Set("profile", "other")
				profile = "other"
				t.Setenv("MULTICA_SERVER_URL", srv.URL)
			case "flag":
				t.Setenv("MULTICA_SERVER_URL", unused.URL)
				_ = cmd.Flags().Set("server-url", srv.URL)
			case "workspace-env", "workspace-flag":
				cfg.ServerURL = srv.URL
				cfg.WorkspaceID = "config-workspace-not-selected"
				if err := cli.SaveCLIConfig(cfg); err != nil {
					t.Fatal(err)
				}
				if mode == "workspace-env" {
					t.Setenv("MULTICA_WORKSPACE_ID", "synthetic-workspace")
				} else {
					t.Setenv("MULTICA_WORKSPACE_ID", "env-workspace-not-selected")
					_ = cmd.Flags().Set("workspace-id", "synthetic-workspace")
				}
			case "task", "task-config":
				t.Setenv("MULTICA_AGENT_ID", "synthetic-agent")
				t.Setenv("MULTICA_TOKEN", "mat_synthetic")
				t.Setenv("MULTICA_WORKSPACE_ID", "synthetic-workspace")
				// Poison the Owner config; only task-local configuration may be used.
				sentinel := filepath.Join(t.TempDir(), "owner")
				if err := os.MkdirAll(filepath.Join(sentinel, ".multica"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(sentinel, ".multica", "config.json"), []byte("OWNER_SENTINEL_INVALID_JSON"), 0600); err != nil {
					t.Fatal(err)
				}
				redirectTestHome(t, sentinel)
				if mode == "task" {
					t.Setenv("MULTICA_SERVER_URL", srv.URL)
				} else {
					t.Setenv(cli.TaskConfigRootEnv, t.TempDir())
					cfg.ServerURL = srv.URL
					cfg.WorkspaceID = "task-config-workspace-not-selected"
					if err := cli.SaveCLIConfig(cfg); err != nil {
						t.Fatal(err)
					}
				}
			}
			stdout, err := captureStdout(t, func() error { return runIssueGet(cmd, []string{"00000000-0000-0000-0000-000000000001"}) })
			var httpErr *cli.HTTPError
			if !errors.As(err, &httpErr) || cli.ExitCodeFor(err) != 4 {
				t.Fatalf("expected preserved HTTP404/exit4, got %v", err)
			}
			if stdout != "" || selected.Load() != 1 || other.Load() != 0 {
				t.Fatalf("stdout=%q requests=%d other=%d", stdout, selected.Load(), other.Load())
			}
			for _, locale := range []string{"en_US.UTF-8", "zh_CN.UTF-8"} {
				t.Setenv("LC_ALL", locale)
				msg := cli.FormatError(err, false)
				if !strings.Contains(msg, srv.URL) {
					t.Errorf("missing effective server context: %s", msg)
				}
				if strings.HasPrefix(mode, "task") {
					if !strings.Contains(msg, "task") || strings.Contains(msg, "profile=") || strings.Contains(msg, "--profile") {
						t.Errorf("incorrect task context: %s", msg)
					}
				} else if !strings.Contains(msg, "profile="+profile) {
					t.Errorf("missing effective profile: %s", msg)
				}
				if strings.Contains(msg, "synthetic-workspace") || strings.Contains(msg, "synthetic-member") || strings.Contains(msg, "OWNER_SENTINEL") {
					t.Errorf("unsafe diagnostic: %s", msg)
				}
			}
		})
	}
}
