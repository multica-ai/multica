package sessions

// FIR-1931: the context bar is clickable and opens a sheet that breaks the
// issue's spend down PER SESSION (= per thread) and aggregates a total for the
// whole issue. This file serves that read-only breakdown from data we already
// have (task_usage + the last-turn footprint) — no new collection.
//
// Cost rule mirrors the rest of the cost views: prefer the exact gateway charge
// stored in task_usage.cost_cents; where a run reported none (cost_cents = 0,
// e.g. a daemon/non-gateway runtime), fall back to the token × pricing-table
// estimate (pricing.ComputeCents), so the figure is never silently zero.

import (
	"context"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/pricing"
)

type sessionUsageRow struct {
	SessionID         string `json:"session_id"` // thread root comment id
	Title             string `json:"title"`      // first line of the root comment (trimmed)
	CreatedAt         string `json:"created_at"`
	RunCount          int64  `json:"run_count"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	CacheReadTokens   int64  `json:"cache_read_tokens"`
	CacheWriteTokens  int64  `json:"cache_write_tokens"`
	CostCents         int64  `json:"cost_cents"`
	Model             string `json:"model"`
	UsedPercent       int    `json:"used_percent"` // current window fullness (latest run)
	ContextTokens     int64  `json:"context_tokens"`
	MaxContextTokens  int64  `json:"max_context_tokens"`
	CacheSharePercent int    `json:"cache_share_percent"`
	Approximate       bool   `json:"approximate"`
}

type usageBreakdownResponse struct {
	Sessions              []sessionUsageRow `json:"sessions"`
	TotalCostCents        int64             `json:"total_cost_cents"`
	TotalInputTokens      int64             `json:"total_input_tokens"`
	TotalOutputTokens     int64             `json:"total_output_tokens"`
	TotalCacheReadTokens  int64             `json:"total_cache_read_tokens"`
	TotalCacheWriteTokens int64             `json:"total_cache_write_tokens"`
}

// UsageBreakdown serves GET
// /api/cerebro/issues/{issueId}/sessions/usage-breakdown — one row per session
// (thread) with its summed cost/tokens/cache + current context fullness, plus
// the issue totals. Sessions are ordered oldest-first; the oldest (session 1)
// adopts the cold-start run with no trigger comment, exactly as ContextUsage.
func (h *Handler) UsageBreakdown(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}

	rows, err := h.pool.Query(r.Context(), `
		SELECT id, created_at, COALESCE(content, '')
		FROM comment
		WHERE issue_id = $1 AND parent_id IS NULL AND type = 'comment'
		ORDER BY created_at ASC, id ASC`, issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}
	type root struct {
		id        pgtype.UUID
		createdAt pgtype.Timestamptz
		content   string
	}
	var roots []root
	for rows.Next() {
		var rt root
		if err := rows.Scan(&rt.id, &rt.createdAt, &rt.content); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to read sessions")
			return
		}
		roots = append(roots, rt)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read sessions")
		return
	}

	resp := usageBreakdownResponse{Sessions: []sessionUsageRow{}}
	for i, rt := range roots {
		isFirst := i == 0
		agg, err := h.sessionUsageAgg(r.Context(), issue.ID, rt.id, isFirst)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read session usage")
			return
		}
		if agg.runCount == 0 {
			continue // a thread with no agent run yet contributes nothing
		}

		out := sessionUsageRow{
			SessionID:        util.UUIDToString(rt.id),
			Title:            snippet(rt.content, 80),
			RunCount:         agg.runCount,
			InputTokens:      agg.input,
			OutputTokens:     agg.output,
			CacheReadTokens:  agg.cacheRead,
			CacheWriteTokens: agg.cacheWrite,
			CostCents:        agg.costCents,
			Model:            agg.model,
		}
		if rt.createdAt.Valid {
			out.CreatedAt = rt.createdAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}

		// Current window fullness = the latest run in this session, exactly as the
		// bar shows it (footprint-preferred, cumulative-fallback approximate).
		if u, found, err := h.latestRunUsage(r.Context(), issue.ID, rt.id, isFirst); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read session context")
			return
		} else if found {
			out.ContextTokens, out.MaxContextTokens, out.UsedPercent, out.CacheSharePercent, out.Approximate = u.derive()
			if out.Model == "" {
				out.Model = u.model
			}
		}

		resp.Sessions = append(resp.Sessions, out)
		resp.TotalCostCents += out.CostCents
		resp.TotalInputTokens += out.InputTokens
		resp.TotalOutputTokens += out.OutputTokens
		resp.TotalCacheReadTokens += out.CacheReadTokens
		resp.TotalCacheWriteTokens += out.CacheWriteTokens
	}

	writeJSON(w, http.StatusOK, resp)
}

// sessionAgg is the summed spend/tokens for one session across all its runs.
type sessionAgg struct {
	runCount                              int64
	input, output, cacheRead, cacheWrite int64
	costCents                            int64
	model                                string // the heaviest-by-input model in the session, for display
}

// sessionUsageAgg sums task_usage across every run in the session's thread
// subtree (same membership as latestRunUsage). Cost is summed per run: the exact
// gateway cost_cents when present, else the pricing-table estimate so a
// daemon/non-gateway run is never booked as free.
func (h *Handler) sessionUsageAgg(ctx context.Context, issueID, rootID pgtype.UUID, isFirst bool) (sessionAgg, error) {
	const q = `
		WITH RECURSIVE thread(id) AS (
			SELECT $2::uuid
			UNION ALL
			SELECT c.id FROM comment c JOIN thread ON c.parent_id = thread.id
		)
		SELECT tu.model,
		       tu.input_tokens, tu.output_tokens,
		       tu.cache_read_tokens, tu.cache_write_tokens,
		       COALESCE(tu.cost_cents, 0)
		FROM agent_task_queue t
		JOIN task_usage tu ON tu.task_id = t.id
		WHERE t.issue_id = $1
		  AND ( t.trigger_comment_id IN (SELECT id FROM thread)
		        OR ($3::bool AND t.trigger_comment_id IS NULL) )`

	rows, err := h.pool.Query(ctx, q, issueID, rootID, isFirst)
	if err != nil {
		return sessionAgg{}, err
	}
	defer rows.Close()

	var agg sessionAgg
	var topInput int64
	for rows.Next() {
		var model pgtype.Text
		var input, output, cacheRead, cacheWrite, cost int64
		if err := rows.Scan(&model, &input, &output, &cacheRead, &cacheWrite, &cost); err != nil {
			return sessionAgg{}, err
		}
		agg.runCount++
		agg.input += input
		agg.output += output
		agg.cacheRead += cacheRead
		agg.cacheWrite += cacheWrite
		if cost > 0 {
			agg.costCents += cost
		} else {
			agg.costCents += pricing.ComputeCents(model.String, pricing.Usage{
				InputTokens:      input,
				OutputTokens:     output,
				CacheReadTokens:  cacheRead,
				CacheWriteTokens: cacheWrite,
			})
		}
		// Display the model that read the most input over the session; ties keep
		// the first non-empty one. Good enough for the per-session model label.
		if strings.TrimSpace(model.String) != "" && (agg.model == "" || input > topInput) {
			agg.model = model.String
			topInput = input
		}
	}
	if err := rows.Err(); err != nil {
		return sessionAgg{}, err
	}
	return agg, nil
}
