package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/crypto"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// PingChecker allows CloudDaemon to check for and respond to pending ping
// requests without importing the handler package.
type PingChecker interface {
	PopPending(runtimeID string) (pingID string, found bool)
	Complete(pingID string, output string, durationMs int64)
	Fail(pingID string, errMsg string, durationMs int64)
}

// CloudDaemon is the embedded sandbox executor that claims and executes tasks
// for cloud-mode runtimes. It mirrors the local daemon's behavior (poll → claim → execute)
// but runs inside the server process and uses SandboxProvider instead of local CLI.
type CloudDaemon struct {
	queries       *db.Queries
	taskService   *service.TaskService
	bus           *events.Bus
	encryptionKey []byte
	pingChecker   PingChecker

	pollInterval      time.Duration
	heartbeatInterval time.Duration
	maxConcurrent     int

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// CloudDaemonConfig configures the CloudDaemon.
type CloudDaemonConfig struct {
	Queries       *db.Queries
	TaskService   *service.TaskService
	Bus           *events.Bus
	EncryptionKey []byte
	PingChecker   PingChecker

	PollInterval      time.Duration // Default: 5s
	HeartbeatInterval time.Duration // Default: 15s
	MaxConcurrent     int           // Default: 10
}

// NewCloudDaemon creates a new CloudDaemon. Returns nil if encryption key is not configured.
func NewCloudDaemon(cfg CloudDaemonConfig) *CloudDaemon {
	if cfg.EncryptionKey == nil {
		slog.Warn("cloud daemon disabled: ENCRYPTION_KEY not configured")
		return nil
	}

	pollInterval := cfg.PollInterval
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}
	heartbeatInterval := cfg.HeartbeatInterval
	if heartbeatInterval == 0 {
		heartbeatInterval = 15 * time.Second
	}
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent == 0 {
		maxConcurrent = 10
	}

	return &CloudDaemon{
		queries:           cfg.Queries,
		taskService:       cfg.TaskService,
		bus:               cfg.Bus,
		encryptionKey:     cfg.EncryptionKey,
		pingChecker:       cfg.PingChecker,
		pollInterval:      pollInterval,
		heartbeatInterval: heartbeatInterval,
		maxConcurrent:     maxConcurrent,
	}
}

// Start begins the CloudDaemon poll and heartbeat loops.
// It issues the first heartbeat synchronously before returning,
// ensuring cloud runtimes are fresh before the sweeper's first tick.
func (d *CloudDaemon) Start(ctx context.Context) {
	ctx, d.cancel = context.WithCancel(ctx)

	// Synchronous first heartbeat — must complete before sweeper starts.
	d.heartbeat(ctx)

	// Heartbeat loop
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.heartbeatLoop(ctx)
	}()

	// Poll loop
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.pollLoop(ctx)
	}()

	slog.Info("cloud daemon started", "poll_interval", d.pollInterval, "max_concurrent", d.maxConcurrent)
}

// Stop gracefully shuts down the CloudDaemon.
func (d *CloudDaemon) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
	d.wg.Wait()
	slog.Info("cloud daemon stopped")
}

// heartbeatLoop updates last_seen_at for all cloud runtimes periodically.
func (d *CloudDaemon) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(d.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.heartbeat(ctx)
		}
	}
}

func (d *CloudDaemon) heartbeat(ctx context.Context) {
	if err := d.queries.UpdateCloudRuntimesHeartbeat(ctx); err != nil {
		slog.Warn("cloud daemon heartbeat failed", "error", err)
	}

	// Check for pending ping requests on cloud runtimes.
	if d.pingChecker == nil {
		return
	}
	runtimes, err := d.queries.ListCloudRuntimes(ctx)
	if err != nil {
		return
	}
	for _, rt := range runtimes {
		rtID := util.UUIDToString(rt.ID)
		if pingID, ok := d.pingChecker.PopPending(rtID); ok {
			go d.handlePing(ctx, rt, pingID)
		}
	}
}

// pollLoop scans for claimable tasks on cloud runtimes.
func (d *CloudDaemon) pollLoop(ctx context.Context) {
	sem := make(chan struct{}, d.maxConcurrent)
	ticker := time.NewTicker(d.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.tryClaimTasks(ctx, sem)
		}
	}
}

func (d *CloudDaemon) tryClaimTasks(ctx context.Context, sem chan struct{}) {
	runtimes, err := d.queries.ListCloudRuntimes(ctx)
	if err != nil {
		slog.Warn("cloud daemon: list cloud runtimes", "error", err)
		return
	}

	for _, rt := range runtimes {
		task, err := d.taskService.ClaimTaskForRuntime(ctx, rt.ID)
		if err != nil {
			slog.Warn("cloud daemon: claim task", "runtime_id", util.UUIDToString(rt.ID), "error", err)
			continue
		}
		if task == nil {
			continue
		}

		// Acquire semaphore slot
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}

		d.wg.Add(1)
		go func(t db.AgentTaskQueue, runtime db.AgentRuntime) {
			defer d.wg.Done()
			defer func() { <-sem }()
			d.handleTask(ctx, t, runtime)
		}(*task, rt)
	}
}

// handleTask executes a single task in a sandbox.
func (d *CloudDaemon) handleTask(ctx context.Context, task db.AgentTaskQueue, runtime db.AgentRuntime) {
	taskID := util.UUIDToString(task.ID)
	issueID := util.UUIDToString(task.IssueID)
	log := slog.With("task_id", taskID, "issue_id", issueID)

	log.Info("cloud daemon: handling task")

	// 1. Start task
	_, err := d.taskService.StartTask(ctx, task.ID)
	if err != nil {
		log.Error("cloud daemon: start task failed", "error", err)
		return
	}

	// 2. Update issue status → "in_progress" (R9)
	wsID := util.UUIDToString(runtime.WorkspaceID)
	d.updateIssueStatus(ctx, task.IssueID, "in_progress", wsID, log)

	// 3. Load sandbox config + decrypt
	if !runtime.SandboxConfigID.Valid {
		log.Error("cloud daemon: runtime has no sandbox_config_id")
		d.failTask(ctx, task.ID, "runtime has no linked sandbox config", log)
		return
	}
	sandboxCfg, gitPat, aiGatewayKey, err := d.loadSandboxConfig(ctx, runtime.SandboxConfigID)
	if err != nil {
		log.Error("cloud daemon: load sandbox config", "error", err)
		d.failTask(ctx, task.ID, fmt.Sprintf("sandbox config error: %v", err), log)
		return
	}

	// 4. Build prompt
	promptResult, err := BuildPrompt(ctx, task, d.queries, *sandboxCfg, gitPat, aiGatewayKey)
	if err != nil {
		log.Error("cloud daemon: build prompt", "error", err)
		d.failTask(ctx, task.ID, fmt.Sprintf("prompt build error: %v", err), log)
		return
	}

	// 5. Create or connect sandbox
	providerKey, err := d.decryptField(sandboxCfg.ProviderApiKey, "provider-api-key")
	if err != nil {
		log.Error("cloud daemon: decrypt provider key", "error", err)
		d.failTask(ctx, task.ID, fmt.Sprintf("provider key decryption failed: %v", err), log)
		return
	}
	provider := NewE2BProvider(providerKey)

	sandboxID := fmt.Sprintf("multica-%s", taskID[:8])
	sb, err := provider.CreateOrConnect(ctx, sandboxID, CreateOpts{
		TemplateID: textToString(sandboxCfg.TemplateID),
		EnvVars:    promptResult.EnvVars,
		Timeout:    30 * time.Minute,
	})
	if err != nil {
		log.Error("cloud daemon: create sandbox", "error", err)
		d.failTask(ctx, task.ID, fmt.Sprintf("sandbox creation error: %v", err), log)
		return
	}

	log.Info("cloud daemon: sandbox connected", "sandbox_id", sb.ID, "provider", runtime.Provider)

	// 6-9. Launch agent (provider-specific).
	workDir := fmt.Sprintf("/workspace/%s", taskID[:8])
	var sessionID string

	switch runtime.Provider {
	case "opencode":
		sessionID, err = d.launchOpencode(ctx, provider, sb, promptResult.Prompt, workDir, log)
	default:
		err = fmt.Errorf("cloud provider %q is not yet supported", runtime.Provider)
	}
	if err != nil {
		d.failTask(ctx, task.ID, fmt.Sprintf("agent launch error: %v", err), log)
		return
	}

	log.Info("cloud daemon: session started", "session_id", sessionID)

	// 10. Poll session
	poller := NewSessionPoller(provider, sb)
	status, err := poller.WatchSession(ctx, sessionID, func(s *SessionStatus) {
		// Check if task was cancelled
		if d.isTaskCancelled(ctx, task.ID) {
			log.Info("cloud daemon: task cancelled, aborting session")
			provider.Exec(ctx, sb, []string{
				"curl", "-sf", "-X", "POST",
				fmt.Sprintf("http://localhost:4096/session/%s/abort", sessionID),
			})
			return
		}

		// Forward messages
		d.forwardMessages(ctx, task.ID, wsID, s.Messages, log)
	})

	if err != nil && status == nil {
		log.Error("cloud daemon: watch session error", "error", err)
		d.failTask(ctx, task.ID, fmt.Sprintf("session watch error: %v", err), log)
		return
	}

	// 11. Handle completion
	switch status.State {
	case SessionIdle:
		d.completeTask(ctx, task, provider, sb, workDir, sessionID, status.Usage, wsID, log)
	case SessionError:
		d.failTask(ctx, task.ID, fmt.Sprintf("agent error: %s", status.Error), log)
	case SessionTimeout:
		d.failTask(ctx, task.ID, "agent timed out", log)
	default:
		d.failTask(ctx, task.ID, fmt.Sprintf("unexpected session state: %s", status.State), log)
	}
}

// launchOpencode starts the opencode serve + run flow and returns the session ID.
func (d *CloudDaemon) launchOpencode(ctx context.Context, provider SandboxProvider, sb *Sandbox, prompt, workDir string, log *slog.Logger) (string, error) {
	// Ensure opencode serve is running
	if err := d.ensureServing(ctx, provider, sb, log); err != nil {
		return "", fmt.Errorf("opencode serve failed: %w", err)
	}

	// Write prompt to file
	if err := provider.WriteFile(ctx, sb, "/tmp/prompt.txt", []byte(prompt)); err != nil {
		return "", fmt.Errorf("write prompt error: %w", err)
	}

	// Launch opencode run
	_, err := provider.Exec(ctx, sb, []string{
		"sh", "-c", fmt.Sprintf(
			"mkdir -p %s && nohup opencode run --attach http://localhost:4096 --dir %s --format json < /tmp/prompt.txt > /tmp/opencode.log 2>&1 &",
			workDir, workDir,
		),
	})
	if err != nil {
		return "", fmt.Errorf("launch opencode: %w", err)
	}

	// Discover session ID
	sessionID, err := d.discoverSessionID(ctx, provider, sb, workDir, log)
	if err != nil {
		return "", fmt.Errorf("session discovery: %w", err)
	}

	return sessionID, nil
}

func (d *CloudDaemon) completeTask(ctx context.Context, task db.AgentTaskQueue, provider SandboxProvider, sb *Sandbox, workDir, sessionID string, usage TokenUsage, wsID string, log *slog.Logger) {
	// Try to read SUMMARY.md; if not found, use the last assistant text from session messages
	summary := ""
	if data, err := provider.ReadFile(ctx, sb, workDir+"/SUMMARY.md"); err == nil {
		summary = string(data)
		log.Info("cloud daemon: read SUMMARY.md", "length", len(summary))
	} else {
		log.Debug("cloud daemon: SUMMARY.md not found, using last session message", "error", err)
	}

	// Fallback: extract last text message from the session poll as output
	if summary == "" {
		stdout, _ := provider.Exec(ctx, sb, []string{"curl", "-s", fmt.Sprintf("http://localhost:4096/session/%s/message", sessionID)})
		if stdout != "" {
			summary = extractLastAssistantText(stdout)
		}
	}

	// Build result payload (let CompleteTask handle comment posting)
	result, _ := json.Marshal(protocol.TaskCompletedPayload{
		Output: summary,
	})

	// Report token usage
	d.reportUsage(ctx, task.ID, usage, log)

	// Complete task
	if _, err := d.taskService.CompleteTask(ctx, task.ID, result, sessionID, workDir); err != nil {
		log.Error("cloud daemon: complete task failed", "error", err)
		return
	}

	// Update issue status → "done" (R9)
	d.updateIssueStatus(ctx, task.IssueID, "done", wsID, log)

	log.Info("cloud daemon: task completed")
}

func (d *CloudDaemon) failTask(ctx context.Context, taskID pgtype.UUID, errMsg string, log *slog.Logger) {
	if _, err := d.taskService.FailTask(ctx, taskID, errMsg); err != nil {
		log.Error("cloud daemon: fail task failed", "error", err)
	}
}

func (d *CloudDaemon) updateIssueStatus(ctx context.Context, issueID pgtype.UUID, status, wsID string, log *slog.Logger) {
	if !issueID.Valid {
		return
	}
	issue, err := d.queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{
		ID:     issueID,
		Status: status,
	})
	if err != nil {
		log.Warn("cloud daemon: update issue status failed", "status", status, "error", err)
		return
	}
	// Broadcast issue update via WS so frontend refreshes
	d.bus.Publish(events.Event{
		Type:        protocol.EventIssueUpdated,
		WorkspaceID: wsID,
		ActorType:   "agent",
		Payload: map[string]any{
			"issue":          issue,
			"status_changed": true,
		},
	})
}

func (d *CloudDaemon) forwardMessages(ctx context.Context, taskID pgtype.UUID, wsID string, messages []TaskMessage, log *slog.Logger) {
	for _, msg := range messages {
		var inputBytes []byte
		if msg.Input != "" {
			inputBytes = []byte(msg.Input)
		}
		if _, err := d.queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{
			TaskID:  taskID,
			Seq:     int32(msg.Seq),
			Type:    msg.Type,
			Tool:    pgtype.Text{String: msg.Tool, Valid: msg.Tool != ""},
			Content: pgtype.Text{String: msg.Content, Valid: msg.Content != ""},
			Input:   inputBytes,
			Output:  pgtype.Text{String: msg.Output, Valid: msg.Output != ""},
		}); err != nil {
			log.Warn("cloud daemon: forward message failed", "seq", msg.Seq, "error", err)
		}
	}

	// Broadcast task message event with WorkspaceID so it reaches WS clients
	if len(messages) > 0 {
		d.bus.Publish(events.Event{
			Type:        protocol.EventTaskMessage,
			WorkspaceID: wsID,
			Payload:     map[string]string{"task_id": util.UUIDToString(taskID)},
		})
	}
}

func (d *CloudDaemon) reportUsage(ctx context.Context, taskID pgtype.UUID, usage TokenUsage, log *slog.Logger) {
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		return
	}
	if err := d.queries.UpsertTaskUsage(ctx, db.UpsertTaskUsageParams{
		TaskID:           taskID,
		Provider:         "opencode",
		Model:            "unknown", // OpenCode serve doesn't expose model per-step
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CacheReadTokens:  usage.CacheReadTokens,
		CacheWriteTokens: usage.CacheWriteTokens,
	}); err != nil {
		log.Warn("cloud daemon: report usage failed", "error", err)
	}
}

func (d *CloudDaemon) isTaskCancelled(ctx context.Context, taskID pgtype.UUID) bool {
	task, err := d.queries.GetAgentTask(ctx, taskID)
	if err != nil {
		return false
	}
	return task.Status == "cancelled"
}

func (d *CloudDaemon) ensureServing(ctx context.Context, provider SandboxProvider, sb *Sandbox, log *slog.Logger) error {
	// Check if opencode serve is already running (no /health endpoint; use /session)
	_, err := provider.Exec(ctx, sb, []string{"curl", "-sf", "http://localhost:4096/session"})
	if err == nil {
		return nil // already serving
	}

	// Start opencode serve
	log.Info("cloud daemon: starting opencode serve")
	_, err = provider.Exec(ctx, sb, []string{
		"sh", "-c", "nohup opencode serve --port 4096 > /tmp/opencode-serve.log 2>&1 &",
	})
	if err != nil {
		return fmt.Errorf("start opencode serve: %w", err)
	}

	// Wait for serve to be ready (check /session endpoint)
	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		if _, err := provider.Exec(ctx, sb, []string{"curl", "-sf", "http://localhost:4096/session"}); err == nil {
			return nil
		}
	}
	return fmt.Errorf("opencode serve did not become ready after 30s")
}

func (d *CloudDaemon) discoverSessionID(ctx context.Context, provider SandboxProvider, sb *Sandbox, workDir string, log *slog.Logger) (string, error) {
	// Wait for the session to appear, matched by the working directory
	for i := 0; i < 15; i++ {
		time.Sleep(2 * time.Second)
		stdout, err := provider.Exec(ctx, sb, []string{"curl", "-s", "http://localhost:4096/session"})
		if err != nil {
			log.Warn("cloud daemon: session discovery exec error", "attempt", i, "error", err)
			continue
		}

		var sessions []struct {
			ID        string `json:"id"`
			Directory string `json:"directory"`
		}
		if err := json.Unmarshal([]byte(stdout), &sessions); err != nil {
			log.Warn("cloud daemon: session discovery parse error", "attempt", i, "error", err)
			continue
		}

		// Match by directory (the workDir we passed to opencode run --attach --dir)
		for _, s := range sessions {
			if s.Directory == workDir {
				return s.ID, nil
			}
		}
		log.Debug("cloud daemon: session not found yet", "attempt", i, "sessions", len(sessions), "workDir", workDir)
	}
	return "", fmt.Errorf("no session with directory %s found after 30s", workDir)
}

func (d *CloudDaemon) loadSandboxConfig(ctx context.Context, configID pgtype.UUID) (*db.WorkspaceSandboxConfig, string, string, error) {
	cfg, err := d.queries.GetSandboxConfigByID(ctx, configID)
	if err != nil {
		return nil, "", "", fmt.Errorf("get sandbox config: %w", err)
	}

	gitPat := ""
	if cfg.GitPat.Valid {
		var err error
		gitPat, err = d.decryptField(cfg.GitPat.String, "git-pat")
		if err != nil {
			return nil, "", "", fmt.Errorf("decrypt git pat: %w", err)
		}
	}

	aiGatewayKey := ""
	if cfg.AiGatewayApiKey.Valid {
		var err error
		aiGatewayKey, err = d.decryptField(cfg.AiGatewayApiKey.String, "ai-gateway-api-key")
		if err != nil {
			return nil, "", "", fmt.Errorf("decrypt ai gateway key: %w", err)
		}
	}

	return &cfg, gitPat, aiGatewayKey, nil
}

func (d *CloudDaemon) decryptField(ciphertext, purpose string) (string, error) {
	derived, err := crypto.DeriveKey(d.encryptionKey, purpose)
	if err != nil {
		return "", err
	}
	return crypto.Decrypt(ciphertext, derived)
}

// extractLastAssistantText parses the session message JSON and returns the
// last non-empty text part from assistant messages. Used as fallback when
// the agent doesn't write SUMMARY.md.
func extractLastAssistantText(raw string) string {
	var messages []struct {
		Info struct {
			Role string `json:"role"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	if err := json.Unmarshal([]byte(raw), &messages); err != nil {
		return ""
	}
	lastText := ""
	for _, msg := range messages {
		if msg.Info.Role != "assistant" {
			continue
		}
		for _, part := range msg.Parts {
			if part.Type == "text" && part.Text != "" {
				lastText = part.Text
			}
		}
	}
	return lastText
}

// handlePing tests sandbox connectivity by creating a short-lived sandbox,
// running `echo pong`, and reporting the result.
func (d *CloudDaemon) handlePing(ctx context.Context, runtime db.AgentRuntime, pingID string) {
	log := slog.With("runtime_id", util.UUIDToString(runtime.ID), "ping_id", pingID)
	log.Info("cloud daemon: ping requested")

	start := time.Now()

	if !runtime.SandboxConfigID.Valid {
		d.pingChecker.Fail(pingID, "runtime has no linked sandbox config", time.Since(start).Milliseconds())
		return
	}
	sandboxCfg, _, _, err := d.loadSandboxConfig(ctx, runtime.SandboxConfigID)
	if err != nil {
		d.pingChecker.Fail(pingID, fmt.Sprintf("sandbox config error: %v", err), time.Since(start).Milliseconds())
		return
	}

	providerKey, err := d.decryptField(sandboxCfg.ProviderApiKey, "provider-api-key")
	if err != nil {
		d.pingChecker.Fail(pingID, fmt.Sprintf("provider key error: %v", err), time.Since(start).Milliseconds())
		return
	}

	provider := NewE2BProvider(providerKey)

	sb, err := provider.CreateOrConnect(ctx, "", CreateOpts{
		TemplateID: textToString(sandboxCfg.TemplateID),
		Timeout:    2 * time.Minute,
	})
	if err != nil {
		d.pingChecker.Fail(pingID, fmt.Sprintf("sandbox creation failed: %v", err), time.Since(start).Milliseconds())
		return
	}

	defer func() {
		if destroyErr := provider.Destroy(ctx, sb.ID); destroyErr != nil {
			log.Warn("cloud daemon: ping sandbox cleanup failed", "error", destroyErr)
		}
	}()

	stdout, err := provider.Exec(ctx, sb, []string{"echo", "pong"})
	if err != nil {
		d.pingChecker.Fail(pingID, fmt.Sprintf("exec failed: %v", err), time.Since(start).Milliseconds())
		return
	}

	d.pingChecker.Complete(pingID, strings.TrimSpace(stdout), time.Since(start).Milliseconds())
	log.Info("cloud daemon: ping completed", "duration_ms", time.Since(start).Milliseconds())
}

func textToString(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}
