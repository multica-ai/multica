package handler

import (
	"context"
	"log/slog"
)

// recordCerebroTaskContextFootprint persists the last-turn prompt footprint for
// the context-window indicator (FIR-1856). The cumulative token columns on
// task_usage stay the source of truth for cost; this side table holds the size
// of the prompt the model last read — how full the window actually is.
//
// Only Codex/gpt runtimes report a footprint (codex.go reads Codex'
// last_token_usage). Everything else leaves it zero, and the indicator falls
// back to the input+cache_read+cache_write formula in
// server/internal/cerebro/sessions, which is already correct for those runtimes.
func (h *Handler) recordCerebroTaskContextFootprint(ctx context.Context, taskID string, u TaskUsagePayload) {
	if h.DB == nil || u.ContextInputTokens <= 0 {
		return
	}
	if _, err := h.DB.Exec(ctx, `
		INSERT INTO cerebro_task_context_footprint (task_id, model, input_tokens, cache_read_tokens, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (task_id) DO UPDATE SET
			model = EXCLUDED.model,
			input_tokens = EXCLUDED.input_tokens,
			cache_read_tokens = EXCLUDED.cache_read_tokens,
			updated_at = now()
	`, parseUUID(taskID), u.Model, u.ContextInputTokens, u.ContextCacheReadTokens); err != nil {
		slog.Warn("record cerebro task context footprint failed",
			"task_id", taskID, "model", u.Model, "error", err)
	}
}
