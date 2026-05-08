package runtime

import (
	"context"
	"encoding/json"
	"errors"
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

const firtalGatewayServerDaemonID = "server:firtal-gateway"

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
	if gateway == nil {
		gateway = NewGatewayClient(cfg, nil)
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
	workspaceIDs, err := e.workspaceIDs(ctx)
	if err != nil {
		e.logger.Warn("firtal gateway runtime workspace sync failed", "error", err)
		return
	}
	if len(workspaceIDs) == 0 {
		return
	}

	metadata, _ := json.Marshal(map[string]any{
		"managed_by": "multica-server",
		"chat_only":  true,
	})
	for _, workspaceID := range workspaceIDs {
		registered, err := e.queries.UpsertAgentRuntime(ctx, db.UpsertAgentRuntimeParams{
			WorkspaceID: workspaceID,
			DaemonID:    pgtype.Text{String: firtalGatewayServerDaemonID, Valid: true},
			Name:        e.cfg.RuntimeName,
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

func (e *FirtalGatewayExecutor) workspaceIDs(ctx context.Context) ([]pgtype.UUID, error) {
	if len(e.cfg.WorkspaceIDs) > 0 {
		return e.cerebro.ListFirtalGatewayWorkspaceIDsByID(ctx, e.cfg.WorkspaceIDs)
	}
	return e.cerebro.ListFirtalGatewayWorkspaceIDs(ctx)
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
		claimed, err := e.cerebro.ClaimFirtalGatewayChatTask(ctx, rt.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			e.release()
			continue
		}
		if err != nil {
			e.release()
			e.logger.Warn("firtal gateway chat task claim failed", "runtime_id", util.UUIDToString(rt.ID), "error", err)
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
	if !task.ChatSessionID.Valid {
		e.failTask(parent, task, "firtal gateway server runtime only supports chat tasks", "unsupported_task")
		return
	}
	chatSession, err := e.queries.GetChatSession(parent, task.ChatSessionID)
	if err != nil {
		e.failTask(parent, task, "failed to load chat session: "+err.Error(), "agent_error")
		return
	}
	if e.budgetSvc != nil {
		decision := e.budgetSvc.CheckPreClaim(parent, chatSession.WorkspaceID, task.AgentID)
		if !decision.Allowed {
			e.failTask(parent, task, decision.Reason, "budget_blocked")
			return
		}
	}

	history, err := e.queries.ListChatMessages(parent, task.ChatSessionID)
	if err != nil {
		e.failTask(parent, task, "failed to load chat history: "+err.Error(), "agent_error")
		return
	}
	messages := buildGatewayMessages(agent, history, task.StartedAt, e.cfg.HistoryLimit)
	if !hasUserGatewayMessage(messages) {
		e.failTask(parent, task, "chat task has no user message to answer", "agent_error")
		return
	}

	runCtx, cancel := context.WithTimeout(parent, e.cfg.TaskTimeout)
	defer cancel()
	completion, err := e.gateway.Complete(runCtx, agent.Model.String, messages, GatewayRequestMeta{
		TaskID:      taskID,
		AgentID:     util.UUIDToString(task.AgentID),
		WorkspaceID: util.UUIDToString(chatSession.WorkspaceID),
	})
	if err != nil {
		e.failTask(parent, task, err.Error(), "agent_error")
		return
	}

	finalCtx, finalCancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer finalCancel()
	e.recordTaskMessage(finalCtx, task, completion.Output)
	e.recordTaskUsage(finalCtx, task, chatSession.WorkspaceID, completion)

	result, _ := json.Marshal(protocol.TaskCompletedPayload{
		TaskID: taskID,
		Output: completion.Output,
	})
	if _, err := e.taskSvc.CompleteTask(finalCtx, task.ID, result, "", ""); err != nil {
		e.logger.Warn("firtal gateway task complete failed", "task_id", taskID, "error", err)
	}
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
		LastHeartbeatAt:   t.LastHeartbeatAt,
		TriggerSummary:    t.TriggerSummary,
		ForceFreshSession: t.ForceFreshSession,
		Title:             t.Title,
	}
}
