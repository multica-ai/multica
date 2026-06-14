package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIConfigPathForInstance_UsesExplicitConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	want := filepath.Join(home, "custom", "dev.json")
	got, err := CLIConfigPathForInstance("ignored", want)
	if err != nil {
		t.Fatalf("CLIConfigPathForInstance: %v", err)
	}
	if got != want {
		t.Fatalf("CLIConfigPathForInstance() = %q, want %q", got, want)
	}
}

func TestStateDirForInstance_UsesConfigParentDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, "instances", "local", "config.json")
	got, err := StateDirForInstance("ignored", configPath)
	if err != nil {
		t.Fatalf("StateDirForInstance: %v", err)
	}
	want := filepath.Dir(configPath)
	if got != want {
		t.Fatalf("StateDirForInstance() = %q, want %q", got, want)
	}
}

// TestCLIConfig_BackwardCompat_OldFileLoadsWithNilBackends verifies that a
// config.json written by an older daemon (no `backends` key at all) loads
// correctly into the new schema, with Backends == nil.
func TestCLIConfig_BackwardCompat_OldFileLoadsWithNilBackends(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfgDir := filepath.Join(tmp, ".multica")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	historical := `{
  "server_url": "https://api.multica.ai",
  "app_url": "https://app.multica.ai",
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

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal saved config: %v", err)
	}
	if _, ok := raw["backends"]; ok {
		t.Errorf("backends key should be omitted when nil, got: %s", string(data))
	}
}

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

func TestCLIConfig_OpenClawOverride_PartialFieldsOmitted(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	cfg := CLIConfig{
		ServerURL: "https://api.multica.ai",
		Token:     "mul_xyz",
		Backends: &BackendOverrides{
			OpenClaw: &OpenClawOverride{
				StateDir: "/var/lib/openclaw-prod",
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

	data, _ := os.ReadFile(filepath.Join(cfgDir, "config.json"))
	if !strings.Contains(string(data), "future_backend_xyz") {
		t.Error("unknown field future_backend_xyz was dropped on round-trip")
	}
}
