package daemon

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func piTestDaemon(piEnabled bool) *Daemon {
	cfg := testDetToolsCfg()
	cfg.Enabled = true
	cfg.PiAdapterEnabled = piEnabled
	cfg.PiConfigRelPath = DefaultPiConfigRelPath
	return &Daemon{cfg: Config{DetTools: cfg}}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestPreparePiToolPlane_WritesProjectLocalConfig(t *testing.T) {
	workDir := t.TempDir()
	d := piTestDaemon(true)

	cleanup := d.preparePiToolPlane("pi", workDir, false, nil, nil, nil, map[string]string{}, discardLogger())
	defer cleanup()

	cfgPath := filepath.Join(workDir, ".pi", "mcp.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("adapter config not written to %s: %v", cfgPath, err)
	}
	servers := parseServers(t, json.RawMessage(data))
	if _, ok := servers[dettoolsServerName]; !ok {
		t.Errorf("adapter config missing %q server: %s", dettoolsServerName, data)
	}
}

func TestPreparePiToolPlane_MergesAndPreservesExisting(t *testing.T) {
	workDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workDir, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(workDir, ".pi", "mcp.json")
	// Existing project config with a user server and top-level settings.
	existing := `{"settings":{"debug":true},"mcpServers":{"mine":{"command":"x"}}}`
	if err := os.WriteFile(cfgPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	d := piTestDaemon(true)
	// local_directory=true so cleanup restores the original.
	cleanup := d.preparePiToolPlane("pi", workDir, true, nil, nil, nil, map[string]string{}, discardLogger())

	data, _ := os.ReadFile(cfgPath)
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(data, &merged); err != nil {
		t.Fatalf("merged config invalid: %v", err)
	}
	if _, ok := merged["settings"]; !ok {
		t.Error("top-level settings was dropped during merge")
	}
	servers := parseServers(t, data)
	if _, ok := servers["mine"]; !ok {
		t.Error("user-defined server was dropped during merge")
	}
	if _, ok := servers[dettoolsServerName]; !ok {
		t.Error("deterministic server was not added")
	}

	// Cleanup restores the exact original on the user's own repo.
	cleanup()
	after, _ := os.ReadFile(cfgPath)
	if string(after) != existing {
		t.Errorf("original .pi/mcp.json not restored:\n got: %s\nwant: %s", after, existing)
	}
}

func TestPreparePiToolPlane_CleanupRemovesCreatedFileOnLocalDir(t *testing.T) {
	workDir := t.TempDir()
	d := piTestDaemon(true)
	cleanup := d.preparePiToolPlane("pi", workDir, true, nil, nil, nil, map[string]string{}, discardLogger())

	cfgPath := filepath.Join(workDir, ".pi", "mcp.json")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config should exist before cleanup: %v", err)
	}
	cleanup()
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Error("cleanup should remove a config it created when no original existed")
	}
}

func TestPreparePiToolPlane_NoopWhenDisabledOrNotPi(t *testing.T) {
	logger := discardLogger()

	dOff := piTestDaemon(false)
	wd := t.TempDir()
	dOff.preparePiToolPlane("pi", wd, false, nil, nil, nil, map[string]string{}, logger)()
	if _, err := os.Stat(filepath.Join(wd, ".pi", "mcp.json")); !os.IsNotExist(err) {
		t.Error("disabled: no config should be written")
	}

	dOn := piTestDaemon(true)
	wd2 := t.TempDir()
	dOn.preparePiToolPlane("claude", wd2, false, nil, nil, nil, map[string]string{}, logger)()
	if _, err := os.Stat(filepath.Join(wd2, ".pi", "mcp.json")); !os.IsNotExist(err) {
		t.Error("non-pi provider: no config should be written")
	}
}

func TestPreparePiToolPlane_SkipsWhenNoToolsAllowed(t *testing.T) {
	wd := t.TempDir()
	d := piTestDaemon(true)
	rc := json.RawMessage(`{"deterministic_tools":{"allowed_tools":["nonexistent"]}}`)
	d.preparePiToolPlane("pi", wd, false, nil, rc, nil, map[string]string{}, discardLogger())()
	if _, err := os.Stat(filepath.Join(wd, ".pi", "mcp.json")); !os.IsNotExist(err) {
		t.Error("empty effective allowlist: no config should be written")
	}
}
