package handler

// CEREBRO-PATCH(chat-message-cost): net-new cerebro endpoint that returns the
// per-task spend for a chat session so the chat UI can render a price badge
// under each assistant reply (FIR-31). Keyed by task_id (= chat_message.task_id).

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/util"
)

type chatMessageCost struct {
	TaskID           string `json:"task_id"`
	Model            string `json:"model"`
	CostCents        int64  `json:"cost_cents"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
}

// GetChatSessionMessageCosts returns the spend for every task in a chat
// session, keyed by task_id. The chat message footer looks up its own
// message.task_id to render "· $0.05" beneath the assistant reply. Cost
// prefers the gateway-reported spend and otherwise falls back to the
// pricing-table estimate — identical to the session total in
// GetChatSessionUsage, so the per-reply numbers sum to the header chip.
func (h *Handler) GetChatSessionMessageCosts(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID := ctxWorkspaceID(r.Context())
	sessionID := chi.URLParam(r, "sessionId")

	session, ok := h.loadChatSessionForUser(w, r, userID, workspaceID, sessionID)
	if !ok {
		return
	}

	if h.CerebroQueries == nil {
		writeJSON(w, http.StatusOK, map[string]any{"costs": []chatMessageCost{}})
		return
	}

	rows, err := h.CerebroQueries.GetChatSessionMessageCosts(r.Context(), session.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get chat message costs")
		return
	}

	// One task can span multiple model rows (e.g. a fallback mid-run). Sum per
	// task, applying the gateway-vs-estimate rule per (task, model) row. When a
	// task used a single model we keep it for the tooltip; mixed-model tasks
	// blank it so the UI shows cost without a misleading single model label.
	byTask := make(map[string]*chatMessageCost, len(rows))
	for _, row := range rows {
		id := util.UUIDToString(row.TaskID)
		c := byTask[id]
		if c == nil {
			c = &chatMessageCost{TaskID: id, Model: row.Model}
			byTask[id] = c
		} else if c.Model != row.Model {
			c.Model = ""
		}
		c.CostCents += preferGatewayCost(row.TotalCostCents, row.Model, row.TotalInputTokens, row.TotalOutputTokens, row.TotalCacheReadTokens, row.TotalCacheWriteTokens)
		c.InputTokens += row.TotalInputTokens
		c.OutputTokens += row.TotalOutputTokens
		c.CacheReadTokens += row.TotalCacheReadTokens
		c.CacheWriteTokens += row.TotalCacheWriteTokens
	}

	costs := make([]chatMessageCost, 0, len(byTask))
	for _, c := range byTask {
		costs = append(costs, *c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"costs": costs})
}
