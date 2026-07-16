package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

	cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, mcpConfig, "", manifest)
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
	cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, json.RawMessage(`{"mcpServers":{}}`), "", &sidecarManifest{})
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

	cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx","args":["mcp-server-fetch"]}}}`), sourceProjectDir, &sidecarManifest{})
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
	info, err := os.Lstat(targetAuth)
	if err != nil {
		t.Fatalf("lstat seeded auth: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		t.Fatalf("seeded auth mode = %v, want task-private regular file", info.Mode())
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("seeded auth mode = %#o, want 0600", info.Mode().Perm())
	}
	if err := os.WriteFile(sourceAuth, []byte(`{"tokens":{"fetch":"rotated"}}`), 0o600); err != nil {
		t.Fatalf("rotate source auth: %v", err)
	}
	data, err = os.ReadFile(targetAuth)
	if err != nil {
		t.Fatalf("read seeded auth after source mutation: %v", err)
	}
	if string(data) != `{"tokens":{"fetch":"secret"}}` {
		t.Fatalf("seeded auth changed with source: %s", data)
	}
}

func TestPrepareCursorMcpConfigRejectsSymlinkAuthSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires elevated privileges on some Windows hosts")
	}
	envRoot := t.TempDir()
	workDir := filepath.Join(envRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir workDir: %v", err)
	}
	realAuth := filepath.Join(envRoot, "owner-auth.json")
	if err := os.WriteFile(realAuth, []byte(`{"token":"owner"}`), 0o600); err != nil {
		t.Fatalf("write real auth: %v", err)
	}
	sourceDir := filepath.Join(envRoot, "source-project")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatalf("mkdir source project: %v", err)
	}
	if err := os.Symlink(realAuth, filepath.Join(sourceDir, cursorMcpAuthFile)); err != nil {
		t.Fatalf("symlink auth: %v", err)
	}

	_, err := prepareCursorMcpConfig(envRoot, workDir, json.RawMessage(`{"mcpServers":{}}`), sourceDir, &sidecarManifest{})
	if err == nil {
		t.Fatal("expected symlink mcp-auth source to fail closed")
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

	cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, mcpConfig, sourceProjectDir, nil)
	if err != nil {
		t.Fatalf("prepareCursorMcpConfig with auth source: %v", err)
	}
	projectRoot := cursorProjectRoot(workDir)
	targetAuth := filepath.Join(cursorDataDir, "projects", cursorSlugifyPath(projectRoot), cursorMcpAuthFile)
	if _, err := os.Stat(targetAuth); err != nil {
		t.Fatalf("seeded auth missing: %v", err)
	}

	cursorDataDir, err = prepareCursorMcpConfig(envRoot, workDir, mcpConfig, "", nil)
	if err != nil {
		t.Fatalf("prepareCursorMcpConfig without auth source: %v", err)
	}
	targetAuth = filepath.Join(cursorDataDir, "projects", cursorSlugifyPath(projectRoot), cursorMcpAuthFile)
	if _, err := os.Stat(targetAuth); !os.IsNotExist(err) {
		t.Fatalf("auth file should be removed after opt-out, stat err=%v", err)
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

	_, err := prepareCursorMcpConfig(envRoot, workDir, json.RawMessage(`{"mcpServers":{}}`), source, &sidecarManifest{})
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
	cursorDataDir, err := prepareCursorMcpConfig(envRoot, workDir, nil, "", &sidecarManifest{})
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
	_, err := prepareCursorMcpConfig(envRoot, workDir, json.RawMessage(`{"mcpServers":{"bad":42}}`), "", &sidecarManifest{})
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
