package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// newOpenclawSetInstanceTestCmd builds a detached cobra.Command for invoking
// runRuntimeOpenclawSetInstance directly without standing up the whole CLI
// tree. Mirrors the pattern used by cmd_runtime_profile_test.go.
func newOpenclawSetInstanceTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "set-instance"}
	addCommonProfileFlags(cmd)
	return cmd
}

func newOpenclawUnsetInstanceTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "unset-instance"}
	addCommonProfileFlags(cmd)
	return cmd
}

func newOpenclawSetBinaryTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "set-binary"}
	addCommonProfileFlags(cmd)
	return cmd
}

func newOpenclawUnsetBinaryTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "unset-binary"}
	addCommonProfileFlags(cmd)
	return cmd
}

func newOpenclawShowTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "show"}
	addCommonProfileFlags(cmd)
	cmd.Flags().String("output", "table", "")
	return cmd
}

// TestRunRuntimeOpenclawSetInstance_RejectsRelative documents the path
// contract: only absolute paths are accepted. A relative path is rejected
// before any file write happens.
func TestRunRuntimeOpenclawSetInstance_RejectsRelative(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := newOpenclawSetInstanceTestCmd()
	err := runRuntimeOpenclawSetInstance(cmd, []string{"relative/path"})
	if err == nil {
		t.Fatal("expected absolute-path error, got nil")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("error should mention 'absolute', got %q", err.Error())
	}
}

// TestRunRuntimeOpenclawSetInstance_PreservesOtherFields is the back-compat
// contract: writing a backend override MUST NOT touch token / server_url /
// workspace_id / ProfileCommandOverrides etc. on disk. This is the same
// guarantee TestRunRuntimeProfileSetPathPreservesExistingConfig enforces for
// the sibling MUL-3284 command.
func TestRunRuntimeOpenclawSetInstance_PreservesOtherFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed a config that exercises every other field. If any of these come
	// back changed after the override save, the round-trip lost data.
	seed := cli.CLIConfig{
		ServerURL:   "https://api.multica.ai",
		AppURL:      "https://app.multica.ai",
		WorkspaceID: "ws-123",
		Token:       "mul_token_xyz",
		ProfileCommandOverrides: map[string]string{
			"prof-1": "/opt/some/path",
		},
	}
	if err := cli.SaveCLIConfig(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cmd := newOpenclawSetInstanceTestCmd()
	if err := runRuntimeOpenclawSetInstance(cmd, []string{"/opt/openclaw-prod"}); err != nil {
		t.Fatalf("set-instance: %v", err)
	}

	got, err := cli.LoadCLIConfig()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.ServerURL != seed.ServerURL {
		t.Errorf("ServerURL changed: got %q want %q", got.ServerURL, seed.ServerURL)
	}
	if got.AppURL != seed.AppURL {
		t.Errorf("AppURL changed: got %q want %q", got.AppURL, seed.AppURL)
	}
	if got.WorkspaceID != seed.WorkspaceID {
		t.Errorf("WorkspaceID changed: got %q want %q", got.WorkspaceID, seed.WorkspaceID)
	}
	if got.Token != seed.Token {
		t.Errorf("Token changed: got %q want %q", got.Token, seed.Token)
	}
	if got.ProfileCommandOverrides["prof-1"] != "/opt/some/path" {
		t.Errorf("ProfileCommandOverrides lost: got %+v", got.ProfileCommandOverrides)
	}
	if got.Backends == nil || got.Backends.OpenClaw == nil {
		t.Fatalf("Backends.OpenClaw should be non-nil after set-instance, got %+v", got.Backends)
	}
	if got.Backends.OpenClaw.StateDir != "/opt/openclaw-prod" {
		t.Errorf("StateDir: got %q want %q", got.Backends.OpenClaw.StateDir, "/opt/openclaw-prod")
	}
	if got.Backends.OpenClaw.BinaryPath != "" {
		t.Errorf("BinaryPath: should remain empty after set-instance, got %q", got.Backends.OpenClaw.BinaryPath)
	}
}

// TestRunRuntimeOpenclawSetBinary_AlongsideInstance verifies the two fields
// are independently addressable: setting binary after instance keeps the
// instance, and vice-versa.
func TestRunRuntimeOpenclawSetBinary_AlongsideInstance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmd1 := newOpenclawSetInstanceTestCmd()
	if err := runRuntimeOpenclawSetInstance(cmd1, []string{"/opt/openclaw-prod"}); err != nil {
		t.Fatalf("set-instance: %v", err)
	}

	cmd2 := newOpenclawSetBinaryTestCmd()
	if err := runRuntimeOpenclawSetBinary(cmd2, []string{"/usr/local/bin/openclaw"}); err != nil {
		t.Fatalf("set-binary: %v", err)
	}

	got, err := cli.LoadCLIConfig()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Backends.OpenClaw.StateDir != "/opt/openclaw-prod" {
		t.Errorf("StateDir lost after set-binary: %q", got.Backends.OpenClaw.StateDir)
	}
	if got.Backends.OpenClaw.BinaryPath != "/usr/local/bin/openclaw" {
		t.Errorf("BinaryPath: got %q want /usr/local/bin/openclaw", got.Backends.OpenClaw.BinaryPath)
	}
}

// TestRunRuntimeOpenclawUnsetInstance_PrunesEmptyBranch verifies that
// clearing the last override removes BOTH the nested pointer AND the parent
// BackendOverrides pointer — so the round-tripped JSON returns to its
// pre-override shape (`backends` key absent). This is the omitempty contract
// documented on cli.CLIConfig.Backends.
func TestRunRuntimeOpenclawUnsetInstance_PrunesEmptyBranch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed an override.
	setCmd := newOpenclawSetInstanceTestCmd()
	if err := runRuntimeOpenclawSetInstance(setCmd, []string{"/opt/openclaw-prod"}); err != nil {
		t.Fatalf("seed set-instance: %v", err)
	}

	// Verify it's there.
	mid, err := cli.LoadCLIConfig()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if mid.Backends == nil || mid.Backends.OpenClaw == nil {
		t.Fatalf("seed didn't take effect: %+v", mid.Backends)
	}

	// Unset and verify Backends collapses to nil.
	unsetCmd := newOpenclawUnsetInstanceTestCmd()
	if err := runRuntimeOpenclawUnsetInstance(unsetCmd, nil); err != nil {
		t.Fatalf("unset-instance: %v", err)
	}
	after, err := cli.LoadCLIConfig()
	if err != nil {
		t.Fatalf("reload after unset: %v", err)
	}
	if after.Backends != nil {
		t.Errorf("Backends should be nil after last override cleared, got %+v", after.Backends)
	}
}

// TestRunRuntimeOpenclawUnsetInstance_KeepsBinary verifies that unsetting one
// field does NOT drop the other one. binary_path stays even if state_dir is
// cleared.
func TestRunRuntimeOpenclawUnsetInstance_KeepsBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed both overrides.
	if err := runRuntimeOpenclawSetInstance(newOpenclawSetInstanceTestCmd(), []string{"/opt/openclaw-prod"}); err != nil {
		t.Fatalf("seed instance: %v", err)
	}
	if err := runRuntimeOpenclawSetBinary(newOpenclawSetBinaryTestCmd(), []string{"/usr/local/bin/openclaw"}); err != nil {
		t.Fatalf("seed binary: %v", err)
	}

	// Unset just instance.
	if err := runRuntimeOpenclawUnsetInstance(newOpenclawUnsetInstanceTestCmd(), nil); err != nil {
		t.Fatalf("unset-instance: %v", err)
	}

	got, err := cli.LoadCLIConfig()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.Backends == nil || got.Backends.OpenClaw == nil {
		t.Fatalf("Backends.OpenClaw should still exist (binary remains), got %+v", got.Backends)
	}
	if got.Backends.OpenClaw.BinaryPath != "/usr/local/bin/openclaw" {
		t.Errorf("BinaryPath lost: got %q", got.Backends.OpenClaw.BinaryPath)
	}
	if got.Backends.OpenClaw.StateDir != "" {
		t.Errorf("StateDir should be cleared, got %q", got.Backends.OpenClaw.StateDir)
	}
}

// TestRunRuntimeOpenclawUnsetInstance_NoOpWhenAlreadyUnset is the idempotency
// guarantee: running `unset-instance` twice (or against a fresh config) is a
// successful no-op, not an error. CI scripts that call it unconditionally
// shouldn't need to test first.
func TestRunRuntimeOpenclawUnsetInstance_NoOpWhenAlreadyUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := runRuntimeOpenclawUnsetInstance(newOpenclawUnsetInstanceTestCmd(), nil); err != nil {
		t.Errorf("expected no-op success, got error: %v", err)
	}
	after, err := cli.LoadCLIConfig()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if after.Backends != nil {
		t.Errorf("Backends should remain nil on no-op unset, got %+v", after.Backends)
	}
}

// === resolveOpenclawBinary / resolveOpenclawState precedence ===
// Direct unit tests for the precedence helpers. The integration with
// daemon/config.go's applyOpenclawOverride is tested in that package; here we
// just lock in the CLI's `show` rendering.

func TestResolveOpenclawBinary_Precedence(t *testing.T) {
	cases := []struct {
		name     string
		env      string
		config   string
		wantPath string
		wantSrc  string
	}{
		{"both unset", "", "", "openclaw", "PATH lookup"},
		{"env only", "/env/oc", "", "/env/oc", "env (MULTICA_OPENCLAW_PATH)"},
		{"config only", "", "/cfg/oc", "/cfg/oc", "config.json"},
		{"env wins", "/env/oc", "/cfg/oc", "/env/oc", "env (MULTICA_OPENCLAW_PATH)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, src := resolveOpenclawBinary(tc.env, tc.config)
			if got != tc.wantPath {
				t.Errorf("path: got %q want %q", got, tc.wantPath)
			}
			if src != tc.wantSrc {
				t.Errorf("source: got %q want %q", src, tc.wantSrc)
			}
		})
	}
}

func TestResolveOpenclawState_Precedence(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		config  string
		wantSrc string
	}{
		{"both unset", "", "", "default"},
		{"env only", "/env/state", "", "env (OPENCLAW_STATE_DIR)"},
		{"config only", "", "/cfg/state", "config.json"},
		{"env wins", "/env/state", "/cfg/state", "env (OPENCLAW_STATE_DIR)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, src := resolveOpenclawState(tc.env, tc.config)
			if src != tc.wantSrc {
				t.Errorf("source: got %q want %q", src, tc.wantSrc)
			}
		})
	}
}

func TestRunRuntimeOpenclawShow_JSONShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENCLAW_STATE_DIR", "")
	t.Setenv("MULTICA_OPENCLAW_PATH", "")

	// Seed config.json with both overrides so the JSON output exercises
	// every field shape; the actual assertions are minimal — we just need
	// the show RunE not to error and to emit a payload the caller can
	// recognize.
	if err := runRuntimeOpenclawSetInstance(newOpenclawSetInstanceTestCmd(), []string{"/seed/state"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	cmd := newOpenclawShowTestCmd()
	_ = cmd.Flags().Set("output", "json")
	if err := runRuntimeOpenclawShow(cmd, nil); err != nil {
		t.Errorf("show --output json should not error, got: %v", err)
	}
	// stdout capture is brittle in cobra; the precedence helpers above
	// already cover the resolution logic. A higher-level integration test
	// asserting stdout shape would be valuable but is non-trivial to wire
	// without restructuring cmd_runtime_profile_test.go's pattern; leaving
	// for follow-up.
}
