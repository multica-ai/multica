package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCursorMcpApprovalKeyMatchesCursorAgent(t *testing.T) {
	t.Parallel()

	keys, err := cursorMcpApprovalKeys("/tmp/work", map[string]json.RawMessage{
		"fetch": json.RawMessage(`{"command":"uvx","args":["mcp-server-fetch"]}`),
	})
	if err != nil {
		t.Fatalf("cursorMcpApprovalKeys: %v", err)
	}
	want := []string{"fetch-b3a6127d3cbd8e52"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("approval keys = %v, want %v", keys, want)
	}
}

func TestCursorMcpApprovalKeyMatchesCursorAgentWithReorderedStdioConfig(t *testing.T) {
	t.Parallel()

	keys, err := cursorMcpApprovalKeys("/tmp/work", map[string]json.RawMessage{
		"fetch": json.RawMessage(`{ "args": [ "mcp-server-fetch" ], "command": "uvx", "env": { "TOKEN": "x" } }`),
	})
	if err != nil {
		t.Fatalf("cursorMcpApprovalKeys: %v", err)
	}
	want := []string{"fetch-4c7232835349773b"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("approval keys = %v, want %v", keys, want)
	}
}

func TestCursorMcpApprovalKeyMatchesJSONStringifyWithHTMLCharacters(t *testing.T) {
	t.Parallel()

	keys, err := cursorMcpApprovalKeys("/tmp/work", map[string]json.RawMessage{
		"fetch": json.RawMessage(`{"command":"uvx","args":["mcp"],"env":{"URL":"https://x.test?a=1&b=2"}}`),
	})
	if err != nil {
		t.Fatalf("cursorMcpApprovalKeys: %v", err)
	}
	want := []string{"fetch-37ab92a3e088d73a"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("approval keys = %v, want %v", keys, want)
	}
}

func TestCursorMcpApprovalKeyMatchesCursorAgentWithStdioTypeAndCwd(t *testing.T) {
	t.Parallel()

	keys, err := cursorMcpApprovalKeys("/private/tmp/work", map[string]json.RawMessage{
		"withtypecwd": json.RawMessage(`{"enabled":true,"cwd":"/tmp/probe","timeout":30,"args":["mcp"],"command":"uvx","type":"stdio"}`),
	})
	if err != nil {
		t.Fatalf("cursorMcpApprovalKeys: %v", err)
	}
	// Captured from cursor-agent 2026.08.04-aaa8809 via mcp enable.
	want := []string{"withtypecwd-ba71d2f70913528e"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("approval keys = %v, want %v", keys, want)
	}
}

func TestCursorMcpApprovalKeyMatchesCursorAgentForRemoteServer(t *testing.T) {
	t.Parallel()

	keys, err := cursorMcpApprovalKeys("/private/tmp/work", map[string]json.RawMessage{
		"remote": json.RawMessage(`{"enabled":true,"headers":{"Authorization":"Bearer x"},"timeout":30,"url":"https://mcp.example.com","type":"http"}`),
	})
	if err != nil {
		t.Fatalf("cursorMcpApprovalKeys: %v", err)
	}
	// Captured from cursor-agent 2026.08.04-aaa8809 via mcp enable.
	want := []string{"remote-abc2ffe78532b37e"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("approval keys = %v, want %v", keys, want)
	}
}

func TestPrepareCursorMcpConfigWritesProjectConfigAndApprovals(t *testing.T) {
	t.Parallel()

	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	manifest := &sidecarManifest{}
	mcpConfig := json.RawMessage(`{
		"mcpServers": {
			"fetch": {"command":"uvx","args":["mcp-server-fetch"]},
			"http": {"url":"https://mcp.example.com","type":"http"}
		}
	}`)

	cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, mcpConfig, "", "", manifest, nil)
	if err != nil {
		t.Fatalf("prepareCursorMcpConfig: %v", err)
	}
	if cursorDataDir != filepath.Join(envRoot, "cursor-data") {
		t.Fatalf("CursorDataDir = %q, want envRoot/cursor-data", cursorDataDir)
	}

	rawConfig, err := os.ReadFile(filepath.Join(workDir, ".cursor", "mcp.json"))
	if err != nil {
		t.Fatalf("read .cursor/mcp.json: %v", err)
	}
	var cfg cursorMcpConfigFile
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		t.Fatalf("unmarshal .cursor/mcp.json: %v\n%s", err, rawConfig)
	}
	if len(cfg.McpServers) != 2 {
		t.Fatalf("mcpServers length = %d, want 2: %s", len(cfg.McpServers), rawConfig)
	}
	if mode := filePerm(t, filepath.Join(workDir, ".cursor", "mcp.json")); mode != 0o600 {
		t.Fatalf(".cursor/mcp.json mode = %#o, want 0600", mode)
	}

	projectRoot := cursorProjectRoot(workDir)
	projectDataDir := filepath.Join(cursorDataDir, "projects", cursorSlugifyPath(projectRoot))
	rawApprovals, err := os.ReadFile(filepath.Join(projectDataDir, "mcp-approvals.json"))
	if err != nil {
		t.Fatalf("read mcp-approvals.json: %v", err)
	}
	var approvals []string
	if err := json.Unmarshal(rawApprovals, &approvals); err != nil {
		t.Fatalf("unmarshal mcp-approvals.json: %v\n%s", err, rawApprovals)
	}
	wantApprovals, err := cursorMcpApprovalKeys(projectRoot, cfg.McpServers)
	if err != nil {
		t.Fatalf("expected approvals: %v", err)
	}
	if !reflect.DeepEqual(approvals, wantApprovals) {
		t.Fatalf("approvals = %v, want %v", approvals, wantApprovals)
	}
	if mode := filePerm(t, filepath.Join(projectDataDir, "mcp-approvals.json")); mode != 0o600 {
		t.Fatalf("mcp-approvals.json mode = %#o, want 0600", mode)
	}
	if _, err := os.Stat(filepath.Join(projectDataDir, cursorWorkspaceTrustedFile)); err != nil {
		t.Fatalf("workspace trust file missing: %v", err)
	}
	if len(manifest.Files) == 0 {
		t.Fatal("manifest did not record .cursor/mcp.json")
	}
}

func TestPrepareCursorMcpConfigManagedEmptySet(t *testing.T) {
	t.Parallel()

	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, json.RawMessage(`{"mcpServers":{}}`), "", "", &sidecarManifest{}, nil)
	if err != nil {
		t.Fatalf("prepareCursorMcpConfig: %v", err)
	}
	if cursorDataDir == "" {
		t.Fatal("managed empty mcp_config should still isolate CursorDataDir")
	}
	projectRoot := cursorProjectRoot(workDir)
	rawApprovals, err := os.ReadFile(filepath.Join(cursorDataDir, "projects", cursorSlugifyPath(projectRoot), "mcp-approvals.json"))
	if err != nil {
		t.Fatalf("read mcp-approvals.json: %v", err)
	}
	var approvals []string
	if err := json.Unmarshal(rawApprovals, &approvals); err != nil {
		t.Fatalf("unmarshal approvals: %v", err)
	}
	if len(approvals) != 0 {
		t.Fatalf("approvals length = %d, want 0: %v", len(approvals), approvals)
	}
}

func TestPrepareCursorMcpConfigSeedsExplicitAuthSource(t *testing.T) {
	t.Parallel()

	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	sourceProjectDir := filepath.Join(envRoot, "source-project")
	if err := os.MkdirAll(sourceProjectDir, 0o700); err != nil {
		t.Fatalf("mkdir source project: %v", err)
	}
	sourceAuth := filepath.Join(sourceProjectDir, cursorMcpAuthFile)
	if err := os.WriteFile(sourceAuth, []byte(`{"tokens":{"fetch":"secret"}}`), 0o600); err != nil {
		t.Fatalf("write source auth: %v", err)
	}

	cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"]}}}`), sourceProjectDir, "", &sidecarManifest{}, nil)
	if err != nil {
		t.Fatalf("prepareCursorMcpConfig: %v", err)
	}
	projectRoot := cursorProjectRoot(workDir)
	targetAuth := filepath.Join(cursorDataDir, "projects", cursorSlugifyPath(projectRoot), cursorMcpAuthFile)
	data, err := os.ReadFile(targetAuth)
	if err != nil {
		t.Fatalf("read seeded auth: %v", err)
	}
	if string(data) != `{"tokens":{"fetch":"secret"}}` {
		t.Fatalf("seeded auth = %s", data)
	}
}

func TestPrepareCursorMcpConfigRemovesPriorAuthOnOptOut(t *testing.T) {
	t.Parallel()

	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	sourceProjectDir := filepath.Join(envRoot, "source-project")
	if err := os.MkdirAll(sourceProjectDir, 0o700); err != nil {
		t.Fatalf("mkdir source project: %v", err)
	}
	sourceAuth := filepath.Join(sourceProjectDir, cursorMcpAuthFile)
	if err := os.WriteFile(sourceAuth, []byte(`{"tokens":{"fetch":"secret"}}`), 0o600); err != nil {
		t.Fatalf("write source auth: %v", err)
	}
	mcpConfig := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"]}}}`)

	cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, mcpConfig, sourceProjectDir, "", nil, nil)
	if err != nil {
		t.Fatalf("prepareCursorMcpConfig with auth source: %v", err)
	}
	projectRoot := cursorProjectRoot(workDir)
	targetAuth := filepath.Join(cursorDataDir, "projects", cursorSlugifyPath(projectRoot), cursorMcpAuthFile)
	if _, err := os.Stat(targetAuth); err != nil {
		t.Fatalf("seeded auth missing: %v", err)
	}

	cursorDataDir, err = prepareCursorMcpConfig(envRoot, workDir, mcpConfig, "", "", nil, nil)
	if err != nil {
		t.Fatalf("prepareCursorMcpConfig without auth source: %v", err)
	}
	targetAuth = filepath.Join(cursorDataDir, "projects", cursorSlugifyPath(projectRoot), cursorMcpAuthFile)
	if _, err := os.Stat(targetAuth); !os.IsNotExist(err) {
		t.Fatalf("auth file should be removed after opt-out, stat err=%v", err)
	}
}

func TestPrepareCursorMcpConfigLoginPersistsAcrossTasksViaAgentStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	mcpConfig := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"]}}}`)

	firstRoot := filepath.Join(root, "task-1")
	firstWork := filepath.Join(firstRoot, "workdir")
	if err := os.MkdirAll(firstWork, 0o755); err != nil {
		t.Fatalf("mkdir first workdir: %v", err)
	}
	firstData, err := prepareCursorMcpConfig(firstRoot, firstWork, mcpConfig, "", store, nil, nil)
	if err != nil {
		t.Fatalf("prepare first task: %v", err)
	}
	if mode := filePerm(t, store); mode != 0o600 {
		t.Fatalf("store mode = %#o, want 0600", mode)
	}
	if info, err := os.Stat(filepath.Dir(store)); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("store dir stat = %v, %v; want 0700", info, err)
	}

	// Cursor writes a login through the task's file in place.
	login := []byte(`{"fetch":{"tokens":{"access_token":"a","refresh_token":"r"}}}`)
	if err := os.WriteFile(CursorMcpAuthPath(firstData, firstWork), login, 0o600); err != nil {
		t.Fatalf("write login: %v", err)
	}

	// Re-preparing the same env (reuse) must not wipe the login.
	if _, err := prepareCursorMcpConfig(firstRoot, firstWork, mcpConfig, "", store, nil, nil); err != nil {
		t.Fatalf("re-prepare first task: %v", err)
	}
	if data, err := os.ReadFile(CursorMcpAuthPath(firstData, firstWork)); err != nil || string(data) != string(login) {
		t.Fatalf("reused task auth = %q, %v; want login", data, err)
	}

	// A new task of the same agent sees the same login.
	secondRoot := filepath.Join(root, "task-2")
	secondWork := filepath.Join(secondRoot, "workdir")
	if err := os.MkdirAll(secondWork, 0o755); err != nil {
		t.Fatalf("mkdir second workdir: %v", err)
	}
	secondData, err := prepareCursorMcpConfig(secondRoot, secondWork, mcpConfig, "", store, nil, nil)
	if err != nil {
		t.Fatalf("prepare second task: %v", err)
	}
	if data, err := os.ReadFile(CursorMcpAuthPath(secondData, secondWork)); err != nil || string(data) != string(login) {
		t.Fatalf("second task auth = %q, %v; want login", data, err)
	}

	// A token refresh in one task is what the other task reads next.
	refreshed := []byte(`{"fetch":{"tokens":{"access_token":"b","refresh_token":"r2"}}}`)
	if err := os.WriteFile(CursorMcpAuthPath(secondData, secondWork), refreshed, 0o600); err != nil {
		t.Fatalf("write refresh: %v", err)
	}
	if data, err := os.ReadFile(CursorMcpAuthPath(firstData, firstWork)); err != nil || string(data) != string(refreshed) {
		t.Fatalf("first task auth after refresh = %q, %v; want refreshed", data, err)
	}

	// Removing a task env leaves the store intact.
	if err := os.RemoveAll(firstRoot); err != nil {
		t.Fatalf("remove first task: %v", err)
	}
	if data, err := os.ReadFile(store); err != nil || string(data) != string(refreshed) {
		t.Fatalf("store after task cleanup = %q, %v; want refreshed", data, err)
	}
}

func TestPrepareCursorMcpConfigExplicitAuthSourceOverridesAgentStore(t *testing.T) {
	t.Parallel()

	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	sourceProjectDir := filepath.Join(envRoot, "source-project")
	if err := os.MkdirAll(sourceProjectDir, 0o700); err != nil {
		t.Fatalf("mkdir source project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceProjectDir, cursorMcpAuthFile), []byte(`{"from":"source"}`), 0o600); err != nil {
		t.Fatalf("write source auth: %v", err)
	}
	store := filepath.Join(envRoot, "store", cursorMcpAuthFile)
	mcpConfig := json.RawMessage(`{"mcpServers":{}}`)

	cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, mcpConfig, sourceProjectDir, store, nil, nil)
	if err != nil {
		t.Fatalf("prepareCursorMcpConfig: %v", err)
	}
	if data, err := os.ReadFile(CursorMcpAuthPath(cursorDataDir, workDir)); err != nil || string(data) != `{"from":"source"}` {
		t.Fatalf("task auth = %q, %v; want explicit source", data, err)
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Fatalf("store should not be created when an explicit source is set, stat err=%v", err)
	}

	// Dropping the explicit source switches the task back to the agent store.
	cursorDataDir, err = prepareCursorMcpConfig(envRoot, workDir, mcpConfig, "", store, nil, nil)
	if err != nil {
		t.Fatalf("prepareCursorMcpConfig without source: %v", err)
	}
	if data, err := os.ReadFile(CursorMcpAuthPath(cursorDataDir, workDir)); err != nil || string(data) != "{}\n" {
		t.Fatalf("task auth = %q, %v; want empty agent store", data, err)
	}
}

func TestCursorMcpServersNeedingLogin(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	workDir := t.TempDir()
	if got := CursorMcpServersNeedingLogin(dataDir, workDir); got != nil {
		t.Fatalf("missing file = %v, want nil", got)
	}
	if got := CursorMcpServersNeedingLogin("", workDir); got != nil {
		t.Fatalf("no data dir = %v, want nil", got)
	}

	authPath := CursorMcpAuthPath(dataDir, workDir)
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Shapes captured from cursor-agent 2026.11.13: a server that challenged
	// for OAuth gets clientInfo (and scopeStepUp) without tokens.
	if err := os.WriteFile(authPath, []byte(`{
		"mobbin": {"clientInfo": {"client_id": "c"}, "scopeStepUp": {"stepUpScopes": []}},
		"plugin-mobbin-mobbin": {"clientInfo": {"client_id": "c"}},
		"linear": {"tokens": {"access_token": "a", "refresh_token": "r", "token_type": "bearer"}, "clientInfo": {"client_id": "c"}},
		"expired": {"tokens": {"refresh_token": "r"}},
		"emptied": {"tokens": {}}
	}`), 0o600); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	want := []string{"emptied", "mobbin", "plugin-mobbin-mobbin"}
	if got := CursorMcpServersNeedingLogin(dataDir, workDir); !reflect.DeepEqual(got, want) {
		t.Fatalf("needing login = %v, want %v", got, want)
	}

	if err := os.WriteFile(authPath, []byte(`not json`), 0o600); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	if got := CursorMcpServersNeedingLogin(dataDir, workDir); got != nil {
		t.Fatalf("malformed file = %v, want nil", got)
	}
}

func TestCursorMcpLoginInOneTaskClearsNoticeForNextTask(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	mcpConfig := json.RawMessage(`{"mcpServers":{"multica_apps":{"url":"http://127.0.0.1:1/mcp"}}}`)
	prepare := func(name string) (string, string) {
		t.Helper()
		envRoot := filepath.Join(root, name)
		workDir := filepath.Join(envRoot, "workdir")
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
		dataDir, err := prepareCursorMcpConfig(envRoot, workDir, mcpConfig, "", store, nil, nil)
		if err != nil {
			t.Fatalf("prepare %s: %v", name, err)
		}
		return dataDir, workDir
	}

	firstData, firstWork := prepare("task-1")
	if got := CursorMcpServersNeedingLogin(firstData, firstWork); got != nil {
		t.Fatalf("fresh store = %v, want nothing to report", got)
	}
	// Cursor meets Mobbin's OAuth challenge during the first run.
	if err := os.WriteFile(CursorMcpAuthPath(firstData, firstWork), []byte(`{"mobbin":{"clientInfo":{"client_id":"c"}}}`), 0o600); err != nil {
		t.Fatalf("write challenge: %v", err)
	}
	if got := CursorMcpServersNeedingLogin(firstData, firstWork); !reflect.DeepEqual(got, []string{"mobbin"}) {
		t.Fatalf("after challenge = %v, want [mobbin]", got)
	}

	secondData, secondWork := prepare("task-2")
	if got := CursorMcpServersNeedingLogin(secondData, secondWork); !reflect.DeepEqual(got, []string{"mobbin"}) {
		t.Fatalf("next task before login = %v, want [mobbin]", got)
	}

	// `cursor-agent mcp login mobbin`, run with the first task's workdir and
	// CURSOR_DATA_DIR, writes the tokens through that task's link.
	if err := os.WriteFile(CursorMcpAuthPath(firstData, firstWork), []byte(`{"mobbin":{"clientInfo":{"client_id":"c"},"tokens":{"access_token":"a","refresh_token":"r"}}}`), 0o600); err != nil {
		t.Fatalf("write login: %v", err)
	}
	thirdData, thirdWork := prepare("task-3")
	if got := CursorMcpServersNeedingLogin(thirdData, thirdWork); got != nil {
		t.Fatalf("next task after login = %v, want nothing to report", got)
	}
	if got := CursorMcpServersNeedingLogin(secondData, secondWork); got != nil {
		t.Fatalf("task prepared before login = %v, want it to see the login too", got)
	}
}

// loginThroughWorkspaceAfterTaskCleanup is the user path the run-record notice
// describes: the task's workdir is cleaned up, then `cursor-agent mcp login`
// runs in the login workspace, then the agent's next task must see the login.
func loginThroughWorkspaceAfterTaskCleanup(t *testing.T, store, authSource string, mcpConfig json.RawMessage, cleanup func()) (string, string) {
	t.Helper()
	cleanup()

	loginWork, loginData, err := PrepareCursorMcpLoginWorkspace(authSource, store, mcpConfig, []string{"oauth"})
	if err != nil {
		t.Fatalf("PrepareCursorMcpLoginWorkspace: %v", err)
	}
	var cfg cursorMcpConfigFile
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(loginWork, ".cursor", "mcp.json"))), &cfg); err != nil {
		t.Fatalf("login workspace mcp.json: %v", err)
	}
	if len(cfg.McpServers) != 1 || cfg.McpServers["oauth"] == nil {
		t.Fatalf("login workspace servers = %v, want only the server needing login", cfg.McpServers)
	}
	if _, err := os.Stat(filepath.Join(loginData, "projects", cursorSlugifyPath(cursorProjectRoot(loginWork)), "mcp-approvals.json")); err != nil {
		t.Fatalf("login workspace approvals missing: %v", err)
	}
	login := `{"oauth":{"clientInfo":{"client_id":"c"},"tokens":{"access_token":"a","refresh_token":"r"}}}`
	if err := os.WriteFile(CursorMcpAuthPath(loginData, loginWork), []byte(login), 0o600); err != nil {
		t.Fatalf("log in through workspace: %v", err)
	}
	return loginWork, login
}

func TestCursorMcpLoginWorkspaceSurvivesInPlaceCleanup(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	mcpConfig := json.RawMessage(`{"mcpServers":{"oauth":{"url":"https://mcp.example.com/mcp"},"apps":{"url":"http://127.0.0.1:1/mcp","headers":{"Authorization":"Bearer task-token"}}}}`)

	userDir := newTestRepo(t)
	envRoot := filepath.Join(root, "task-1")
	manifest := &sidecarManifest{}
	dataDir, err := prepareCursorMcpConfig(envRoot, userDir, mcpConfig, "", store, manifest, nil)
	if err != nil {
		t.Fatalf("prepare in-place task: %v", err)
	}
	if err := os.WriteFile(CursorMcpAuthPath(dataDir, userDir), []byte(`{"oauth":{"clientInfo":{"client_id":"c"}}}`), 0o600); err != nil {
		t.Fatalf("write challenge: %v", err)
	}
	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatalf("writeSidecarManifest: %v", err)
	}

	_, login := loginThroughWorkspaceAfterTaskCleanup(t, store, "", mcpConfig, func() {
		if err := CleanupSidecars(envRoot); err != nil {
			t.Fatalf("CleanupSidecars: %v", err)
		}
		if _, err := os.Stat(filepath.Join(userDir, ".cursor", "mcp.json")); !os.IsNotExist(err) {
			t.Fatalf("in-place cleanup should remove the task's .cursor/mcp.json, stat err=%v", err)
		}
	})

	nextRoot := filepath.Join(root, "task-2")
	nextData, err := prepareCursorMcpConfig(nextRoot, userDir, mcpConfig, "", store, &sidecarManifest{}, nil)
	if err != nil {
		t.Fatalf("prepare next in-place task: %v", err)
	}
	if got := CursorMcpServersNeedingLogin(nextData, userDir); got != nil {
		t.Fatalf("next task still needs login: %v", got)
	}
	if got := readFile(t, CursorMcpAuthPath(nextData, userDir)); got != login {
		t.Fatalf("next task auth = %s, want the workspace login", got)
	}
}

func TestCursorMcpLoginWorkspaceSurvivesWorktreeRemoval(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	mcpConfig := json.RawMessage(`{"mcpServers":{"oauth":{"url":"https://mcp.example.com/mcp"}}}`)

	repo := newTestRepo(t)
	prepareWorktree := func(name string) (string, string, string) {
		t.Helper()
		envRoot := filepath.Join(root, name)
		worktree := filepath.Join(envRoot, "workdir")
		gitRun(t, repo, "worktree", "add", "-b", "agent/"+name, worktree)
		dataDir, err := prepareCursorMcpConfig(envRoot, worktree, mcpConfig, "", store, &sidecarManifest{}, nil)
		if err != nil {
			t.Fatalf("prepare %s: %v", name, err)
		}
		return envRoot, worktree, dataDir
	}

	firstRoot, firstWork, firstData := prepareWorktree("task-1")
	if err := os.WriteFile(CursorMcpAuthPath(firstData, firstWork), []byte(`{"oauth":{"clientInfo":{"client_id":"c"}}}`), 0o600); err != nil {
		t.Fatalf("write challenge: %v", err)
	}

	_, login := loginThroughWorkspaceAfterTaskCleanup(t, store, "", mcpConfig, func() {
		if err := removeLocalWorktreeDir(repo, firstWork, nil); err != nil {
			t.Fatalf("removeLocalWorktreeDir: %v", err)
		}
		if err := os.RemoveAll(firstRoot); err != nil {
			t.Fatalf("remove task env: %v", err)
		}
	})

	_, nextWork, nextData := prepareWorktree("task-2")
	if got := CursorMcpServersNeedingLogin(nextData, nextWork); got != nil {
		t.Fatalf("next task still needs login: %v", got)
	}
	if got := readFile(t, CursorMcpAuthPath(nextData, nextWork)); got != login {
		t.Fatalf("next task auth = %s, want the workspace login", got)
	}
}

func TestCursorMcpLoginWorkspaceWritesToExplicitAuthSource(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	sourceDir := filepath.Join(root, "user-cursor-project")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	source := filepath.Join(sourceDir, cursorMcpAuthFile)
	if err := os.WriteFile(source, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	_, login := loginThroughWorkspaceAfterTaskCleanup(t, store, sourceDir, json.RawMessage(`{"mcpServers":{"oauth":{"url":"https://mcp.example.com/mcp"}}}`), func() {})
	if got := readFile(t, source); got != login {
		t.Fatalf("explicit source = %s, want the workspace login", got)
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Fatalf("agent store should stay untouched while an explicit source is set, stat err=%v", err)
	}
}

func TestCursorMcpAuthStorePathRequiresAgent(t *testing.T) {
	t.Parallel()

	if got := CursorMcpAuthStorePath("", ""); got != "" {
		t.Fatalf("CursorMcpAuthStorePath with no agent = %q, want empty", got)
	}
	if got := CursorMcpAuthStorePath("", "../.."); got != "" {
		t.Fatalf("CursorMcpAuthStorePath with traversal-only agent = %q, want empty", got)
	}
}

func TestPrepareCursorMcpConfigRejectsArbitraryAuthSourceFile(t *testing.T) {
	t.Parallel()

	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	source := filepath.Join(envRoot, "other.json")
	if err := os.WriteFile(source, []byte(`{"secret":true}`), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	_, err := prepareCursorMcpConfig(envRoot, workDir, json.RawMessage(`{"mcpServers":{}}`), source, "", &sidecarManifest{}, nil)
	if err == nil {
		t.Fatal("expected non-mcp-auth source file to fail")
	}
}

func TestPrepareCursorMcpConfigNilDoesNotTakeOwnership(t *testing.T) {
	t.Parallel()

	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, nil, "", "", &sidecarManifest{}, nil)
	if err != nil {
		t.Fatalf("prepareCursorMcpConfig: %v", err)
	}
	if cursorDataDir != "" {
		t.Fatalf("CursorDataDir = %q, want empty", cursorDataDir)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".cursor", "mcp.json")); !os.IsNotExist(err) {
		t.Fatalf(".cursor/mcp.json should not exist, stat err=%v", err)
	}
}

func TestPrepareCursorMcpConfigRejectsMalformedConfig(t *testing.T) {
	t.Parallel()

	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	_, err := prepareCursorMcpConfig(envRoot, workDir, json.RawMessage(`{"mcpServers":{"bad":42}}`), "", "", &sidecarManifest{}, nil)
	if err == nil {
		t.Fatal("expected malformed server config to fail")
	}
}

func filePerm(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}
