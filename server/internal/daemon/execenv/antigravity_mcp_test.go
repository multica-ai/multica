package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const antigravityMcpServers = `{"mcpServers":{"fetch":{"command":"uvx"}}}`

// A local resource puts the plugin inside the user's repository, in place or in
// a linked worktree. It is written with owner-only modes, stays out of git for
// as long as it exists, and its cleanup leaves the user's files alone.
func TestAntigravityMcpPluginInUserRepository(t *testing.T) {
	inPlace := newTestRepo(t)
	bare, linked := filepath.Join(t.TempDir(), "bare.git"), filepath.Join(t.TempDir(), "linked")
	gitRun(t, inPlace, "clone", "--bare", inPlace, bare)
	gitRun(t, bare, "worktree", "add", "--detach", linked, "HEAD")
	for _, workDir := range []string{inPlace, linked} {
		// The user's own plugin, skill, rules and ignore configuration.
		writeFile(t, filepath.Join(workDir, ".agents", "plugins", "user-plugin", "plugin.json"), "user")
		writeFile(t, filepath.Join(workDir, ".agents", "skills", "user-skill", "SKILL.md"), "user")
		writeFile(t, filepath.Join(workDir, ".agents", "rules", "AGENTS.md"), "user rule\n")
		writeFile(t, filepath.Join(workDir, ".gitignore"), "secret.env\n")
		writeFile(t, filepath.Join(workDir, "secret.env"), "TOKEN=x\n")

		envRoot := t.TempDir()
		manifest := &sidecarManifest{}
		if err := prepareAntigravityMcpConfig(workDir, "antigravity", json.RawMessage(antigravityMcpServers), manifest); err != nil {
			t.Fatalf("%s: prepareAntigravityMcpConfig: %v", workDir, err)
		}
		pluginDir := filepath.Join(workDir, ".agents", "plugins", "multica")
		configPath := filepath.Join(pluginDir, "mcp_config.json")
		if data := mustRead(t, configPath); string(data) != antigravityMcpServers {
			t.Fatalf("config = %s, want %s", data, antigravityMcpServers)
		}
		// The name fixes the multica_<server> tool names a resumed session relies on.
		var plugin map[string]string
		if err := json.Unmarshal(mustRead(t, filepath.Join(pluginDir, "plugin.json")), &plugin); err != nil || plugin["name"] != "multica" {
			t.Fatalf("plugin.json name = %q, %v; want multica", plugin["name"], err)
		}
		if runtime.GOOS != "windows" && (filePerm(t, pluginDir) != 0o700 || filePerm(t, configPath) != 0o600) {
			t.Fatalf("modes = %o, %o; want 700, 600", filePerm(t, pluginDir), filePerm(t, configPath))
		}
		// A bulk add must stage the user's rules, but never the plugin's
		// credentials nor what the user ignores themselves.
		gitRun(t, workDir, "add", "-A")
		staged := gitRun(t, workDir, "diff", "--cached", "--name-only")
		if strings.Contains(staged, ".agents/plugins/multica") || strings.Contains(staged, "secret.env") || !strings.Contains(staged, ".agents/rules/AGENTS.md") {
			t.Fatalf("%s: staged %q, want the user's rules but neither the plugin nor their ignored file", workDir, staged)
		}

		if err := writeSidecarManifest(envRoot, manifest); err != nil {
			t.Fatal(err)
		}
		if err := CleanupSidecars(envRoot); err != nil {
			t.Fatal(err)
		}
		assertAbsent(t, pluginDir)
		assertPresent(t, filepath.Join(workDir, ".agents", "plugins", "user-plugin"))
		assertPresent(t, filepath.Join(workDir, ".agents", "skills", "user-skill"))
		assertPresent(t, filepath.Join(workDir, ".agents", "rules"))
	}
}

// No config, a null one, or another provider's writes nothing; a config that is
// not a JSON object is refused. Either way the workdir stays as it was.
func TestPrepareAntigravityMcpConfigWritesNothingUnmanaged(t *testing.T) {
	for _, tc := range []struct {
		provider, config string
		wantErr          bool
	}{
		{"antigravity", "", false},
		{"antigravity", "null", false},
		{"claude", antigravityMcpServers, false},
		{"antigravity", "[]", true},
	} {
		workDir := t.TempDir()
		err := prepareAntigravityMcpConfig(workDir, tc.provider, json.RawMessage(tc.config), &sidecarManifest{})
		if (err != nil) != tc.wantErr {
			t.Fatalf("%s %q: error = %v, wantErr %v", tc.provider, tc.config, err, tc.wantErr)
		}
		assertAbsent(t, filepath.Join(workDir, ".agents"))
	}
}

// A plugin directory that is already there, or a symlinked ancestor, is refused
// without touching the user's content in or behind it.
func TestPrepareAntigravityMcpConfigRefusesExistingPaths(t *testing.T) {
	// The plain directory goes first: it needs no symlink privilege to check.
	for _, rel := range []string{".agents/plugins/multica", ".agents", ".agents/plugins"} {
		workDir := t.TempDir()
		target := filepath.Join(workDir, rel)
		// userDir holds the user's content: the plugin directory they already
		// own, or the directory a symlinked ancestor points at.
		userDir := target
		if rel != ".agents/plugins/multica" {
			requireSymlinks(t)
			userDir = t.TempDir()
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(userDir, target); err != nil {
				t.Fatal(err)
			}
		}
		writeFile(t, filepath.Join(userDir, "keep.txt"), "user")

		err := prepareAntigravityMcpConfig(workDir, "antigravity", json.RawMessage(antigravityMcpServers), &sidecarManifest{})
		if err == nil {
			t.Fatalf("%s: prepareAntigravityMcpConfig succeeded, want a refusal", rel)
		}
		if entries, err := os.ReadDir(userDir); err != nil || len(entries) != 1 {
			t.Fatalf("%s: user directory changed: %v, %v", rel, entries, err)
		}
	}
}

// A reused conversation takes the newly saved config, and loses the plugin once
// managed MCP is unset.
func TestReuseAntigravityMcpConfigFollowsSavedConfig(t *testing.T) {
	task := TaskContextForEnv{IssueID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-antigravity-mcp",
		TaskID:         task.IssueID,
		AgentName:      "Antigravity",
		Provider:       "antigravity",
		McpConfig:      json.RawMessage(antigravityMcpServers),
		Task:           task,
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer env.Cleanup(true)

	pluginDir := filepath.Join(env.WorkDir, ".agents", "plugins", "multica")
	saved := `{"mcpServers":{"docs":{"url":"https://docs.example/mcp"}}}`
	for _, tc := range []struct{ mcpConfig, want string }{{saved, saved}, {"null", ""}} {
		reused := Reuse(ReuseParams{WorkDir: env.WorkDir, Provider: "antigravity", McpConfig: json.RawMessage(tc.mcpConfig), Task: task}, testLogger())
		data, err := os.ReadFile(filepath.Join(pluginDir, "mcp_config.json"))
		if reused == nil || string(data) != tc.want || (tc.want == "" && !os.IsNotExist(err)) {
			t.Fatalf("Reuse with %s: declined=%v config=%q err=%v, want %q", tc.mcpConfig, reused == nil, data, err, tc.want)
		}
	}
}
