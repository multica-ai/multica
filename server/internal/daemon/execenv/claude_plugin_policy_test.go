package execenv

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func claudePluginPolicyFixture(t *testing.T) ClaudePluginCopiesParams {
	t.Helper()
	source := pluginSkillFixture(t)
	config := t.TempDir()
	if err := os.MkdirAll(filepath.Join(config, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config, "plugins/known_marketplaces.json"), []byte(`{"market":{"source":{"source":"github","repo":"fixture/market"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return ClaudePluginCopiesParams{
		RootDir: t.TempDir(), WorkDir: t.TempDir(), ConfigDir: config,
		Disabled: []RuntimeSkillRefForEnv{{Root: "plugin", Plugin: "paper@market", Key: "paper:hidden", PluginPath: "/not-authoritative"}},
		Plugins:  []agent.ClaudePluginState{{ID: "paper@market", Scope: "user", Enabled: true, InstallPath: source}},
	}
}

func TestClaudePluginPolicyRespectsNativeEnablement(t *testing.T) {
	t.Parallel()
	params := claudePluginPolicyFixture(t)
	for _, enabled := range []bool{true, false, true} {
		params.Plugins[0].Enabled = enabled
		paths, err := PrepareClaudePluginCopies(params)
		if err != nil {
			t.Fatal(err)
		}
		if !enabled {
			if len(paths) != 0 {
				t.Fatal("project/local-disabled plugin re-enabled through a copy")
			}
			continue
		}
		if len(paths) != 1 {
			t.Fatalf("active plugin copies = %v", paths)
		}
		if _, err := os.Stat(filepath.Join(paths[0], "skills/hidden/SKILL.md")); !os.IsNotExist(err) {
			t.Fatalf("disabled skill remains: %v", err)
		}
		if _, err := os.Stat(filepath.Join(paths[0], "skills/visible/SKILL.md")); err != nil {
			t.Fatal(err)
		}
	}
}

func TestClaudePluginPolicyUninstalledAndOrphaned(t *testing.T) {
	t.Parallel()
	params := claudePluginPolicyFixture(t)
	params.Plugins = []agent.ClaudePluginState{}
	for _, settings := range []string{`{}`, `{"enabledPlugins":{"paper@market":false}}`} {
		if err := os.WriteFile(filepath.Join(params.ConfigDir, "settings.json"), []byte(settings), 0o600); err != nil {
			t.Fatal(err)
		}
		if dirs, err := PrepareClaudePluginCopies(params); err != nil || len(dirs) != 0 {
			t.Fatalf("ordinary uninstall blocks task: %v, %v", dirs, err)
		}
	}
	if err := os.WriteFile(filepath.Join(params.ConfigDir, "settings.json"), []byte(`{"enabledPlugins":{"paper@market":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareClaudePluginCopies(params); err == nil || !strings.Contains(err.Error(), "unregistered") {
		t.Fatalf("orphaned enablement accepted: %v", err)
	}
}

func TestClaudePluginPolicyRejectsAmbiguousOrBrokenInventory(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"missing snapshot", "duplicate", "project", "missing path", "load error", "relative path"} {
		t.Run(mode, func(t *testing.T) {
			params := claudePluginPolicyFixture(t)
			switch mode {
			case "missing snapshot":
				params.Plugins = nil
			case "duplicate":
				other := params.Plugins[0]
				other.InstallPath = t.TempDir()
				params.Plugins = append(params.Plugins, other)
			case "project":
				params.Plugins[0].Scope = "project"
			case "missing path":
				params.Plugins[0].InstallPath = ""
			case "relative path":
				params.Plugins[0].InstallPath = "relative/plugin"
			case "load error":
				params.Plugins[0].Errors = []string{"cannot load plugin"}
			}
			if _, err := PrepareClaudePluginCopies(params); err == nil {
				t.Fatal("uncertain policy accepted")
			}
			entries, err := os.ReadDir(params.RootDir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed selection left copies: %v, %v", entries, err)
			}
		})
	}
}

func TestClaudePluginPolicyRejectsNonCachedMarketplace(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"directory", "file", "settings", "future-source", ""} {
		t.Run(source, func(t *testing.T) {
			params := claudePluginPolicyFixture(t)
			raw, _ := json.Marshal(map[string]any{"market": map[string]any{"source": map[string]string{"source": source}}})
			if err := os.WriteFile(filepath.Join(params.ConfigDir, "plugins/known_marketplaces.json"), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := PrepareClaudePluginCopies(params); err == nil {
				t.Fatal("accepted a source whose native discovery can differ from the install cache")
			}
		})
	}
}

func TestClaudePluginPolicyCoalescesSameInstallAcrossScopes(t *testing.T) {
	t.Parallel()
	params := claudePluginPolicyFixture(t)
	local := params.Plugins[0]
	local.Scope = "local"
	params.Plugins = append(params.Plugins, local)
	dirs, err := PrepareClaudePluginCopies(params)
	if err != nil || len(dirs) != 1 {
		t.Fatalf("same user/local install is not ambiguous: %v, %v", dirs, err)
	}
}

func TestClaudePluginPolicyChecksProjectOverrides(t *testing.T) {
	t.Parallel()
	for _, file := range []string{"settings.json", "settings.local.json"} {
		t.Run(file, func(t *testing.T) {
			params := claudePluginPolicyFixture(t)
			writeFile(t, filepath.Join(params.WorkDir, ".claude", file), `{"extraKnownMarketplaces":{"market":{"source":{"source":"directory","path":"./plugins"}}}}`)
			if _, err := PrepareClaudePluginCopies(params); err == nil {
				t.Fatal("local-development override accepted using cached registry metadata")
			}
			params.Plugins[0].Enabled = false
			if dirs, err := PrepareClaudePluginCopies(params); err != nil || len(dirs) != 0 {
				t.Fatalf("disabled plugin was validated as an active copy: %v, %v", dirs, err)
			}
		})
	}
}

func TestClaudePluginPolicyChecksMainCheckoutLocalSettings(t *testing.T) {
	t.Parallel()
	params := claudePluginPolicyFixture(t)
	repo := newTestRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	gitRun(t, repo, "worktree", "add", "-b", "linked", linked)
	params.WorkDir = filepath.Join(linked, "subdir")
	if err := os.MkdirAll(params.WorkDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, ".claude/settings.local.json"), `{"enabledPlugins":{"paper@market":true}}`)
	params.Plugins = []agent.ClaudePluginState{}
	if _, err := PrepareClaudePluginCopies(params); err == nil || !strings.Contains(err.Error(), "unregistered") {
		t.Fatalf("missed main-checkout orphan: %v", err)
	}
}

func TestClaudePluginPolicyChecksPhysicalWorkdir(t *testing.T) {
	t.Parallel()
	params := claudePluginPolicyFixture(t)
	real := t.TempDir()
	writeFile(t, filepath.Join(real, ".claude/settings.local.json"), `{"enabledPlugins":{"paper@market":true}}`)
	subdir := filepath.Join(real, "subdir")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	params.WorkDir = filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(subdir, params.WorkDir); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	params.Plugins = []agent.ClaudePluginState{}
	if _, err := PrepareClaudePluginCopies(params); err == nil || !strings.Contains(err.Error(), "unregistered") {
		t.Fatalf("missed physical ancestor orphan: %v", err)
	}
}

func TestClaudePluginPolicyRejectsCorruptRegistry(t *testing.T) {
	t.Parallel()
	params := claudePluginPolicyFixture(t)
	params.Plugins = []agent.ClaudePluginState{}
	if err := os.MkdirAll(filepath.Join(params.ConfigDir, "plugins"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(params.ConfigDir, "plugins/installed_plugins.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareClaudePluginCopies(params); err == nil {
		t.Fatal("corrupt registry treated as an uninstall")
	}
}

func TestClaudePluginCopiesIsolatedPreservesPeerAndCleanup(t *testing.T) {
	t.Parallel()
	params := claudePluginPolicyFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	paths, err := PrepareClaudePluginCopiesIsolated(ctx, preparationHelperTestCommand(), params, testLogger())
	if err != nil || len(paths) != 1 {
		t.Fatalf("isolated copies: %v, %v", paths, err)
	}
	peer, err := PrepareClaudePluginCopiesIsolated(ctx, preparationHelperTestCommand(), params, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	env := &Environment{RootDir: params.RootDir, ClaudePluginDirs: paths}
	if err := env.CleanupClaudePluginCopies(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(peer[0]); err != nil {
		t.Fatalf("cleanup touched peer: %v", err)
	}
	if _, err := os.Stat(paths[0]); !os.IsNotExist(err) {
		t.Fatalf("completed copy remains: %v", err)
	}
}
