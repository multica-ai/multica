package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	daemonterminal "github.com/multica-ai/multica/server/internal/daemon/terminal"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func (d *Daemon) ptyTaskAllowed(provider, workspaceID string, customProfile bool) bool {
	if !d.cfg.PTYEnabled || d.terminalManager == nil || !d.terminalConnected.Load() || customProfile {
		return false
	}
	if !containsString(d.cfg.PTYRuntimeAllowlist, provider) {
		return false
	}
	return len(d.cfg.PTYWorkspaceAllowlist) == 0 || containsString(d.cfg.PTYWorkspaceAllowlist, workspaceID)
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (d *Daemon) runInteractivePTYTask(ctx context.Context, task Task, executable string, env *execenv.Environment, agentEnv map[string]string, prompt string, opts agent.ExecOptions, taskLog *slog.Logger) (TaskResult, error) {
	if err := execenv.TrustCodexWorkdir(env.CodexHome, env.WorkDir); err != nil {
		return TaskResult{}, err
	}
	launch, err := agent.BuildInteractiveLaunch("codex", agent.InteractiveOptions{Prompt: prompt, Model: opts.Model, ThinkingLevel: opts.ThinkingLevel, ServiceTier: opts.ServiceTier, ResumeSessionID: opts.ResumeSessionID, ExtraArgs: opts.ExtraArgs, CustomArgs: opts.CustomArgs}, taskLog)
	if err != nil {
		return TaskResult{}, err
	}
	processEnv := mergeTerminalEnvironment(os.Environ(), agentEnv)
	processEnv = mergeTerminalEnvironment(processEnv, map[string]string{"TERM": "xterm-256color", "COLORTERM": "truecolor"})
	sessionID := uuid.New()
	generation := int(task.Attempt)
	if generation < 1 {
		generation = 1
	}
	started, err := d.terminalManager.Start(ctx, daemonterminal.StartRequest{
		SessionID: sessionID, TaskID: task.ID, IssueID: task.IssueID, AgentID: task.AgentID, WorkspaceID: task.WorkspaceID, RuntimeID: task.RuntimeID, DaemonID: d.cfg.DaemonID, Provider: "codex", Generation: generation,
		StructuredObservation: launch.Capabilities.StructuredObservation, Cols: daemonterminal.DefaultCols, Rows: daemonterminal.DefaultRows,
		Command: daemonterminal.Command{Path: executable, Args: launch.Args, Dir: env.WorkDir, Env: processEnv},
	})
	if err != nil {
		return TaskResult{}, err
	}
	taskLog.Info("starting interactive agent PTY", "workspace_id", task.WorkspaceID, "task_id", task.ID, "terminal_session_id", sessionID, "daemon_id", d.cfg.DaemonID, "runtime_type", "codex", "generation", generation, "protocol_version", 1)
	exit := started.Wait()
	providerSessionID := opts.ResumeSessionID
	if discovered := discoverLatestCodexSessionID(env.CodexHome, time.Now().Add(-24*time.Hour)); discovered != "" {
		providerSessionID = discovered
	}
	started.SetProviderSessionID(providerSessionID)
	result := TaskResult{WorkDir: env.WorkDir, EnvRoot: env.RootDir, SessionID: providerSessionID}
	if exit.Code == 0 {
		result.Status = "completed"
		result.Comment = "Interactive terminal session completed."
		return result, nil
	}
	result.Status = "failed"
	result.Comment = "Interactive terminal session exited before completion."
	if exit.Err != nil && !errors.Is(ctx.Err(), context.Canceled) {
		return result, nil
	}
	return result, nil
}

func mergeTerminalEnvironment(base []string, extra map[string]string) []string {
	values := make(map[string]string, len(base)+len(extra))
	order := make([]string, 0, len(base)+len(extra))
	for _, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = item
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = key + "=" + extra[key]
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, values[key])
	}
	return out
}

func discoverLatestCodexSessionID(codexHome string, since time.Time) string {
	type candidate struct {
		path string
		mod  time.Time
	}
	var files []candidate
	root := filepath.Join(codexHome, "sessions")
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			return nil
		}
		info, err := entry.Info()
		if err == nil && !info.ModTime().Before(since) {
			files = append(files, candidate{path: path, mod: info.ModTime()})
		}
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	for _, file := range files {
		f, err := os.Open(file.path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			var record struct {
				Type    string `json:"type"`
				Payload struct {
					ID string `json:"id"`
				} `json:"payload"`
			}
			if json.Unmarshal(scanner.Bytes(), &record) == nil && record.Type == "session_meta" {
				_ = f.Close()
				if _, err := uuid.Parse(record.Payload.ID); err == nil {
					return record.Payload.ID
				}
				break
			}
		}
		_ = f.Close()
	}
	return ""
}
