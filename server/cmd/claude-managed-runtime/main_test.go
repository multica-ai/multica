package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/claudemanagedruntime"
)

func TestLoadConfigReadsEnvironment(t *testing.T) {
	env := map[string]string{
		"MULTICA_SERVER_URL":            "http://localhost:8080",
		"MULTICA_TOKEN":                 "mul_token",
		"MULTICA_WORKSPACE_ID":          "workspace",
		"ANTHROPIC_API_KEY":             "sk-ant-key",
		"ANTHROPIC_BASE_URL":            "http://localhost:9090/v1",
		"CLAUDE_MANAGED_ENVIRONMENT_ID": "env_123",
		"CLAUDE_MANAGED_STATE_DIR":      "/var/lib/state",
		"CLAUDE_MANAGED_NAME":           "Claude runtime",
		"CLAUDE_MANAGED_VAULT_IDS":      "vlt_github, ,vlt_other",
	}
	cfg, err := loadConfig("", func(key string) string { return env[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MulticaURL != "http://localhost:8080" || cfg.MulticaToken != "mul_token" || cfg.WorkspaceID != "workspace" ||
		cfg.AnthropicToken != "sk-ant-key" || cfg.AnthropicURL != "http://localhost:9090/v1" || cfg.EnvironmentID != "env_123" ||
		cfg.StateDir != "/var/lib/state" || cfg.Name != "Claude runtime" || strings.Join(cfg.VaultIDs, ",") != "vlt_github,vlt_other" {
		t.Fatalf("environment not mapped: %+v", cfg)
	}
}

func TestLoadConfigPrecedenceAndValidation(t *testing.T) {
	env := func(key string) string {
		if key == "MULTICA_TOKEN" {
			return "private-env-token"
		}
		if key == "CLAUDE_MANAGED_ENVIRONMENT_ID" {
			return "env_environment"
		}
		return ""
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"environment_id":"env_file","workspace_id":"workspace","anthropic_token":"file-key"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnvironmentID != "env_file" || cfg.MulticaToken != "private-env-token" || cfg.AnthropicToken != "file-key" {
		t.Fatal("configuration precedence changed")
	}
	for _, body := range []string{`{"misspelled_secret":"sensitive"}`, `{"qoder_token":"sensitive"}`, `{"anthropic_token":`, `{} {}`, `null`, `[]`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := loadConfig(path, env)
		if err == nil || strings.Contains(err.Error(), "sensitive") {
			t.Fatalf("unsafe or missing validation for %s: %v", body, err)
		}
	}
	if _, err := loadConfig(filepath.Join(t.TempDir(), "missing.json"), env); err == nil {
		t.Fatal("missing configuration file accepted")
	}
}

func TestMissingRequiredFieldsAreRejected(t *testing.T) {
	complete := map[string]string{
		"MULTICA_SERVER_URL":            "http://localhost:8080",
		"MULTICA_TOKEN":                 "mul_token",
		"MULTICA_WORKSPACE_ID":          "6f1c1c58-3b8e-4a43-9d6e-2f0b7c1a9e10",
		"ANTHROPIC_API_KEY":             "sk-ant-key",
		"CLAUDE_MANAGED_ENVIRONMENT_ID": "env_123",
		"CLAUDE_MANAGED_STATE_DIR":      t.TempDir(),
	}
	cfg, err := loadConfig("", func(key string) string { return complete[key] })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := claudemanagedruntime.New(cfg, nil); err != nil {
		t.Fatalf("complete configuration rejected: %v", err)
	}
	for missing := range complete {
		cfg, err := loadConfig("", func(key string) string {
			if key == missing {
				return ""
			}
			return complete[key]
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := claudemanagedruntime.New(cfg, nil); err == nil {
			t.Fatalf("configuration without %s accepted", missing)
		}
	}
}

func TestExampleConfigIsAccepted(t *testing.T) {
	cfg, err := loadConfig("config.example.json", func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != "Claude Managed Agents" || len(cfg.GitHubTokenFiles) != 1 {
		t.Fatalf("example configuration not decoded: %+v", cfg)
	}
}
