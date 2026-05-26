package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hookStateHeader(path, suffix string) string {
	return "[hooks.state." + quoteTOMLBasicString(path+suffix) + "]"
}

func assertConfigContains(t *testing.T, content, want string) {
	t.Helper()
	if !strings.Contains(content, want) {
		t.Fatalf("config missing %q:\n%s", want, content)
	}
}

func TestSyncCodexHookTrustStateMapsSharedHooksJSONSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sharedHome := filepath.Join(dir, "shared")
	codexHome := filepath.Join(dir, "task", "codex-home")
	if err := os.MkdirAll(sharedHome, 0o755); err != nil {
		t.Fatalf("create shared home: %v", err)
	}
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("create codex home: %v", err)
	}

	sharedHooksPath := filepath.Join(sharedHome, "hooks.json")
	taskHooksPath := filepath.Join(codexHome, "hooks.json")
	sharedConfigPath := filepath.Join(sharedHome, "config.toml")
	taskConfigPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(sharedHooksPath, []byte(`{"hooks":true}`), 0o644); err != nil {
		t.Fatalf("write shared hooks.json: %v", err)
	}
	if err := os.WriteFile(taskHooksPath, []byte(`{"hooks":true}`), 0o644); err != nil {
		t.Fatalf("write task hooks.json: %v", err)
	}

	sharedConfig := `[hooks.state]

` + hookStateHeader(sharedHooksPath, ":pre_tool_use:0:0") + `
trusted_hash = "sha256:aaa"

` + hookStateHeader(sharedHooksPath, ":pre_tool_use:0:1") + `
trusted_hash = "sha256:bbb"

[hooks.state."plugin@local:hooks/codex-hooks.json:pre_tool_use:0:0"]
trusted_hash = "sha256:plugin"
`
	if err := os.WriteFile(sharedConfigPath, []byte(sharedConfig), 0o644); err != nil {
		t.Fatalf("write shared config.toml: %v", err)
	}
	if err := os.WriteFile(taskConfigPath, []byte(`model = "o3"`+"\n"), 0o644); err != nil {
		t.Fatalf("write task config.toml: %v", err)
	}

	if err := syncCodexHookTrustState(sharedConfigPath, taskConfigPath, sharedHooksPath, taskHooksPath); err != nil {
		t.Fatalf("syncCodexHookTrustState: %v", err)
	}
	data, err := os.ReadFile(taskConfigPath)
	if err != nil {
		t.Fatalf("read task config.toml: %v", err)
	}
	content := string(data)
	assertConfigContains(t, content, hookStateHeader(taskHooksPath, ":pre_tool_use:0:0"))
	assertConfigContains(t, content, `trusted_hash = "sha256:aaa"`)
	assertConfigContains(t, content, hookStateHeader(taskHooksPath, ":pre_tool_use:0:1"))
	assertConfigContains(t, content, `trusted_hash = "sha256:bbb"`)
	if strings.Contains(content, "plugin@local") {
		t.Fatalf("plugin hook trust state should not be remapped into task hooks.json:\n%s", content)
	}

	if err := syncCodexHookTrustState(sharedConfigPath, taskConfigPath, sharedHooksPath, taskHooksPath); err != nil {
		t.Fatalf("syncCodexHookTrustState second run: %v", err)
	}
	data, err = os.ReadFile(taskConfigPath)
	if err != nil {
		t.Fatalf("read task config.toml after second run: %v", err)
	}
	content = string(data)
	if count := strings.Count(content, taskHooksPath+":pre_tool_use:0:0"); count != 1 {
		t.Fatalf("mapped hook state should be idempotent, count=%d:\n%s", count, content)
	}
}

func TestSyncCodexHookTrustStateRefreshesMappedBlocksFromSharedConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sharedHome := filepath.Join(dir, "shared")
	codexHome := filepath.Join(dir, "task", "codex-home")
	if err := os.MkdirAll(sharedHome, 0o755); err != nil {
		t.Fatalf("create shared home: %v", err)
	}
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("create codex home: %v", err)
	}

	sharedHooksPath := filepath.Join(sharedHome, "hooks.json")
	taskHooksPath := filepath.Join(codexHome, "hooks.json")
	sharedConfigPath := filepath.Join(sharedHome, "config.toml")
	taskConfigPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(sharedHooksPath, []byte(`{"hooks":true}`), 0o644); err != nil {
		t.Fatalf("write shared hooks.json: %v", err)
	}
	if err := os.WriteFile(taskHooksPath, []byte(`{"hooks":true}`), 0o644); err != nil {
		t.Fatalf("write task hooks.json: %v", err)
	}
	if err := os.WriteFile(sharedConfigPath, []byte(hookStateHeader(sharedHooksPath, ":pre_tool_use:0:0")+"\ntrusted_hash = \"sha256:new\"\n"), 0o644); err != nil {
		t.Fatalf("write shared config.toml: %v", err)
	}
	staleTaskConfig := hookStateHeader(taskHooksPath, ":pre_tool_use:0:0") + "\ntrusted_hash = \"sha256:old\"\n"
	if err := os.WriteFile(taskConfigPath, []byte(staleTaskConfig), 0o644); err != nil {
		t.Fatalf("write stale task config.toml: %v", err)
	}

	if err := syncCodexHookTrustState(sharedConfigPath, taskConfigPath, sharedHooksPath, taskHooksPath); err != nil {
		t.Fatalf("syncCodexHookTrustState: %v", err)
	}
	data, err := os.ReadFile(taskConfigPath)
	if err != nil {
		t.Fatalf("read task config.toml: %v", err)
	}
	content := string(data)
	assertConfigContains(t, content, `trusted_hash = "sha256:new"`)
	if strings.Contains(content, "sha256:old") {
		t.Fatalf("stale mapped trust state was not removed:\n%s", content)
	}
}

func TestSyncCodexHookTrustStateReportsCounts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sharedHome := filepath.Join(dir, "shared")
	codexHome := filepath.Join(dir, "task", "codex-home")
	if err := os.MkdirAll(sharedHome, 0o755); err != nil {
		t.Fatalf("create shared home: %v", err)
	}
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("create codex home: %v", err)
	}

	sharedHooksPath := filepath.Join(sharedHome, "hooks.json")
	taskHooksPath := filepath.Join(codexHome, "hooks.json")
	sharedConfigPath := filepath.Join(sharedHome, "config.toml")
	taskConfigPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(sharedHooksPath, []byte(`{"hooks":true}`), 0o644); err != nil {
		t.Fatalf("write shared hooks.json: %v", err)
	}
	if err := os.WriteFile(taskHooksPath, []byte(`{"hooks":true}`), 0o644); err != nil {
		t.Fatalf("write task hooks.json: %v", err)
	}
	sharedConfig := hookStateHeader(sharedHooksPath, ":pre_tool_use:0:0") + "\ntrusted_hash = \"sha256:a\"\n\n" +
		hookStateHeader(sharedHooksPath, ":post_tool_use:0:0") + "\ntrusted_hash = \"sha256:b\"\n"
	if err := os.WriteFile(sharedConfigPath, []byte(sharedConfig), 0o644); err != nil {
		t.Fatalf("write shared config.toml: %v", err)
	}
	taskConfig := hookStateHeader(taskHooksPath, ":pre_tool_use:0:0") + "\ntrusted_hash = \"sha256:stale\"\n"
	if err := os.WriteFile(taskConfigPath, []byte(taskConfig), 0o644); err != nil {
		t.Fatalf("write task config.toml: %v", err)
	}

	result, err := syncCodexHookTrustStateWithResult(sharedConfigPath, taskConfigPath, sharedHooksPath, taskHooksPath)
	if err != nil {
		t.Fatalf("syncCodexHookTrustStateWithResult: %v", err)
	}
	if result.SharedHooksCount != 2 || result.MappedHooksCount != 2 || result.StaleHooksCount != 1 || !result.Changed {
		t.Fatalf("unexpected sync result: %+v", result)
	}
}

func TestSyncCodexHookTrustStateClearsMappedBlocksWhenHooksMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sharedHome := filepath.Join(dir, "shared")
	codexHome := filepath.Join(dir, "task", "codex-home")
	if err := os.MkdirAll(sharedHome, 0o755); err != nil {
		t.Fatalf("create shared home: %v", err)
	}
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("create codex home: %v", err)
	}

	sharedHooksPath := filepath.Join(sharedHome, "hooks.json")
	taskHooksPath := filepath.Join(codexHome, "hooks.json")
	sharedConfigPath := filepath.Join(sharedHome, "config.toml")
	taskConfigPath := filepath.Join(codexHome, "config.toml")
	taskConfig := `model = "o3"

` + hookStateHeader(taskHooksPath, ":pre_tool_use:0:0") + `
trusted_hash = "sha256:stale"

[hooks.state."plugin@local:hooks/codex-hooks.json:pre_tool_use:0:0"]
trusted_hash = "sha256:plugin"
`
	if err := os.WriteFile(taskConfigPath, []byte(taskConfig), 0o644); err != nil {
		t.Fatalf("write task config.toml: %v", err)
	}

	if err := syncCodexHookTrustState(sharedConfigPath, taskConfigPath, sharedHooksPath, taskHooksPath); err != nil {
		t.Fatalf("syncCodexHookTrustState: %v", err)
	}
	data, err := os.ReadFile(taskConfigPath)
	if err != nil {
		t.Fatalf("read task config.toml: %v", err)
	}
	content := string(data)
	if strings.Contains(content, taskHooksPath) || strings.Contains(content, "sha256:stale") {
		t.Fatalf("stale task hooks.json trust state should be cleared:\n%s", content)
	}
	assertConfigContains(t, content, "plugin@local")
}

func TestPrepareCodexHomeMapsCodexHookTrustStateFromSharedConfig(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	sharedHooksPath := filepath.Join(sharedHome, "hooks.json")
	if err := os.WriteFile(sharedHooksPath, []byte(`{"hooks":true}`), 0o644); err != nil {
		t.Fatalf("write shared hooks.json: %v", err)
	}
	sharedConfig := hookStateHeader(sharedHooksPath, ":session_start:0:0") + "\ntrusted_hash = \"sha256:session\"\n"
	if err := os.WriteFile(filepath.Join(sharedHome, "config.toml"), []byte(sharedConfig), 0o644); err != nil {
		t.Fatalf("write shared config.toml: %v", err)
	}
	t.Setenv("CODEX_HOME", sharedHome)

	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := prepareCodexHome(codexHome, discardLogger()); err != nil {
		t.Fatalf("prepareCodexHome failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read per-task config.toml: %v", err)
	}
	taskHooksPath := filepath.Join(codexHome, "hooks.json")
	content := string(data)
	assertConfigContains(t, content, hookStateHeader(taskHooksPath, ":session_start:0:0"))
	assertConfigContains(t, content, `trusted_hash = "sha256:session"`)
}

func TestReuseRefreshesCodexHookTrustStateFromSharedConfig(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	sharedHooksPath := filepath.Join(sharedHome, "hooks.json")
	if err := os.WriteFile(sharedHooksPath, []byte(`{"hooks":true}`), 0o644); err != nil {
		t.Fatalf("write shared hooks.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sharedHome, "hooks"), 0o755); err != nil {
		t.Fatalf("create shared hooks dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sharedHome, "config.toml"), []byte(hookStateHeader(sharedHooksPath, ":pre_tool_use:0:0")+"\ntrusted_hash = \"sha256:v1\"\n"), 0o644); err != nil {
		t.Fatalf("write shared config.toml: %v", err)
	}
	t.Setenv("CODEX_HOME", sharedHome)

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-codex-hook-trust-reuse",
		TaskID:         "a6f7a8b9-c0d1-2345-fabc-678901234567",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "reuse-hook-trust-test"},
	}, discardLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if err := os.WriteFile(filepath.Join(sharedHome, "config.toml"), []byte(hookStateHeader(sharedHooksPath, ":pre_tool_use:0:0")+"\ntrusted_hash = \"sha256:v2\"\n"), 0o644); err != nil {
		t.Fatalf("update shared config.toml: %v", err)
	}

	reused := Reuse(ReuseParams{WorkDir: env.WorkDir, Provider: "codex", Task: TaskContextForEnv{IssueID: "reuse-hook-trust-test"}}, discardLogger())
	if reused == nil {
		t.Fatal("Reuse returned nil")
	}
	data, err := os.ReadFile(filepath.Join(reused.CodexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read reused config.toml: %v", err)
	}
	content := string(data)
	assertConfigContains(t, content, `trusted_hash = "sha256:v2"`)
	staleMappedBlock := hookStateHeader(filepath.Join(reused.CodexHome, "hooks.json"), ":pre_tool_use:0:0") + "\ntrusted_hash = \"sha256:v1\""
	if strings.Contains(content, staleMappedBlock) {
		t.Fatalf("reuse should refresh mapped hook trust state from shared config:\n%s", content)
	}
}
