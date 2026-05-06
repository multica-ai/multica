package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func assertValidTOML(t *testing.T, content string) {
	t.Helper()
	var parsed map[string]any
	if err := toml.Unmarshal([]byte(content), &parsed); err != nil {
		t.Fatalf("config should parse as TOML: %v\n%s", err, content)
	}
}

func TestEnsureCodexTrustedProjectConfigAddsWorkdir(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	workDir := filepath.Join(dir, "workdir")
	if err := os.WriteFile(configPath, []byte("model = \"gpt-5\"\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := ensureCodexTrustedProjectConfig(configPath, workDir); err != nil {
		t.Fatalf("ensure trusted project: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	assertValidTOML(t, content)
	if !strings.Contains(content, "[projects."+tomlBasicString(workDir)+"]") {
		t.Fatalf("config missing trusted project table:\n%s", content)
	}
	if !strings.Contains(content, `trust_level = "trusted"`) {
		t.Fatalf("config missing trusted trust_level:\n%s", content)
	}
}

func TestEnsureCodexTrustedProjectConfigIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	workDir := filepath.Join(dir, "workdir")

	if err := ensureCodexTrustedProjectConfig(configPath, workDir); err != nil {
		t.Fatalf("first ensure trusted project: %v", err)
	}
	if err := ensureCodexTrustedProjectConfig(configPath, workDir); err != nil {
		t.Fatalf("second ensure trusted project: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	content := string(data)
	assertValidTOML(t, content)
	if count := strings.Count(content, "[projects."+tomlBasicString(workDir)+"]"); count != 1 {
		t.Fatalf("expected one project table, got %d:\n%s", count, content)
	}
}

func TestEnsureCodexTrustedProjectConfigUpdatesExistingProjectTable(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	workDir := filepath.Join(dir, "workdir")
	content := "[projects." + tomlBasicString(workDir) + "]\ntrust_level = \"untrusted\"\nextra = true\n"
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := ensureCodexTrustedProjectConfig(configPath, workDir); err != nil {
		t.Fatalf("ensure trusted project: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	updated := string(data)
	assertValidTOML(t, updated)
	if strings.Count(updated, "[projects."+tomlBasicString(workDir)+"]") != 1 {
		t.Fatalf("expected one project table:\n%s", updated)
	}
	if !strings.Contains(updated, "trust_level = \"trusted\"") || !strings.Contains(updated, "extra = true") {
		t.Fatalf("expected trust_level update with table contents preserved:\n%s", updated)
	}
}

func TestPrepareCodexHomeTrustsFreshWorkdir(t *testing.T) {
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: filepath.Join(t.TempDir(), "workspaces"),
		WorkspaceID:    "workspace-1",
		TaskID:         "task-12345678",
		Provider:       "codex",
		Task: TaskContextForEnv{
			IssueID: "issue-1",
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(env.CodexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read codex config: %v", err)
	}
	content := string(data)
	assertValidTOML(t, content)
	if !strings.Contains(content, "[projects."+tomlBasicString(env.WorkDir)+"]") {
		t.Fatalf("config missing fresh workdir trust entry:\n%s", content)
	}
}
