package handler

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// defaultResumeContextBudgetTokens is the billed-input ceiling above which a
// comment follow-up stops inheriting the issue's provider session (GH #4754).
//
// Deliberately generous. The number this gate protects against is not a large
// session, it is a runaway one: #4754 measured a no-op scheduled scan at 330k
// billed tokens for the whole session, against 11.0M and 11.5M for the two
// sessions where a short comment's FIRST generation already carried ~190k
// input tokens. 8M sits an order of magnitude above ordinary work and just
// below the observed failure region, so on a normal issue this never fires and
// nothing about resume behaviour changes.
//
// It is a starting point, not a tuned value — the unit is billed input tokens
// across the session, not context-window occupancy (see the query comment on
// GetSessionBilledInputTokens for why no better signal exists yet). Operators
// who want the short-session workflow #5653 describes can set
// RESUME_CONTEXT_BUDGET_TOKENS far lower; 0 restores the previous
// always-resume behaviour exactly.
const defaultResumeContextBudgetTokens int64 = 8_000_000

// resumeContextBudgetTokens is the configured ceiling, or the default when the
// deployment set none. An explicit 0 is honoured as "disabled" — see the
// Config field for why that has to survive the read.
func (h *Handler) resumeContextBudgetTokens() int64 {
	if h.cfg.ResumeContextBudgetTokens != nil {
		return *h.cfg.ResumeContextBudgetTokens
	}
	return defaultResumeContextBudgetTokens
}

// resumeBudgetExceeded is the policy, separated from the lookup so it can be
// tested without a database.
//
// `>=` rather than `>`: the budget is a ceiling the session has reached, and an
// exactly-at-budget session is the one the next turn would push over.
func resumeBudgetExceeded(billed, budget int64) bool {
	return budget > 0 && billed >= budget
}

// resumeExceedsContextBudget reports whether the prior session for this
// (agent, issue) has already billed more input than the budget allows, so this
// turn should start a fresh conversation instead of inheriting it.
//
// Fails OPEN. A usage read that errors, or a deployment whose daemons never
// reported usage (every row zero), must not silently convert every follow-up
// into a cold start — losing continuity on a working issue is a worse outcome
// than paying for one more expensive turn, and it would be invisible. So an
// unknown cost is treated as within budget.
func (h *Handler) resumeExceedsContextBudget(ctx context.Context, task *db.AgentTaskQueue, sessionID string) bool {
	budget := h.resumeContextBudgetTokens()
	if budget <= 0 {
		return false
	}
	billed, err := h.Queries.GetSessionBilledInputTokens(ctx, db.GetSessionBilledInputTokensParams{
		AgentID:   task.AgentID,
		IssueID:   task.IssueID,
		SessionID: pgtype.Text{String: sessionID, Valid: true},
	})
	if err != nil {
		slog.Warn("resume context budget: usage lookup failed, resuming anyway",
			"task_id", uuidToString(task.ID),
			"agent_id", uuidToString(task.AgentID),
			"issue_id", uuidToString(task.IssueID),
			"session_id", sessionID,
			"error", err)
		return false
	}
	if !resumeBudgetExceeded(billed, budget) {
		return false
	}
	slog.Info("resume context budget exceeded; starting a fresh session",
		"task_id", uuidToString(task.ID),
		"agent_id", uuidToString(task.AgentID),
		"issue_id", uuidToString(task.IssueID),
		"session_id", sessionID,
		"billed_input_tokens", billed,
		"budget_tokens", budget)
	return true
}
