package execenv

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSyncCodexNativeConfigRestrictsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not supported on Windows")
	}
	shared, task := t.TempDir(), t.TempDir()
	names := []string{"AGENTS.md", "AGENTS.override.md", "agents/architect.toml"}
	for _, name := range names {
		path := filepath.Join(shared, name)
		writeFile(t, path, "private configuration")
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		// Reuse also replaces copies inherited with broader permissions.
		writeFile(t, filepath.Join(task, name), "old configuration")
	}
	for range 2 {
		if err := syncCodexNativeConfig(task, shared); err != nil {
			t.Fatal(err)
		}
		for _, name := range append(names, codexNativeAgentsManifest) {
			info, err := os.Stat(filepath.Join(task, name))
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o600 {
				t.Errorf("%s permissions = %04o, want 0600", name, got)
			}
		}
	}
}

func TestPrepareCodexHomeNativeConfig(t *testing.T) {
	shared, task := t.TempDir(), t.TempDir()
	t.Setenv("CODEX_HOME", shared)
	for _, name := range []string{"AGENTS.md", "AGENTS.override.md", "agents/architect.toml", "agents/code-reviewer.toml"} {
		writeFile(t, filepath.Join(shared, name), "original")
	}
	writeFile(t, filepath.Join(shared, "agents/notes.md"), "not a role")
	writeFile(t, filepath.Join(shared, "agents/nested/private.toml"), "not a direct role")
	writeFile(t, filepath.Join(task, "agents/local.toml"), "task-owned")
	if err := prepareCodexHome(task, testLogger()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"AGENTS.md", "AGENTS.override.md", "agents/architect.toml", "agents/code-reviewer.toml"} {
		assertNativeConfigContent(t, filepath.Join(task, name), "original")
	}
	assertAbsent(t, filepath.Join(task, "agents/notes.md"))
	assertAbsent(t, filepath.Join(task, "agents/nested"))
	writeFile(t, filepath.Join(task, "agents/architect.toml"), "task mutation")
	writeFile(t, filepath.Join(shared, "agents/architect.toml"), "refreshed")
	for _, name := range []string{"AGENTS.override.md", "agents/code-reviewer.toml"} {
		if err := os.Remove(filepath.Join(shared, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := prepareCodexHome(task, testLogger()); err != nil {
		t.Fatal(err)
	}
	assertNativeConfigContent(t, filepath.Join(task, "agents/architect.toml"), "refreshed")
	assertNativeConfigContent(t, filepath.Join(task, "agents/local.toml"), "task-owned")
	assertAbsent(t, filepath.Join(task, "AGENTS.override.md"))
	assertAbsent(t, filepath.Join(task, "agents/code-reviewer.toml"))
	if err := os.RemoveAll(filepath.Join(shared, "agents")); err != nil {
		t.Fatal(err)
	}
	if err := prepareCodexHome(task, testLogger()); err != nil {
		t.Fatal(err)
	}
	assertAbsent(t, filepath.Join(task, "agents/architect.toml"))
	assertNativeConfigContent(t, filepath.Join(task, "agents/local.toml"), "task-owned")
}

func assertNativeConfigContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Errorf("%s = %q, want %q", path, data, want)
	}
}

func TestSyncCodexNativeConfigRejectsSourceErrors(t *testing.T) {
	for _, kind := range []string{"instructions directory", "agents file", "broken role link", "broken instructions link", "broken agents link"} {
		t.Run(kind, func(t *testing.T) {
			shared, task := t.TempDir(), t.TempDir()
			writeFile(t, filepath.Join(shared, "AGENTS.md"), "new instructions")
			writeFile(t, filepath.Join(task, "AGENTS.md"), "last good instructions")
			writeFile(t, filepath.Join(task, "agents/old.toml"), "last good role")
			writeFile(t, filepath.Join(task, codexNativeAgentsManifest), `["old.toml"]`)
			switch kind {
			case "instructions directory":
				if err := os.Mkdir(filepath.Join(shared, "AGENTS.override.md"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "agents file":
				writeFile(t, filepath.Join(shared, "agents"), "not a directory")
			case "broken instructions link", "broken agents link":
				name := "AGENTS.override.md"
				if kind == "broken agents link" {
					name = "agents"
				}
				if err := os.Symlink(filepath.Join(shared, "missing"), filepath.Join(shared, name)); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			case "broken role link":
				if err := os.Mkdir(filepath.Join(shared, "agents"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(shared, "missing"), filepath.Join(shared, "agents/broken.toml")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			if err := syncCodexNativeConfig(task, shared); err == nil {
				t.Fatal("expected source error")
			}
			assertNativeConfigContent(t, filepath.Join(task, "AGENTS.md"), "last good instructions")
			assertNativeConfigContent(t, filepath.Join(task, "agents/old.toml"), "last good role")
		})
	}
}

func TestSyncCodexNativeConfigConfinesDestinations(t *testing.T) {
	for _, kind := range []string{"home", "agents", "role", "instructions", "manifest", "manifest traversal"} {
		t.Run(kind, func(t *testing.T) {
			shared, task, outside := t.TempDir(), t.TempDir(), t.TempDir()
			writeFile(t, filepath.Join(shared, "agents/architect.toml"), "host role")
			writeFile(t, filepath.Join(shared, "AGENTS.md"), "host instructions")
			writeFile(t, filepath.Join(outside, "architect.toml"), "outside role")
			writeFile(t, filepath.Join(outside, "AGENTS.md"), "outside instructions")
			writeFile(t, filepath.Join(outside, codexNativeAgentsManifest), `[]`)
			var link, target string
			switch kind {
			case "home":
				if err := os.Remove(task); err != nil {
					t.Fatal(err)
				}
				link, target = task, outside
			case "agents":
				link, target = filepath.Join(task, "agents"), outside
			case "role":
				if err := os.Mkdir(filepath.Join(task, "agents"), 0o755); err != nil {
					t.Fatal(err)
				}
				link, target = filepath.Join(task, "agents/architect.toml"), filepath.Join(outside, "architect.toml")
			case "instructions":
				link, target = filepath.Join(task, "AGENTS.md"), filepath.Join(outside, "AGENTS.md")
			case "manifest":
				link, target = filepath.Join(task, codexNativeAgentsManifest), filepath.Join(outside, codexNativeAgentsManifest)
			case "manifest traversal":
				writeFile(t, filepath.Join(task, codexNativeAgentsManifest), `["../architect.toml"]`)
			}
			if link != "" {
				if err := os.Symlink(target, link); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			err := syncCodexNativeConfig(task, shared)
			if kind == "role" || kind == "instructions" {
				if err != nil {
					t.Fatal(err)
				}
				assertNativeConfigContent(t, filepath.Join(task, "agents/architect.toml"), "host role")
				assertNativeConfigContent(t, filepath.Join(task, "AGENTS.md"), "host instructions")
			} else if err == nil {
				t.Fatal("expected unsafe destination error")
			}
			assertNativeConfigContent(t, filepath.Join(outside, "architect.toml"), "outside role")
			assertNativeConfigContent(t, filepath.Join(outside, "AGENTS.md"), "outside instructions")
			assertNativeConfigContent(t, filepath.Join(outside, codexNativeAgentsManifest), `[]`)
		})
	}
}

func TestSyncCodexNativeConfigCopiesLinkedSources(t *testing.T) {
	shared, task, outside := t.TempDir(), t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(outside, "architect.toml"), "host role")
	if err := os.Mkdir(filepath.Join(shared, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "architect.toml"), filepath.Join(shared, "agents/architect.toml")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := syncCodexNativeConfig(task, shared); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(task, "agents/architect.toml"), "task mutation")
	assertNativeConfigContent(t, filepath.Join(outside, "architect.toml"), "host role")
}

func TestSyncCodexNativeConfigRejectsSharedDestination(t *testing.T) {
	shared := t.TempDir()
	writeFile(t, filepath.Join(shared, "AGENTS.md"), "host instructions")
	if err := syncCodexNativeConfig(shared, shared); err == nil {
		t.Fatal("expected same-directory error")
	}
	assertNativeConfigContent(t, filepath.Join(shared, "AGENTS.md"), "host instructions")
	assertAbsent(t, filepath.Join(shared, codexNativeAgentsManifest))
}

func TestSyncCodexNativeConfigManifestRetry(t *testing.T) {
	shared, task := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(shared, "agents/.toml"), "host role")
	// A blocked destination must not leave a truncated ownership manifest.
	writeFile(t, filepath.Join(task, "agents/.toml/keep"), "task file")
	if err := syncCodexNativeConfig(task, shared); err == nil {
		t.Fatal("expected destination error")
	}
	assertNativeConfigContent(t, filepath.Join(task, codexNativeAgentsManifest), `[".toml"]`)
	if err := os.RemoveAll(filepath.Join(task, "agents/.toml")); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := syncCodexNativeConfig(task, shared); err != nil {
			t.Fatal(err)
		}
	}
	assertNativeConfigContent(t, filepath.Join(task, "agents/.toml"), "host role")
}

func TestSyncCodexNativeConfigReplacesHardlinks(t *testing.T) {
	shared, task := t.TempDir(), t.TempDir()
	role := "name = 'architect'\ndeveloper_instructions = 'Inspect architecture.'\n"
	writeFile(t, filepath.Join(shared, "agents/architect.toml"), role)
	if err := os.Mkdir(filepath.Join(task, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(shared, "agents/architect.toml"), filepath.Join(task, "agents/architect.toml")); err != nil {
		t.Skipf("hardlink unavailable: %v", err)
	}
	if err := syncCodexNativeConfig(task, shared); err != nil {
		t.Fatal(err)
	}
	assertNativeConfigContent(t, filepath.Join(task, "agents/architect.toml"), role)
	writeFile(t, filepath.Join(task, "agents/architect.toml"), "task mutation")
	assertNativeConfigContent(t, filepath.Join(shared, "agents/architect.toml"), role)
}

func TestPrepareCodexHomeNativeConfigPreservesMultiAgentPolicy(t *testing.T) {
	for _, optIn := range []string{"", "1"} {
		t.Run("opt-in="+optIn, func(t *testing.T) {
			shared, task := t.TempDir(), t.TempDir()
			t.Setenv("CODEX_HOME", shared)
			t.Setenv(MulticaCodexMultiAgentEnv, optIn)
			writeFile(t, filepath.Join(shared, "config.toml"), "[features]\nmulti_agent = true\n")
			role := "name = 'architect'\ndeveloper_instructions = 'Inspect architecture.'\n"
			writeFile(t, filepath.Join(shared, "agents/architect.toml"), role)
			if err := prepareCodexHome(task, testLogger()); err != nil {
				t.Fatal(err)
			}
			assertNativeConfigContent(t, filepath.Join(task, "agents/architect.toml"), role)
			data, err := os.ReadFile(filepath.Join(task, "config.toml"))
			if err != nil {
				t.Fatal(err)
			}
			config := parseTOML(t, string(data))
			if optIn == "" {
				requireMultiAgentDisabled(t, config)
			} else if config["features"].(map[string]any)["multi_agent"] != true {
				t.Fatal("opt-in lost shared multi_agent=true")
			}
			assertNativeConfigContent(t, filepath.Join(shared, "config.toml"), "[features]\nmulti_agent = true\n")
		})
	}
}

func TestSyncCodexNativeConfigRejectsDanglingSharedHome(t *testing.T) {
	shared, task := filepath.Join(t.TempDir(), "shared"), t.TempDir()
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), shared); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, name := range []string{"AGENTS.md", "AGENTS.override.md", "agents/architect.toml"} {
		writeFile(t, filepath.Join(task, name), "last good copy")
	}
	writeFile(t, filepath.Join(task, codexNativeAgentsManifest), `["architect.toml"]`)
	if err := syncCodexNativeConfig(task, shared); err == nil {
		t.Error("expected dangling shared home error")
	}
	for _, name := range []string{"AGENTS.md", "AGENTS.override.md", "agents/architect.toml"} {
		assertNativeConfigContent(t, filepath.Join(task, name), "last good copy")
	}
	assertNativeConfigContent(t, filepath.Join(task, codexNativeAgentsManifest), `["architect.toml"]`)
}

func TestPrepareCodexHomeNativeConfigReferencedPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not supported on Windows")
	}
	for _, key := range []string{"model_instructions_file", "model_catalog_json"} {
		for _, name := range []string{"AGENTS.md", "AGENTS.override.md", "agents/architect.toml"} {
			t.Run(key+"/"+name, func(t *testing.T) {
				shared, task := t.TempDir(), t.TempDir()
				t.Setenv("CODEX_HOME", shared)
				writeFile(t, filepath.Join(shared, "config.toml"), key+" = '"+name+"'\n")
				writeFile(t, filepath.Join(shared, name), "private configuration")
				if err := os.Chmod(filepath.Join(shared, name), 0o600); err != nil {
					t.Fatal(err)
				}
				for range 2 {
					if err := prepareCodexHome(task, testLogger()); err != nil {
						t.Fatal(err)
					}
					info, err := os.Stat(filepath.Join(task, name))
					if err != nil {
						t.Fatal(err)
					}
					if got := info.Mode().Perm(); got != 0o600 {
						t.Errorf("%s permissions = %04o, want 0600", name, got)
					}
					assertNativeConfigContent(t, filepath.Join(task, name), "private configuration")
				}
			})
		}
	}
}

func TestMaterialiseInCodexHomeNewReferencePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not supported on Windows")
	}
	task, source := t.TempDir(), filepath.Join(t.TempDir(), "instructions.md")
	writeFile(t, source, "referenced instructions")
	sourceInfo, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := materialiseInCodexHome(task, "instructions.md", source, "model_instructions_file"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(task, "instructions.md"))
	if err != nil {
		t.Fatal(err)
	}
	// writeFile and new references both use 0644, subject to the same umask.
	if info.Mode().Perm() != sourceInfo.Mode().Perm() {
		t.Errorf("new reference permissions = %04o, want %04o", info.Mode().Perm(), sourceInfo.Mode().Perm())
	}
}
