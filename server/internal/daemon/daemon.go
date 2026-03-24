package daemon

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// Daemon is the local agent runtime that polls for and executes tasks.
type Daemon struct {
	cfg    Config
	client *Client
	logger *log.Logger
}

// New creates a new Daemon instance.
func New(cfg Config, logger *log.Logger) *Daemon {
	return &Daemon{
		cfg:    cfg,
		client: NewClient(cfg.ServerBaseURL),
		logger: logger,
	}
}

// Run starts the daemon: pairs if needed, registers runtimes, then polls for tasks.
func (d *Daemon) Run(ctx context.Context) error {
	agentNames := make([]string, 0, len(d.cfg.Agents))
	for name := range d.cfg.Agents {
		agentNames = append(agentNames, name)
	}
	d.logger.Printf("starting daemon agents=%v workspace=%s server=%s repos_root=%s",
		agentNames, d.cfg.WorkspaceID, d.cfg.ServerBaseURL, d.cfg.ReposRoot)

	if strings.TrimSpace(d.cfg.WorkspaceID) == "" {
		workspaceID, err := d.ensurePaired(ctx)
		if err != nil {
			return err
		}
		d.cfg.WorkspaceID = workspaceID
		d.logger.Printf("pairing completed for workspace=%s", workspaceID)
	}

	runtimes, err := d.registerRuntimes(ctx)
	if err != nil {
		return err
	}
	runtimeIDs := make([]string, 0, len(runtimes))
	for _, rt := range runtimes {
		d.logger.Printf("registered runtime id=%s provider=%s status=%s", rt.ID, rt.Provider, rt.Status)
		runtimeIDs = append(runtimeIDs, rt.ID)
	}

	go d.heartbeatLoop(ctx, runtimeIDs)
	return d.pollLoop(ctx, runtimeIDs)
}

func (d *Daemon) registerRuntimes(ctx context.Context) ([]Runtime, error) {
	var runtimes []map[string]string
	for name, entry := range d.cfg.Agents {
		version, err := agent.DetectVersion(ctx, entry.Path)
		if err != nil {
			d.logger.Printf("skip registering %s: %v", name, err)
			continue
		}
		runtimes = append(runtimes, map[string]string{
			"name":    fmt.Sprintf("Local %s", strings.ToUpper(name[:1])+name[1:]),
			"type":    name,
			"version": version,
			"status":  "online",
		})
	}
	if len(runtimes) == 0 {
		return nil, fmt.Errorf("no agent runtimes could be registered")
	}

	req := map[string]any{
		"workspace_id": d.cfg.WorkspaceID,
		"daemon_id":    d.cfg.DaemonID,
		"device_name":  d.cfg.DeviceName,
		"runtimes":     runtimes,
	}

	rts, err := d.client.Register(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("register runtimes: %w", err)
	}
	if len(rts) == 0 {
		return nil, fmt.Errorf("register runtimes: empty response")
	}
	return rts, nil
}

func (d *Daemon) ensurePaired(ctx context.Context) (string, error) {
	// Use a deterministic agent for the pairing session metadata (prefer codex for backward compat).
	var firstName string
	var firstEntry AgentEntry
	for _, preferred := range []string{"codex", "claude"} {
		if entry, ok := d.cfg.Agents[preferred]; ok {
			firstName = preferred
			firstEntry = entry
			break
		}
	}
	version, err := agent.DetectVersion(ctx, firstEntry.Path)
	if err != nil {
		return "", err
	}

	session, err := d.client.CreatePairingSession(ctx, map[string]string{
		"daemon_id":       d.cfg.DaemonID,
		"device_name":     d.cfg.DeviceName,
		"runtime_name":    d.cfg.RuntimeName,
		"runtime_type":    firstName,
		"runtime_version": version,
	})
	if err != nil {
		return "", fmt.Errorf("create pairing session: %w", err)
	}
	if session.LinkURL != nil {
		d.logger.Printf("open this link to pair the daemon: %s", *session.LinkURL)
	} else {
		d.logger.Printf("pairing session created: %s", session.Token)
	}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		current, err := d.client.GetPairingSession(ctx, session.Token)
		if err != nil {
			return "", fmt.Errorf("poll pairing session: %w", err)
		}

		switch current.Status {
		case "approved", "claimed":
			if current.WorkspaceID == nil || strings.TrimSpace(*current.WorkspaceID) == "" {
				return "", fmt.Errorf("pairing session approved without workspace")
			}
			if err := SavePersistedConfig(d.cfg.ConfigPath, PersistedConfig{
				WorkspaceID: strings.TrimSpace(*current.WorkspaceID),
			}); err != nil {
				return "", err
			}
			if current.Status != "claimed" {
				if _, err := d.client.ClaimPairingSession(ctx, current.Token); err != nil {
					return "", fmt.Errorf("claim pairing session: %w", err)
				}
			}
			return strings.TrimSpace(*current.WorkspaceID), nil
		case "expired":
			return "", fmt.Errorf("pairing session expired before approval")
		}

		if err := sleepWithContext(ctx, d.cfg.PollInterval); err != nil {
			return "", err
		}
	}
}

func (d *Daemon) heartbeatLoop(ctx context.Context, runtimeIDs []string) {
	ticker := time.NewTicker(d.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, rid := range runtimeIDs {
				if err := d.client.SendHeartbeat(ctx, rid); err != nil {
					d.logger.Printf("heartbeat failed for runtime %s: %v", rid, err)
				}
			}
		}
	}
}

func (d *Daemon) pollLoop(ctx context.Context, runtimeIDs []string) error {
	pollOffset := 0
	pollCount := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		claimed := false
		n := len(runtimeIDs)
		for i := 0; i < n; i++ {
			rid := runtimeIDs[(pollOffset+i)%n]
			task, err := d.client.ClaimTask(ctx, rid)
			if err != nil {
				d.logger.Printf("claim task failed for runtime %s: %v", rid, err)
				continue
			}
			if task != nil {
				d.logger.Printf("poll: got task=%s issue=%s title=%q", task.ID, task.IssueID, task.Context.Issue.Title)
				d.handleTask(ctx, *task)
				claimed = true
				pollOffset = (pollOffset + i + 1) % n
				break
			}
		}

		if !claimed {
			pollCount++
			if pollCount%20 == 1 {
				d.logger.Printf("poll: no tasks (runtimes=%v, cycle=%d)", runtimeIDs, pollCount)
			}
			pollOffset = (pollOffset + 1) % n
			if err := sleepWithContext(ctx, d.cfg.PollInterval); err != nil {
				return err
			}
		} else {
			pollCount = 0
		}
	}
}

func (d *Daemon) handleTask(ctx context.Context, task Task) {
	provider := task.Context.Runtime.Provider
	d.logger.Printf("picked task=%s issue=%s provider=%s title=%q", task.ID, task.IssueID, provider, task.Context.Issue.Title)

	if err := d.client.StartTask(ctx, task.ID); err != nil {
		d.logger.Printf("start task %s failed: %v", task.ID, err)
		return
	}

	_ = d.client.ReportProgress(ctx, task.ID, fmt.Sprintf("Launching %s", provider), 1, 2)

	result, err := d.runTask(ctx, task)
	if err != nil {
		d.logger.Printf("task %s failed: %v", task.ID, err)
		if failErr := d.client.FailTask(ctx, task.ID, err.Error()); failErr != nil {
			d.logger.Printf("fail task %s callback failed: %v", task.ID, failErr)
		}
		return
	}

	_ = d.client.ReportProgress(ctx, task.ID, "Finishing task", 2, 2)

	switch result.Status {
	case "blocked":
		if err := d.client.FailTask(ctx, task.ID, result.Comment); err != nil {
			d.logger.Printf("report blocked task %s failed: %v", task.ID, err)
		}
	default:
		d.logger.Printf("task %s completed status=%s", task.ID, result.Status)
		if err := d.client.CompleteTask(ctx, task.ID, result.Comment); err != nil {
			d.logger.Printf("complete task %s failed: %v", task.ID, err)
		}
	}
}

func (d *Daemon) runTask(ctx context.Context, task Task) (TaskResult, error) {
	provider := task.Context.Runtime.Provider
	entry, ok := d.cfg.Agents[provider]
	if !ok {
		return TaskResult{}, fmt.Errorf("no agent configured for provider %q", provider)
	}

	workdir, err := ResolveTaskWorkdir(d.cfg.ReposRoot, task.Context.Issue.Repository)
	if err != nil {
		return TaskResult{}, err
	}

	prompt := BuildPrompt(task, workdir)

	backend, err := agent.New(provider, agent.Config{
		ExecutablePath: entry.Path,
		Logger:         d.logger,
	})
	if err != nil {
		return TaskResult{}, fmt.Errorf("create agent backend: %w", err)
	}

	d.logger.Printf(
		"starting %s task=%s workdir=%s model=%s timeout=%s",
		provider, task.ID, workdir, entry.Model, d.cfg.AgentTimeout,
	)

	session, err := backend.Execute(ctx, prompt, agent.ExecOptions{
		Cwd:     workdir,
		Model:   entry.Model,
		Timeout: d.cfg.AgentTimeout,
	})
	if err != nil {
		return TaskResult{}, err
	}

	// Drain message channel (log tool uses, ignore text since Result has output)
	go func() {
		for msg := range session.Messages {
			switch msg.Type {
			case agent.MessageToolUse:
				d.logger.Printf("[%s] tool-use: %s (call=%s)", provider, msg.Tool, msg.CallID)
			case agent.MessageError:
				d.logger.Printf("[%s] error: %s", provider, msg.Content)
			}
		}
	}()

	result := <-session.Result

	switch result.Status {
	case "completed":
		if result.Output == "" {
			return TaskResult{}, fmt.Errorf("%s returned empty output", provider)
		}
		return TaskResult{Status: "completed", Comment: result.Output}, nil
	case "timeout":
		return TaskResult{}, fmt.Errorf("%s timed out after %s", provider, d.cfg.AgentTimeout)
	default:
		errMsg := result.Error
		if errMsg == "" {
			errMsg = fmt.Sprintf("%s execution %s", provider, result.Status)
		}
		return TaskResult{Status: "blocked", Comment: errMsg}, nil
	}
}
