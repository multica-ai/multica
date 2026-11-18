package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// cursorMcpLoginNotice is the run-record message for MCP servers that need an
// OAuth login before this agent can use them. The login has to happen on the
// runtime machine, in a Cursor project whose mcp-auth.json is the login the
// agent's tasks share.
type cursorMcpLoginNotice struct {
	Servers []string
	// WorkDir and CursorDataDir are where the login command runs: the agent's
	// login workspace, or the task's own directories when that cannot be built.
	WorkDir       string
	CursorDataDir string
	// Executable is the cursor-agent the daemon launches. Empty falls back to
	// `cursor-agent` on PATH (custom runtime commands may not speak `mcp login`).
	Executable string
	Host       string
	GOOS       string
}

// cursorMcpLoginCheck is what a task needs to detect a missing login and point
// the user at a login workspace that outlives the task.
type cursorMcpLoginCheck struct {
	TaskWorkDir       string
	TaskCursorDataDir string
	AuthSource        string
	AuthStore         string
	McpConfig         json.RawMessage
	Executable        string
	Host              string
	GOOS              string
	// UserDataDir marks TaskCursorDataDir as the user's own Cursor data dir
	// (no managed mcp_config) rather than a per-task one.
	UserDataDir bool
}

func (n cursorMcpLoginNotice) String() string {
	if len(n.Servers) == 0 {
		return ""
	}
	exe := n.Executable
	if exe == "" {
		exe = "cursor-agent"
	}
	machine := "the runtime machine"
	if n.Host != "" {
		machine = fmt.Sprintf("the runtime machine (%s)", n.Host)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Cursor MCP login needed: %s. These tools are unavailable to this agent until someone logs in on %s. Run this there, once per server:\n\n",
		strings.Join(n.Servers, ", "), machine)
	for _, server := range n.Servers {
		b.WriteString("    ")
		b.WriteString(n.loginCommand(exe, server))
		b.WriteString("\n")
	}
	b.WriteString("\nThe login is saved for this agent on that machine and its next runs use it; the current run does not pick it up.")
	return b.String()
}

func (n cursorMcpLoginNotice) loginCommand(exe, server string) string {
	if n.GOOS == "windows" {
		return fmt.Sprintf("Set-Location -LiteralPath %s; $env:CURSOR_DATA_DIR = %s; & %s mcp login %s",
			powerShellLiteral(n.WorkDir), powerShellLiteral(n.CursorDataDir), powerShellLiteral(exe), powerShellLiteral(server))
	}
	return fmt.Sprintf("cd %s && CURSOR_DATA_DIR=%s %s mcp login %s",
		posixShellLiteral(n.WorkDir), posixShellLiteral(n.CursorDataDir), posixShellLiteral(exe), posixShellLiteral(server))
}

func posixShellLiteral(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_./:@+=,", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func powerShellLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// cursorMcpLoginNoticeFor returns the notice for the servers that still need a
// login, or a zero notice when none do.
func cursorMcpLoginNoticeFor(check cursorMcpLoginCheck, taskLog *slog.Logger) cursorMcpLoginNotice {
	servers := execenv.CursorMcpServersNeedingLogin(check.TaskCursorDataDir, check.TaskWorkDir)
	if len(servers) == 0 {
		return cursorMcpLoginNotice{}
	}
	notice := cursorMcpLoginNotice{
		Servers:       servers,
		WorkDir:       check.TaskWorkDir,
		CursorDataDir: check.TaskCursorDataDir,
		Executable:    check.Executable,
		Host:          check.Host,
		GOOS:          check.GOOS,
	}
	if check.AuthStore == "" {
		return notice
	}
	if check.UserDataDir {
		workDir, err := execenv.PrepareCursorMcpUserLoginWorkspace(check.TaskCursorDataDir, check.AuthSource, check.AuthStore)
		if err != nil {
			taskLog.Warn("cursor mcp login workspace unavailable; pointing the login at the task directory", "error", err)
			return notice
		}
		notice.WorkDir = workDir
		return notice
	}
	workDir, dataDir, err := execenv.PrepareCursorMcpLoginWorkspace(check.AuthSource, check.AuthStore, check.McpConfig, servers)
	if err != nil {
		taskLog.Warn("cursor mcp login workspace unavailable; pointing the login at the task directory", "error", err)
		return notice
	}
	notice.WorkDir = workDir
	notice.CursorDataDir = dataDir
	return notice
}

// reportCursorMcpLoginNotice posts the login notice for the servers that still
// need one, and reports whether it posted.
func (d *Daemon) reportCursorMcpLoginNotice(ctx context.Context, taskID string, check cursorMcpLoginCheck, msgSeq *atomic.Int32, taskLog *slog.Logger) bool {
	notice := cursorMcpLoginNoticeFor(check, taskLog)
	text := notice.String()
	if text == "" {
		return false
	}
	taskLog.Info("cursor mcp servers need login", "servers", notice.Servers)
	d.reportTaskNotice(ctx, taskID, text, msgSeq, taskLog)
	return true
}
