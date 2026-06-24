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
// FIR-1870: the footprint is no longer Codex-only. The streaming runtimes
// (Claude, Cursor, Pi, OpenCode, OpenClaw, the local OpenAI-compatible runtime)
// accumulate per-turn usage, so the cumulative input+cache_read+cache_write sum
// over-counts the cached prefix and pins the gauge at 100% — exactly the Codex
// bug, just on every other runtime. Those backends now report a last-turn
// footprint too (see server/pkg/agent/cerebro_context_footprint.go). Runtimes
// that report no token usage, or only a cumulative snapshot we cannot safely
// split into a last turn, still leave this zero and fall back to the cumulative
// formula in server/internal/cerebro/sessions.
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

	// FIR-1931: also append a history row (one point per run) so the session sheet
	// can draw the development curve over a session. Best-effort, like the upsert
	// above — a failure here must never fail the usage report.
	if _, err := h.DB.Exec(ctx, `
		INSERT INTO cerebro_task_context_footprint_history (task_id, model, input_tokens, cache_read_tokens)
		VALUES ($1, $2, $3, $4)
	`, parseUUID(taskID), u.Model, u.ContextInputTokens, u.ContextCacheReadTokens); err != nil {
		slog.Warn("record cerebro task context footprint history failed",
			"task_id", taskID, "model", u.Model, "error", err)
	}
}
