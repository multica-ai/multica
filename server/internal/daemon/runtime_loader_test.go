package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRuntimeManifests(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create a valid manifest (external-runtime as example).
	subDir := filepath.Join(dir, "external-runtime")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	valid := RuntimeManifest{
		ID:        "external-runtime",
		Name:      "External Runtime",
		Version:   "1.0.0",
		Provider:  "external-runtime",
		Transport: "acp-stdio",
		Command: RuntimeManifestCommand{
			Executable:  "/usr/bin/runtime",
			Args:        []string{"--acp"},
			BlockedArgs: map[string]string{"--output-format": "value"},
		},
		Capabilities: &RuntimeManifestCaps{
			Thinking:       true,
			McpConfig:      true,
			ModelSelection: true,
			SessionResume:  true,
			MaxTurns:       true,
			ToolCalls:      true,
			Attachments:    true,
		},
		Models: []RuntimeManifestModel{
			{ID: "model-a", Label: "Model A", Default: true, Thinking: []string{"none", "low", "high"}},
		},
		Pricing: map[string]RuntimePricing{
			"model-a": {Input: 0.5, Output: 1.5, CacheRead: 0.05},
		},
		Env:           map[string]string{"FOO": "bar"},
		MinCLIVersion: "1.0.0",
		IconURL:       "https://example.com/icon.png",
		Description:   "Reference external runtime",
	}
	data, _ := json.MarshalIndent(valid, "", "  ")
	if err := os.WriteFile(filepath.Join(subDir, "runtime.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create an invalid manifest (missing required fields).
	invalidDir := filepath.Join(dir, "broken")
	if err := os.MkdirAll(invalidDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalidDir, "runtime.json"), []byte(`{"id": "broken"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Manifest with an unsupported transport — must be skipped at load.
	badTransportDir := filepath.Join(dir, "badtransport")
	if err := os.MkdirAll(badTransportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := RuntimeManifest{
		ID:        "bad",
		Name:      "Bad",
		Provider:  "bad",
		Transport: "carrier-pigeon",
		Command:   RuntimeManifestCommand{Executable: "/usr/bin/true"},
	}
	data, _ = json.Marshal(bad)
	if err := os.WriteFile(filepath.Join(badTransportDir, "runtime.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Stream-json transport should load fine.
	streamDir := filepath.Join(dir, "stream")
	if err := os.MkdirAll(streamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stream := RuntimeManifest{
		ID:        "stream",
		Name:      "Stream",
		Provider:  "stream",
		Transport: "stream-json",
		Command:   RuntimeManifestCommand{Executable: "/usr/bin/true"},
	}
	data, _ = json.Marshal(stream)
	if err := os.WriteFile(filepath.Join(streamDir, "runtime.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a directory without runtime.json — should be skipped silently.
	emptyDir := filepath.Join(dir, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifests, err := LoadRuntimeManifests(dir)
	if err != nil {
		t.Fatalf("LoadRuntimeManifests: %v", err)
	}
	if len(manifests) != 2 {
		t.Fatalf("expected 2 manifests (external-runtime+stream), got %d", len(manifests))
	}

	// Find the external-runtime manifest
	var m RuntimeManifest
	for _, x := range manifests {
		if x.ID == "external-runtime" {
			m = x
			break
		}
	}
	if m.ID == "" {
		t.Fatalf("external-runtime manifest not loaded")
	}
	if m.Provider != "external-runtime" {
		t.Errorf("provider = %q, want external-runtime", m.Provider)
	}
	if m.Transport != "acp-stdio" {
		t.Errorf("transport = %q, want acp-stdio", m.Transport)
	}
	if m.Command.Executable != "/usr/bin/runtime" {
		t.Errorf("command.executable = %q, want /usr/bin/runtime", m.Command.Executable)
	}
	if len(m.Command.Args) != 1 || m.Command.Args[0] != "--acp" {
		t.Errorf("command.args = %v, want [--acp]", m.Command.Args)
	}
	if got := m.Command.BlockedArgs["--output-format"]; got != "value" {
		t.Errorf("blocked_args lost: %v", m.Command.BlockedArgs)
	}
	if !m.Capabilities.ToolCalls {
		t.Errorf("capabilities.tool_calls not preserved: %+v", m.Capabilities)
	}
}

func TestLoadRuntimeManifestsAcceptsJSONCComments(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	subDir := filepath.Join(dir, "codebuddy")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := `{
		"id": "codebuddy",
		"name": "CodeBuddy Code",
		"version": "1.0.0",
		"description": "uses https://example.com/catalog.json",
		"provider": "codebuddy",
		"transport": "stream-json",
		"command": {
			"executable": "codebuddy",
			"args": ["-p"]
		},
		"models_discovery": {
			"method": "cli",
			"cli": {
				"args": ["--list-models", "--format", "json"]
			}
		},
		// Keep sample models here while testing local catalogs.
		"icon_url": "https://example.com/codebuddy.svg"
	}`
	if err := os.WriteFile(filepath.Join(subDir, "runtime.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	manifests, err := LoadRuntimeManifests(dir)
	if err != nil {
		t.Fatalf("LoadRuntimeManifests: %v", err)
	}
	if len(manifests) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(manifests))
	}
	m := manifests[0]
	if m.IconURL != "https://example.com/codebuddy.svg" {
		t.Fatalf("icon_url = %q", m.IconURL)
	}
	if m.Description != "uses https://example.com/catalog.json" {
		t.Fatalf("comment stripper corrupted URL string: %q", m.Description)
	}
	if m.ModelsDiscovery == nil || m.ModelsDiscovery.CLI == nil || len(m.ModelsDiscovery.CLI.Args) != 3 {
		t.Fatalf("models_discovery lost: %+v", m.ModelsDiscovery)
	}
}

func TestLoadRuntimeManifestsSchemaVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeManifest := func(name string, body string) {
		t.Helper()
		subDir := filepath.Join(dir, name)
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(subDir, "runtime.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeManifest("implicit-v1", `{
		"id": "implicit-v1",
		"name": "Implicit v1",
		"provider": "implicit-v1",
		"transport": "acp-stdio",
		"command": {"executable": "/usr/bin/true"}
	}`)
	writeManifest("explicit-v1", `{
		"schema_version": 1,
		"id": "explicit-v1",
		"name": "Explicit v1",
		"provider": "explicit-v1",
		"transport": "stream-json",
		"command": {"executable": "/usr/bin/true"}
	}`)
	writeManifest("future-v99", `{
		"schema_version": 99,
		"id": "future-v99",
		"name": "Future v99",
		"provider": "future-v99",
		"transport": "acp-stdio",
		"command": {"executable": "/usr/bin/true"}
	}`)

	manifests, err := LoadRuntimeManifests(dir)
	if err != nil {
		t.Fatalf("LoadRuntimeManifests: %v", err)
	}
	if len(manifests) != 2 {
		t.Fatalf("expected implicit+explicit v1 manifests only, got %d: %+v", len(manifests), manifests)
	}
	loaded := map[string]bool{}
	for _, m := range manifests {
		loaded[m.ID] = true
	}
	if !loaded["implicit-v1"] {
		t.Errorf("implicit v1 manifest should load")
	}
	if !loaded["explicit-v1"] {
		t.Errorf("explicit v1 manifest should load")
	}
	if loaded["future-v99"] {
		t.Errorf("unsupported future manifest must be skipped")
	}
}

// TestRuntimeManifestToAgentEntryPropagatesEverything makes sure every
// new field added to RuntimeManifest also gets copied into AgentEntry.
// A future addition that forgets to extend ToAgentEntry will fail this
// test, which is much cheaper than discovering the gap at runtime.
func TestRuntimeManifestToAgentEntryPropagatesEverything(t *testing.T) {
	t.Parallel()
	m := RuntimeManifest{
		ID:            "rt-full",
		Name:          "Full Runtime",
		Provider:      "rt-full",
		Version:       "2.0.1",
		Description:   "fully populated",
		Transport:     "stream-json",
		LaunchHeader:  "rt-full --serve",
		IconURL:       "https://example.com/x.png",
		ConfigFile:    "AGENTS.md",
		SkillsRoot:    "/var/skills",
		MinCLIVersion: "1.2.3",
		Env: map[string]string{
			"RT_API_KEY": "test",
		},
		Command: RuntimeManifestCommand{
			Executable:  "/usr/bin/rt",
			Args:        []string{"--serve"},
			BlockedArgs: map[string]string{"--output-format": "value"},
		},
		Capabilities: &RuntimeManifestCaps{
			Thinking:           true,
			McpConfig:          true,
			InlineSystemPrompt: true,
			SessionResume:      true,
			MaxTurns:           true,
			ModelSelection:     true,
			LocalSkills:        true,
			SlashCommands:      true,
			ToolCalls:          true,
			Attachments:        true,
			ImageInput:         true,
			WebSearch:          true,
			CustomArgs:         true,
			ExtraArgs:          true,
		},
		Models: []RuntimeManifestModel{
			{ID: "model-a", Label: "Model A", Default: true},
		},
		Pricing: map[string]RuntimePricing{
			"model-a": {Input: 1, Output: 2},
		},
	}
	entry := m.ToAgentEntry()
	if entry.Path != "/usr/bin/rt" {
		t.Errorf("path lost")
	}
	if entry.Transport != "stream-json" {
		t.Errorf("transport lost: %q", entry.Transport)
	}
	if entry.LaunchHeader != "rt-full --serve" {
		t.Errorf("launch header lost")
	}
	if entry.IconURL != "https://example.com/x.png" {
		t.Errorf("icon url lost")
	}
	if entry.SkillsRoot != "/var/skills" {
		t.Errorf("skills root lost")
	}
	if entry.MinCLIVersion != "1.2.3" {
		t.Errorf("min cli version lost")
	}
	if entry.Env["RT_API_KEY"] != "test" {
		t.Errorf("env lost: %v", entry.Env)
	}
	if entry.BlockedArgs["--output-format"] != "value" {
		t.Errorf("blocked args lost: %v", entry.BlockedArgs)
	}
	if !entry.HasCapability("tool_calls") {
		t.Errorf("tool_calls capability lost")
	}
	if !entry.HasCapability("custom_args") {
		t.Errorf("custom_args capability lost")
	}
	if entry.Caps() == nil || !entry.Caps().Thinking {
		t.Errorf("caps pointer lost")
	}
	if len(entry.Models) != 1 || entry.Models[0].ID != "model-a" {
		t.Errorf("models lost: %v", entry.Models)
	}
	if entry.Pricing["model-a"].Output != 2 {
		t.Errorf("pricing lost: %v", entry.Pricing)
	}
	if !entry.IsExternal {
		t.Errorf("IsExternal must be true for manifest-loaded entries")
	}
	if entry.Description != "fully populated" {
		t.Errorf("description lost")
	}
}

func TestLoadRuntimeManifestsMissingDir(t *testing.T) {
	t.Parallel()

	manifests, err := LoadRuntimeManifests("/nonexistent/path/12345")
	if err != nil {
		t.Fatalf("LoadRuntimeManifests should not error on missing dir: %v", err)
	}
	if len(manifests) != 0 {
		t.Errorf("expected 0 manifests from missing dir, got %d", len(manifests))
	}
}

func TestLoadRuntimeManifestsEmptyDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifests, err := LoadRuntimeManifests(dir)
	if err != nil {
		t.Fatalf("LoadRuntimeManifests: %v", err)
	}
	if len(manifests) != 0 {
		t.Errorf("expected 0 manifests from empty dir, got %d", len(manifests))
	}
}

func TestDefaultRuntimesDir(t *testing.T) {
	t.Parallel()
	dir := DefaultRuntimesDir()
	if dir == "" {
		t.Fatal("DefaultRuntimesDir returned empty string")
	}
	// Should contain "runtimes" somewhere.
	if !stringsContains(dir, "runtimes") {
		t.Errorf("DefaultRuntimesDir = %q, want to contain 'runtimes'", dir)
	}
}

func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
