package daemon

import (
	"context"
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

	dataDir := t.TempDir()
	workDir := t.TempDir()
	authPath := execenv.CursorMcpAuthPath(dataDir, workDir)
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	notice := cursorMcpLoginNotice{WorkDir: workDir, CursorDataDir: dataDir, GOOS: "darwin"}

	d, rec := newTranscriptRecorder(t)
	var seq atomic.Int32
	if d.reportCursorMcpLoginNotice(context.Background(), "task-1", notice, &seq, quietTaskLog()) {
		t.Fatal("no auth file should post nothing")
	}

	if err := os.WriteFile(authPath, []byte(`{"mobbin":{"clientInfo":{"client_id":"c"}}}`), 0o600); err != nil {
		t.Fatalf("write challenge: %v", err)
	}
	if !d.reportCursorMcpLoginNotice(context.Background(), "task-1", notice, &seq, quietTaskLog()) {
		t.Fatal("a missing login should post a notice")
	}
	rows := rec.snapshot()
	if len(rows) != 1 || rows[0].Type != "text" || rows[0].Seq != 1 ||
		!strings.Contains(rows[0].Content, "mcp login mobbin") || !strings.Contains(rows[0].Content, dataDir) {
		t.Fatalf("persisted notice = %+v", rows)
	}

	if err := os.WriteFile(authPath, []byte(`{"mobbin":{"tokens":{"access_token":"a","refresh_token":"r"}}}`), 0o600); err != nil {
		t.Fatalf("write login: %v", err)
	}
	if d.reportCursorMcpLoginNotice(context.Background(), "task-2", notice, &seq, quietTaskLog()) {
		t.Fatal("a logged-in server should post nothing")
	}
	if rows := rec.snapshot(); len(rows) != 1 {
		t.Fatalf("logged-in check posted rows: %+v", rows)
	}
}
