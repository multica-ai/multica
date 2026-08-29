package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// LocalExecutorConfig holds configuration for the local task executor.
type LocalExecutorConfig struct {
	MaxConcurrentTasks int           // default: 3
	AgentTimeout       time.Duration // default: 2 hours
	WorkspacesRoot     string        // base path for execution envs
}

// LocalExecutor runs agent tasks in-process without requiring a daemon.
// It directly spawns agent CLI processes and streams output via WebSocket.
type LocalExecutor struct {
	cfg         LocalExecutorConfig
	queries     *db.Queries
	hub         *realtime.Hub
	bus         *events.Bus
	taskService *TaskService

	mu       sync.Mutex
	sem      chan struct{}            // concurrency limiter
	running  map[string]context.CancelFunc // taskID -> cancel
}

// NewLocalExecutor creates a new LocalExecutor.
func NewLocalExecutor(cfg LocalExecutorConfig, q *db.Queries, hub *realtime.Hub, bus *events.Bus, taskService *TaskService) *LocalExecutor {
	if cfg.MaxConcurrentTasks <= 0 {
		cfg.MaxConcurrentTasks = 3
	}
	if cfg.AgentTimeout <= 0 {
		cfg.AgentTimeout = 2 * time.Hour
	}
	if cfg.WorkspacesRoot == "" {
		home, _ := os.UserHomeDir()
		cfg.WorkspacesRoot = filepath.Join(home, "multica_workspaces")
	}

	return &LocalExecutor{
		cfg:         cfg,
		queries:     q,
		hub:         hub,
		bus:         bus,
		taskService: taskService,
		sem:         make(chan struct{}, cfg.MaxConcurrentTasks),
		running:     make(map[string]context.CancelFunc),
	}
}

// RunningTaskCount returns the number of currently executing tasks.
func (e *LocalExecutor) RunningTaskCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.running)
}

// ExecuteTask starts a task execution directly (no daemon polling).
// The task must already exist in the DB with status 'queued'.
// This transitions: queued → dispatched → running → completed/failed.
func (e *LocalExecutor) ExecuteTask(ctx context.Context, taskID pgtype.UUID) error {
	// Acquire concurrency slot.
	select {
	case e.sem <- struct{}{}:
	default:
		return fmt.Errorf("at capacity: %d concurrent tasks running", e.cfg.MaxConcurrentTasks)
	}

	taskIDStr := util.UUIDToString(taskID)

	// Load task from DB.
	task, err := e.queries.GetAgentTask(ctx, taskID)
	if err != nil {
		<-e.sem
		return fmt.Errorf("load task: %w", err)
	}

	// Load agent.
	agentRow, err := e.queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		<-e.sem
		return fmt.Errorf("load agent: %w", err)
	}

	// Load issue for workspace context.
	issue, err := e.queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		<-e.sem
		return fmt.Errorf("load issue: %w", err)
	}

	// Determine provider from runtime.
	provider := "claude" // default
	if task.RuntimeID.Valid {
		if rt, err := e.queries.GetAgentRuntime(ctx, task.RuntimeID); err == nil {
			provider = rt.Provider
		}
	}

	// Detect CLI path.
	cliPath := provider
	configs, _ := e.queries.ListLocalAgentConfigs(ctx, issue.WorkspaceID)
	for _, cfg := range configs {
		if cfg.Provider == provider && cfg.Status == "available" {
			cliPath = cfg.CliPath
			break
		}
	}

	// Claim task (queued → dispatched).
	claimedTask, err := e.taskService.ClaimTask(ctx, task.AgentID)
	if err != nil {
		<-e.sem
		return fmt.Errorf("claim task: %w", err)
	}
	if claimedTask == nil || util.UUIDToString(claimedTask.ID) != taskIDStr {
		<-e.sem
		return fmt.Errorf("task was not claimed (may already be running)")
	}

	// Start task (dispatched → running).
	if _, err := e.taskService.StartTask(ctx, taskID); err != nil {
		<-e.sem
		e.taskService.FailTask(ctx, taskID, fmt.Sprintf("start failed: %v", err))
		return fmt.Errorf("start task: %w", err)
	}

	// Register cancel function.
	runCtx, runCancel := context.WithCancel(ctx)
	e.mu.Lock()
	e.running[taskIDStr] = runCancel
	e.mu.Unlock()

	// Run in background.
	go func() {
		defer func() {
			<-e.sem
			e.mu.Lock()
			delete(e.running, taskIDStr)
			e.mu.Unlock()
			runCancel()
		}()

		e.runTask(runCtx, task, agentRow, issue, provider, cliPath)
	}()

	return nil
}

// CancelTask cancels a running task by sending a signal to its context.
func (e *LocalExecutor) CancelTask(taskID string) bool {
	e.mu.Lock()
	cancel, ok := e.running[taskID]
	e.mu.Unlock()

	if ok {
		cancel()
		return true
	}
	return false
}

func (e *LocalExecutor) runTask(ctx context.Context, task db.AgentTaskQueue, agentRow db.Agent, issue db.Issue, provider, cliPath string) {
	taskIDStr := util.UUIDToString(task.ID)
	issueIDStr := util.UUIDToString(task.IssueID)
	workspaceIDStr := util.UUIDToString(issue.WorkspaceID)

	taskLog := slog.With("task_id", taskIDStr[:8], "provider", provider)
	taskLog.Info("local executor: starting agent")

	// Load agent skills.
	skills := e.taskService.LoadAgentSkills(ctx, task.AgentID)

	// Build prompt.
	prompt := buildLocalPrompt(issueIDStr)

	// Prepare work directory.
	workDir := filepath.Join(e.cfg.WorkspacesRoot, workspaceIDStr, taskIDStr[:8], "workdir")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		taskLog.Error("failed to create workdir", "error", err)
		e.taskService.FailTask(ctx, task.ID, fmt.Sprintf("create workdir: %v", err))
		return
	}

	// Write context files (CLAUDE.md, skills, etc).
	if err := writeLocalContextFiles(workDir, provider, agentRow, issue, skills); err != nil {
		taskLog.Warn("failed to write context files", "error", err)
	}

	// Create agent backend.
	backend, err := agent.New(provider, agent.Config{
		ExecutablePath: cliPath,
		Logger:         slog.Default(),
	})
	if err != nil {
		taskLog.Error("failed to create agent backend", "error", err)
		e.taskService.FailTask(ctx, task.ID, fmt.Sprintf("create backend: %v", err))
		return
	}

	// Determine model from agent config.
	model := ""
	if agentRow.RuntimeConfig != nil {
		var rc map[string]any
		json.Unmarshal(agentRow.RuntimeConfig, &rc)
		if m, ok := rc["model"].(string); ok {
			model = m
		}
	}

	// Check for prior session to resume.
	var resumeSessionID string
	if prior, err := e.queries.GetLastTaskSession(ctx, db.GetLastTaskSessionParams{
		AgentID: task.AgentID,
		IssueID: task.IssueID,
	}); err == nil && prior.SessionID.Valid {
		resumeSessionID = prior.SessionID.String
	}

	session, err := backend.Execute(ctx, prompt, agent.ExecOptions{
		Cwd:             workDir,
		Model:           model,
		Timeout:         e.cfg.AgentTimeout,
		ResumeSessionID: resumeSessionID,
	})
	if err != nil {
		taskLog.Error("agent execution failed", "error", err)
		e.taskService.FailTask(ctx, task.ID, err.Error())
		return
	}

	// Stream messages to WebSocket.
	var seq atomic.Int32
	go func() {
		var batch []map[string]any
		var mu sync.Mutex

		flush := func() {
			mu.Lock()
			toSend := batch
			batch = nil
			mu.Unlock()

			for _, msg := range toSend {
				e.bus.Publish(events.Event{
					Type:        protocol.EventTaskMessage,
					WorkspaceID: workspaceIDStr,
					ActorType:   "system",
					Payload:     msg,
				})
			}
		}

		flushTicker := time.NewTicker(100 * time.Millisecond)
		defer flushTicker.Stop()
		defer flush()

		for msg := range session.Messages {
			s := seq.Add(1)

			payload := map[string]any{
				"task_id":  taskIDStr,
				"issue_id": issueIDStr,
				"seq":      int(s),
				"type":     mapMessageType(msg.Type),
				"content":  redact.Text(msg.Content),
			}
			if msg.Tool != "" {
				payload["tool"] = msg.Tool
			}
			if msg.Input != nil {
				payload["input"] = msg.Input
			}
			if msg.Output != "" {
				payload["output"] = redact.Text(msg.Output)
			}

			// Store in DB.
			var inputJSON []byte
			if msg.Input != nil {
				inputJSON, _ = json.Marshal(msg.Input)
			}
			e.queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{
				TaskID:  task.ID,
				Seq:     s,
				Type:    string(msg.Type),
				Tool:    pgtype.Text{String: msg.Tool, Valid: msg.Tool != ""},
				Content: pgtype.Text{String: redact.Text(msg.Content), Valid: msg.Content != ""},
				Input:   inputJSON,
				Output:  pgtype.Text{String: redact.Text(msg.Output), Valid: msg.Output != ""},
			})

			mu.Lock()
			batch = append(batch, payload)
			mu.Unlock()

			select {
			case <-flushTicker.C:
				flush()
			default:
			}
		}
	}()

	// Wait for result.
	result := <-session.Result

	if result.Status == "completed" {
		taskLog.Info("agent completed", "duration_ms", result.DurationMs)
		resultJSON, _ := json.Marshal(map[string]any{
			"status": "completed",
			"output": redact.Text(result.Output),
		})
		e.taskService.CompleteTask(ctx, task.ID, resultJSON, result.SessionID, workDir)
	} else {
		errMsg := result.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("agent returned status: %s", result.Status)
		}
		taskLog.Warn("agent failed", "status", result.Status, "error", errMsg)
		e.taskService.FailTask(ctx, task.ID, errMsg)
	}
}

func mapMessageType(t agent.MessageType) string {
	switch t {
	case agent.MessageToolUse:
		return "tool_use"
	case agent.MessageToolResult:
		return "tool_result"
	default:
		return string(t)
	}
}

func buildLocalPrompt(issueID string) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", issueID)
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", issueID)
	return b.String()
}

func writeLocalContextFiles(workDir, provider string, agentRow db.Agent, issue db.Issue, skills []AgentSkillData) error {
	// Write CLAUDE.md / AGENTS.md with agent identity and instructions.
	var b strings.Builder
	b.WriteString("# Multica Agent Runtime\n\n")
	b.WriteString("You are a coding agent in the Multica platform. Use the `multica` CLI to interact with the platform.\n\n")

	if agentRow.Instructions != "" {
		b.WriteString("## Agent Identity\n\n")
		b.WriteString(agentRow.Instructions)
		b.WriteString("\n\n")
	}

	b.WriteString("## Available Commands\n\n")
	b.WriteString("**Always use `--output json` for all read commands** to get structured data with full IDs.\n\n")
	b.WriteString("### Read\n")
	b.WriteString("- `multica issue get <id> --output json` — Get full issue details\n")
	b.WriteString("- `multica issue list [--status X] --output json` — List issues in workspace\n")
	b.WriteString("- `multica issue comment list <issue-id> --output json` — List comments on an issue\n\n")
	b.WriteString("### Write\n")
	b.WriteString("- `multica issue comment add <issue-id> --content \"...\"` — Post a comment\n")
	b.WriteString("- `multica issue status <id> <status>` — Update issue status\n\n")

	issueID := util.UUIDToString(issue.ID)
	b.WriteString("### Workflow\n\n")
	fmt.Fprintf(&b, "1. Run `multica issue get %s --output json` to understand your task\n", issueID)
	fmt.Fprintf(&b, "2. Run `multica issue status %s in_progress`\n", issueID)
	b.WriteString("3. Implement the changes, commit, and push\n")
	fmt.Fprintf(&b, "4. Run `multica issue status %s in_review`\n\n", issueID)

	if len(skills) > 0 {
		b.WriteString("## Skills\n\n")
		for _, skill := range skills {
			fmt.Fprintf(&b, "- **%s**\n", skill.Name)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Output\n\n")
	b.WriteString("Keep comments concise and natural — state the outcome, not the process.\n")

	content := b.String()

	switch provider {
	case "claude":
		return os.WriteFile(filepath.Join(workDir, "CLAUDE.md"), []byte(content), 0o644)
	case "codex", "opencode":
		return os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte(content), 0o644)
	default:
		return os.WriteFile(filepath.Join(workDir, "CLAUDE.md"), []byte(content), 0o644)
	}
}
