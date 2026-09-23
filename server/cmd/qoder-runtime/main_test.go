package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigPrecedenceAndValidation(t *testing.T) {
	env := func(key string) string {
		if key == "MULTICA_TOKEN" {
			return "private-env-token"
		}
		if key == "QODER_CLOUD_ENVIRONMENT_ID" {
			return "env_environment"
		}
		return ""
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"environment_id":"env_file","workspace_id":"workspace"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnvironmentID != "env_file" || cfg.MulticaToken != "private-env-token" {
		t.Fatal("configuration precedence changed")
	}
	for _, body := range []string{`{"misspelled_secret":"sensitive"}`, `{"qoder_token":`, `{} {}`, `null`, `[]`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := loadConfig(path, env)
		if err == nil || strings.Contains(err.Error(), "sensitive") {
			t.Fatalf("unsafe or missing validation: %v", err)
		}
	}
}
