package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

const codexAppVisibleEntryTimeout = 5 * time.Minute

var codexAppSlugUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

type codexAppVisiblePortal struct {
	Dir         string
	DisplayPath string
	LinksPath   string
	ReadmePath  string
	ResultPath  string
}

func (d *Daemon) maybeCreateCodexAppVisibleTaskEntry(ctx context.Context, task Task, result TaskResult, provider string, taskLog *slog.Logger) {
	if !d.cfg.CodexAppVisibleTasks || provider != "codex" || task.IssueID == "" {
		return
	}

	entry, ok := d.cfg.Agents["codex"]
	if !ok {
		taskLog.Warn("codex app visible entry skipped: codex runtime is not configured")
		return
	}

	portal, err := writeCodexAppVisiblePortal(d.cfg.CodexAppPortalRoot, d.cfg.CodexAppVisibleCwd, task, result)
	if err != nil {
		taskLog.Warn("codex app visible portal failed", "error", err)
		return
	}

	if err := os.MkdirAll(d.cfg.CodexAppVisibleCwd, 0o755); err != nil {
		taskLog.Warn("codex app visible cwd failed", "cwd", d.cfg.CodexAppVisibleCwd, "error", err)
		return
	}

	visibleCtx, cancel := context.WithTimeout(ctx, codexAppVisibleEntryTimeout)
	defer cancel()

	backend, err := agent.New("codex", agent.Config{
		ExecutablePath: entry.Path,
		Logger:         d.logger,
	})
	if err != nil {
		taskLog.Warn("codex app visible backend failed", "error", err)
		return
	}

	session, err := backend.Execute(visibleCtx, codexAppVisiblePrompt(task, result, portal), agent.ExecOptions{
		Cwd:                       d.cfg.CodexAppVisibleCwd,
		Model:                     entry.Model,
		Timeout:                   codexAppVisibleEntryTimeout,
		SemanticInactivityTimeout: 2 * time.Minute,
		ExtraArgs:                 d.cfg.CodexArgs,
	})
	if err != nil {
		taskLog.Warn("codex app visible entry start failed", "error", err)
		return
	}

	for range session.Messages {
	}
	final, ok := <-session.Result
	if !ok {
		taskLog.Warn("codex app visible entry ended without result")
		return
	}
	if final.Status != "completed" {
		msg := final.Error
		if msg == "" {
			msg = final.Status
		}
		taskLog.Warn("codex app visible entry did not complete", "status", final.Status, "error", msg)
		return
	}
	taskLog.Info("codex app visible entry created", "portal", portal.Dir, "visible_cwd", d.cfg.CodexAppVisibleCwd, "thread_id", final.SessionID)
}

func writeCodexAppVisiblePortal(root, visibleCwd string, task Task, result TaskResult) (codexAppVisiblePortal, error) {
	if root == "" {
		return codexAppVisiblePortal{}, fmt.Errorf("portal root is empty")
	}
	slug := fmt.Sprintf("issue-%s-task-%s", safeSlug(shortID(task.IssueID)), safeSlug(shortID(task.ID)))
	dir := filepath.Join(root, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return codexAppVisiblePortal{}, fmt.Errorf("create portal dir: %w", err)
	}
	displayPath := dir
	if rel, err := filepath.Rel(visibleCwd, dir); err == nil && rel != "." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, "..") {
		displayPath = rel
	}

	agentName := ""
	if task.Agent != nil {
		agentName = task.Agent.Name
	}
	links := map[string]string{
		"issue_id":         task.IssueID,
		"task_id":          task.ID,
		"workspace_id":     task.WorkspaceID,
		"agent_id":         task.AgentID,
		"agent_name":       agentName,
		"status":           result.Status,
		"codex_session_id": result.SessionID,
		"real_workdir":     result.WorkDir,
		"portal_dir":       dir,
		"visible_cwd":      visibleCwd,
	}
	linksJSON, err := json.MarshalIndent(links, "", "  ")
	if err != nil {
		return codexAppVisiblePortal{}, fmt.Errorf("marshal links: %w", err)
	}

	readmePath := filepath.Join(dir, "README.md")
	resultPath := filepath.Join(dir, "result.md")
	linksPath := filepath.Join(dir, "links.json")

	if err := os.WriteFile(linksPath, append(linksJSON, '\n'), 0o644); err != nil {
		return codexAppVisiblePortal{}, fmt.Errorf("write links: %w", err)
	}
	if err := os.WriteFile(readmePath, []byte(codexAppVisibleReadme(task, result, dir, visibleCwd)), 0o644); err != nil {
		return codexAppVisiblePortal{}, fmt.Errorf("write readme: %w", err)
	}
	if err := os.WriteFile(resultPath, []byte(codexAppVisibleResult(result)), 0o644); err != nil {
		return codexAppVisiblePortal{}, fmt.Errorf("write result: %w", err)
	}
	if result.WorkDir != "" {
		linkPath := filepath.Join(dir, "real-workdir")
		_ = os.Remove(linkPath)
		if err := os.Symlink(result.WorkDir, linkPath); err != nil {
			_ = os.WriteFile(filepath.Join(dir, "real-workdir.txt"), []byte(result.WorkDir+"\n"), 0o644)
		}
	}

	return codexAppVisiblePortal{
		Dir:         dir,
		DisplayPath: displayPath,
		LinksPath:   linksPath,
		ReadmePath:  readmePath,
		ResultPath:  resultPath,
	}, nil
}

func codexAppVisiblePrompt(task Task, result TaskResult, portal codexAppVisiblePortal) string {
	displayPath := portal.DisplayPath
	if displayPath == "" {
		displayPath = portal.Dir
	}
	return fmt.Sprintf(
		"[Multica] issue %s task %s: Codex result is available at %s. Reply with one short sentence confirming this visible entry.",
		shortID(task.IssueID),
		shortID(task.ID),
		displayPath,
	)
}

func codexAppVisibleReadme(task Task, result TaskResult, portalDir, visibleCwd string) string {
	agentName := "unknown"
	if task.Agent != nil && strings.TrimSpace(task.Agent.Name) != "" {
		agentName = task.Agent.Name
	}
	return fmt.Sprintf(`# Multica Codex Task

This directory is a Codex App-visible portal for a Multica task.

## Task

- Issue id: %s
- Task id: %s
- Workspace id: %s
- Agent: %s
- Status: %s
- Codex session id: %s
- Real workdir: %s
- Visible cwd: %s
- Portal dir: %s

## Files

- result.md: final agent output
- links.json: machine-readable Multica/Codex linkage
- real-workdir: symlink to the original Multica workdir when the OS allows it
`, task.IssueID, task.ID, task.WorkspaceID, agentName, result.Status, valueOrUnknown(result.SessionID), valueOrUnknown(result.WorkDir), visibleCwd, portalDir)
}

func codexAppVisibleResult(result TaskResult) string {
	body := strings.TrimSpace(result.Comment)
	if body == "" {
		body = "No final output was captured."
	}
	return fmt.Sprintf(`# Result

Status: %s

%s
`, result.Status, body)
}

func safeSlug(value string) string {
	value = strings.Trim(codexAppSlugUnsafe.ReplaceAllString(value, "-"), "-._")
	if value == "" {
		return "unknown"
	}
	return value
}

func valueOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}
