package handler

import (
	"context"
	"log/slog"
)

// sweepCerebroScheduledMessagesOnce claims and delivers at most limit due
// messages. Keeping one pass separate from the ticker makes delivery
// deterministic in tests while preserving the production scheduling loop.
func (h *Handler) sweepCerebroScheduledMessagesOnce(ctx context.Context, limit int) int {
	processed := 0
	for processed < limit {
		m, ok := h.claimScheduledMessage(ctx, "send_at <= now()")
		if !ok {
			break
		}
		processed++
		if err := h.deliverScheduledMessage(ctx, m); err != nil {
			slog.Error("scheduled message delivery failed", "scheduled_message_id", m.ID, "error", err)
		}
	}
	return processed
}
