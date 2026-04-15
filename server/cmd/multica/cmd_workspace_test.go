package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon"
)

func TestWorkspaceSkillsSyncSetUpdatesWatchedWorkspaceConfig(t *testing.T) {
	setTestHome(t)

	skillDir := t.TempDir()
	cfg := cli.CLIConfig{
		WatchedWorkspaces: []cli.WatchedWorkspace{
			{ID: "ws-1", Name: "Workspace 1"},
		},
	}
	if err := cli.SaveCLIConfigForProfile(cfg, ""); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd, _, _ := newWorkspaceTestCommand()
	if err := cmd.Flags().Set("dir", skillDir); err != nil {
		t.Fatalf("set dir flag: %v", err)
	}
	if err := runWorkspaceSkillsSyncSet(cmd, []string{"ws-1"}); err != nil {
		t.Fatalf("runWorkspaceSkillsSyncSet returned error: %v", err)
	}

	reloaded, err := cli.LoadCLIConfigForProfile("")
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if len(reloaded.WatchedWorkspaces) != 1 {
		t.Fatalf("watched workspaces = %d, want 1", len(reloaded.WatchedWorkspaces))
	}

	ws := reloaded.WatchedWorkspaces[0]
	if ws.SkillSync == nil {
		t.Fatal("expected skill sync config to be set")
	}

	wantDir, err := filepath.Abs(skillDir)
	if err != nil {
		t.Fatalf("resolve abs dir: %v", err)
	}
	if ws.SkillSync.Dir != wantDir {
		t.Fatalf("skill sync dir = %q, want %q", ws.SkillSync.Dir, wantDir)
	}
	if !ws.SkillSync.Enabled {
		t.Fatal("expected skill sync to be enabled")
	}
}

func TestWorkspaceSkillsSyncSetResetsSyncStateWhenDirChanges(t *testing.T) {
	setTestHome(t)

	oldDir := t.TempDir()
	newDir := t.TempDir()
	cfg := cli.CLIConfig{
		WatchedWorkspaces: []cli.WatchedWorkspace{
			{
				ID:   "ws-1",
				Name: "Workspace 1",
				SkillSync: &cli.WorkspaceSkillSync{
					Dir:           oldDir,
					Enabled:       true,
					LastSyncAt:    "2026-04-15T10:00:00Z",
					LastSyncError: "old sync error",
				},
			},
		},
	}
	if err := cli.SaveCLIConfigForProfile(cfg, ""); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd, _, _ := newWorkspaceTestCommand()
	if err := cmd.Flags().Set("dir", newDir); err != nil {
		t.Fatalf("set dir flag: %v", err)
	}
	if err := runWorkspaceSkillsSyncSet(cmd, []string{"ws-1"}); err != nil {
		t.Fatalf("runWorkspaceSkillsSyncSet returned error: %v", err)
	}

	reloaded, err := cli.LoadCLIConfigForProfile("")
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}

	ws := reloaded.WatchedWorkspaces[0]
	if ws.SkillSync == nil {
		t.Fatal("expected skill sync config to be set")
	}

	wantDir, err := filepath.Abs(newDir)
	if err != nil {
		t.Fatalf("resolve abs dir: %v", err)
	}
	if ws.SkillSync.Dir != wantDir {
		t.Fatalf("skill sync dir = %q, want %q", ws.SkillSync.Dir, wantDir)
	}
	if ws.SkillSync.LastSyncAt != "" {
		t.Fatalf("expected last_sync_at to reset, got %q", ws.SkillSync.LastSyncAt)
	}
	if ws.SkillSync.LastSyncError != "" {
		t.Fatalf("expected last_sync_error to reset, got %q", ws.SkillSync.LastSyncError)
	}
}

func TestWorkspaceSkillsSyncDisablePreservesDir(t *testing.T) {
	setTestHome(t)

	cfg := cli.CLIConfig{
		WatchedWorkspaces: []cli.WatchedWorkspace{
			{
				ID:   "ws-1",
				Name: "Workspace 1",
				SkillSync: &cli.WorkspaceSkillSync{
					Dir:           filepath.Join(t.TempDir(), "skills"),
					Enabled:       true,
					DeleteManaged: true,
				},
			},
		},
	}
	if err := cli.SaveCLIConfigForProfile(cfg, ""); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd, _, _ := newWorkspaceTestCommand()
	if err := runWorkspaceSkillsSyncDisable(cmd, []string{"ws-1"}); err != nil {
		t.Fatalf("runWorkspaceSkillsSyncDisable returned error: %v", err)
	}

	reloaded, err := cli.LoadCLIConfigForProfile("")
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}

	ws := reloaded.WatchedWorkspaces[0]
	if ws.SkillSync == nil {
		t.Fatal("expected skill sync config to exist")
	}
	if ws.SkillSync.Enabled {
		t.Fatal("expected skill sync to be disabled")
	}
	if ws.SkillSync.Dir == "" {
		t.Fatal("expected skill sync dir to be preserved")
	}
	if !ws.SkillSync.DeleteManaged {
		t.Fatal("expected delete_managed to be preserved")
	}
}

func TestWorkspaceSkillsSyncStatusRendersFields(t *testing.T) {
	setTestHome(t)

	cfg := cli.CLIConfig{
		WorkspaceID: "ws-1",
		WatchedWorkspaces: []cli.WatchedWorkspace{
			{
				ID:   "ws-1",
				Name: "Workspace 1",
				SkillSync: &cli.WorkspaceSkillSync{
					Dir:           "C:\\skills",
					Enabled:       true,
					DeleteManaged: true,
					LastSyncAt:    "2026-04-15T10:00:00Z",
					LastSyncError: "conflict: alpha",
				},
			},
		},
	}
	if err := cli.SaveCLIConfigForProfile(cfg, ""); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd, stdout, _ := newWorkspaceTestCommand()
	if err := runWorkspaceSkillsSyncStatus(cmd, nil); err != nil {
		t.Fatalf("runWorkspaceSkillsSyncStatus returned error: %v", err)
	}

	output := stdout.String()
	for _, want := range []string{
		"workspace_id:",
		"enabled:",
		"dir:",
		"delete_managed:",
		"last_sync_at:",
		"last_sync_error:",
		"ws-1",
		"C:\\skills",
		"2026-04-15T10:00:00Z",
		"conflict: alpha",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("status output missing %q:\n%s", want, output)
		}
	}
}

func TestRunWorkspaceSkillsSync(t *testing.T) {
	setTestHome(t)

	skillRoot := t.TempDir()
	writeWorkspaceSkillTestFile(t, filepath.Join(skillRoot, "alpha", "SKILL.md"), "# Alpha\n")
	writeWorkspaceSkillTestFile(t, filepath.Join(skillRoot, "alpha", "docs", "guide.md"), "guide")

	var (
		gotAuthHeader      string
		gotWorkspaceHeader string
		createdReq         daemon.CreateWorkspaceSkillRequest
		createCalls        int
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		gotWorkspaceHeader = r.Header.Get("X-Workspace-ID")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/skills":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
		case r.Method == http.MethodPost && r.URL.Path == "/api/skills":
			createCalls++
			if err := json.NewDecoder(r.Body).Decode(&createdReq); err != nil {
				t.Fatalf("decode create request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "skill-1",
				"name":    createdReq.Name,
				"content": createdReq.Content,
				"config":  createdReq.Config,
				"files":   createdReq.Files,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := cli.CLIConfig{
		ServerURL:   srv.URL,
		Token:       "test-token",
		WorkspaceID: "ws-1",
		WatchedWorkspaces: []cli.WatchedWorkspace{
			{
				ID:   "ws-1",
				Name: "Workspace 1",
				SkillSync: &cli.WorkspaceSkillSync{
					Dir:     skillRoot,
					Enabled: false,
				},
			},
		},
	}
	if err := cli.SaveCLIConfigForProfile(cfg, ""); err != nil {
		t.Fatalf("save config: %v", err)
	}

	cmd, stdout, _ := newWorkspaceTestCommand()
	if err := runWorkspaceSkillsSyncRun(cmd, nil); err != nil {
		t.Fatalf("runWorkspaceSkillsSyncRun returned error: %v", err)
	}

	if createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", createCalls)
	}
	if gotAuthHeader != "Bearer test-token" {
		t.Fatalf("authorization header = %q, want %q", gotAuthHeader, "Bearer test-token")
	}
	if gotWorkspaceHeader != "ws-1" {
		t.Fatalf("workspace header = %q, want %q", gotWorkspaceHeader, "ws-1")
	}
	if createdReq.Name != "alpha" {
		t.Fatalf("created skill name = %q, want %q", createdReq.Name, "alpha")
	}
	if createdReq.Content != "# Alpha\n" {
		t.Fatalf("created skill content = %q", createdReq.Content)
	}
	if len(createdReq.Files) != 1 || createdReq.Files[0].Path != "docs/guide.md" || createdReq.Files[0].Content != "guide" {
		t.Fatalf("created skill files = %#v", createdReq.Files)
	}

	output := stdout.String()
	for _, want := range []string{"created:", "1", "updated:", "deleted:", "unchanged:", "conflicts:"} {
		if !strings.Contains(output, want) {
			t.Fatalf("run output missing %q:\n%s", want, output)
		}
	}

	reloaded, err := cli.LoadCLIConfigForProfile("")
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if reloaded.WatchedWorkspaces[0].SkillSync == nil || reloaded.WatchedWorkspaces[0].SkillSync.LastSyncAt == "" {
		t.Fatal("expected last_sync_at to be updated after manual sync")
	}
	if reloaded.WatchedWorkspaces[0].SkillSync.LastSyncError != "" {
		t.Fatalf("expected last_sync_error to be cleared, got %q", reloaded.WatchedWorkspaces[0].SkillSync.LastSyncError)
	}
}

func newWorkspaceTestCommand() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{}
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("dir", "", "")
	cmd.Flags().Bool("delete-managed", false, "")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	return cmd, stdout, stderr
}

func setTestHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func writeWorkspaceSkillTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
