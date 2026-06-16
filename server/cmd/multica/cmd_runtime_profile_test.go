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

// addCommonProfileFlags wires the persistent-style flags the run functions
// resolve (server-url, workspace-id, profile, token) onto a detached test
// command so the helpers can be invoked directly.
func addCommonProfileFlags(cmd *cobra.Command) {
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "", "")
	cmd.Flags().String("profile", "", "")
	cmd.Flags().String("token", "", "")
}

func newProfileListTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "list"}
	addCommonProfileFlags(cmd)
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newProfileCreateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "create"}
	addCommonProfileFlags(cmd)
	cmd.Flags().String("protocol-family", "", "")
	cmd.Flags().String("command-name", "", "")
	cmd.Flags().String("display-name", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("visibility", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newProfileUpdateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "update"}
	addCommonProfileFlags(cmd)
	cmd.Flags().String("display-name", "", "")
	cmd.Flags().String("command-name", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("visibility", "", "")
	cmd.Flags().Bool("enabled", true, "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newProfileDeleteTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "delete"}
	addCommonProfileFlags(cmd)
	return cmd
}

func newProfileSetPathTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "set-path"}
	addCommonProfileFlags(cmd)
	cmd.Flags().String("path", "", "")
	return cmd
}

func newProfileUnsetPathTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "unset-path"}
	addCommonProfileFlags(cmd)
	return cmd
}

// TestRuntimeProfileCommandsRegistered verifies the subcommands are wired
// under `runtime profile`.
func TestRuntimeProfileCommandsRegistered(t *testing.T) {
	for _, name := range []string{"list", "create", "update", "delete", "set-path", "unset-path"} {
		cmd, _, err := runtimeProfileCmd.Find([]string{name})
		if err != nil {
			t.Fatalf("find %q: %v", name, err)
		}
		if cmd == nil || cmd.Name() != name {
			t.Fatalf("%q not registered under `runtime profile`; got %#v", name, cmd)
		}
	}
	// And `profile` itself must hang off `runtime`.
	cmd, _, err := runtimeCmd.Find([]string{"profile", "list"})
	if err != nil || cmd == nil || cmd.Name() != "list" {
		t.Fatalf("`runtime profile list` not reachable from runtime command: %v / %#v", err, cmd)
	}
}

func TestRunRuntimeProfileList(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-123")

	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"runtime_profiles": []map[string]any{
				{"id": "prof-1", "display_name": "Company Codex", "protocol_family": "codex", "command_name": "company-codex", "visibility": "workspace", "enabled": true},
			},
		})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newProfileListTestCmd()
	_ = cmd.Flags().Set("output", "json")
	if err := runRuntimeProfileList(cmd, nil); err != nil {
		t.Fatalf("runRuntimeProfileList: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	if gotPath != "/api/workspaces/ws-123/runtime-profiles" {
		t.Errorf("path = %q, want /api/workspaces/ws-123/runtime-profiles", gotPath)
	}
}

func TestRunRuntimeProfileCreate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-123")

	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "prof-1", "display_name": "Company Codex"})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newProfileCreateTestCmd()
	_ = cmd.Flags().Set("protocol-family", "codex")
	_ = cmd.Flags().Set("command-name", "company-codex")
	_ = cmd.Flags().Set("display-name", "Company Codex")
	_ = cmd.Flags().Set("visibility", "workspace")

	if err := runRuntimeProfileCreate(cmd, nil); err != nil {
		t.Fatalf("runRuntimeProfileCreate: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/api/workspaces/ws-123/runtime-profiles" {
		t.Errorf("path = %q, want /api/workspaces/ws-123/runtime-profiles", gotPath)
	}
	if gotBody["protocol_family"] != "codex" || gotBody["command_name"] != "company-codex" || gotBody["display_name"] != "Company Codex" {
		t.Errorf("unexpected body: %#v", gotBody)
	}
	// fixed_args is intentionally NOT exposed by the CLI in v1 (the daemon does
	// not yet wire it into the launch command), so it must never be sent.
	if _, present := gotBody["fixed_args"]; present {
		t.Errorf("fixed_args must not be sent by the CLI, got %#v", gotBody["fixed_args"])
	}
	if gotBody["visibility"] != "workspace" {
		t.Errorf("visibility = %v, want workspace", gotBody["visibility"])
	}
}

func TestRunRuntimeProfileCreateRejectsBadFamily(t *testing.T) {
	cmd := newProfileCreateTestCmd()
	_ = cmd.Flags().Set("protocol-family", "not-a-real-backend")
	_ = cmd.Flags().Set("command-name", "x")
	_ = cmd.Flags().Set("display-name", "X")
	// No server should ever be contacted; this must fail client-side.
	if err := runRuntimeProfileCreate(cmd, nil); err == nil {
		t.Fatal("expected invalid --protocol-family error")
	}
}

func TestRunRuntimeProfileCreateRequiresFlags(t *testing.T) {
	cmd := newProfileCreateTestCmd()
	if err := runRuntimeProfileCreate(cmd, nil); err == nil {
		t.Fatal("expected missing --protocol-family error")
	}
}

func TestRunRuntimeProfileUpdateOnlySendsChangedFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-123")

	var gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "prof-1"})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newProfileUpdateTestCmd()
	_ = cmd.Flags().Set("command-name", "new-codex")
	_ = cmd.Flags().Set("enabled", "false")

	if err := runRuntimeProfileUpdate(cmd, []string{"prof-1"}); err != nil {
		t.Fatalf("runRuntimeProfileUpdate: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %s, want PATCH", gotMethod)
	}
	if gotPath != "/api/workspaces/ws-123/runtime-profiles/prof-1" {
		t.Errorf("path = %q, want .../runtime-profiles/prof-1", gotPath)
	}
	// Only the two changed flags must be present.
	if gotBody["command_name"] != "new-codex" {
		t.Errorf("command_name = %v, want new-codex", gotBody["command_name"])
	}
	if gotBody["enabled"] != false {
		t.Errorf("enabled = %v, want false", gotBody["enabled"])
	}
	if _, ok := gotBody["display_name"]; ok {
		t.Errorf("display_name should not be sent when unchanged: %#v", gotBody)
	}
	if _, ok := gotBody["visibility"]; ok {
		t.Errorf("visibility should not be sent when unchanged: %#v", gotBody)
	}
}

func TestRunRuntimeProfileUpdateNoFieldsErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-123")
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:0")

	cmd := newProfileUpdateTestCmd()
	if err := runRuntimeProfileUpdate(cmd, []string{"prof-1"}); err == nil {
		t.Fatal("expected 'no fields to update' error")
	}
}

func TestRunRuntimeProfileDeleteSuccess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-123")

	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newProfileDeleteTestCmd()
	if err := runRuntimeProfileDelete(cmd, []string{"prof-1"}); err != nil {
		t.Fatalf("runRuntimeProfileDelete: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", gotMethod)
	}
	if gotPath != "/api/workspaces/ws-123/runtime-profiles/prof-1" {
		t.Errorf("path = %q, want .../runtime-profiles/prof-1", gotPath)
	}
}

func TestRunRuntimeProfileDeleteConflictSurfacesServerMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-123")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte("2 active agents are bound to this profile"))
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)

	cmd := newProfileDeleteTestCmd()
	err := runRuntimeProfileDelete(cmd, []string{"prof-1"})
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if got := err.Error(); !strings.Contains(got, "2 active agents are bound to this profile") {
		t.Errorf("error %q should surface the server message", got)
	}
}

func TestRunRuntimeProfileSetAndUnsetPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// set-path
	setCmd := newProfileSetPathTestCmd()
	_ = setCmd.Flags().Set("path", "/opt/bin/company-codex")
	if err := runRuntimeProfileSetPath(setCmd, []string{"prof-1"}); err != nil {
		t.Fatalf("runRuntimeProfileSetPath: %v", err)
	}

	cfg, err := cli.LoadCLIConfig()
	if err != nil {
		t.Fatalf("LoadCLIConfig: %v", err)
	}
	if got := cfg.ProfileCommandOverrides["prof-1"]; got != "/opt/bin/company-codex" {
		t.Fatalf("override after set = %q, want /opt/bin/company-codex", got)
	}

	// unset-path
	unsetCmd := newProfileUnsetPathTestCmd()
	if err := runRuntimeProfileUnsetPath(unsetCmd, []string{"prof-1"}); err != nil {
		t.Fatalf("runRuntimeProfileUnsetPath: %v", err)
	}
	cfg, err = cli.LoadCLIConfig()
	if err != nil {
		t.Fatalf("LoadCLIConfig after unset: %v", err)
	}
	if _, ok := cfg.ProfileCommandOverrides["prof-1"]; ok {
		t.Fatalf("override should be removed after unset, got %#v", cfg.ProfileCommandOverrides)
	}
}

func TestRunRuntimeProfileSetPathRejectsRelative(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := newProfileSetPathTestCmd()
	_ = cmd.Flags().Set("path", "relative/path")
	if err := runRuntimeProfileSetPath(cmd, []string{"prof-1"}); err == nil {
		t.Fatal("expected absolute-path error")
	}
}

func TestRunRuntimeProfileSetPathPreservesExistingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed an existing config with unrelated fields.
	seed := cli.CLIConfig{ServerURL: "https://api.multica.ai", WorkspaceID: "ws-123", Token: "mul_xyz"}
	if err := cli.SaveCLIConfig(seed); err != nil {
		t.Fatal(err)
	}

	cmd := newProfileSetPathTestCmd()
	_ = cmd.Flags().Set("path", "/opt/bin/company-codex")
	if err := runRuntimeProfileSetPath(cmd, []string{"prof-1"}); err != nil {
		t.Fatalf("runRuntimeProfileSetPath: %v", err)
	}

	cfg, err := cli.LoadCLIConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerURL != "https://api.multica.ai" || cfg.WorkspaceID != "ws-123" || cfg.Token != "mul_xyz" {
		t.Errorf("set-path clobbered existing config: %#v", cfg)
	}
	if cfg.ProfileCommandOverrides["prof-1"] != "/opt/bin/company-codex" {
		t.Errorf("override not written: %#v", cfg.ProfileCommandOverrides)
	}
}
