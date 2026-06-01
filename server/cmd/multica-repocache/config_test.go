package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	cfgDir := t.TempDir()
	cfgYAML := []byte(`
workspaces:
  - id: 11111111-1111-1111-1111-111111111111
  - id: 22222222-2222-2222-2222-222222222222
`)
	if err := os.WriteFile(filepath.Join(cfgDir, "runtime.yaml"), cfgYAML, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_SERVER_URL", "http://multica-backend.multica.svc:8080")
	t.Setenv("MULTICA_TOKEN", "tk")
	t.Setenv("REPOCACHE_CONFIG_DIR", cfgDir)

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.RepoRoot != "/repos" {
		t.Errorf("RepoRoot default = %q", got.RepoRoot)
	}
	if got.FetchInterval != 60*time.Second {
		t.Errorf("FetchInterval default = %v", got.FetchInterval)
	}
	if got.AdminAddr != ":8080" {
		t.Errorf("AdminAddr default = %q", got.AdminAddr)
	}
	if got.MetricsAddr != ":9090" {
		t.Errorf("MetricsAddr default = %q", got.MetricsAddr)
	}
	if len(got.Workspaces) != 2 {
		t.Errorf("Workspaces count: %d", len(got.Workspaces))
	}
}

func TestLoadConfig_EnvOverrides(t *testing.T) {
	cfgDir := t.TempDir()
	cfgYAML := []byte(`
workspaces:
  - id: 11111111-1111-1111-1111-111111111111
`)
	if err := os.WriteFile(filepath.Join(cfgDir, "runtime.yaml"), cfgYAML, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_SERVER_URL", "http://multica:8080")
	t.Setenv("MULTICA_TOKEN", "tk")
	t.Setenv("REPOCACHE_CONFIG_DIR", cfgDir)
	t.Setenv("REPOCACHE_REPO_ROOT", "/data/repos")
	t.Setenv("REPOCACHE_FETCH_INTERVAL", "15s")
	t.Setenv("REPOCACHE_ADMIN_ADDR", ":7777")
	t.Setenv("REPOCACHE_METRICS_ADDR", ":7778")

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got.RepoRoot != "/data/repos" {
		t.Errorf("RepoRoot override = %q", got.RepoRoot)
	}
	if got.FetchInterval != 15*time.Second {
		t.Errorf("FetchInterval override = %v", got.FetchInterval)
	}
	if got.AdminAddr != ":7777" {
		t.Errorf("AdminAddr override = %q", got.AdminAddr)
	}
	if got.MetricsAddr != ":7778" {
		t.Errorf("MetricsAddr override = %q", got.MetricsAddr)
	}
}

func TestLoadConfig_MissingServerURL(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "")
	t.Setenv("MULTICA_TOKEN", "tk")
	if _, err := LoadConfig(); err == nil {
		t.Error("expected error for missing MULTICA_SERVER_URL")
	}
}

func TestLoadConfig_MissingToken(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://x")
	t.Setenv("MULTICA_TOKEN", "")
	if _, err := LoadConfig(); err == nil {
		t.Error("expected error for missing MULTICA_TOKEN")
	}
}

func TestLoadConfig_EmptyWorkspaces(t *testing.T) {
	cfgDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfgDir, "runtime.yaml"), []byte("workspaces: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_SERVER_URL", "http://x")
	t.Setenv("MULTICA_TOKEN", "tk")
	t.Setenv("REPOCACHE_CONFIG_DIR", cfgDir)
	if _, err := LoadConfig(); err == nil {
		t.Error("expected error for empty workspaces list")
	}
}

func TestLoadConfig_WorkspaceMissingID(t *testing.T) {
	cfgDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(cfgDir, "runtime.yaml"), []byte("workspaces:\n  - {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_SERVER_URL", "http://x")
	t.Setenv("MULTICA_TOKEN", "tk")
	t.Setenv("REPOCACHE_CONFIG_DIR", cfgDir)
	if _, err := LoadConfig(); err == nil {
		t.Error("expected error for workspace missing id")
	}
}
