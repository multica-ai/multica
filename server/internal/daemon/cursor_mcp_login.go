package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// cursorMcpLoginNotice is the run-record message for MCP servers that need an
// OAuth login before this agent can use them. The login has to happen on the
// runtime machine, from the task's workdir and CURSOR_DATA_DIR, because that is
// the mcp-auth.json the agent's later tasks share.
type cursorMcpLoginNotice struct {
	Servers       []string
	WorkDir       string
	CursorDataDir string
	// Executable is the cursor-agent the daemon launches. Empty falls back to
	// `cursor-agent` on PATH (custom runtime commands may not speak `mcp login`).
	Executable string
	Host       string
	GOOS       string
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

// reportCursorMcpLoginNotice posts the login notice for the servers that still
// need one, and reports whether it posted.
func (d *Daemon) reportCursorMcpLoginNotice(ctx context.Context, taskID string, notice cursorMcpLoginNotice, msgSeq *atomic.Int32, taskLog *slog.Logger) bool {
	notice.Servers = execenv.CursorMcpServersNeedingLogin(notice.CursorDataDir, notice.WorkDir)
	text := notice.String()
	if text == "" {
		return false
	}
	taskLog.Info("cursor mcp servers need login", "servers", notice.Servers)
	d.reportTaskNotice(ctx, taskID, text, msgSeq, taskLog)
	return true
}
