package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCLIConfig_BackwardCompat_OldFileLoadsWithNilBackends verifies that a
// config.json written by an older daemon (no `backends` key at all) loads
// correctly into the new schema, with Backends == nil. This is the most
// important guarantee of issue #3875's PR: existing on-disk configs MUST
// continue to work byte-for-byte.
func TestCLIConfig_BackwardCompat_OldFileLoadsWithNilBackends(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// Write a 4-field config exactly as the historical daemon would have.
	cfgDir := filepath.Join(tmp, ".multica")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	historical := `{
  "server_url": "https://api.multica.ai",
  "app_url": "https://multica.ai",
  "workspace_id": "ws-123",
  "token": "mul_abcdef"
}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(historical), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadCLIConfig()
	if err != nil {
		t.Fatalf("LoadCLIConfig on historical file: %v", err)
	}

	if cfg.ServerURL != "https://api.multica.ai" {
		t.Errorf("ServerURL: got %q, want historical value", cfg.ServerURL)
	}
	if cfg.Token != "mul_abcdef" {
		t.Errorf("Token: got %q, want historical value", cfg.Token)
	}
	if cfg.Backends != nil {
		t.Errorf("Backends should be nil for historical config, got %+v", cfg.Backends)
	}
}

// TestCLIConfig_BackwardCompat_NilBackendsOmittedFromJSON verifies that
// saving a config without backend overrides does NOT add a `backends` key
// to the on-disk JSON. This matters for users who never set overrides —
// their config files must stay byte-identical, so a future downgrade to
// an older daemon doesn't trip on an empty `backends: null` line.
func TestCLIConfig_BackwardCompat_NilBackendsOmittedFromJSON(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg := CLIConfig{
		ServerURL: "https://api.multica.ai",
		Token:     "mul_xyz",
	}
	if err := SaveCLIConfig(cfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, ".multica", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" {
		t.Fatal("config file is empty")
	}

	// The omitempty tag on Backends should keep it out of the JSON entirely.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal saved config: %v", err)
	}
	if _, ok := raw["backends"]; ok {
		t.Errorf("backends key should be omitted when nil, got: %s", string(data))
	}
}

// TestCLIConfig_OpenClawOverride_RoundTrip verifies that setting BinaryPath
// and StateDir survives a save/load cycle.
func TestCLIConfig_OpenClawOverride_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	original := CLIConfig{
		ServerURL: "https://api.multica.ai",
		Token:     "mul_xyz",
		Backends: &BackendOverrides{
			OpenClaw: &OpenClawOverride{
				BinaryPath: "/opt/openclaw-prod/bin/openclaw",
				StateDir:   "/var/lib/openclaw-prod",
			},
		},
	}
	if err := SaveCLIConfig(original); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadCLIConfig()
	if err != nil {
		t.Fatal(err)
	}

	if loaded.Backends == nil || loaded.Backends.OpenClaw == nil {
		t.Fatalf("Backends.OpenClaw should be non-nil after round-trip, got %+v", loaded.Backends)
	}
	if loaded.Backends.OpenClaw.BinaryPath != original.Backends.OpenClaw.BinaryPath {
		t.Errorf("BinaryPath round-trip: got %q, want %q",
			loaded.Backends.OpenClaw.BinaryPath, original.Backends.OpenClaw.BinaryPath)
	}
	if loaded.Backends.OpenClaw.StateDir != original.Backends.OpenClaw.StateDir {
		t.Errorf("StateDir round-trip: got %q, want %q",
			loaded.Backends.OpenClaw.StateDir, original.Backends.OpenClaw.StateDir)
	}
}

// TestCLIConfig_OpenClawOverride_PartialFieldsOmitted verifies that an
// override with only one field set does not emit empty strings for the
// unset field. Important so users can intentionally set only BinaryPath
// (or only StateDir) and have the other follow the historical default,
// without an empty string overriding env-var precedence.
func TestCLIConfig_OpenClawOverride_PartialFieldsOmitted(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg := CLIConfig{
		ServerURL: "https://api.multica.ai",
		Token:     "mul_xyz",
		Backends: &BackendOverrides{
			OpenClaw: &OpenClawOverride{
				StateDir: "/var/lib/openclaw-prod",
				// BinaryPath intentionally left empty
			},
		},
	}
	if err := SaveCLIConfig(cfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, ".multica", "config.json"))
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	openclaw, ok := raw["backends"].(map[string]any)["openclaw"].(map[string]any)
	if !ok {
		t.Fatalf("could not navigate to backends.openclaw in: %s", string(data))
	}
	if _, present := openclaw["binary_path"]; present {
		t.Errorf("binary_path should be omitted when empty, got: %s", string(data))
	}
	if _, present := openclaw["state_dir"]; !present {
		t.Errorf("state_dir should be present when set, got: %s", string(data))
	}
}

// TestCLIConfig_ProfileCommandOverrides_RoundTrip verifies that pinning a
// per-machine profile command path survives a save/load cycle AND that
// unrelated fields (server_url, token, backends) are preserved across the
// round-trip — the set-path / unset-path CLI commands rely on a
// load->modify->save cycle never dropping config the user already had.
func TestCLIConfig_ProfileCommandOverrides_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	original := CLIConfig{
		ServerURL:   "https://api.multica.ai",
		AppURL:      "https://multica.ai",
		WorkspaceID: "ws-123",
		Token:       "mul_xyz",
		Backends: &BackendOverrides{
			OpenClaw: &OpenClawOverride{StateDir: "/var/lib/openclaw-prod"},
		},
		ProfileCommandOverrides: map[string]string{
			"prof-1": "/opt/bin/company-codex",
			"prof-2": "/usr/local/bin/special-claude",
		},
	}
	if err := SaveCLIConfig(original); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadCLIConfig()
	if err != nil {
		t.Fatal(err)
	}

	// The override map must round-trip intact.
	if len(loaded.ProfileCommandOverrides) != 2 {
		t.Fatalf("ProfileCommandOverrides len = %d, want 2: %+v", len(loaded.ProfileCommandOverrides), loaded.ProfileCommandOverrides)
	}
	if got := loaded.ProfileCommandOverrides["prof-1"]; got != "/opt/bin/company-codex" {
		t.Errorf("prof-1 override = %q, want /opt/bin/company-codex", got)
	}
	if got := loaded.ProfileCommandOverrides["prof-2"]; got != "/usr/local/bin/special-claude" {
		t.Errorf("prof-2 override = %q, want /usr/local/bin/special-claude", got)
	}

	// Every other field must be preserved (no clobbering on round-trip).
	if loaded.ServerURL != original.ServerURL {
		t.Errorf("ServerURL = %q, want %q", loaded.ServerURL, original.ServerURL)
	}
	if loaded.AppURL != original.AppURL {
		t.Errorf("AppURL = %q, want %q", loaded.AppURL, original.AppURL)
	}
	if loaded.WorkspaceID != original.WorkspaceID {
		t.Errorf("WorkspaceID = %q, want %q", loaded.WorkspaceID, original.WorkspaceID)
	}
	if loaded.Token != original.Token {
		t.Errorf("Token = %q, want %q", loaded.Token, original.Token)
	}
	if loaded.Backends == nil || loaded.Backends.OpenClaw == nil ||
		loaded.Backends.OpenClaw.StateDir != "/var/lib/openclaw-prod" {
		t.Errorf("Backends.OpenClaw not preserved: %+v", loaded.Backends)
	}
}

// TestCLIConfig_ProfileCommandOverrides_OmittedWhenEmpty verifies the
// omitempty tag keeps the key out of the on-disk JSON when no overrides are
// set, so configs for users who never pin a path stay byte-stable.
func TestCLIConfig_ProfileCommandOverrides_OmittedWhenEmpty(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg := CLIConfig{ServerURL: "https://api.multica.ai", Token: "mul_xyz"}
	if err := SaveCLIConfig(cfg); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(tmp, ".multica", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["profile_command_overrides"]; ok {
		t.Errorf("profile_command_overrides should be omitted when empty, got: %s", string(data))
	}
}

// TestCLIConfig_UnknownFieldsArePreserved verifies forward-compat: a future
// daemon that adds, say, a `backends.codex` key should not have its data
// destroyed when an older daemon (without knowledge of that key) reads and
// re-saves the file. Today Go's encoding/json silently DROPS unknown fields
// on round-trip. This test documents the gap so future maintainers know.
//
// Skipped today (encoding/json does not preserve unknown fields), but the
// test is written so a future change to a preserve-unknown encoder
// (json.RawMessage, mapstructure, etc.) will pick it up.
func TestCLIConfig_UnknownFieldsArePreserved(t *testing.T) {
	t.Skip("documenting known limitation: encoding/json drops unknown fields on round-trip; future PR can switch to a preserving encoder")

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfgDir := filepath.Join(tmp, ".multica")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	withFutureField := `{
  "server_url": "https://api.multica.ai",
  "token": "mul_xyz",
  "backends": {
    "openclaw": {"state_dir": "/x"},
    "future_backend_xyz": {"some_setting": "preserve me"}
  }
}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(withFutureField), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadCLIConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveCLIConfig(cfg); err != nil {
		t.Fatal(err)
	}

	// After round-trip, future_backend_xyz should still be in the file.
	data, _ := os.ReadFile(filepath.Join(cfgDir, "config.json"))
	if !strings.Contains(string(data), "future_backend_xyz") {
		t.Error("unknown field future_backend_xyz was dropped on round-trip")
	}
}
