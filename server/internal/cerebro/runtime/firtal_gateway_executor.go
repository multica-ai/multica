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
	"github.com/multica-ai/multica/server/internal/mcp"
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
	registry      *Registry
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
	registry ...*Registry,
) *FirtalGatewayExecutor {
	cfg = withFirtalGatewayDefaults(cfg)
	if logger == nil {
		logger = slog.Default()
	}
	var reg *Registry
	if len(registry) > 0 {
		reg = registry[0]
	}
	return &FirtalGatewayExecutor{
		cfg:           cfg,
		queries:       queries,
		cerebro:       cerebroQueries,
		taskSvc:       taskSvc,
		budgetSvc:     service.NewBudgetService(queries),
		bus:           bus,
		gateway:       gateway,
		registry:      reg,
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
	meta := GatewayRequestMeta{
		TaskID:      taskID,
		AgentID:     util.UUIDToString(task.AgentID),
		WorkspaceID: util.UUIDToString(plan.workspaceID),
	}
	var completion GatewayCompletion
	if cfg.ToolsEnabledForAgent(task.AgentID) {
		completion, err = e.runToolLoop(runCtx, cfg, agent, messages, meta, task.AgentID, plan.workspaceID, task.OriginalUserID)
	} else {
		completion, err = e.completeGateway(runCtx, cfg, agent.Model.String, messages, meta)
	}
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

func (e *FirtalGatewayExecutor) completeGatewayWithTools(ctx context.Context, cfg FirtalGatewayRuntimeConfig, model string, messages []GatewayMessage, tools []GatewayToolDef, meta GatewayRequestMeta) (GatewayCompletion, error) {
	if e.gateway != nil {
		return e.gateway.CompleteWithTools(ctx, model, messages, tools, meta)
	}
	return NewGatewayClient(cfg, nil).CompleteWithTools(ctx, model, messages, tools, meta)
}

// runToolLoop runs the model in an Anthropic-native tool-call loop. Up to
// firtalGatewayMaxToolRounds (or cfg.MaxToolRounds when set) rounds dispatch
// tools; each round sends the running transcript plus tool definitions, and if
// the model emits tool_use blocks they are dispatched in-process against the
// registry or the MCP server, results are appended, and the next round runs.
// The first round without tool calls becomes the final output. Budget is
// checked before each tool dispatch. Usage is summed across all rounds.
func (e *FirtalGatewayExecutor) runToolLoop(ctx context.Context, cfg FirtalGatewayRuntimeConfig, agent db.Agent, initialMessages []GatewayMessage, meta GatewayRequestMeta, agentID, workspaceID, originalUserID pgtype.UUID) (GatewayCompletion, error) {
	tctx := ToolContext{AgentID: agentID, WorkspaceID: workspaceID}

	// Determine tools: registry-backed when available, else fall back to the
	// hardcoded MCP server (POC compatibility).
	var (
		anthropicTools []AnthropicTool
		enabledTools   []Tool
		toolSrv        *mcp.Server
		activeRegistry *Registry
		useRegistry    bool
	)
	if e.registry != nil {
		taskRegistry := NewDefaultRegistry(nil, e.queries, tctx, e.cerebro) // CEREBRO-PATCH(firtal-gateway-cerebro-tools): wire concrete Cerebro-family handlers into per-task registries.
		taskRegistry.db = e.registry.db
		// JEH-1710 bid 5: enforce the cerebro_runtime_tool cascade (runtime
		// enable + user/group grants + agent override) when the new grant
		// system is configured for this runtime; otherwise fall back to the
		// legacy agent_tool_grant path. originalUserID carries the user who
		// triggered the task (chat message, comment, or autopilot manual run)
		// so the cascade can apply user/group access rules.
		enabledTools = taskRegistry.GetCascadeEnabledToolsForAgent(ctx, e.cerebro, agentID, originalUserID)
		if len(enabledTools) > 0 {
			// Also register the MCP-backed tools (get_issue, list_comments,
			// add_comment) that the Registry wraps via its Call method.
			// For backward compat, create an MCP server and also expose its
			// tools — the registry dispatch handles the extended tools.
			anthropicTools = taskRegistry.ToAnthropicTools(enabledTools)
			activeRegistry = taskRegistry
			useRegistry = true
		}
	}
	if !useRegistry {
		// POC fallback: MCP server with hardcoded 3 tools.
		toolSrv = NewFirtalGatewayToolServer(e.queries, tctx)
		for _, def := range GatewayToolDefs() {
			anthropicTools = append(anthropicTools, AnthropicTool{
				Name:        def.Function.Name,
				Description: def.Function.Description,
				InputSchema: def.Function.Parameters,
			})
		}
		if len(anthropicTools) > 0 {
			anthropicTools[len(anthropicTools)-1].CacheControl = &AnthropicCacheControl{Type: "ephemeral"}
		}
	}

	if useRegistry && activeRegistry != nil {
		// CEREBRO-PATCH(firtal-gateway-tool-loop-fallback): use the provider-compatible tool loop as the primary path for enabled registry tools; prod Anthropic-native failures did not reliably lift Kristian into fallback.
		return e.runGatewayCompatRegistryToolLoop(ctx, cfg, agent, initialMessages, meta, agentID, workspaceID, activeRegistry, enabledTools)
	}
	if toolSrv != nil {
		// CEREBRO-PATCH(firtal-gateway-tool-loop-fallback): keep the legacy three-tool path on the same compat transport so tool-enabled tasks avoid Anthropic-native malformed requests entirely.
		return e.runToolLoopWithServer(ctx, cfg, agent, initialMessages, meta, toolSrv, anthropicToolsToGatewayToolDefs(anthropicTools))
	}

	completion, err := e.runAnthropicToolLoop(ctx, cfg, agent, initialMessages, meta, agentID, workspaceID, anthropicTools, tctx, toolSrv, activeRegistry, useRegistry)
	if err == nil || !isAnthropicMalformedToolRequest(err) || len(anthropicTools) == 0 {
		return completion, err
	}

	e.logger.Warn("firtal gateway anthropic tool request malformed; retrying via chat completions tool loop",
		"task_id", meta.TaskID,
		"agent_id", meta.AgentID,
		"workspace_id", meta.WorkspaceID,
		"tool_count", len(anthropicTools),
		"error", err,
	)
	if useRegistry && activeRegistry != nil {
		return e.runGatewayCompatRegistryToolLoop(ctx, cfg, agent, initialMessages, meta, agentID, workspaceID, activeRegistry, enabledTools)
	}
	if toolSrv != nil {
		return e.runToolLoopWithServer(ctx, cfg, agent, initialMessages, meta, toolSrv, anthropicToolsToGatewayToolDefs(anthropicTools))
	}
	return completion, err
}

func isAnthropicMalformedToolRequest(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "anthropic gateway returned HTTP 400") &&
		(strings.Contains(msg, "upstream_rejected_request") || strings.Contains(msg, "request malformed"))
}

func toolsToGatewayToolDefs(tools []Tool) []GatewayToolDef {
	out := make([]GatewayToolDef, 0, len(tools))
	for _, tool := range tools {
		out = append(out, GatewayToolDef{
			Type: "function",
			Function: GatewayToolFunction{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  normalizeAnthropicToolSchema(tool.InputSchema()), // CEREBRO-PATCH(firtal-gateway-tool-loop-fallback): use provider-safe schemas on chat-completions fallback too.
			},
		})
	}
	return out
}

func anthropicToolsToGatewayToolDefs(tools []AnthropicTool) []GatewayToolDef {
	out := make([]GatewayToolDef, 0, len(tools))
	for _, tool := range tools {
		out = append(out, GatewayToolDef{
			Type: "function",
			Function: GatewayToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  normalizeAnthropicToolSchema(tool.InputSchema),
			},
		})
	}
	return out
}

// runToolLoopWithServer is the testable inner form of the legacy OpenAI-compat
// tool loop. Tests can register their own handlers on toolSrv so the loop's
// control flow can be exercised without a real database.
func (e *FirtalGatewayExecutor) runToolLoopWithServer(ctx context.Context, cfg FirtalGatewayRuntimeConfig, agent db.Agent, initialMessages []GatewayMessage, meta GatewayRequestMeta, toolSrv *mcp.Server, tools []GatewayToolDef) (GatewayCompletion, error) {
	history := withToolUsageHint(initialMessages)

	var acc GatewayCompletion
	accumulate := func(c GatewayCompletion) {
		acc.Model = c.Model
		acc.Usage.InputTokens += c.Usage.InputTokens
		acc.Usage.OutputTokens += c.Usage.OutputTokens
		acc.Usage.CacheReadTokens += c.Usage.CacheReadTokens
		acc.Usage.CacheWriteTokens += c.Usage.CacheWriteTokens
		acc.Usage.CostCents += c.Usage.CostCents
	}

	maxRounds := firtalGatewayMaxToolRounds
	if cfg.MaxToolRounds > 0 {
		maxRounds = cfg.MaxToolRounds
	}

	for round := 0; round < maxRounds; round++ {
		completion, err := e.completeGatewayWithTools(ctx, cfg, agent.Model.String, history, tools, meta)
		if err != nil {
			return GatewayCompletion{}, err
		}
		accumulate(completion)

		if len(completion.ToolCalls) == 0 {
			acc.Output = completion.Output
			return acc, nil
		}

		history = append(history, GatewayMessage{
			Role:      "assistant",
			Content:   completion.Output,
			ToolCalls: completion.ToolCalls,
		})
		for _, call := range completion.ToolCalls {
			result, dur := e.dispatchTool(ctx, toolSrv, call)
			e.logger.Info("firtal gateway tool call",
				"task_id", meta.TaskID,
				"agent_id", meta.AgentID,
				"workspace_id", meta.WorkspaceID,
				"round", round+1,
				"tool", call.Function.Name,
				"tool_call_id", call.ID,
				"duration_ms", dur.Milliseconds(),
				"is_error", result.IsError,
			)
			history = append(history, GatewayMessage{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    toolResultText(result),
			})
		}
	}

	// Tool-round budget exhausted. Make one final gateway call WITHOUT tools
	// so the model has to produce a text answer.
	e.logger.Info("firtal gateway tool loop forcing final no-tool call",
		"task_id", meta.TaskID,
		"agent_id", meta.AgentID,
		"workspace_id", meta.WorkspaceID,
		"max_tool_rounds", maxRounds,
	)
	final, err := e.completeGateway(ctx, cfg, agent.Model.String, history, meta)
	if err != nil {
		return GatewayCompletion{}, err
	}
	accumulate(final)
	acc.Output = final.Output
	return acc, nil
}

func (e *FirtalGatewayExecutor) runGatewayCompatRegistryToolLoop(
	ctx context.Context,
	cfg FirtalGatewayRuntimeConfig,
	agent db.Agent,
	initialMessages []GatewayMessage,
	meta GatewayRequestMeta,
	agentID, workspaceID pgtype.UUID,
	registry *Registry,
	tools []Tool,
) (GatewayCompletion, error) {
	history := withRegistryToolUsageHint(initialMessages, tools)
	toolDefs := toolsToGatewayToolDefs(tools)

	var acc GatewayCompletion
	accumulate := func(c GatewayCompletion) {
		acc.Model = c.Model
		acc.Usage.InputTokens += c.Usage.InputTokens
		acc.Usage.OutputTokens += c.Usage.OutputTokens
		acc.Usage.CacheReadTokens += c.Usage.CacheReadTokens
		acc.Usage.CacheWriteTokens += c.Usage.CacheWriteTokens
		acc.Usage.CostCents += c.Usage.CostCents
	}

	maxRounds := firtalGatewayMaxToolRounds
	if cfg.MaxToolRounds > 0 {
		maxRounds = cfg.MaxToolRounds
	}

	for round := 0; round < maxRounds; round++ {
		completion, err := e.completeGatewayWithTools(ctx, cfg, agent.Model.String, history, toolDefs, meta)
		if err != nil {
			return GatewayCompletion{}, err
		}
		accumulate(completion)

		if len(completion.ToolCalls) == 0 {
			acc.Output = completion.Output
			return acc, nil
		}

		history = append(history, GatewayMessage{
			Role:      "assistant",
			Content:   completion.Output,
			ToolCalls: completion.ToolCalls,
		})

		if e.budgetSvc != nil {
			decision := e.budgetSvc.CheckPreClaim(ctx, workspaceID, agentID)
			if !decision.Allowed {
				e.logger.Info("firtal gateway chat-completions fallback budget blocked",
					"task_id", meta.TaskID,
					"agent_id", meta.AgentID,
					"reason", decision.Reason,
				)
				break
			}
		}

		for _, call := range completion.ToolCalls {
			start := time.Now()
			args := map[string]any{}
			resultText := ""
			isError := false
			if raw := strings.TrimSpace(call.Function.Arguments); raw != "" {
				if err := json.Unmarshal([]byte(raw), &args); err != nil {
					resultText = fmt.Sprintf("invalid tool arguments JSON: %v", err)
					isError = true
				}
			}
			if !isError {
				var err error
				resultText, err = registry.Call(ctx, call.Function.Name, args)
				if err != nil {
					resultText = fmt.Sprintf("tool error: %v", err)
					isError = true
				}
			}
			e.logger.Info("firtal gateway chat-completions fallback tool call",
				"task_id", meta.TaskID,
				"agent_id", meta.AgentID,
				"workspace_id", meta.WorkspaceID,
				"round", round+1,
				"tool_name", call.Function.Name,
				"tool_call_id", call.ID,
				"duration_ms", time.Since(start).Milliseconds(),
				"is_error", isError,
			)
			history = append(history, GatewayMessage{
				Role:       "tool",
				ToolCallID: call.ID,
				Content:    resultText,
			})
		}
	}

	e.logger.Info("firtal gateway chat-completions fallback forcing final no-tool call",
		"task_id", meta.TaskID,
		"agent_id", meta.AgentID,
		"workspace_id", meta.WorkspaceID,
		"max_tool_rounds", maxRounds,
	)
	final, err := e.completeGateway(ctx, cfg, agent.Model.String, history, meta)
	if err != nil {
		return GatewayCompletion{}, err
	}
	accumulate(final)
	acc.Output = final.Output
	return acc, nil
}

// runAnthropicToolLoop is the production Anthropic-native tool loop. It
// converts the initial OpenAI-compat messages to Anthropic format, sends
// requests to /v1/messages with tool definitions, dispatches tool calls via
// the Registry (or MCP server fallback), and accumulates usage across rounds.
// Budget is checked before each tool dispatch; if the budget is exhausted the
// loop stops and forces a final text-only call.
func (e *FirtalGatewayExecutor) runAnthropicToolLoop(
	ctx context.Context,
	cfg FirtalGatewayRuntimeConfig,
	agent db.Agent,
	initialMessages []GatewayMessage,
	meta GatewayRequestMeta,
	agentID, workspaceID pgtype.UUID,
	tools []AnthropicTool,
	tctx ToolContext,
	toolSrv *mcp.Server,
	registry *Registry,
	useRegistry bool,
) (GatewayCompletion, error) {
	// Extract system prompt from the first message if present.
	systemText := ""
	msgStart := 0
	if len(initialMessages) > 0 && initialMessages[0].Role == "system" {
		systemText = initialMessages[0].Content
		msgStart = 1
	}
	// Patch system prompt to list available tools.
	if len(tools) > 0 {
		toolNames := make([]string, len(tools))
		for i, t := range tools {
			toolNames[i] = t.Name
		}
		systemText = strings.TrimRight(systemText, "\n") +
			"\n\nYou have the following tools available: " + strings.Join(toolNames, ", ") + "." +
			" Use them to accomplish the task. Post your final answer as a comment when asked."
		// TODO(persona): enforce persona policy-engine when ready.
	}

	history := ConvertGatewayMessagesToAnthropic(initialMessages[msgStart:])

	maxRounds := firtalGatewayMaxToolRounds
	if cfg.MaxToolRounds > 0 {
		maxRounds = cfg.MaxToolRounds
	}

	var acc GatewayCompletion
	accumulate := func(c GatewayCompletion) {
		acc.Model = c.Model
		acc.Usage.InputTokens += c.Usage.InputTokens
		acc.Usage.OutputTokens += c.Usage.OutputTokens
		acc.Usage.CacheReadTokens += c.Usage.CacheReadTokens
		acc.Usage.CacheWriteTokens += c.Usage.CacheWriteTokens
		acc.Usage.CostCents += c.Usage.CostCents
	}

	gwClient := e.gateway
	if gwClient == nil {
		gwClient = NewGatewayClient(cfg, nil)
	}

	for round := 0; round < maxRounds; round++ {
		completion, err := gwClient.CompleteAnthropicWithTools(ctx, agent.Model.String, systemText, history, tools, meta)
		if err != nil {
			return GatewayCompletion{}, err
		}
		accumulate(completion)

		if len(completion.ToolCalls) == 0 {
			acc.Output = completion.Output
			return acc, nil
		}

		// Append assistant turn with tool_use blocks.
		history = append(history, AnthropicMessage{
			Role:    "assistant",
			Content: buildAnthropicAssistantContent(completion),
		})

		// Dispatch each tool call. Budget check before first dispatch each round.
		if e.budgetSvc != nil {
			decision := e.budgetSvc.CheckPreClaim(ctx, workspaceID, agentID)
			if !decision.Allowed {
				e.logger.Info("firtal gateway tool loop budget blocked",
					"task_id", meta.TaskID,
					"agent_id", meta.AgentID,
					"reason", decision.Reason,
				)
				break
			}
		}

		toolResultBlocks := make([]AnthropicContentBlock, 0, len(completion.ToolCalls))
		for _, call := range completion.ToolCalls {
			start := time.Now()
			var (
				resultText string
				isError    bool
			)
			if useRegistry && registry != nil {
				args := map[string]any{}
				if raw := strings.TrimSpace(call.Function.Arguments); raw != "" {
					if err := json.Unmarshal([]byte(raw), &args); err != nil {
						resultText = fmt.Sprintf("invalid tool arguments JSON: %v", err)
						isError = true
					}
				}
				if !isError {
					resultText, err = registry.Call(ctx, call.Function.Name, args)
					if err != nil {
						// Fall through to MCP server if registry doesn't know this tool.
						if toolSrv != nil {
							mcpResult, dur := e.dispatchTool(ctx, toolSrv, call)
							e.logger.Info("firtal gateway anthropic tool call (mcp fallback)",
								"task_id", meta.TaskID,
								"agent_id", meta.AgentID,
								"round", round+1,
								"tool", call.Function.Name,
								"duration_ms", dur.Milliseconds(),
								"is_error", mcpResult.IsError,
							)
							resultText = toolResultText(mcpResult)
							isError = mcpResult.IsError
						} else {
							resultText = fmt.Sprintf("tool error: %v", err)
							isError = true
						}
					}
				}
			} else if toolSrv != nil {
				mcpResult, _ := e.dispatchTool(ctx, toolSrv, call)
				resultText = toolResultText(mcpResult)
				isError = mcpResult.IsError
			} else {
				resultText = fmt.Sprintf("tool %q not available", call.Function.Name)
				isError = true
			}

			dur := time.Since(start)
			e.logger.Info("firtal gateway anthropic tool call",
				"task_id", meta.TaskID,
				"agent_id", meta.AgentID,
				"workspace_id", meta.WorkspaceID,
				"round", round+1,
				"tool_name", call.Function.Name,
				"tool_call_id", call.ID,
				"duration_ms", dur.Milliseconds(),
				"is_error", isError,
			)

			toolResultBlocks = append(toolResultBlocks, AnthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: call.ID,
				Content: []AnthropicContentBlock{
					{Type: "text", Text: resultText},
				},
			})
		}

		// Append tool results as a user turn.
		history = append(history, AnthropicMessage{
			Role:    "user",
			Content: toolResultBlocks,
		})
	}

	// Tool-round budget exhausted. Make one final call without tools so the
	// model can emit a text summary.
	e.logger.Info("firtal gateway anthropic tool loop forcing final no-tool call",
		"task_id", meta.TaskID,
		"agent_id", meta.AgentID,
		"workspace_id", meta.WorkspaceID,
		"max_tool_rounds", maxRounds,
	)
	final, err := gwClient.CompleteAnthropicWithTools(ctx, agent.Model.String, systemText, history, nil, meta)
	if err != nil {
		return GatewayCompletion{}, err
	}
	accumulate(final)
	acc.Output = final.Output
	return acc, nil
}

// buildAnthropicAssistantContent converts a GatewayCompletion (which may have
// both text output and tool_calls) into the content array expected for an
// assistant message in the Anthropic messages protocol.
func buildAnthropicAssistantContent(c GatewayCompletion) []AnthropicContentBlock {
	var blocks []AnthropicContentBlock
	if c.Output != "" {
		blocks = append(blocks, AnthropicContentBlock{Type: "text", Text: c.Output})
	}
	for _, tc := range c.ToolCalls {
		var input map[string]any
		if raw := strings.TrimSpace(tc.Function.Arguments); raw != "" {
			_ = json.Unmarshal([]byte(raw), &input)
		}
		if input == nil {
			input = map[string]any{}
		}
		blocks = append(blocks, AnthropicContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}
	return blocks
}

func (e *FirtalGatewayExecutor) dispatchTool(ctx context.Context, srv *mcp.Server, call GatewayToolCall) (mcp.CallToolResult, time.Duration) {
	args := map[string]any{}
	if raw := strings.TrimSpace(call.Function.Arguments); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return mcp.ErrorResult(fmt.Sprintf("invalid tool arguments JSON: %v", err)), 0
		}
	}
	start := time.Now()
	result, err := srv.Call(ctx, call.Function.Name, args)
	dur := time.Since(start)
	if err != nil {
		return mcp.ErrorResult(err.Error()), dur
	}
	return result, dur
}

// withToolUsageHint appends a brief instruction to the existing system
// message so a tool-enabled agent stops parroting "I don't have tools" — the
// inherited chat/issue prompts explicitly tell the model it has none. The
// prompt rewrite stays minimal so we don't drift away from the chat-only
// baseline; W2/W3 will introduce a proper tool-aware system prompt.
func withToolUsageHint(messages []GatewayMessage) []GatewayMessage {
	if len(messages) == 0 || messages[0].Role != "system" {
		return messages
	}
	hint := "\n\nYou have these tools available: get_issue(issue_id), list_comments(issue_id), add_comment(issue_id, content). Use them to read the issue and post your reply as a comment when asked."
	out := append([]GatewayMessage(nil), messages...)
	out[0].Content = strings.TrimRight(out[0].Content, "\n") + hint
	return out
}

func withRegistryToolUsageHint(messages []GatewayMessage, tools []Tool) []GatewayMessage {
	if len(messages) == 0 || messages[0].Role != "system" || len(tools) == 0 {
		return messages
	}
	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Name()
	}
	hint := "\n\nYou have the following tools available: " + strings.Join(names, ", ") + "." +
		" Use them to accomplish the task. Post your final answer as a comment when asked."
	out := append([]GatewayMessage(nil), messages...)
	out[0].Content = strings.TrimRight(out[0].Content, "\n") + hint
	return out
}

func toolResultText(r mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
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
