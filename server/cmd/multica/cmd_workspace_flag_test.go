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

// Commands that build workspace-scoped URL paths (repo, runtime-profile, ...)
// read the id through requireWorkspaceID; it must honor --workspace so the path
// and the X-Workspace-ID header never point at different workspaces.
func TestRequireWorkspaceID_HonorsWorkspaceFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clearDaemonContextEnv(t)
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	if err := cli.SaveCLIConfig(cli.CLIConfig{WorkspaceID: digLabWorkspaceID}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cmd := workspaceFlagTestCmd()
	if err := cmd.Flags().Set("workspace", bureauWorkspaceID); err != nil {
		t.Fatalf("set --workspace: %v", err)
	}

	got, err := requireWorkspaceID(cmd)
	if err != nil {
		t.Fatalf("requireWorkspaceID: %v", err)
	}
	if got != bureauWorkspaceID {
		t.Fatalf("requireWorkspaceID() = %q, want %q", got, bureauWorkspaceID)
	}
}

func TestWorkspaceFlagPinsResolvedSlugAcrossPathAndHeader(t *testing.T) {
	lists := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lists++
		id := bureauWorkspaceID
		if lists > 1 {
			// Another user renamed/reassigned the slug between requests.
			id = digLabWorkspaceID
		}
		json.NewEncoder(w).Encode([]map[string]any{{"id": id, "slug": "bureau"}})
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	clearDaemonContextEnv(t)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "")
	cmd := workspaceFlagTestCmd()
	_ = cmd.Flags().Set("workspace", "bureau")
	client, pathID, err := repoCommandClient(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if client.WorkspaceID != pathID || pathID != bureauWorkspaceID {
		t.Fatalf("header workspace %q differs from pinned path workspace %q", client.WorkspaceID, pathID)
	}
	if lists != 1 {
		t.Fatalf("workspace list requests = %d, want one per invocation", lists)
	}
}

func workspaceFlagRootForTest(serverURL string) *cobra.Command {
	root := &cobra.Command{Use: "multica", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("workspace", "", "")
	root.PersistentFlags().String("workspace-id", "", "")
	root.PersistentFlags().String("server-url", serverURL, "")
	_ = root.PersistentFlags().Set("server-url", serverURL)
	root.PersistentFlags().String("profile", "", "")
	return root
}

func TestWorkspaceUpdateRejectsPositionalWithWorkspaceFlag(t *testing.T) {
	for _, target := range []string{bureauWorkspaceID, "bureau"} {
		t.Run(target, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			clearDaemonContextEnv(t)
			t.Setenv("MULTICA_TOKEN", "test-token")
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path == "/api/workspaces" {
					_ = json.NewEncoder(w).Encode([]map[string]any{{"id": digLabWorkspaceID, "slug": "bureau"}})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": bureauWorkspaceID})
			}))
			defer srv.Close()
			root := workspaceFlagRootForTest(srv.URL)
			workspace := &cobra.Command{Use: "workspace"}
			update := &cobra.Command{Use: "update", RunE: runWorkspaceUpdate}
			update.Flags().String("name", "", "")
			update.Flags().String("output", "json", "")
			workspace.AddCommand(update)
			root.AddCommand(workspace)
			root.SetArgs([]string{"workspace", "update", target, "--workspace", "bureau", "--name", "must-not-write"})
			err := root.Execute()
			if err == nil || !strings.Contains(err.Error(), "--workspace") {
				t.Errorf("conflicting workspace targets error = %v", err)
			}
			if requests != 0 {
				t.Errorf("requests = %d, want rejection before any lookup or write", requests)
			}
		})
	}
}

func TestWorkspaceFlagKeepsTaskAuthorityWithoutIdentityMarkers(t *testing.T) {
	for _, mode := range []string{"token", "config-root", "token-without-workspace"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			clearDaemonContextEnv(t)
			t.Setenv("MULTICA_TOKEN", "mat_test_token")
			t.Setenv("MULTICA_WORKSPACE_ID", digLabWorkspaceID)
			if err := cli.SaveCLIConfig(cli.CLIConfig{WorkspaceID: bureauWorkspaceID}); err != nil {
				t.Fatal(err)
			}
			if mode == "config-root" {
				t.Setenv(cli.TaskConfigRootEnv, t.TempDir())
				t.Setenv("MULTICA_TOKEN", "test-token")
			}
			if mode == "token-without-workspace" {
				t.Setenv("MULTICA_WORKSPACE_ID", "")
			}
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path != "/api/workspaces/"+digLabWorkspaceID || r.Header.Get("X-Workspace-ID") != digLabWorkspaceID {
					t.Errorf("task request escaped its workspace: %s header=%q", r.URL.Path, r.Header.Get("X-Workspace-ID"))
					http.Error(w, "unexpected task request", http.StatusForbidden)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": digLabWorkspaceID, "repos": []any{}})
			}))
			defer srv.Close()
			root := workspaceFlagRootForTest(srv.URL)
			repo := &cobra.Command{Use: "repo"}
			list := &cobra.Command{Use: "list", RunE: runRepoList}
			list.Flags().String("output", "json", "")
			repo.AddCommand(list)
			root.AddCommand(repo)
			root.SetArgs([]string{"repo", "list", "--workspace", "bureau"})
			err := root.Execute()
			if mode == "token-without-workspace" {
				if err == nil || requests != 0 {
					t.Fatalf("missing task workspace: error=%v requests=%d, want failure without profile fallback", err, requests)
				}
			} else if err != nil || requests != 1 {
				t.Fatalf("bound task command: error=%v requests=%d, want one scoped request", err, requests)
			}
		})
	}
}
