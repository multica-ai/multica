package execenv

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func pluginSkillFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range map[string]string{
		".claude-plugin/plugin.json":            `{"name":"paper","skills":["./extra-skills"],"mcpServers":{"docs":{"command":"node","args":["${CLAUDE_PLUGIN_ROOT}/server.js"]}}}`,
		"skills/hidden/SKILL.md":                "---\nname: hidden\n---\nDisabled skill",
		"skills/hidden/helper.sh":               "#!/bin/sh\nexit 0\n",
		"skills/hidden/examples/child/SKILL.md": "Nested example must not become a skill when its parent is disabled",
		"skills/visible/SKILL.md":               "Enabled skill",
		"extra-skills/nested/hidden/SKILL.md":   "Disabled nested skill",
		"hooks/hooks.json":                      `{"hooks":{}}`,
		"agents/reviewer.md":                    "Review agent",
		"commands/check.md":                     "Check command",
		"server.js":                             "console.log('fixture')",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestClaudePluginSkillCopiesFilterOnlyDisabledEntrypoints(t *testing.T) {
	t.Parallel()
	source := pluginSkillFixture(t)
	refs := []RuntimeSkillRefForEnv{
		{Root: "plugin", Key: "paper:hidden", Plugin: "paper@market", PluginPath: source},
		{Root: "plugin", Key: "paper:nested/hidden", Plugin: "paper@market", PluginPath: source},
		{Root: "provider", Key: "visible"},
	}
	paths, err := prepareClaudePluginSkillCopies(t.TempDir(), refs)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("copies = %v, want one per plugin", paths)
	}
	for _, rel := range []string{"skills/hidden/SKILL.md", "skills/hidden/examples/child/SKILL.md", "extra-skills/nested/hidden/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(paths[0], rel)); !os.IsNotExist(err) {
			t.Fatalf("disabled entry %s remains: %v", rel, err)
		}
		if _, err := os.Stat(filepath.Join(source, rel)); err != nil {
			t.Fatalf("source entry changed: %v", err)
		}
	}
	for _, rel := range []string{".claude-plugin/plugin.json", "skills/visible/SKILL.md", "skills/hidden/helper.sh", "hooks/hooks.json", "agents/reviewer.md", "commands/check.md", "server.js"} {
		want, err := os.ReadFile(filepath.Join(source, rel))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(paths[0], rel))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Fatalf("component changed: %s", rel)
		}
	}
	if err := os.WriteFile(filepath.Join(paths[0], "server.js"), []byte("task edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(source, "server.js")); string(raw) == "task edit" {
		t.Fatal("copy aliases host file")
	}
}

func TestClaudePluginSkillCopiesRejectUnsafeInputs(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"other:hidden", "paper:../hidden", "paper:/hidden", "paper:"} {
		t.Run(key, func(t *testing.T) {
			_, err := prepareClaudePluginSkillCopies(t.TempDir(), []RuntimeSkillRefForEnv{{Root: "plugin", Key: key, Plugin: "paper@market", PluginPath: pluginSkillFixture(t)}})
			if err == nil {
				t.Fatal("unsafe or mismatched key accepted")
			}
		})
	}
	_, err := prepareClaudePluginSkillCopies(t.TempDir(), []RuntimeSkillRefForEnv{{Root: "plugin", Key: "paper:hidden", Plugin: "paper@market"}})
	if err == nil {
		t.Fatal("missing source accepted")
	}
}

func TestClaudePluginSkillCopiesRejectExternalLinks(t *testing.T) {
	t.Parallel()
	source := pluginSkillFixture(t)
	if err := os.Symlink(t.TempDir(), filepath.Join(source, "external")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	root := t.TempDir()
	_, err := prepareClaudePluginSkillCopies(root, []RuntimeSkillRefForEnv{{Root: "plugin", Key: "paper:hidden", Plugin: "paper@market", PluginPath: source}})
	if err == nil {
		t.Fatal("external symlink accepted")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed staging left copies: %v", entries)
	}
}

func TestClaudePluginSkillCopiesRefreshAndIsolation(t *testing.T) {
	t.Parallel()
	source := pluginSkillFixture(t)
	refs := []RuntimeSkillRefForEnv{{Root: "plugin", Key: "paper:hidden", Plugin: "paper@market", PluginPath: source}}
	root := t.TempDir()
	first, err := prepareClaudePluginSkillCopies(root, refs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "skills/visible/SKILL.md"), []byte("updated"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := prepareClaudePluginSkillCopies(root, refs)
	if err != nil {
		t.Fatal(err)
	}
	if first[0] == second[0] {
		t.Fatal("reuse mutates an existing copy")
	}
	old, _ := os.ReadFile(filepath.Join(first[0], "skills/visible/SKILL.md"))
	fresh, _ := os.ReadFile(filepath.Join(second[0], "skills/visible/SKILL.md"))
	if string(old) != "Enabled skill" || string(fresh) != "updated" {
		t.Fatalf("copy isolation: old=%s fresh=%s", old, fresh)
	}
	cleared, err := prepareClaudePluginSkillCopies(root, nil)
	if err != nil || len(cleared) != 0 {
		t.Fatalf("cleared policy: %v, %v", cleared, err)
	}
}

func TestClaudePluginSkillCopiesKeepNamespaceWithoutManifest(t *testing.T) {
	t.Parallel()
	source := pluginSkillFixture(t)
	if err := os.Remove(filepath.Join(source, ".claude-plugin/plugin.json")); err != nil {
		t.Fatal(err)
	}
	paths, err := prepareClaudePluginSkillCopies(t.TempDir(), []RuntimeSkillRefForEnv{{Root: "plugin", Key: "paper:hidden", Plugin: "paper@market", PluginPath: source}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(paths[0], ".claude-plugin/plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["name"] != "paper" {
		t.Fatalf("namespace lost: %s", raw)
	}
	if !strings.Contains(paths[0], "claude-plugin-skills-") {
		t.Fatalf("not task-local: %s", paths[0])
	}
}

func TestClaudePluginSkillsPrepareReuseAndCleanup(t *testing.T) {
	t.Parallel()
	source := pluginSkillFixture(t)
	task := TaskContextForEnv{IssueID: "plugin-filter", DisabledRuntimeSkills: []RuntimeSkillRefForEnv{
		{Root: "plugin", Key: "paper:hidden", Plugin: "paper@market", PluginPath: source},
	}}
	env, err := Prepare(PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "ws", TaskID: "task-plugin", Provider: "claude", Task: task}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer env.Cleanup(true)
	if len(env.ClaudePluginDirs) != 0 || env.ClaudeSettingsPath == "" {
		t.Fatalf("missing policy wiring: %+v", env)
	}
	resolve := func(env *Environment) {
		t.Helper()
		params := claudePluginPolicyFixture(t)
		params.RootDir, params.WorkDir = env.RootDir, env.WorkDir
		params.Disabled = task.DisabledRuntimeSkills
		params.Plugins[0].InstallPath = source
		var err error
		env.ClaudePluginDirs, err = PrepareClaudePluginCopies(params)
		if err != nil {
			t.Fatal(err)
		}
	}
	resolve(env)
	if err := os.WriteFile(filepath.Join(source, "skills/visible/SKILL.md"), []byte("next run"), 0o600); err != nil {
		t.Fatal(err)
	}
	reused := Reuse(ReuseParams{WorkDir: env.WorkDir, Provider: "claude", Task: task}, testLogger())
	if reused == nil || len(reused.ClaudePluginDirs) != 0 {
		t.Fatal("reuse lost plugin filter")
	}
	resolve(reused)
	fresh, err := os.ReadFile(filepath.Join(reused.ClaudePluginDirs[0], "skills/visible/SKILL.md"))
	if err != nil || string(fresh) != "next run" {
		t.Fatalf("reuse is stale: %s, %v", fresh, err)
	}
	task.DisabledRuntimeSkills = nil
	reenabled := Reuse(ReuseParams{WorkDir: env.WorkDir, Provider: "claude", Task: task}, testLogger())
	if reenabled == nil || len(reenabled.ClaudePluginDirs) != 0 || reenabled.ClaudeSettingsPath != "" {
		t.Fatal("reenable retained filtering")
	}
	if err := env.Cleanup(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reused.ClaudePluginDirs[0]); !os.IsNotExist(err) {
		t.Fatalf("cleanup retained copies: %v", err)
	}
	if _, err := os.Stat(filepath.Join(source, "skills/hidden/SKILL.md")); err != nil {
		t.Fatalf("cleanup changed host: %v", err)
	}
}

func TestClaudePluginSkillsReuseFailsClosed(t *testing.T) {
	t.Parallel()
	task := TaskContextForEnv{IssueID: "plugin-failure"}
	env, err := Prepare(PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "ws", TaskID: "task-plugin", Provider: "claude", Task: task}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer env.Cleanup(true)
	task.DisabledRuntimeSkills = []RuntimeSkillRefForEnv{{Root: "plugin", Key: "paper:hidden", Plugin: "paper@market"}}
	if reused := Reuse(ReuseParams{WorkDir: env.WorkDir, LocalDirectory: true, Provider: "claude", Task: task}, testLogger()); reused != nil {
		t.Fatal("reuse without a task root bypassed filtering")
	}
	if reused := Reuse(ReuseParams{WorkDir: env.WorkDir, Provider: "claude", Task: task}, testLogger()); reused == nil || reused.ClaudeSettingsPath == "" {
		t.Fatal("reuse must defer plugin resolution until the native query")
	}
	if _, err := Prepare(PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "ws", TaskID: "task-plugin", Provider: "claude", Task: task}, testLogger()); err != nil {
		t.Fatalf("fresh prepare must defer plugin resolution: %v", err)
	}
}

func TestClaudePluginSkillCopiesConcurrentTasks(t *testing.T) {
	t.Parallel()
	source := pluginSkillFixture(t)
	root := t.TempDir()
	keys := []string{"paper:hidden", "paper:visible"}
	paths := make([][]string, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, key := range keys {
		wg.Go(func() {
			paths[i], errs[i] = prepareClaudePluginSkillCopies(root, []RuntimeSkillRefForEnv{{Root: "plugin", Key: key, Plugin: "paper@market", PluginPath: source}})
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("task %d: %v", i, err)
		}
	}
	for i, absent := range []string{"hidden", "visible"} {
		if _, err := os.Stat(filepath.Join(paths[i][0], "skills", absent, "SKILL.md")); !os.IsNotExist(err) {
			t.Fatalf("task %d disabled skill exists", i)
		}
		if _, err := os.Stat(filepath.Join(paths[1-i][0], "skills", absent, "SKILL.md")); err != nil {
			t.Fatalf("task %d affected peer: %v", i, err)
		}
	}
}

func TestClaudePluginSkillCopiesInternalLinksAndCycles(t *testing.T) {
	t.Parallel()
	source := pluginSkillFixture(t)
	if err := os.Symlink("visible", filepath.Join(source, "skills", "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	refs := []RuntimeSkillRefForEnv{{Root: "plugin", Key: "paper:alias", Plugin: "paper@market", PluginPath: source}}
	paths, err := prepareClaudePluginSkillCopies(t.TempDir(), refs)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alias", "visible"} {
		if _, err := os.Stat(filepath.Join(paths[0], "skills", name, "SKILL.md")); !os.IsNotExist(err) {
			t.Fatalf("disabled alias remains: %s", name)
		}
	}
	if link, err := os.Readlink(filepath.Join(paths[0], "skills", "alias")); err != nil || link != "visible" {
		t.Fatalf("internal dependency topology changed: %q, %v", link, err)
	}
	if err := os.Symlink(".", filepath.Join(source, "cycle")); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareClaudePluginSkillCopies(t.TempDir(), refs); err == nil {
		t.Fatal("cycle accepted")
	}
}

func TestClaudePluginSkillCopiesRejectIdentityScopedConfiguration(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"userConfig", "channels"} {
		t.Run(field, func(t *testing.T) {
			source := pluginSkillFixture(t)
			raw, err := json.Marshal(map[string]any{"name": "paper", field: map[string]any{}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, ".claude-plugin/plugin.json"), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = prepareClaudePluginSkillCopies(t.TempDir(), []RuntimeSkillRefForEnv{{Root: "plugin", Key: "paper:hidden", Plugin: "paper@market", PluginPath: source}})
			if err == nil || !strings.Contains(err.Error(), field) {
				t.Fatalf("configuration silently lost: %v", err)
			}
		})
	}
}

func TestClaudePluginCopyCleanupIsRunScoped(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := pluginSkillFixture(t)
	refs := []RuntimeSkillRefForEnv{{Root: "plugin", Key: "paper:hidden", Plugin: "paper@market", PluginPath: source}}
	first, err := prepareClaudePluginSkillCopies(root, refs)
	if err != nil {
		t.Fatal(err)
	}
	second, err := prepareClaudePluginSkillCopies(root, refs)
	if err != nil {
		t.Fatal(err)
	}
	env := &Environment{RootDir: root, ClaudePluginDirs: first}
	if err := env.CleanupClaudePluginCopies(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(first[0]); !os.IsNotExist(err) {
		t.Fatalf("run copy remains: %v", err)
	}
	if _, err := os.Stat(second[0]); err != nil {
		t.Fatalf("peer copy removed: %v", err)
	}
	env.ClaudePluginDirs = []string{source}
	if err := env.CleanupClaudePluginCopies(); err == nil {
		t.Fatal("accepted cleanup of host install")
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatal(err)
	}
}

func TestClaudePluginCopyRejectsDataIdentityAcrossChunks(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	w := &pluginCopyContentWriter{output: &output}
	if _, err := w.Write([]byte("before ${CLAUDE_PLUGIN_")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("DATA}/deps")); err == nil {
		t.Fatal("identity reference split across chunks was accepted")
	}
	source := pluginSkillFixture(t)
	if err := os.WriteFile(filepath.Join(source, "server.js"), []byte("process.env.CLAUDE_PLUGIN_DATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := prepareClaudePluginSkillCopies(t.TempDir(), []RuntimeSkillRefForEnv{{Root: "plugin", Key: "paper:hidden", Plugin: "paper@market", PluginPath: source}})
	if err == nil || !strings.Contains(err.Error(), "CLAUDE_PLUGIN_DATA") {
		t.Fatalf("data identity silently changed: %v", err)
	}
}

func TestClaudePluginSkillCopiesRejectNamespaceCollisions(t *testing.T) {
	t.Parallel()
	_, err := prepareClaudePluginSkillCopies(t.TempDir(), []RuntimeSkillRefForEnv{
		{Root: "plugin", Key: "paper:hidden", Plugin: "paper@first", PluginPath: pluginSkillFixture(t)},
		{Root: "plugin", Key: "paper:hidden", Plugin: "paper@second", PluginPath: pluginSkillFixture(t)},
	})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous override accepted: %v", err)
	}
}

func TestClaudePluginSkillCopiesBoundDiskUsage(t *testing.T) {
	t.Parallel()
	source := pluginSkillFixture(t)
	if err := os.Truncate(filepath.Join(source, "server.js"), 257<<20); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	_, err := prepareClaudePluginSkillCopies(root, []RuntimeSkillRefForEnv{{Root: "plugin", Key: "paper:hidden", Plugin: "paper@market", PluginPath: source}})
	if err == nil || !strings.Contains(err.Error(), "256 MiB") {
		t.Fatalf("unbounded copy: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("over-budget copy left staging behind")
	}
}
