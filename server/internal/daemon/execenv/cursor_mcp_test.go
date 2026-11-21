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
	firstData, firstMirror, err := prepareCursorMcpConfigMirror(firstRoot, firstWork, mcpConfig, "", store, nil, nil)
	if err != nil {
		t.Fatalf("prepare first task: %v", err)
	}
	if mode := filePerm(t, store); mode != 0o600 {
		t.Fatalf("store mode = %#o, want 0600", mode)
	}
	if info, err := os.Stat(filepath.Dir(store)); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("store dir stat = %v, %v; want 0700", info, err)
	}
	if info, err := os.Lstat(CursorMcpAuthPath(firstData, firstWork)); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("task auth = %v, %v; want a regular file Cursor can replace", info, err)
	}

	// Cursor saves a login by renaming a new file over the task's copy.
	login := `{"fetch":{"tokens":{"access_token":"a","refresh_token":"r"}}}`
	cursorSave(t, CursorMcpAuthPath(firstData, firstWork), login)
	if got := readFile(t, store); got != "{}\n" {
		t.Fatalf("store before sync = %s, want untouched", got)
	}
	if err := firstMirror.SyncIfChanged(); err != nil {
		t.Fatalf("sync first task: %v", err)
	}
	assertCursorAuthJSON(t, store, login)

	// Re-preparing the same env (reuse) must not wipe the login.
	if _, err := prepareCursorMcpConfig(firstRoot, firstWork, mcpConfig, "", store, nil, nil); err != nil {
		t.Fatalf("re-prepare first task: %v", err)
	}
	assertCursorAuthJSON(t, CursorMcpAuthPath(firstData, firstWork), login)

	// A new task of the same agent sees the same login.
	secondRoot := filepath.Join(root, "task-2")
	secondWork := filepath.Join(secondRoot, "workdir")
	if err := os.MkdirAll(secondWork, 0o755); err != nil {
		t.Fatalf("mkdir second workdir: %v", err)
	}
	secondData, secondMirror, err := prepareCursorMcpConfigMirror(secondRoot, secondWork, mcpConfig, "", store, nil, nil)
	if err != nil {
		t.Fatalf("prepare second task: %v", err)
	}
	assertCursorAuthJSON(t, CursorMcpAuthPath(secondData, secondWork), login)

	// A token refresh in one task is what the other task reads next.
	refreshed := `{"fetch":{"tokens":{"access_token":"b","refresh_token":"r2"}}}`
	cursorSave(t, CursorMcpAuthPath(secondData, secondWork), refreshed)
	if err := secondMirror.Sync(); err != nil {
		t.Fatalf("sync second task: %v", err)
	}
	if _, err := prepareCursorMcpConfig(firstRoot, firstWork, mcpConfig, "", store, nil, nil); err != nil {
		t.Fatalf("re-prepare first task after refresh: %v", err)
	}
	assertCursorAuthJSON(t, CursorMcpAuthPath(firstData, firstWork), refreshed)

	// Removing a task env leaves the store intact.
	if err := os.RemoveAll(firstRoot); err != nil {
		t.Fatalf("remove first task: %v", err)
	}
	assertCursorAuthJSON(t, store, refreshed)
}

// cursorSave writes path the way cursor-agent 2026.11.19 does: a new file
// renamed over the old one, which replaces a link instead of writing through.
func cursorSave(t *testing.T, path, content string) {
	t.Helper()
	tmp := path + ".cursor-tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("rename over %s: %v", path, err)
	}
}

func assertCursorAuthJSON(t *testing.T, path, want string) {
	t.Helper()
	got := readFile(t, path)
	if !cursorJSONEqual(json.RawMessage(got), json.RawMessage(want)) {
		t.Fatalf("%s = %s, want %s", path, got, want)
	}
}

func prepareCursorTask(t *testing.T, root, name, store string, mcpConfig json.RawMessage) (string, string, *CursorMcpAuthMirror) {
	t.Helper()
	envRoot := filepath.Join(root, name)
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
	env := &Environment{}
	if err := prepareCursorMcp(env, envRoot, workDir, mcpConfig, "", store, "", nil, nil); err != nil {
		t.Fatalf("prepare %s: %v", name, err)
	}
	return env.CursorDataDir, workDir, env.CursorMcpAuthMirror
}

func TestCursorMcpAuthMirrorMergesOnlyWhatEachTaskChanged(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	writeTestFile(t, store, `{"mobbin":{"tokens":{"access_token":"m1","refresh_token":"mr1"}},"linear":{"tokens":{"access_token":"l1"}}}`)
	mcpConfig := json.RawMessage(`{"mcpServers":{}}`)

	aData, aWork, aMirror := prepareCursorTask(t, root, "task-a", store, mcpConfig)
	bData, bWork, bMirror := prepareCursorTask(t, root, "task-b", store, mcpConfig)

	// Task A refreshes Mobbin; task B, running at the same time, re-logs into
	// Linear and still holds the Mobbin tokens A has since spent.
	cursorSave(t, CursorMcpAuthPath(aData, aWork), `{"mobbin":{"tokens":{"access_token":"m2","refresh_token":"mr2"}},"linear":{"tokens":{"access_token":"l1"}}}`)
	cursorSave(t, CursorMcpAuthPath(bData, bWork), `{"mobbin":{"tokens":{"access_token":"m1","refresh_token":"mr1"}},"linear":{"tokens":{"access_token":"l2"}}}`)
	if err := aMirror.Sync(); err != nil {
		t.Fatalf("sync a: %v", err)
	}
	if err := bMirror.Sync(); err != nil {
		t.Fatalf("sync b: %v", err)
	}
	assertCursorAuthJSON(t, store, `{"mobbin":{"tokens":{"access_token":"m2","refresh_token":"mr2"}},"linear":{"tokens":{"access_token":"l2"}}}`)

	// Nothing changed since the last sync: nothing is written.
	before := readFile(t, store)
	if err := bMirror.Sync(); err != nil {
		t.Fatalf("resync b: %v", err)
	}
	if got := readFile(t, store); got != before {
		t.Fatalf("store after no-op sync = %s, want %s", got, before)
	}
}

func TestCursorMcpAuthMirrorRetriesFailedPoll(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	writeTestFile(t, store, `{}`)
	data, work, mirror := prepareCursorTask(t, root, "task-1", store, json.RawMessage(`{"mcpServers":{}}`))
	login := `{"mobbin":{"tokens":{"access_token":"a","refresh_token":"r"}}}`
	cursorSave(t, CursorMcpAuthPath(data, work), login)

	// The store cannot be read for one poll.
	aside := store + ".aside"
	if err := os.Rename(store, aside); err != nil {
		t.Fatalf("move store aside: %v", err)
	}
	if err := os.Mkdir(store, 0o700); err != nil {
		t.Fatalf("block store: %v", err)
	}
	if err := mirror.SyncIfChanged(); err == nil {
		t.Fatal("poll with an unreadable store succeeded")
	}
	if err := os.Remove(store); err != nil {
		t.Fatalf("unblock store: %v", err)
	}
	if err := os.Rename(aside, store); err != nil {
		t.Fatalf("restore store: %v", err)
	}

	if err := mirror.SyncIfChanged(); err != nil {
		t.Fatalf("next poll: %v", err)
	}
	assertCursorAuthJSON(t, store, login)
}

func TestCursorMcpAuthMirrorNeverSignsTheAgentOut(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	login := `{"mobbin":{"clientInfo":{"client_id":"c"},"tokens":{"access_token":"a","refresh_token":"r"}}}`
	writeTestFile(t, store, login)

	data, work, mirror := prepareCursorTask(t, root, "task-1", store, json.RawMessage(`{"mcpServers":{}}`))
	// The task's refresh token was spent elsewhere, so Cursor drops back to an
	// OAuth challenge for Mobbin.
	cursorSave(t, CursorMcpAuthPath(data, work), `{"mobbin":{"clientInfo":{"client_id":"c"}}}`)
	if err := mirror.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	assertCursorAuthJSON(t, store, login)
	if got := CursorMcpServersNeedingLogin(data, work); !reflect.DeepEqual(got, []string{"mobbin"}) {
		t.Fatalf("task needing login = %v, want [mobbin] so the run record still reports it", got)
	}
}

func TestCursorMcpAuthMirrorMergesLeftoverCopyOnReuse(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	mcpConfig := json.RawMessage(`{"mcpServers":{}}`)

	data, work, _ := prepareCursorTask(t, root, "task-1", store, mcpConfig)
	// The run ended without a final sync (the daemon stopped mid-run).
	login := `{"mobbin":{"tokens":{"access_token":"a","refresh_token":"r"}}}`
	cursorSave(t, CursorMcpAuthPath(data, work), login)

	prepareCursorTask(t, root, "task-1", store, mcpConfig)
	assertCursorAuthJSON(t, store, login)
	assertCursorAuthJSON(t, CursorMcpAuthPath(data, work), login)
}

func TestCursorMcpAuthMirrorReplacesLinkFromOlderDaemon(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	login := `{"mobbin":{"tokens":{"access_token":"a","refresh_token":"r"}}}`
	writeTestFile(t, store, login)
	mcpConfig := json.RawMessage(`{"mcpServers":{}}`)

	data, work, _ := prepareCursorTask(t, root, "task-1", store, mcpConfig)
	taskAuth := CursorMcpAuthPath(data, work)
	if err := os.Remove(taskAuth); err != nil {
		t.Fatalf("remove task auth: %v", err)
	}
	if err := os.Symlink(store, taskAuth); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	prepareCursorTask(t, root, "task-1", store, mcpConfig)
	if info, err := os.Lstat(taskAuth); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("task auth = %v, %v; want the link replaced by a copy", info, err)
	}
	assertCursorAuthJSON(t, taskAuth, login)
	assertCursorAuthJSON(t, store, login)
}

func TestCursorMcpAuthMirrorSkipsUnreadableCopy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
	data, work, mirror := prepareCursorTask(t, root, "task-1", store, json.RawMessage(`{"mcpServers":{}}`))
	cursorSave(t, CursorMcpAuthPath(data, work), `{"mobbin":`)
	if err := mirror.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := readFile(t, store); got != "{}\n" {
		t.Fatalf("store = %s, want untouched", got)
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

	firstData, firstWork, firstMirror := prepareCursorTask(t, root, "task-1", store, mcpConfig)
	if got := CursorMcpServersNeedingLogin(firstData, firstWork); got != nil {
		t.Fatalf("fresh store = %v, want nothing to report", got)
	}
	// Cursor meets Mobbin's OAuth challenge during the first run.
	cursorSave(t, CursorMcpAuthPath(firstData, firstWork), testCursorChallenge)
	if err := firstMirror.Sync(); err != nil {
		t.Fatalf("sync challenge: %v", err)
	}
	if got := CursorMcpServersNeedingLogin(firstData, firstWork); !reflect.DeepEqual(got, []string{"mobbin"}) {
		t.Fatalf("after challenge = %v, want [mobbin]", got)
	}

	secondData, secondWork, _ := prepareCursorTask(t, root, "task-2", store, mcpConfig)
	if got := CursorMcpServersNeedingLogin(secondData, secondWork); !reflect.DeepEqual(got, []string{"mobbin"}) {
		t.Fatalf("next task before login = %v, want [mobbin]", got)
	}

	// `cursor-agent mcp login mobbin`, run with the first task's workdir and
	// CURSOR_DATA_DIR while that run is still going.
	cursorSave(t, CursorMcpAuthPath(firstData, firstWork), testCursorLogin)
	if err := firstMirror.SyncIfChanged(); err != nil {
		t.Fatalf("sync login: %v", err)
	}
	thirdData, thirdWork, _ := prepareCursorTask(t, root, "task-3", store, mcpConfig)
	if got := CursorMcpServersNeedingLogin(thirdData, thirdWork); got != nil {
		t.Fatalf("next task after login = %v, want nothing to report", got)
	}
	secondData, secondWork, _ = prepareCursorTask(t, root, "task-2", store, mcpConfig)
	if got := CursorMcpServersNeedingLogin(secondData, secondWork); got != nil {
		t.Fatalf("task prepared before login, on its next run = %v, want it to see the login", got)
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
	cursorSave(t, CursorMcpAuthPath(loginData, loginWork), login)
	return loginWork, login
}

// A login workspace an older daemon set up holds a link to the store and no
// base. A login made there after the upgrade replaces the link, and must reach
// the store whether the agent's next task collects it or the workspace is
// seeded again first, although the store still holds the spent login.
func TestCursorMcpLoginWorkspaceFromOlderDaemonKeepsNewLogin(t *testing.T) {
	t.Parallel()

	mcpConfig := json.RawMessage(`{"mcpServers":{"oauth":{"url":"https://mcp.example.com/mcp"}}}`)
	spent := `{"oauth":{"clientInfo":{"client_id":"c"},"tokens":{"access_token":"old","refresh_token":"spent"}}}`
	fresh := `{"oauth":{"clientInfo":{"client_id":"c"},"tokens":{"access_token":"new","refresh_token":"r"}}}`
	legacyLogin := func(t *testing.T) (string, string) {
		t.Helper()
		root := t.TempDir()
		store := filepath.Join(root, "profile", cursorMcpAuthStoreRoot, "agent-1", cursorMcpAuthFile)
		writeTestFile(t, store, spent)
		loginWork, loginData, err := PrepareCursorMcpLoginWorkspace("", store, mcpConfig, []string{"oauth"})
		if err != nil {
			t.Fatalf("PrepareCursorMcpLoginWorkspace: %v", err)
		}
		makeCursorLoginWorkspaceLegacy(t, store, cursorMcpLoginWorkspaceDir)
		cursorSave(t, CursorMcpAuthPath(loginData, loginWork), fresh)
		return root, store
	}

	t.Run("next task collects it", func(t *testing.T) {
		t.Parallel()
		root, store := legacyLogin(t)
		dataDir, workDir, _ := prepareCursorTask(t, root, "task-1", store, mcpConfig)
		assertCursorAuthJSON(t, store, fresh)
		assertCursorAuthJSON(t, CursorMcpAuthPath(dataDir, workDir), fresh)
	})

	t.Run("workspace seeded again first", func(t *testing.T) {
		t.Parallel()
		_, store := legacyLogin(t)
		loginWork, loginData, err := PrepareCursorMcpLoginWorkspace("", store, mcpConfig, []string{"oauth"})
		if err != nil {
			t.Fatalf("rebuild login workspace: %v", err)
		}
		assertCursorAuthJSON(t, store, fresh)
		assertCursorAuthJSON(t, CursorMcpAuthPath(loginData, loginWork), fresh)
	})

	t.Run("file older than the login is left out", func(t *testing.T) {
		t.Parallel()
		_, store := legacyLogin(t)
		writeTestFile(t, store, spent)
		SyncCursorMcpLoginWorkspaces("", store, "", nil)
		assertCursorAuthJSON(t, store, spent)
	})
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
	next := &Environment{}
	if err := prepareCursorMcp(next, nextRoot, userDir, mcpConfig, "", store, "", &sidecarManifest{}, nil); err != nil {
		t.Fatalf("prepare next in-place task: %v", err)
	}
	if got := CursorMcpServersNeedingLogin(next.CursorDataDir, userDir); got != nil {
		t.Fatalf("next task still needs login: %v", got)
	}
	assertCursorAuthJSON(t, CursorMcpAuthPath(next.CursorDataDir, userDir), login)
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
		env := &Environment{}
		if err := prepareCursorMcp(env, envRoot, worktree, mcpConfig, "", store, "", &sidecarManifest{}, nil); err != nil {
			t.Fatalf("prepare %s: %v", name, err)
		}
		return envRoot, worktree, env.CursorDataDir
	}

	firstRoot, firstWork, firstData := prepareWorktree("task-1")
	cursorSave(t, CursorMcpAuthPath(firstData, firstWork), `{"oauth":{"clientInfo":{"client_id":"c"}}}`)

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
	assertCursorAuthJSON(t, CursorMcpAuthPath(nextData, nextWork), login)
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
	SyncCursorMcpLoginWorkspaces(sourceDir, store, "", nil)
	assertCursorAuthJSON(t, source, login)
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
