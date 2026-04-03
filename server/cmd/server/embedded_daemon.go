package main

// MULTICA-LOCAL: Embedded daemon — runs agent tasks in-process.
// Replaces the separate daemon process with a goroutine that polls the DB
// directly for pending tasks and executes them via the agent backends.

import (
	"context"
	"database/sql"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EmbeddedDaemon runs agent tasks in-process as part of the server.
type EmbeddedDaemon struct {
	queries     *db.Queries
	taskService *service.TaskService
	runtimeIDs  []string           // registered runtime IDs
	backends    map[string]agent.Backend // provider -> backend
	mu          sync.Mutex
	maxTasks    int
	running     int
}

// NewEmbeddedDaemon creates a daemon that auto-detects installed agent CLIs.
func NewEmbeddedDaemon(queries *db.Queries, taskSvc *service.TaskService) *EmbeddedDaemon {
	d := &EmbeddedDaemon{
		queries:     queries,
		taskService: taskSvc,
		backends:    make(map[string]agent.Backend),
		maxTasks:    3,
	}

	// Auto-detect agent CLIs
	for _, provider := range []string{"claude", "codex", "opencode"} {
		if path, err := exec.LookPath(provider); err == nil {
			slog.Info("detected agent CLI", "provider", provider, "path", path)
			backend, err := agent.New(provider, agent.Config{ExecutablePath: path})
			if err != nil {
				slog.Error("failed to create backend", "provider", provider, "error", err)
				continue
			}
			d.backends[provider] = backend
		}
	}

	if len(d.backends) == 0 {
		slog.Warn("no agent CLIs detected — agent execution disabled")
	} else {
		providers := make([]string, 0, len(d.backends))
		for p := range d.backends {
			providers = append(providers, p)
		}
		slog.Info("embedded daemon ready", "providers", providers)
	}

	return d
}

// RegisterRuntimes creates runtime records for detected agent backends in each workspace.
func (d *EmbeddedDaemon) RegisterRuntimes(ctx context.Context) {
	workspaces, err := d.queries.ListWorkspaces(ctx, "") // empty user_id won't match anything
	if err != nil {
		// For local mode, we register runtimes for all workspaces
		// by listing agent_runtime directly or getting workspaces another way.
		return
	}

	hostname := "local"

	for _, ws := range workspaces {
		for provider := range d.backends {
			rt, err := d.queries.UpsertAgentRuntime(ctx, db.UpsertAgentRuntimeParams{
				ID:          uuid.New().String(),
				WorkspaceID: ws.ID,
				DaemonID:    sql.NullString{String: "embedded", Valid: true},
				Name:        hostname + " (" + provider + ")",
				RuntimeMode: "local",
				Provider:    provider,
				Status:      "online",
				DeviceInfo:  hostname,
				Metadata:    "{}",
			})
			if err != nil {
				slog.Error("failed to register runtime", "provider", provider, "workspace", ws.ID, "error", err)
				continue
			}
			d.runtimeIDs = append(d.runtimeIDs, rt.ID)
			slog.Info("runtime registered", "runtime_id", rt.ID, "provider", provider, "workspace", ws.ID)
		}
	}
}

// Run starts the task polling loop. Call in a goroutine.
func (d *EmbeddedDaemon) Run(ctx context.Context) {
	if len(d.backends) == 0 {
		slog.Info("embedded daemon: no backends, skipping task loop")
		return
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("embedded daemon stopping")
			return
		case <-ticker.C:
			d.pollTasks(ctx)
		}
	}
}

func (d *EmbeddedDaemon) pollTasks(ctx context.Context) {
	d.mu.Lock()
	if d.running >= d.maxTasks {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	for _, runtimeID := range d.runtimeIDs {
		task, err := d.taskService.ClaimTaskForRuntime(ctx, runtimeID)
		if err != nil {
			slog.Error("task claim error", "runtime_id", runtimeID, "error", err)
			continue
		}
		if task == nil {
			continue
		}

		d.mu.Lock()
		d.running++
		d.mu.Unlock()

		go d.executeTask(ctx, task)
	}
}

func (d *EmbeddedDaemon) executeTask(ctx context.Context, task *db.AgentTaskQueue) {
	defer func() {
		d.mu.Lock()
		d.running--
		d.mu.Unlock()
	}()

	// Determine provider from runtime
	rt, err := d.queries.GetAgentRuntime(ctx, task.RuntimeID)
	if err != nil {
		slog.Error("failed to get runtime for task", "task_id", task.ID, "error", err)
		d.taskService.FailTask(ctx, task.ID, "runtime not found")
		return
	}

	backend, ok := d.backends[rt.Provider]
	if !ok {
		slog.Error("no backend for provider", "provider", rt.Provider, "task_id", task.ID)
		d.taskService.FailTask(ctx, task.ID, "no backend for provider: "+rt.Provider)
		return
	}

	// Start the task
	d.taskService.StartTask(ctx, task.ID)

	// Build prompt from issue
	issue, err := d.queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		d.taskService.FailTask(ctx, task.ID, "issue not found")
		return
	}

	prompt := issue.Title
	if issue.Description.Valid && issue.Description.String != "" {
		prompt += "\n\n" + issue.Description.String
	}

	// Load agent skills for system prompt
	skills := d.taskService.LoadAgentSkills(ctx, task.AgentID)
	var systemPrompt string
	for _, sk := range skills {
		systemPrompt += "\n\n# " + sk.Name + "\n" + sk.Content
	}

	// Determine working directory
	workDir := "."
	if task.WorkDir.Valid && task.WorkDir.String != "" {
		workDir = task.WorkDir.String
	}

	// Execute
	session, err := backend.Execute(ctx, prompt, agent.ExecOptions{
		Cwd:          workDir,
		SystemPrompt: systemPrompt,
	})
	if err != nil {
		d.taskService.FailTask(ctx, task.ID, err.Error())
		return
	}

	// Stream messages
	go func() {
		seq := 0
		for msg := range session.Messages {
			seq++
			d.queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{
				ID:     uuid.New().String(),
				TaskID: task.ID,
				Seq:    int64(seq),
				Type:   string(msg.Type),
			})
		}
	}()

	// Wait for result
	result := <-session.Result
	if result.Status == "completed" {
		d.taskService.CompleteTask(ctx, task.ID, []byte(`{"output":"`+result.Output+`"}`), result.SessionID, workDir)
	} else {
		d.taskService.FailTask(ctx, task.ID, result.Error)
	}
}
