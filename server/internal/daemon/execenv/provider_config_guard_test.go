package execenv

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInheritedProviderEnvUnderWorkspacesRootFailsClosed(t *testing.T) {
	workspacesRoot := t.TempDir()

	t.Run("codex", func(t *testing.T) {
		dirtyHome := filepath.Join(workspacesRoot, "ws", "task", codexHomeDirName)
		mustWrite(t, filepath.Join(dirtyHome, "config.toml"), "sandbox_mode = \"danger-full-access\"\n")
		t.Setenv("CODEX_HOME", dirtyHome)

		_, err := resolveSharedCodexHome(workspacesRoot)
		if err == nil {
			t.Fatal("resolveSharedCodexHome succeeded with inherited per-task CODEX_HOME; expected fail closed")
		}
		if !strings.Contains(err.Error(), "CODEX_HOME") || !strings.Contains(err.Error(), "workspaces root") {
			t.Fatalf("error = %v, want CODEX_HOME/workspaces root detail", err)
		}
	})

	t.Run("hermes", func(t *testing.T) {
		dirtyHome := filepath.Join(workspacesRoot, "ws", "task", "hermes-home")
		mustWrite(t, filepath.Join(dirtyHome, "config.yaml"), "skills: {}\n")
		t.Setenv("HERMES_HOME", dirtyHome)

		res := ResolveHermesProfileWithWorkspacesRoot("", "", false, false, workspacesRoot)
		if res.Err == nil {
			t.Fatal("ResolveHermesProfile succeeded with inherited per-task HERMES_HOME; expected fail closed")
		}
		if !strings.Contains(res.Err.Error(), "HERMES_HOME") || !strings.Contains(res.Err.Error(), "workspaces root") {
			t.Fatalf("error = %v, want HERMES_HOME/workspaces root detail", res.Err)
		}
	})

	t.Run("openclaw", func(t *testing.T) {
		envRoot := t.TempDir()
		workDir := filepath.Join(envRoot, "workdir")
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			t.Fatalf("mkdir workdir: %v", err)
		}
		dirtyConfig := filepath.Join(workspacesRoot, "ws", "task", "openclaw-config.json")
		stub := installOpenclawStub(t, map[string]openclawResponse{
			"config file": {stdout: dirtyConfig},
		})

		_, err := prepareOpenclawConfig(envRoot, workDir, OpenclawConfigPrep{
			OpenclawBin:    stub.bin,
			WorkspacesRoot: workspacesRoot,
		})
		if err == nil {
			t.Fatal("prepareOpenclawConfig succeeded with inherited per-task active config; expected fail closed")
		}
		if !strings.Contains(err.Error(), "openclaw") || !strings.Contains(err.Error(), "workspaces root") {
			t.Fatalf("error = %v, want openclaw/workspaces root detail", err)
		}
		if _, statErr := os.Stat(filepath.Join(envRoot, openclawConfigFile)); !os.IsNotExist(statErr) {
			t.Fatalf("wrapper config should not be written after fail-fast, stat err = %v", statErr)
		}
	})
}

func TestHermesExplicitHomeBypassesInheritedGuard(t *testing.T) {
	workspacesRoot := t.TempDir()
	explicitTaskHome := filepath.Join(workspacesRoot, "ws", "task", "hermes-home")

	res := ResolveHermesProfileWithWorkspacesRoot(explicitTaskHome, "", false, false, workspacesRoot)
	if res.Err != nil {
		t.Fatalf("explicit Hermes home should not be treated as inherited process env: %v", res.Err)
	}
	if res.SourceHome != explicitTaskHome {
		t.Fatalf("SourceHome = %q, want %q", res.SourceHome, explicitTaskHome)
	}
}

func TestCodexInheritedHomeWithProviderlessConfigFailsClosed(t *testing.T) {
	sharedHome := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.toml"), "# only daemon-managed defaults\nsandbox_mode = \"danger-full-access\"\n")
	t.Setenv("CODEX_HOME", sharedHome)

	_, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-providerless-codex",
		TaskID:         "11111111-2222-3333-4444-555555555555",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "providerless-codex"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("Prepare succeeded with providerless inherited CODEX_HOME; expected fail closed")
	}
	if !strings.Contains(err.Error(), "provider/config") {
		t.Fatalf("error = %v, want provider/config detail", err)
	}
}
