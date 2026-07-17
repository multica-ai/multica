package handler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/pricing"
)

type modelUsageKey struct {
	provider string
	model    string
}

func modelUsageEventCoverage(events []agent.ModelUsageEvent) map[modelUsageKey]struct{} {
	covered := make(map[modelUsageKey]struct{}, len(events))
	for _, event := range events {
		covered[modelUsageKey{provider: normalizeProvider(event.Provider), model: strings.TrimSpace(event.Model)}] = struct{}{}
	}
	return covered
}

func modelUsageCovered(covered map[modelUsageKey]struct{}, provider, model string) bool {
	_, ok := covered[modelUsageKey{provider: normalizeProvider(provider), model: strings.TrimSpace(model)}]
	return ok
}

// normalizeAndValidateModelUsageEvent is the core-side boundary. Runtime
// adapters translate provider formats; core owns canonical normalization and
// contract rejection before persistence.
func normalizeAndValidateModelUsageEvent(event agent.ModelUsageEvent, fallbackProvider string) (agent.ModelUsageEvent, error) {
	event.Provider = normalizeProvider(event.Provider)
	if event.Provider == "" {
		event.Provider = normalizeProvider(fallbackProvider)
	}
	if err := agent.ValidateModelUsageEvent(event); err != nil {
		return agent.ModelUsageEvent{}, err
	}
	return event, nil
}

// ingestModelUsageEvent atomically appends one canonical call and posts its
// budget/account projections. A daemon retry observes inserted=false and has
// no financial side effects.
func (h *Handler) ingestModelUsageEvent(ctx context.Context, task db.AgentTaskQueue, event agent.ModelUsageEvent) (bool, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin model usage event transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := h.Queries.WithTx(tx)
	inserted, err := qtx.InsertModelUsageEvent(ctx, db.InsertModelUsageEventParams{
		SchemaVersion:       event.SchemaVersion,
		EventID:             event.EventID,
		ProviderSessionID:   event.ProviderSessionID,
		CallID:              event.CallID,
		Sequence:            event.Sequence,
		ObservedAt:          pgtype.Timestamptz{Time: event.ObservedAt, Valid: true},
		Provider:            event.Provider,
		Model:               event.Model,
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		ReasoningTokens:     event.ReasoningTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheWriteTokens:    event.CacheWriteTokens,
		CostCents:           event.CostCents,
		ContextTokens:       event.ContextTokens,
		ContextWindowTokens: event.ContextWindowTokens,
		CompactionKind:      event.CompactionKind,
		Source:              event.Source,
		Completeness:        event.Completeness,
		CounterSemantics:    event.CounterSemantics,
		TaskID:              task.ID,
	})
	if err != nil {
		return false, fmt.Errorf("insert model usage event: %w", err)
	}
	if !inserted {
		return false, nil
	}

	var cqtx *cerebrodb.Queries
	if h.CerebroQueries != nil {
		cqtx = h.CerebroQueries.WithTx(tx)
	}
	if err := recordModelUsageEventBudgetAndAccount(ctx, qtx, cqtx, task, event); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit model usage event transaction: %w", err)
	}
	return true, nil
}

func recordModelUsageEventBudgetAndAccount(
	ctx context.Context,
	queries *db.Queries,
	cerebroQueries *cerebrodb.Queries,
	task db.AgentTaskQueue,
	event agent.ModelUsageEvent,
) error {
	if !task.RuntimeID.Valid || !task.AgentID.Valid {
		return nil
	}
	runtime, err := queries.GetAgentRuntime(ctx, task.RuntimeID)
	if err != nil {
		return fmt.Errorf("load runtime for model usage projection: %w", err)
	}
	if !runtime.WorkspaceID.Valid {
		return nil
	}

	cents := event.CostCents
	if cents <= 0 {
		cents = pricing.ComputeCents(event.Model, pricing.Usage{
			InputTokens:      event.InputTokens,
			OutputTokens:     event.OutputTokens + event.ReasoningTokens,
			CacheReadTokens:  event.CacheReadTokens,
			CacheWriteTokens: event.CacheWriteTokens,
		})
	}
	if cents > 0 {
		now := time.Now().UTC()
		dayStart := pgtype.Date{Time: time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
		monthStart := pgtype.Date{Time: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC), Valid: true}
		for _, params := range []db.IncrementBudgetStateParams{
			{WorkspaceID: runtime.WorkspaceID, ScopeType: "agent", ScopeID: task.AgentID, WindowType: "day", WindowStart: dayStart, CentsSpent: cents},
			{WorkspaceID: runtime.WorkspaceID, ScopeType: "agent", ScopeID: task.AgentID, WindowType: "month", WindowStart: monthStart, CentsSpent: cents},
			{WorkspaceID: runtime.WorkspaceID, ScopeType: "workspace", ScopeID: runtime.WorkspaceID, WindowType: "day", WindowStart: dayStart, CentsSpent: cents},
			{WorkspaceID: runtime.WorkspaceID, ScopeType: "workspace", ScopeID: runtime.WorkspaceID, WindowType: "month", WindowStart: monthStart, CentsSpent: cents},
		} {
			if err := queries.IncrementBudgetState(ctx, params); err != nil {
				return fmt.Errorf("post model usage budget: %w", err)
			}
		}
	}

	if cerebroQueries == nil || !runtime.CurrentAccountID.Valid {
		return nil
	}
	tokens := event.InputTokens + event.OutputTokens + event.ReasoningTokens + event.CacheReadTokens + event.CacheWriteTokens
	if tokens <= 0 {
		return nil
	}
	if err := cerebroQueries.InsertCerebroAccountTokenUsage(ctx, cerebrodb.InsertCerebroAccountTokenUsageParams{
		AccountID:   runtime.CurrentAccountID,
		WorkspaceID: runtime.WorkspaceID,
		Tokens:      tokens,
	}); err != nil {
		return fmt.Errorf("post model usage account tokens: %w", err)
	}
	return nil
}

func (h *Handler) logModelUsageEventShadowReconciliation(ctx context.Context, taskID pgtype.UUID) {
	shadow, err := h.Queries.GetModelUsageEventTaskReconciliation(ctx, taskID)
	if err != nil {
		slog.Warn("model usage shadow reconciliation failed", "task_id", uuidToString(taskID), "error", err)
		return
	}
	log := slog.Info
	if shadow.InputTokenDrift != 0 || shadow.OutputTokenDrift != 0 ||
		shadow.CacheReadTokenDrift != 0 || shadow.CacheWriteTokenDrift != 0 ||
		shadow.CostCentsDrift != 0 || shadow.ContextTokenDrift != 0 {
		log = slog.Warn
	}
	log("model usage shadow reconciliation",
		"task_id", uuidToString(taskID),
		"event_count", shadow.EventCount,
		"input_token_drift", shadow.InputTokenDrift,
		"output_token_drift", shadow.OutputTokenDrift,
		"cache_read_token_drift", shadow.CacheReadTokenDrift,
		"cache_write_token_drift", shadow.CacheWriteTokenDrift,
		"cost_cents_drift", shadow.CostCentsDrift,
		"context_token_drift", shadow.ContextTokenDrift,
	)
	h.logModelUsageEventScopeReconciliation(ctx, taskID)
}

func (h *Handler) logModelUsageEventScopeReconciliation(ctx context.Context, taskID pgtype.UUID) {
	task, err := h.Queries.GetAgentTask(ctx, taskID)
	if err != nil || !task.IssueID.Valid {
		return
	}

	issueRows, err := h.Queries.GetModelUsageEventIssueReconciliation(ctx, task.IssueID)
	if err != nil {
		slog.Warn("model usage issue shadow reconciliation failed", "issue_id", uuidToString(task.IssueID), "error", err)
	} else {
		for _, row := range issueRows {
			logModelUsageEventScopeRow("issue", task.IssueID, row.AgentID, row.Provider, row.Model,
				row.EventCount, row.InputTokenDrift, row.OutputTokenDrift, row.CacheReadTokenDrift,
				row.CacheWriteTokenDrift, row.CostCentsDrift, row.ContextTokenDrift)
		}
	}

	if !task.TriggerCommentID.Valid {
		return
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return
	}
	root, err := h.Queries.GetThreadRoot(ctx, db.GetThreadRootParams{
		CommentID: task.TriggerCommentID, WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		return
	}
	var isFirst bool
	if err := h.DB.QueryRow(ctx, `
		SELECT $2::uuid = (
			SELECT id FROM comment
			WHERE issue_id = $1 AND parent_id IS NULL AND type = 'comment'
			ORDER BY created_at ASC, id ASC LIMIT 1
		)`, task.IssueID, root.ID).Scan(&isFirst); err != nil {
		return
	}
	sessionRows, err := h.Queries.GetModelUsageEventSessionReconciliation(ctx, db.GetModelUsageEventSessionReconciliationParams{
		SessionRootCommentID: root.ID, IssueID: task.IssueID, IsFirst: isFirst,
	})
	if err != nil {
		slog.Warn("model usage session shadow reconciliation failed", "session_id", uuidToString(root.ID), "error", err)
		return
	}
	for _, row := range sessionRows {
		logModelUsageEventScopeRow("session", root.ID, row.AgentID, row.Provider, row.Model,
			row.EventCount, row.InputTokenDrift, row.OutputTokenDrift, row.CacheReadTokenDrift,
			row.CacheWriteTokenDrift, row.CostCentsDrift, row.ContextTokenDrift)
	}
}

func logModelUsageEventScopeRow(
	scope string,
	scopeID, agentID pgtype.UUID,
	provider, model string,
	eventCount, inputDrift, outputDrift, cacheReadDrift, cacheWriteDrift, costDrift, contextDrift int64,
) {
	log := slog.Info
	if inputDrift != 0 || outputDrift != 0 || cacheReadDrift != 0 || cacheWriteDrift != 0 || costDrift != 0 || contextDrift != 0 {
		log = slog.Warn
	}
	log("model usage scope shadow reconciliation",
		"scope", scope,
		"scope_id", uuidToString(scopeID),
		"agent_id", uuidToString(agentID),
		"provider", provider,
		"model", model,
		"event_count", eventCount,
		"input_token_drift", inputDrift,
		"output_token_drift", outputDrift,
		"cache_read_token_drift", cacheReadDrift,
		"cache_write_token_drift", cacheWriteDrift,
		"cost_cents_drift", costDrift,
		"context_token_drift", contextDrift,
	)
}
