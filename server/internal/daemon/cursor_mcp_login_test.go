package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func TestCursorMcpLoginNoticePosixCommand(t *testing.T) {
	t.Parallel()

	got := cursorMcpLoginNotice{
		Servers:       []string{"mobbin", "plugin-mobbin-mobbin"},
		WorkDir:       "/Users/me/multica_workspaces/ws/mul-1-abc/workdir",
		CursorDataDir: "/Users/me/multica_workspaces/ws/mul-1-abc/cursor-data",
		Executable:    "/Users/me/.local/bin/cursor-agent",
		Host:          "Jiayuan's MacBook",
		GOOS:          "darwin",
	}.String()
	for _, want := range []string{
		"Cursor MCP login needed: mobbin, plugin-mobbin-mobbin.",
		"on the runtime machine (Jiayuan's MacBook)",
		"    cd /Users/me/multica_workspaces/ws/mul-1-abc/workdir && CURSOR_DATA_DIR=/Users/me/multica_workspaces/ws/mul-1-abc/cursor-data /Users/me/.local/bin/cursor-agent mcp login mobbin\n",
		"cursor-agent mcp login plugin-mobbin-mobbin\n",
		"its next runs use it",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("notice missing %q:\n%s", want, got)
		}
	}
}

func TestCursorMcpLoginNoticeQuotesAndDefaults(t *testing.T) {
	t.Parallel()

	posix := cursorMcpLoginNotice{
		Servers:       []string{"mobbin"},
		WorkDir:       "/tmp/it's here",
		CursorDataDir: "/tmp/data dir",
		GOOS:          "linux",
	}.String()
	if !strings.Contains(posix, `cd '/tmp/it'\''s here' && CURSOR_DATA_DIR='/tmp/data dir' cursor-agent mcp login mobbin`) {
		t.Fatalf("posix quoting or executable fallback wrong:\n%s", posix)
	}
	if !strings.Contains(posix, "on the runtime machine. Run") {
		t.Fatalf("notice without a host name should not print an empty name:\n%s", posix)
	}

	windows := cursorMcpLoginNotice{
		Servers:       []string{"mobbin"},
		WorkDir:       `C:\Users\o'neil\workdir`,
		CursorDataDir: `C:\Users\o'neil\cursor-data`,
		Executable:    `C:\Tools\cursor-agent.cmd`,
		GOOS:          "windows",
	}.String()
	if !strings.Contains(windows, `Set-Location -LiteralPath 'C:\Users\o''neil\workdir'; $env:CURSOR_DATA_DIR = 'C:\Users\o''neil\cursor-data'; & 'C:\Tools\cursor-agent.cmd' mcp login 'mobbin'`) {
		t.Fatalf("windows command wrong:\n%s", windows)
	}

	if (cursorMcpLoginNotice{}).String() != "" {
		t.Fatal("no servers should produce no notice")
	}
}

func TestReportCursorMcpLoginNoticePostsOnlyWhenLoginMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	taskRoot := filepath.Join(root, "task")
	workDir := filepath.Join(taskRoot, "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	dataDir := filepath.Join(taskRoot, "cursor-data")
	authPath := execenv.CursorMcpAuthPath(dataDir, workDir)
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	store := filepath.Join(root, "profile", "cursor-mcp-auth", "agent-1", "mcp-auth.json")
	check := cursorMcpLoginCheck{
		TaskWorkDir:       workDir,
		TaskCursorDataDir: dataDir,
		AuthStore:         store,
		McpConfig:         json.RawMessage(`{"mcpServers":{"mobbin":{"url":"https://api.mobbin.com/mcp"}}}`),
		GOOS:              "darwin",
	}

	d, rec := newTranscriptRecorder(t)
	var seq atomic.Int32
	if d.reportCursorMcpLoginNotice(context.Background(), "task-1", check, &seq, quietTaskLog()) {
		t.Fatal("no auth file should post nothing")
	}

	if err := os.WriteFile(authPath, []byte(`{"mobbin":{"clientInfo":{"client_id":"c"}}}`), 0o600); err != nil {
		t.Fatalf("write challenge: %v", err)
	}
	if !d.reportCursorMcpLoginNotice(context.Background(), "task-1", check, &seq, quietTaskLog()) {
		t.Fatal("a missing login should post a notice")
	}
	rows := rec.snapshot()
	loginDir := filepath.Join(filepath.Dir(store), "login")
	if len(rows) != 1 || rows[0].Type != "text" || rows[0].Seq != 1 ||
		!strings.Contains(rows[0].Content, "mcp login mobbin") || !strings.Contains(rows[0].Content, loginDir) ||
		strings.Contains(rows[0].Content, taskRoot) {
		t.Fatalf("notice should point at the agent's login workspace, not the task: %+v", rows)
	}

	// The command must still work once the task directory is gone.
	if err := os.RemoveAll(taskRoot); err != nil {
		t.Fatalf("remove task: %v", err)
	}
	loginWork := filepath.Join(loginDir, "workdir")
	if _, err := os.Stat(filepath.Join(loginWork, ".cursor", "mcp.json")); err != nil {
		t.Fatalf("login workspace config missing after task cleanup: %v", err)
	}
	loginAuth := execenv.CursorMcpAuthPath(filepath.Join(loginDir, "cursor-data"), loginWork)
	if err := os.WriteFile(loginAuth, []byte(`{"mobbin":{"tokens":{"access_token":"a","refresh_token":"r"}}}`), 0o600); err != nil {
		t.Fatalf("log in through workspace: %v", err)
	}
	if got := readTestFile(t, store); !strings.Contains(got, `"access_token":"a"`) {
		t.Fatalf("login did not land in the agent store: %s", got)
	}
}

func TestCursorMcpLoginNoticeOnUserDataDirPointsAtUserLoginWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userData := filepath.Join(root, "home", ".cursor")
	workDir := filepath.Join(root, "task", "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	authPath := execenv.CursorMcpAuthPath(userData, workDir)
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(authPath, []byte(`{"mobbin":{"clientInfo":{"client_id":"c"}}}`), 0o600); err != nil {
		t.Fatalf("write challenge: %v", err)
	}
	store := filepath.Join(root, "profile", "cursor-mcp-auth", "agent-1", "mcp-auth.json")

	notice := cursorMcpLoginNoticeFor(cursorMcpLoginCheck{
		TaskWorkDir:       workDir,
		TaskCursorDataDir: userData,
		AuthStore:         store,
		UserDataDir:       true,
		GOOS:              "darwin",
	}, quietTaskLog())
	loginWork := filepath.Join(filepath.Dir(store), "user-login", "workdir")
	if notice.WorkDir != loginWork || notice.CursorDataDir != userData || len(notice.Servers) != 1 {
		t.Fatalf("notice = %+v, want the user login workspace on the user data dir", notice)
	}

	loginAuth := execenv.CursorMcpAuthPath(userData, loginWork)
	if err := os.WriteFile(loginAuth, []byte(`{"mobbin":{"tokens":{"access_token":"a","refresh_token":"r"}}}`), 0o600); err != nil {
		t.Fatalf("log in through workspace: %v", err)
	}
	if got := readTestFile(t, store); !strings.Contains(got, `"access_token":"a"`) {
		t.Fatalf("login did not land in the agent store: %s", got)
	}
}

func TestCursorMcpLoginNoticeFallsBackToTaskWithoutStore(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	workDir := t.TempDir()
	authPath := execenv.CursorMcpAuthPath(dataDir, workDir)
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(authPath, []byte(`{"mobbin":{"clientInfo":{"client_id":"c"}}}`), 0o600); err != nil {
		t.Fatalf("write challenge: %v", err)
	}
	notice := cursorMcpLoginNoticeFor(cursorMcpLoginCheck{TaskWorkDir: workDir, TaskCursorDataDir: dataDir}, quietTaskLog())
	if notice.WorkDir != workDir || notice.CursorDataDir != dataDir || len(notice.Servers) != 1 {
		t.Fatalf("notice without a store = %+v, want the task directories", notice)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
