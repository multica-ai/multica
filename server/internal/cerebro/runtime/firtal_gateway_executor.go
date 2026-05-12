package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/pricing"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

const (
	firtalGatewayServerDaemonID  = "server:firtal-gateway"
	firtalGatewayIssueCommentCap = 200
)

type FirtalGatewayExecutor struct {
	cfg           FirtalGatewayRuntimeConfig
	queries       *db.Queries
	cerebro       *cerebrodb.Queries
	taskSvc       *service.TaskService
	budgetSvc     *service.BudgetService
	bus           *events.Bus
	gateway       *GatewayClient
	sem           chan struct{}
	logger        *slog.Logger
	publishedOnce map[string]struct{}
}

func NewFirtalGatewayExecutor(
	cfg FirtalGatewayRuntimeConfig,
	queries *db.Queries,
	cerebroQueries *cerebrodb.Queries,
	taskSvc *service.TaskService,
	bus *events.Bus,
	gateway *GatewayClient,
	logger *slog.Logger,
) *FirtalGatewayExecutor {
	cfg = withFirtalGatewayDefaults(cfg)
	if logger == nil {
		logger = slog.Default()
	}
	return &FirtalGatewayExecutor{
		cfg:           cfg,
		queries:       queries,
		cerebro:       cerebroQueries,
		taskSvc:       taskSvc,
		budgetSvc:     service.NewBudgetService(queries),
		bus:           bus,
		gateway:       gateway,
		sem:           make(chan struct{}, cfg.MaxConcurrency),
		logger:        logger,
		publishedOnce: map[string]struct{}{},
	}
}

func (e *FirtalGatewayExecutor) Run(ctx context.Context) {
	if e == nil || !e.cfg.Enabled {
		return
	}
	e.logger.Info("firtal gateway server runtime starting",
		"provider", FirtalGatewayProvider,
		"poll_interval", e.cfg.PollInterval.String(),
		"sync_interval", e.cfg.SyncInterval.String(),
		"max_concurrency", e.cfg.MaxConcurrency,
	)

	e.syncRuntimes(ctx)
	e.dispatchAvailable(ctx)

	pollTicker := time.NewTicker(e.cfg.PollInterval)
	defer pollTicker.Stop()
	syncTicker := time.NewTicker(e.cfg.SyncInterval)
	defer syncTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.logger.Info("firtal gateway server runtime stopped")
			return
		case <-syncTicker.C:
			e.syncRuntimes(ctx)
		case <-pollTicker.C:
			e.dispatchAvailable(ctx)
		}
	}
}

func (e *FirtalGatewayExecutor) syncRuntimes(ctx context.Context) {
	workspaces, err := e.workspaceConfigs(ctx)
	if err != nil {
		e.logger.Warn("firtal gateway runtime workspace sync failed", "error", err)
		return
	}
	if len(workspaces) == 0 {
		return
	}

	metadata, _ := json.Marshal(map[string]any{
		"managed_by":   "multica-server",
		"runtime_kind": "cloud",
		"supports":     []string{"chat", "issue", "autopilot_run_only"},
	})
	for _, workspace := range workspaces {
		workspaceID := workspace.ID
		cfg, ok, err := FirtalGatewayConfigFromWorkspaceSettings(workspace.Settings, e.cfg)
		if err != nil {
			e.logger.Warn("firtal gateway workspace config invalid", "workspace_id", util.UUIDToString(workspaceID), "error", err)
			e.setRuntimeOffline(ctx, workspaceID)
			continue
		}
		if !ok {
			e.setRuntimeOffline(ctx, workspaceID)
			continue
		}
		registered, err := e.queries.UpsertAgentRuntime(ctx, db.UpsertAgentRuntimeParams{
			WorkspaceID: workspaceID,
			DaemonID:    pgtype.Text{String: firtalGatewayServerDaemonID, Valid: true},
			Name:        cfg.RuntimeName,
			RuntimeMode: "cloud",
			Provider:    FirtalGatewayProvider,
			Status:      "online",
			DeviceInfo:  "Multica server HTTPS runtime",
			Metadata:    metadata,
			OwnerID:     pgtype.UUID{},
		})
		if err != nil {
			e.logger.Warn("firtal gateway runtime registration failed", "workspace_id", util.UUIDToString(workspaceID), "error", err)
			continue
		}
		if registered.Inserted {
			e.publishRuntimeRegistered(util.UUIDToString(workspaceID), util.UUIDToString(registered.ID))
		}
	}
}

type firtalGatewayWorkspaceConfig struct {
	ID       pgtype.UUID
	Settings []byte
}

func (e *FirtalGatewayExecutor) workspaceConfigs(ctx context.Context) ([]firtalGatewayWorkspaceConfig, error) {
	if len(e.cfg.WorkspaceIDs) > 0 {
		rows, err := e.cerebro.ListFirtalGatewayWorkspaceConfigsByID(ctx, e.cfg.WorkspaceIDs)
		if err != nil {
			return nil, err
		}
		out := make([]firtalGatewayWorkspaceConfig, 0, len(rows))
		for _, row := range rows {
			out = append(out, firtalGatewayWorkspaceConfig{ID: row.ID, Settings: row.Settings})
		}
		return out, nil
	}
	rows, err := e.cerebro.ListFirtalGatewayWorkspaceConfigs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]firtalGatewayWorkspaceConfig, 0, len(rows))
	for _, row := range rows {
		out = append(out, firtalGatewayWorkspaceConfig{ID: row.ID, Settings: row.Settings})
	}
	return out, nil
}

func (e *FirtalGatewayExecutor) setRuntimeOffline(ctx context.Context, workspaceID pgtype.UUID) {
	if err := e.cerebro.SetFirtalGatewayRuntimeOfflineByWorkspace(ctx, cerebrodb.SetFirtalGatewayRuntimeOfflineByWorkspaceParams{
		WorkspaceID: workspaceID,
		Provider:    FirtalGatewayProvider,
		DaemonID:    pgtype.Text{String: firtalGatewayServerDaemonID, Valid: true},
	}); err != nil {
		e.logger.Warn("firtal gateway runtime offline update failed", "workspace_id", util.UUIDToString(workspaceID), "error", err)
	}
}

func (e *FirtalGatewayExecutor) dispatchAvailable(ctx context.Context) {
	runtimes, err := e.cerebro.ListFirtalGatewayRuntimes(ctx, cerebrodb.ListFirtalGatewayRuntimesParams{
		Provider: FirtalGatewayProvider,
		DaemonID: pgtype.Text{String: firtalGatewayServerDaemonID, Valid: true},
	})
	if err != nil {
		e.logger.Warn("firtal gateway runtime list failed", "error", err)
		return
	}

	for _, rt := range runtimes {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if !e.tryAcquire() {
			return
		}
		if _, ok, err := e.effectiveWorkspaceConfig(ctx, rt.WorkspaceID); err != nil {
			e.release()
			e.logger.Warn("firtal gateway workspace config load failed", "workspace_id", util.UUIDToString(rt.WorkspaceID), "error", err)
			continue
		} else if !ok {
			e.release()
			e.setRuntimeOffline(ctx, rt.WorkspaceID)
			continue
		}
		claimed, err := e.cerebro.ClaimFirtalGatewayTask(ctx, rt.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			e.release()
			continue
		}
		if err != nil {
			e.release()
			e.logger.Warn("firtal gateway task claim failed", "runtime_id", util.UUIDToString(rt.ID), "error", err)
			continue
		}
		task := toDBAgentTaskQueue(claimed)
		e.publishTaskDispatch(ctx, task)
		e.taskSvc.ReconcileAgentStatus(ctx, task.AgentID)
		go func() {
			defer e.release()
			e.executeTask(ctx, task)
		}()
	}
}

func (e *FirtalGatewayExecutor) executeTask(parent context.Context, task db.AgentTaskQueue) {
	taskID := util.UUIDToString(task.ID)
	started, err := e.taskSvc.StartTask(parent, task.ID)
	if err != nil {
		e.logger.Warn("firtal gateway task start failed", "task_id", taskID, "error", err)
		return
	}
	task = *started

	agent, err := e.queries.GetAgent(parent, task.AgentID)
	if err != nil {
		e.failTask(parent, task, "failed to load agent: "+err.Error(), "agent_error")
		return
	}

	plan, err := e.buildTaskPlan(parent, task, agent)
	if err != nil {
		e.failTask(parent, task, err.Error(), "agent_error")
		return
	}

	cfg, ok, err := e.effectiveWorkspaceConfig(parent, plan.workspaceID)
	if err != nil {
		e.failTask(parent, task, "failed to load firtal gateway settings: "+err.Error(), "agent_error")
		return
	}
	if !ok {
		e.failTask(parent, task, "firtal gateway runtime is not configured for this workspace", "agent_error")
		return
	}
	if e.budgetSvc != nil {
		decision := e.budgetSvc.CheckPreClaim(parent, plan.workspaceID, task.AgentID)
		if !decision.Allowed {
			e.failTask(parent, task, decision.Reason, "budget_blocked")
			return
		}
	}

	messages := plan.messagesWithLimit(cfg.HistoryLimit)
	if !hasUserGatewayMessage(messages) {
		e.failTask(parent, task, plan.emptyMessageError, "agent_error")
		return
	}

	runCtx, cancel := context.WithTimeout(parent, cfg.TaskTimeout)
	defer cancel()
	completion, err := e.completeGateway(runCtx, cfg, agent.Model.String, messages, GatewayRequestMeta{
		TaskID:      taskID,
		AgentID:     util.UUIDToString(task.AgentID),
		WorkspaceID: util.UUIDToString(plan.workspaceID),
	})
	if err != nil {
		e.failTask(parent, task, err.Error(), "agent_error")
		return
	}

	finalCtx, finalCancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer finalCancel()
	e.recordTaskMessage(finalCtx, task, completion.Output)
	e.recordTaskUsage(finalCtx, task, plan.workspaceID, completion)

	result, _ := json.Marshal(protocol.TaskCompletedPayload{
		TaskID: taskID,
		Output: completion.Output,
	})
	if _, err := e.taskSvc.CompleteTask(finalCtx, task.ID, result, "", ""); err != nil {
		e.logger.Warn("firtal gateway task complete failed", "task_id", taskID, "error", err)
	}
}

// taskPlan captures everything the gateway needs to fulfil a task: the
// workspace it belongs to (for config + budget + usage rollup), a function
// that produces the message list given the history limit, and the error
// message to surface when the resulting message list contains no user turn.
type taskPlan struct {
	workspaceID       pgtype.UUID
	messagesWithLimit func(limit int) []GatewayMessage
	emptyMessageError string
}

func (e *FirtalGatewayExecutor) buildTaskPlan(ctx context.Context, task db.AgentTaskQueue, agent db.Agent) (taskPlan, error) {
	switch {
	case task.ChatSessionID.Valid:
		chatSession, err := e.queries.GetChatSession(ctx, task.ChatSessionID)
		if err != nil {
			return taskPlan{}, fmt.Errorf("failed to load chat session: %w", err)
		}
		history, err := e.queries.ListChatMessages(ctx, task.ChatSessionID)
		if err != nil {
			return taskPlan{}, fmt.Errorf("failed to load chat history: %w", err)
		}
		return taskPlan{
			workspaceID: chatSession.WorkspaceID,
			messagesWithLimit: func(limit int) []GatewayMessage {
				return buildGatewayMessages(agent, history, task.StartedAt, limit)
			},
			emptyMessageError: "chat task has no user message to answer",
		}, nil

	case task.IssueID.Valid:
		issue, err := e.queries.GetIssue(ctx, task.IssueID)
		if err != nil {
			return taskPlan{}, fmt.Errorf("failed to load issue: %w", err)
		}
		comments, err := e.queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
			Limit:       firtalGatewayIssueCommentCap,
		})
		if err != nil {
			return taskPlan{}, fmt.Errorf("failed to load issue comments: %w", err)
		}
		var triggerComment *db.Comment
		if task.TriggerCommentID.Valid {
			c, err := e.queries.GetComment(ctx, task.TriggerCommentID)
			if err == nil {
				triggerComment = &c
			}
		}
		return taskPlan{
			workspaceID: issue.WorkspaceID,
			messagesWithLimit: func(limit int) []GatewayMessage {
				return buildGatewayIssueMessages(agent, issue, comments, triggerComment, task.StartedAt, limit)
			},
			emptyMessageError: "issue task has no user message to answer",
		}, nil

	case task.AutopilotRunID.Valid:
		run, err := e.queries.GetAutopilotRun(ctx, task.AutopilotRunID)
		if err != nil {
			return taskPlan{}, fmt.Errorf("failed to load autopilot run: %w", err)
		}
		ap, err := e.queries.GetAutopilot(ctx, run.AutopilotID)
		if err != nil {
			return taskPlan{}, fmt.Errorf("failed to load autopilot: %w", err)
		}
		return taskPlan{
			workspaceID: ap.WorkspaceID,
			messagesWithLimit: func(_ int) []GatewayMessage {
				return buildGatewayAutopilotMessages(agent, ap)
			},
			emptyMessageError: "autopilot run has no prompt to answer",
		}, nil

	default:
		return taskPlan{}, fmt.Errorf("firtal gateway runtime cannot fulfil this task kind (no chat, issue, or autopilot link)")
	}
}

func (e *FirtalGatewayExecutor) effectiveWorkspaceConfig(ctx context.Context, workspaceID pgtype.UUID) (FirtalGatewayRuntimeConfig, bool, error) {
	ws, err := e.queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return FirtalGatewayRuntimeConfig{}, false, err
	}
	return FirtalGatewayConfigFromWorkspaceSettings(ws.Settings, e.cfg)
}

func (e *FirtalGatewayExecutor) completeGateway(ctx context.Context, cfg FirtalGatewayRuntimeConfig, model string, messages []GatewayMessage, meta GatewayRequestMeta) (GatewayCompletion, error) {
	if e.gateway != nil {
		return e.gateway.Complete(ctx, model, messages, meta)
	}
	return NewGatewayClient(cfg, nil).Complete(ctx, model, messages, meta)
}

func buildGatewayMessages(agent db.Agent, history []db.ChatMessage, startedAt pgtype.Timestamptz, limit int) []GatewayMessage {
	if limit <= 0 {
		limit = defaultFirtalGatewayHistoryLimit
	}
	out := []GatewayMessage{{Role: "system", Content: buildGatewaySystemPrompt(agent)}}
	chatMessages := make([]GatewayMessage, 0, len(history))
	for _, m := range history {
		if startedAt.Valid && m.CreatedAt.Valid && m.CreatedAt.Time.After(startedAt.Time) {
			continue
		}
		role := strings.TrimSpace(m.Role)
		if role != "user" && role != "assistant" {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		chatMessages = append(chatMessages, GatewayMessage{Role: role, Content: content})
	}
	if len(chatMessages) > limit {
		chatMessages = chatMessages[len(chatMessages)-limit:]
	}
	return append(out, chatMessages...)
}

func buildGatewaySystemPrompt(agent db.Agent) string {
	parts := []string{
		"You are a Multica chat agent running on the server-side Firtal Data Registry AI Gateway runtime.",
		"You can answer chat messages only. You do not have local tools, shell access, repository checkout, files, or daemon state.",
		"Answer directly in the existing chat conversation.",
	}
	if instructions := strings.TrimSpace(agent.Instructions); instructions != "" {
		parts = append(parts, "Agent instructions:\n"+instructions)
	}
	return strings.Join(parts, "\n\n")
}

// buildGatewayIssueSystemPrompt is the issue-task variant of the chat system
// prompt. The model's text reply is written back to the issue as a comment
// via TaskService.CompleteTask, so we frame the task accordingly and remind
// the model it has no tools — it can't fetch repos, run shell, or call CLI
// commands and must answer only from the issue context provided.
func buildGatewayIssueSystemPrompt(agent db.Agent) string {
	parts := []string{
		"You are a Multica agent running on the server-side Firtal Data Registry AI Gateway runtime.",
		"You have been asked to respond to an issue. You do not have local tools, shell access, repository checkout, files, or CLI commands. You cannot run code, edit files, or call multica subcommands.",
		"Your reply will be posted as a comment on the issue. Answer concisely and directly from the issue context provided below.",
	}
	if instructions := strings.TrimSpace(agent.Instructions); instructions != "" {
		parts = append(parts, "Agent instructions:\n"+instructions)
	}
	return strings.Join(parts, "\n\n")
}

// buildGatewayIssueMessages renders an issue task into a chat-completion
// transcript. The issue title + description becomes the opening user message;
// each comment in chronological order becomes a "user" or "assistant" turn
// depending on whether it was authored by this agent itself. When the task
// was triggered by a specific comment, that comment is the final user turn —
// so the model treats it as the message to answer rather than reacting to
// older history. limit caps the transcript at the most recent N entries
// (preserving the opening issue message + the trigger comment when set).
func buildGatewayIssueMessages(agent db.Agent, issue db.Issue, comments []db.Comment, triggerComment *db.Comment, startedAt pgtype.Timestamptz, limit int) []GatewayMessage {
	if limit <= 0 {
		limit = defaultFirtalGatewayHistoryLimit
	}
	out := []GatewayMessage{{Role: "system", Content: buildGatewayIssueSystemPrompt(agent)}}
	out = append(out, GatewayMessage{Role: "user", Content: formatIssueOpening(issue)})

	transcript := make([]GatewayMessage, 0, len(comments))
	for _, c := range comments {
		// Skip comments created after this task started — they belong to a
		// later turn and including them would confuse the model into answering
		// in the past tense.
		if startedAt.Valid && c.CreatedAt.Valid && c.CreatedAt.Time.After(startedAt.Time) {
			continue
		}
		// Skip the trigger comment in the transcript; we re-append it as the
		// final user turn so the model has a clear "answer this" cue.
		if triggerComment != nil && c.ID == triggerComment.ID {
			continue
		}
		content := strings.TrimSpace(c.Content)
		if content == "" {
			continue
		}
		role := "user"
		if c.AuthorType == "agent" && c.AuthorID == agent.ID {
			role = "assistant"
		}
		transcript = append(transcript, GatewayMessage{Role: role, Content: content})
	}
	if len(transcript) > limit {
		transcript = transcript[len(transcript)-limit:]
	}
	out = append(out, transcript...)

	if triggerComment != nil {
		if content := strings.TrimSpace(triggerComment.Content); content != "" {
			out = append(out, GatewayMessage{Role: "user", Content: content})
		}
	}
	return out
}

func formatIssueOpening(issue db.Issue) string {
	var b strings.Builder
	b.WriteString("Issue: ")
	if issue.Title != "" {
		b.WriteString(issue.Title)
	} else {
		b.WriteString("(no title)")
	}
	if desc := strings.TrimSpace(issue.Description.String); desc != "" {
		b.WriteString("\n\n")
		b.WriteString(desc)
	}
	return b.String()
}

// buildGatewayAutopilotMessages renders an autopilot run-only task as a
// single user turn. Run-only autopilots have no issue, no chat, and no
// comment history — only the autopilot's title and description carry the
// instruction, and the model's text reply is written to autopilot_run.result.
func buildGatewayAutopilotMessages(agent db.Agent, ap db.Autopilot) []GatewayMessage {
	systemParts := []string{
		"You are a Multica agent running on the server-side Firtal Data Registry AI Gateway runtime.",
		"You have been triggered by an autopilot. You do not have local tools, shell access, repository checkout, files, or CLI commands. Answer directly with the requested output as text.",
	}
	if instructions := strings.TrimSpace(agent.Instructions); instructions != "" {
		systemParts = append(systemParts, "Agent instructions:\n"+instructions)
	}

	var userParts []string
	if title := strings.TrimSpace(ap.Title); title != "" {
		userParts = append(userParts, "Autopilot: "+title)
	}
	if desc := strings.TrimSpace(ap.Description.String); desc != "" {
		userParts = append(userParts, desc)
	}
	user := strings.Join(userParts, "\n\n")
	if user == "" {
		// Defensive fallback: an autopilot with neither title nor description
		// shouldn't realistically reach here, but if it does, give the model
		// something to answer so hasUserGatewayMessage doesn't trip.
		user = "Respond to the autopilot trigger."
	}
	return []GatewayMessage{
		{Role: "system", Content: strings.Join(systemParts, "\n\n")},
		{Role: "user", Content: user},
	}
}

func hasUserGatewayMessage(messages []GatewayMessage) bool {
	for _, m := range messages {
		if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			return true
		}
	}
	return false
}

func (e *FirtalGatewayExecutor) recordTaskUsage(ctx context.Context, task db.AgentTaskQueue, workspaceID pgtype.UUID, completion GatewayCompletion) {
	usage := completion.Usage
	if err := e.queries.UpsertTaskUsage(ctx, db.UpsertTaskUsageParams{
		TaskID:           task.ID,
		Provider:         FirtalGatewayProvider,
		Model:            completion.Model,
		InputTokens:      usage.InputTokens,
		OutputTokens:     usage.OutputTokens,
		CacheReadTokens:  usage.CacheReadTokens,
		CacheWriteTokens: usage.CacheWriteTokens,
	}); err != nil {
		e.logger.Warn("firtal gateway task usage upsert failed", "task_id", util.UUIDToString(task.ID), "model", completion.Model, "error", err)
	}

	cents := usage.CostCents
	if cents <= 0 {
		cents = pricing.ComputeCents(completion.Model, pricing.Usage{
			InputTokens:      usage.InputTokens,
			OutputTokens:     usage.OutputTokens,
			CacheReadTokens:  usage.CacheReadTokens,
			CacheWriteTokens: usage.CacheWriteTokens,
		})
	}
	if cents <= 0 || !workspaceID.Valid {
		return
	}

	now := time.Now().UTC()
	dayStart := pgtype.Date{Time: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
	monthStart := pgtype.Date{Time: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC), Valid: true}
	rollups := []db.IncrementBudgetStateParams{
		{WorkspaceID: workspaceID, ScopeType: "agent", ScopeID: task.AgentID, WindowType: "day", WindowStart: dayStart, CentsSpent: cents},
		{WorkspaceID: workspaceID, ScopeType: "agent", ScopeID: task.AgentID, WindowType: "month", WindowStart: monthStart, CentsSpent: cents},
		{WorkspaceID: workspaceID, ScopeType: "workspace", ScopeID: workspaceID, WindowType: "day", WindowStart: dayStart, CentsSpent: cents},
		{WorkspaceID: workspaceID, ScopeType: "workspace", ScopeID: workspaceID, WindowType: "month", WindowStart: monthStart, CentsSpent: cents},
	}
	for _, p := range rollups {
		if err := e.queries.IncrementBudgetState(ctx, p); err != nil {
			e.logger.Warn("firtal gateway budget rollup failed",
				"workspace_id", util.UUIDToString(workspaceID),
				"scope_type", p.ScopeType,
				"window_type", p.WindowType,
				"cents", cents,
				"error", err,
			)
		}
	}
}

func (e *FirtalGatewayExecutor) recordTaskMessage(ctx context.Context, task db.AgentTaskQueue, output string) {
	output = redact.Text(output)
	if strings.TrimSpace(output) == "" {
		return
	}
	taskID := util.UUIDToString(task.ID)
	if _, err := e.queries.CreateTaskMessage(ctx, db.CreateTaskMessageParams{
		TaskID:  task.ID,
		Seq:     1,
		Type:    "text",
		Content: pgtype.Text{String: output, Valid: true},
	}); err != nil {
		e.logger.Warn("firtal gateway task message persist failed", "task_id", taskID, "error", err)
	}
	workspaceID := e.taskSvc.ResolveTaskWorkspaceID(ctx, task)
	if workspaceID == "" || e.bus == nil {
		return
	}
	e.bus.Publish(events.Event{
		Type:        protocol.EventTaskMessage,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload: protocol.TaskMessagePayload{
			TaskID:  taskID,
			Seq:     1,
			Type:    "text",
			Content: output,
		},
		TaskID: taskID,
	})
}

func (e *FirtalGatewayExecutor) failTask(parent context.Context, task db.AgentTaskQueue, errMsg, reason string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer cancel()
	if _, err := e.taskSvc.FailTask(ctx, task.ID, errMsg, "", "", reason); err != nil {
		e.logger.Warn("firtal gateway task fail failed", "task_id", util.UUIDToString(task.ID), "error", err)
	}
}

func (e *FirtalGatewayExecutor) publishRuntimeRegistered(workspaceID, runtimeID string) {
	if e.bus == nil {
		return
	}
	if _, ok := e.publishedOnce[workspaceID+":"+runtimeID]; ok {
		return
	}
	e.publishedOnce[workspaceID+":"+runtimeID] = struct{}{}
	e.bus.Publish(events.Event{
		Type:        protocol.EventDaemonRegister,
		WorkspaceID: workspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"runtime_id": runtimeID,
			"provider":   FirtalGatewayProvider,
		},
	})
}

func (e *FirtalGatewayExecutor) publishTaskDispatch(ctx context.Context, task db.AgentTaskQueue) {
	if e.bus == nil {
		return
	}
	workspaceID := e.taskSvc.ResolveTaskWorkspaceID(ctx, task)
	if workspaceID == "" {
		return
	}
	taskID := util.UUIDToString(task.ID)
	payload := map[string]any{
		"task_id":         taskID,
		"runtime_id":      util.UUIDToString(task.RuntimeID),
		"issue_id":        util.UUIDToString(task.IssueID),
		"agent_id":        util.UUIDToString(task.AgentID),
		"chat_session_id": util.UUIDToString(task.ChatSessionID),
	}
	e.bus.Publish(events.Event{
		Type:          protocol.EventTaskDispatch,
		WorkspaceID:   workspaceID,
		ActorType:     "system",
		Payload:       payload,
		TaskID:        taskID,
		ChatSessionID: payload["chat_session_id"].(string),
	})
}

func (e *FirtalGatewayExecutor) tryAcquire() bool {
	select {
	case e.sem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (e *FirtalGatewayExecutor) release() {
	<-e.sem
}

func toDBAgentTaskQueue(t cerebrodb.AgentTaskQueue) db.AgentTaskQueue {
	return db.AgentTaskQueue{
		ID:                t.ID,
		AgentID:           t.AgentID,
		IssueID:           t.IssueID,
		Status:            t.Status,
		Priority:          t.Priority,
		DispatchedAt:      t.DispatchedAt,
		StartedAt:         t.StartedAt,
		CompletedAt:       t.CompletedAt,
		Result:            t.Result,
		Error:             t.Error,
		CreatedAt:         t.CreatedAt,
		Context:           t.Context,
		RuntimeID:         t.RuntimeID,
		SessionID:         t.SessionID,
		WorkDir:           t.WorkDir,
		TriggerCommentID:  t.TriggerCommentID,
		ChatSessionID:     t.ChatSessionID,
		AutopilotRunID:    t.AutopilotRunID,
		Attempt:           t.Attempt,
		MaxAttempts:       t.MaxAttempts,
		ParentTaskID:      t.ParentTaskID,
		FailureReason:     t.FailureReason,
		TriggerSummary:    t.TriggerSummary,
		ForceFreshSession: t.ForceFreshSession,
		Title:             t.Title,
	}
}
