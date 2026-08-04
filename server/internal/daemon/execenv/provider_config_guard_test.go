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

func TestInheritedProviderPathGuardHandlesNonLexicalWorkspacePaths(t *testing.T) {
	t.Run("empty root fails closed", func(t *testing.T) {
		sharedHome := t.TempDir()
		t.Setenv("CODEX_HOME", sharedHome)

		_, err := resolveSharedCodexHome("")
		if err == nil {
			t.Fatal("resolveSharedCodexHome succeeded with empty workspaces root; expected fail closed")
		}
		if !strings.Contains(err.Error(), "workspaces root is empty") {
			t.Fatalf("error = %v, want empty root detail", err)
		}
	})

	t.Run("symlink into workspaces root fails closed", func(t *testing.T) {
		workspacesRoot := t.TempDir()
		dirtyHome := filepath.Join(workspacesRoot, "ws", "task", codexHomeDirName)
		mustWrite(t, filepath.Join(dirtyHome, "config.toml"), "sandbox_mode = \"danger-full-access\"\n")
		link := filepath.Join(t.TempDir(), "codex-link")
		if err := os.Symlink(dirtyHome, link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Setenv("CODEX_HOME", link)

		_, err := resolveSharedCodexHome(workspacesRoot)
		if err == nil {
			t.Fatal("resolveSharedCodexHome succeeded through symlink into workspaces root; expected fail closed")
		}
		if !strings.Contains(err.Error(), "workspaces root") {
			t.Fatalf("error = %v, want workspaces root detail", err)
		}
	})

	t.Run("case-insensitive spelling fails closed", func(t *testing.T) {
		workspacesRoot := t.TempDir()
		dirtyHome := filepath.Join(workspacesRoot, "ws", "task", codexHomeDirName)
		mustWrite(t, filepath.Join(dirtyHome, "config.toml"), "sandbox_mode = \"danger-full-access\"\n")
		upperHome := strings.ToUpper(dirtyHome)
		if upperHome == dirtyHome {
			t.Skip("path has no lowercase bytes to exercise case folding")
		}
		gotInfo, err := os.Stat(upperHome)
		if err != nil {
			t.Skipf("filesystem is case-sensitive for %s: %v", upperHome, err)
		}
		wantInfo, err := os.Stat(dirtyHome)
		if err != nil {
			t.Fatalf("stat dirty home: %v", err)
		}
		if !os.SameFile(gotInfo, wantInfo) {
			t.Skip("uppercase spelling does not resolve to the same file")
		}
		t.Setenv("CODEX_HOME", upperHome)

		_, err = resolveSharedCodexHome(workspacesRoot)
		if err == nil {
			t.Fatal("resolveSharedCodexHome succeeded with case-only path variant; expected fail closed")
		}
		if !strings.Contains(err.Error(), "workspaces root") {
			t.Fatalf("error = %v, want workspaces root detail", err)
		}
	})

	t.Run("relative path from cwd fails closed", func(t *testing.T) {
		cwd := t.TempDir()
		workspacesRoot := filepath.Join(cwd, "root")
		dirtyHome := filepath.Join(workspacesRoot, "ws", "task", codexHomeDirName)
		mustWrite(t, filepath.Join(dirtyHome, "config.toml"), "sandbox_mode = \"danger-full-access\"\n")
		rel, err := filepath.Rel(cwd, dirtyHome)
		if err != nil {
			t.Fatalf("rel: %v", err)
		}
		oldwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("chdir: %v", err)
		}
		t.Cleanup(func() { _ = os.Chdir(oldwd) })
		t.Setenv("CODEX_HOME", rel)

		_, err = resolveSharedCodexHome(workspacesRoot)
		if err == nil {
			t.Fatal("resolveSharedCodexHome succeeded with relative path into workspaces root; expected fail closed")
		}
		if !strings.Contains(err.Error(), "workspaces root") {
			t.Fatalf("error = %v, want workspaces root detail", err)
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
	mustWrite(t, filepath.Join(sharedHome, "config.toml"), "model = \"gpt-5-codex\"\napproval_policy = \"never\"\n")
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
	if !strings.Contains(err.Error(), "model_provider") {
		t.Fatalf("error = %v, want model_provider detail", err)
	}
}

func TestCodexDefaultHomeWithProviderlessConfigFailsClosed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	mustWrite(t, filepath.Join(home, ".codex", "config.toml"), "model = \"gpt-5-codex\"\napproval_policy = \"never\"\n")

	_, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-default-providerless-codex",
		TaskID:         "21111111-2222-3333-4444-555555555555",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "default-providerless-codex"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil {
		t.Fatal("Prepare succeeded with providerless default ~/.codex; expected fail closed")
	}
	if !strings.Contains(err.Error(), "model_provider") {
		t.Fatalf("error = %v, want model_provider detail", err)
	}
}

func TestHermesInheritedHomeProviderlessConfigFailsClosed(t *testing.T) {
	sharedHome := t.TempDir()
	workspacesRoot := t.TempDir()
	mustWrite(t, filepath.Join(sharedHome, "config.yaml"), "skills: {}\n")
	t.Setenv("HERMES_HOME", sharedHome)

	res := ResolveHermesProfileWithWorkspacesRoot("", "", false, false, workspacesRoot)
	if res.Err == nil {
		t.Fatal("ResolveHermesProfile succeeded with providerless inherited HERMES_HOME; expected fail closed")
	}
	if !strings.Contains(res.Err.Error(), "model/provider") {
		t.Fatalf("error = %v, want model/provider detail", res.Err)
	}
	if strings.Contains(res.Err.Error(), "workspaces root") {
		t.Fatalf("error = %v, hermes provider check test should not be satisfied by containment", res.Err)
	}
}

func TestReuseCodexProviderConfigFailureForcesFreshPrepare(t *testing.T) {
	sharedHome := t.TempDir()
	writeCodexTestProviderConfig(t, sharedHome, "")
	t.Setenv("CODEX_HOME", sharedHome)
	workspacesRoot := t.TempDir()

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-codex-reuse-provider-fail",
		TaskID:         "31111111-2222-3333-4444-555555555555",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "reuse-provider-fail"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	mustWrite(t, filepath.Join(sharedHome, "config.toml"), "model = \"gpt-5-codex\"\napproval_policy = \"never\"\n")

	reused := Reuse(ReuseParams{
		WorkspacesRoot: workspacesRoot,
		WorkDir:        env.WorkDir,
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "reuse-provider-fail"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if reused != nil {
		t.Fatal("Reuse succeeded with unusable codex provider config; expected nil to force fresh Prepare")
	}
}
