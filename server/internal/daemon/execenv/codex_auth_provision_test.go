package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareCodexHomeFallsBackFromDeletedManagedHome(t *testing.T) {
	root := t.TempDir()
	if err := EnsureWorkspacesRootMarker(root); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(root, "ws", "task", "codex-home")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", managed)
	t.Setenv("OPENAI_API_KEY", "")
	stable := filepath.Join(home, ".codex")
	if err := os.MkdirAll(stable, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stable, "auth.json"), []byte(`{"tokens":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "codex-home")
	if err := prepareCodexHome(dst, testLogger()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "auth.json"))
	if err != nil || string(data) != `{"tokens":{}}` {
		t.Fatalf("fallback auth: data=%q err=%v", data, err)
	}
	if err := os.RemoveAll(dst); err != nil {
		t.Fatal(err)
	}
	if err := prepareCodexHome(dst, testLogger()); err != nil {
		t.Fatalf("prepare after cleanup: %v", err)
	}
}

func TestPrepareCodexHomeFailsClosedWithoutFileAuth(t *testing.T) {
	root := t.TempDir()
	if err := EnsureWorkspacesRootMarker(root); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", filepath.Join(root, "ws", "task", "codex-home"))
	t.Setenv("OPENAI_API_KEY", "")
	err := prepareCodexHome(filepath.Join(t.TempDir(), "codex-home"), testLogger())
	if err == nil || !strings.Contains(err.Error(), "readable auth.json") {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareCodexHomeAllowsAPIKeyWithoutFileAuth(t *testing.T) {
	root := t.TempDir()
	if err := EnsureWorkspacesRootMarker(root); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", filepath.Join(root, "ws", "task", "codex-home"))
	t.Setenv("OPENAI_API_KEY", "set")
	if err := prepareCodexHome(filepath.Join(t.TempDir(), "codex-home"), testLogger()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
}

func TestPrepareCodexHomeAllowsCustomProviderEnvKeyWithoutFileAuth(t *testing.T) {
	root := t.TempDir()
	if err := EnsureWorkspacesRootMarker(root); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(root, "ws", "task", "codex-home"))
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("CUSTOM_API_KEY", "set")
	stable := filepath.Join(home, ".codex")
	if err := os.MkdirAll(stable, 0o755); err != nil {
		t.Fatal(err)
	}
	config := "model_provider = \"custom\"\n[model_providers.custom]\nenv_key = \"CUSTOM_API_KEY\"\n"
	if err := os.WriteFile(filepath.Join(stable, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareCodexHome(filepath.Join(t.TempDir(), "codex-home"), testLogger()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
}

func TestResolveSharedCodexHomeKeepsStableCustomHome(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-codex")
	t.Setenv("CODEX_HOME", custom)
	if got := resolveSharedCodexHome(); got != custom {
		t.Fatalf("got %q want %q", got, custom)
	}
}
