package handler

// CEREBRO-PATCH(comment-cost): FIR-39 — net-new cerebro endpoint that returns
// the per-task spend for one issue (or channel; channels are issues with
// kind='channel'), pinned to the *last* comment each task produced. The
// frontend looks up its own comment_id to render "cost $X.XX" beneath an
// agent reply, mirroring the chat per-reply badge from FIR-31.

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
)

type issueCommentCost struct {
	TaskID           string `json:"task_id"`
	CommentID        string `json:"comment_id"`
	Model            string `json:"model"`
	CostCents        int64  `json:"cost_cents"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
}

// GetIssueCommentCosts returns the spend of every task that authored a comment
// on the issue, attached to the last comment that task produced. The comment
// footer looks up its own comment_id and renders "cost $0.05" beneath the
// reply when a match is found; comments without a match render nothing (no
// task association, or run still in flight). Cost prefers the gateway-reported
// spend and otherwise falls back to the pricing-table estimate — identical to
// the rule in GetIssueUsage so the per-comment numbers reconcile with the
// issue total chip in the sidebar (JEH-736 / IssueUsageSummary).
func (h *Handler) GetIssueCommentCosts(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	if h.CerebroQueries == nil {
		writeJSON(w, http.StatusOK, map[string]any{"costs": []issueCommentCost{}})
		return
	}

	rows, err := h.CerebroQueries.GetIssueCommentCosts(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get issue comment costs")
		return
	}

	// One task can span multiple model rows (e.g. a fallback mid-run). Sum per
	// task, applying the gateway-vs-estimate rule per (task, model) row. When a
	// task used a single model we keep it for the tooltip; mixed-model tasks
	// blank it so the UI shows cost without a misleading single model label.
	// Mirrors the same shape as GetChatSessionMessageCosts (FIR-31).
	byTask := make(map[string]*issueCommentCost, len(rows))
	for _, row := range rows {
		id := util.UUIDToString(row.TaskID)
		c := byTask[id]
		if c == nil {
			c = &issueCommentCost{
				TaskID:    id,
				CommentID: util.UUIDToString(row.CommentID),
				Model:     row.Model,
			}
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

	costs := make([]issueCommentCost, 0, len(byTask))
	for _, c := range byTask {
		costs = append(costs, *c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"costs": costs})
}

// linkCommentToTaskIfAgent pins this agent-authored comment to the task the
// CLI/runtime stamped in the X-Task-ID header so the per-comment cost badge
// (GetIssueCommentCosts above) can later attach to it. Member comments and
// agent comments without a header are no-ops — only agent runs carry cost.
//
// Best-effort: a failure here must not roll the comment back. The cost badge
// is a read-side affordance; if a link row is missing, the badge just
// disappears for that comment, no other behaviour changes. We log and move on.
func (h *Handler) linkCommentToTaskIfAgent(r *http.Request, commentID, issueID, workspaceID pgtype.UUID, authorType string) {
	if authorType != "agent" || h.CerebroQueries == nil {
		return
	}
	taskHeader := r.Header.Get("X-Task-ID")
	if taskHeader == "" {
		return
	}
	taskID, err := util.ParseUUID(taskHeader)
	if err != nil {
		return
	}
	if err := h.CerebroQueries.LinkCommentToTask(r.Context(), cerebrodb.LinkCommentToTaskParams{
		CommentID:   commentID,
		TaskID:      taskID,
		IssueID:     issueID,
		WorkspaceID: workspaceID,
	}); err != nil {
		slog.Warn("link comment to task failed", append(logger.RequestAttrs(r),
			"error", err,
			"comment_id", util.UUIDToString(commentID),
			"task_id", taskHeader,
		)...)
	}
}

