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

func TestPrepareCodexHomeNeverAnchorsToLiveManagedHome(t *testing.T) {
	root := t.TempDir()
	if err := EnsureWorkspacesRootMarker(root); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(root, "ws", "task-a", "codex-home")
	if err := os.MkdirAll(managed, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managed, "auth.json"), []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	stable := filepath.Join(home, ".codex")
	if err := os.MkdirAll(stable, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stable, "auth.json"), []byte("stable"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", managed)
	t.Setenv("OPENAI_API_KEY", "")
	dst := filepath.Join(root, "ws", "task-b", "codex-home")
	if err := prepareCodexHome(dst, testLogger()); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := os.RemoveAll(filepath.Dir(managed)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dst, "auth.json"))
	if err != nil || string(data) != "stable" {
		t.Fatalf("destination after cleanup: data=%q err=%v", data, err)
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
	if err == nil || !strings.Contains(err.Error(), "readable durable source auth.json") {
		t.Fatalf("error = %v", err)
	}
}

func TestPrepareCodexHomeFailsClosedForDefaultAndCustomHomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		custom bool
	}{{"default", false}, {"custom", true}} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("OPENAI_API_KEY", "")
			if tc.custom {
				t.Setenv("CODEX_HOME", filepath.Join(home, "stable-custom"))
			} else {
				t.Setenv("CODEX_HOME", "")
			}
			err := prepareCodexHome(filepath.Join(t.TempDir(), "codex-home"), testLogger())
			if err == nil || !strings.Contains(err.Error(), "readable durable source auth.json") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestPrepareCodexHomeFailsWhenDestinationCannotBeProvisioned(t *testing.T) {
	shared := t.TempDir()
	if err := os.WriteFile(filepath.Join(shared, "auth.json"), []byte("auth"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", shared)
	t.Setenv("OPENAI_API_KEY", "")
	dst := filepath.Join(t.TempDir(), "codex-home")
	if err := os.MkdirAll(filepath.Join(dst, "auth.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "auth.json", "blocker"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := prepareCodexHome(dst, testLogger())
	if err == nil || !strings.Contains(err.Error(), "provision codex auth file") {
		t.Fatalf("error = %v", err)
	}
}

func TestReuseReturnsNilWhenCodexAuthPreparationFails(t *testing.T) {
	shared := t.TempDir()
	t.Setenv("CODEX_HOME", shared)
	t.Setenv("OPENAI_API_KEY", "")
	root := t.TempDir()
	workDir := filepath.Join(root, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Reuse(ReuseParams{WorkDir: workDir, Provider: "codex"}, testLogger()); got != nil {
		t.Fatalf("Reuse = %#v, want nil", got)
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

func TestManagedCodexHomeDetectionUsesDaemonMarker(t *testing.T) {
	root := t.TempDir()
	if err := EnsureWorkspacesRootMarker(root); err != nil {
		t.Fatal(err)
	}
	if !isManagedCodexHome(filepath.Join(root, "ws", "task", "codex-home")) {
		t.Fatal("marked task home not detected")
	}
	if isManagedCodexHome(filepath.Join(t.TempDir(), "codex-home")) {
		t.Fatal("unmarked custom home detected as managed")
	}
}

func TestValidateCodexAuthDestinationAcceptsWindowsCopyFallback(t *testing.T) {
	shared := t.TempDir()
	if err := os.WriteFile(filepath.Join(shared, "auth.json"), []byte("source-auth"), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(dst, []byte("copied-auth"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "")
	if err := validateCodexAuthDestination(shared, dst); err != nil {
		t.Fatalf("regular-file fallback rejected: %v", err)
	}
}

func TestWindowsCopyFallbackFailsClosedAfterSourceRemoval(t *testing.T) {
	shared := t.TempDir()
	source := filepath.Join(shared, "auth.json")
	if err := os.WriteFile(source, []byte("source-auth"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", shared)
	t.Setenv("OPENAI_API_KEY", "")
	root := t.TempDir()
	workDir := filepath.Join(root, "workdir")
	destination := filepath.Join(root, "codex-home", "auth.json")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	// Models createFileLink's Windows fallback: a regular file, not a symlink.
	if err := os.WriteFile(destination, []byte("copied-auth"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateCodexAuthDestination(shared, destination); err != nil {
		t.Fatalf("initial copy invalid: %v", err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := prepareCodexHome(filepath.Join(t.TempDir(), "codex-home"), testLogger()); err == nil || !strings.Contains(err.Error(), "readable durable source auth.json") {
		t.Fatalf("prepare error = %v", err)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Reuse(ReuseParams{WorkDir: workDir, Provider: "codex"}, testLogger()); got != nil {
		t.Fatalf("Reuse = %#v, want nil", got)
	}
}
