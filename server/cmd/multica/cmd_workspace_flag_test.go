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

// workspaceFlagTestCmd builds a command carrying the same persistent flags the
// real root command exposes, so resolution helpers see --workspace,
// --workspace-id, --server-url, and --profile just as they do at runtime.
func workspaceFlagTestCmd() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	return cmd
}

// clearDaemonContextEnv strips every signal that would make the CLI treat the
// process as a daemon-managed agent task, so --workspace resolution runs the
// human/script path under test.
func clearDaemonContextEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MULTICA_AGENT_ID", "")
	t.Setenv("MULTICA_TASK_ID", "")
	t.Setenv("MULTICA_DAEMON_PORT", "")
	t.Setenv(cli.TaskConfigRootEnv, "")
}

const (
	bureauWorkspaceID = "11111111-1111-1111-1111-111111111111"
	digLabWorkspaceID = "22222222-2222-2222-2222-222222222222"
)

// workspaceListServer serves GET /api/workspaces with a fixed two-workspace
// roster (bureau + dig-lab) so slug resolution has something to match against.
func workspaceListServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/workspaces" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": bureauWorkspaceID, "name": "Bureau", "slug": "bureau"},
			{"id": digLabWorkspaceID, "name": "Dig Lab", "slug": "dig-lab"},
		})
	}))
}

func TestResolveTargetWorkspaceID_FlagUUIDOverridesConfigPointer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clearDaemonContextEnv(t)
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	// The mutable current-workspace pointer targets dig-lab.
	if err := cli.SaveCLIConfig(cli.CLIConfig{WorkspaceID: digLabWorkspaceID}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cmd := workspaceFlagTestCmd()
	if err := cmd.Flags().Set("workspace", bureauWorkspaceID); err != nil {
		t.Fatalf("set --workspace: %v", err)
	}

	got, err := resolveTargetWorkspaceID(cmd)
	if err != nil {
		t.Fatalf("resolveTargetWorkspaceID: %v", err)
	}
	if got != bureauWorkspaceID {
		t.Fatalf("resolveTargetWorkspaceID() = %q, want %q (flag must override config pointer)", got, bureauWorkspaceID)
	}
}

func TestResolveTargetWorkspaceID_AbsentFlagFallsBackToConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clearDaemonContextEnv(t)
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	if err := cli.SaveCLIConfig(cli.CLIConfig{WorkspaceID: digLabWorkspaceID}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	got, err := resolveTargetWorkspaceID(workspaceFlagTestCmd())
	if err != nil {
		t.Fatalf("resolveTargetWorkspaceID: %v", err)
	}
	if got != digLabWorkspaceID {
		t.Fatalf("resolveTargetWorkspaceID() = %q, want %q (config fallback)", got, digLabWorkspaceID)
	}
}

func TestResolveTargetWorkspaceID_FlagSlugResolvesToUUID(t *testing.T) {
	srv := workspaceListServer(t)
	defer srv.Close()

	t.Setenv("HOME", t.TempDir())
	clearDaemonContextEnv(t)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	// Config points at dig-lab; the flag names bureau by slug.
	if err := cli.SaveCLIConfig(cli.CLIConfig{WorkspaceID: digLabWorkspaceID}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cmd := workspaceFlagTestCmd()
	if err := cmd.Flags().Set("workspace", "bureau"); err != nil {
		t.Fatalf("set --workspace: %v", err)
	}

	got, err := resolveTargetWorkspaceID(cmd)
	if err != nil {
		t.Fatalf("resolveTargetWorkspaceID: %v", err)
	}
	if got != bureauWorkspaceID {
		t.Fatalf("resolveTargetWorkspaceID() = %q, want %q (slug must resolve to canonical UUID)", got, bureauWorkspaceID)
	}
}

func TestResolveTargetWorkspaceID_UnknownWorkspaceErrors(t *testing.T) {
	srv := workspaceListServer(t)
	defer srv.Close()

	t.Setenv("HOME", t.TempDir())
	clearDaemonContextEnv(t)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "")

	cmd := workspaceFlagTestCmd()
	if err := cmd.Flags().Set("workspace", "ghost"); err != nil {
		t.Fatalf("set --workspace: %v", err)
	}

	_, err := resolveTargetWorkspaceID(cmd)
	if err == nil {
		t.Fatal("resolveTargetWorkspaceID(): expected error for unknown workspace, got nil")
	}
	if !strings.Contains(err.Error(), "ghost") || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("resolveTargetWorkspaceID() error = %q, want a clear not-found message mentioning the input", err.Error())
	}
}

func TestResolveTargetWorkspaceID_IgnoredInDaemonContext(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Daemon-managed context: workspace is bound by the task token / env, and
	// the flag must not re-target it (nor read user config).
	t.Setenv("MULTICA_AGENT_ID", "agent-123")
	t.Setenv("MULTICA_TASK_ID", "task-456")
	t.Setenv("MULTICA_DAEMON_PORT", "")
	t.Setenv(cli.TaskConfigRootEnv, "")
	t.Setenv("MULTICA_WORKSPACE_ID", digLabWorkspaceID)

	cmd := workspaceFlagTestCmd()
	if err := cmd.Flags().Set("workspace", "bureau"); err != nil {
		t.Fatalf("set --workspace: %v", err)
	}

	got, err := resolveTargetWorkspaceID(cmd)
	if err != nil {
		t.Fatalf("resolveTargetWorkspaceID: %v", err)
	}
	if got != digLabWorkspaceID {
		t.Fatalf("resolveTargetWorkspaceID() = %q, want %q (daemon env, flag ignored)", got, digLabWorkspaceID)
	}
}

func TestNewAPIClient_WorkspaceFlagSetsClientWorkspaceID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clearDaemonContextEnv(t)
	t.Setenv("MULTICA_SERVER_URL", "https://api.example.test")
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	if err := cli.SaveCLIConfig(cli.CLIConfig{WorkspaceID: digLabWorkspaceID}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cmd := workspaceFlagTestCmd()
	if err := cmd.Flags().Set("workspace", bureauWorkspaceID); err != nil {
		t.Fatalf("set --workspace: %v", err)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		t.Fatalf("newAPIClient: %v", err)
	}
	if client.WorkspaceID != bureauWorkspaceID {
		t.Fatalf("client.WorkspaceID = %q, want %q", client.WorkspaceID, bureauWorkspaceID)
	}
}
