package sessions

// FIR-1741 Model B — context-window measurement (item 1 of the engine "motor").
//
// The surface tells you a session exists; the engine (issue_context_session_scope
// in the handler package) makes a run in a new session receive only that
// session's slice. This file answers the question the quiet indicator needs:
// "how full is the active session's context window right now, and how much of it
// was cache?" — from ground truth (task_usage), never a guess.
//
// Definition: the fullness of a session's context window is the WHOLE prompt
// footprint of the most recent agent run that happened inside that session
// (task.created_at within the session's [start,next) window). That is literally
// how many tokens the model last had to read — and the prompt is
// input + cache_read + cache_write (fresh + cache-hits + cache-creation), NOT
// just the fresh `input_tokens`. With prompt caching warm, almost the entire
// prompt is served from cache, so counting `input_tokens` alone reported ~0%
// even when the window was ~40% full (FIR-1839 1D). cache_share = cache_read /
// context — the proportion of that prompt that came from cache rather than fresh.

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

type contextUsageResponse struct {
	SessionID        string `json:"session_id"`
	HasData          bool   `json:"has_data"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	// ContextTokens is the whole prompt the model last read: input + cache_read
	// + cache_write. This — not InputTokens alone — is the numerator for window
	// fullness, and the figure the indicator should display.
	ContextTokens     int64 `json:"context_tokens"`
	MaxContextTokens  int64 `json:"max_context_tokens"`
	UsedPercent       int   `json:"used_percent"`
	CacheSharePercent int   `json:"cache_share_percent"`
}

// ContextUsage serves GET /api/cerebro/issues/{issueId}/sessions/context-usage.
// It reports the context-window state of the issue's ACTIVE session (first
// non-done, else the latest). has_data=false when no run with recorded usage
// has happened in that window yet — the caller then renders nothing.
func (h *Handler) ContextUsage(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}

	sessionID, start, end, hasEnd, err := h.activeSessionWindow(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve active session")
		return
	}

	resp := contextUsageResponse{SessionID: sessionID}

	// Latest run inside the window that has recorded token usage. Membership is
	// by run start time relative to the session markers — the same Model B order
	// the surface uses, so the indicator and the timeline agree.
	// cf.* is the last-turn footprint: the size of the prompt the model last read,
	// i.e. how full the window is. When present it is the authoritative numerator.
	// FIR-1856 recorded it for Codex; FIR-1870 extended it to the other streaming
	// runtimes (Claude, Cursor, Pi, OpenCode, OpenClaw, the local runtime) because
	// the task_usage figure is the LIFETIME SUM for them too — with prompt caching
	// warm each turn re-reads the cached prefix, so the summed cache_read is several
	// times the window and pins the gauge at 100% (FIR-1870's 3350k/1000k). The
	// task_usage SUMs below remain the fallback only for runtimes that report no
	// footprint yet.
	const q = `
		SELECT COALESCE(SUM(tu.input_tokens), 0),
		       COALESCE(SUM(tu.cache_read_tokens), 0),
		       COALESCE(SUM(tu.cache_write_tokens), 0),
		       COALESCE(SUM(tu.output_tokens), 0),
		       MAX(tu.model),
		       COALESCE(MAX(cf.input_tokens), 0),
		       COALESCE(MAX(cf.cache_read_tokens), 0)
		FROM agent_task_queue t
		JOIN task_usage tu ON tu.task_id = t.id
		LEFT JOIN cerebro_task_context_footprint cf ON cf.task_id = t.id
		WHERE t.issue_id = $1
		  AND t.created_at >= $2
		  AND ($3::timestamptz IS NULL OR t.created_at < $3)
		GROUP BY t.id, t.created_at
		ORDER BY t.created_at DESC
		LIMIT 1`

	var endParam any
	if hasEnd {
		endParam = end
	}
	var input, cacheRead, cacheWrite, output int64
	var footprintInput, footprintCacheRead int64
	var model pgtype.Text
	err = h.pool.QueryRow(r.Context(), q, issue.ID, start, endParam).Scan(&input, &cacheRead, &cacheWrite, &output, &model, &footprintInput, &footprintCacheRead)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeJSON(w, http.StatusOK, resp) // no run with usage yet — has_data stays false
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to read context usage")
		return
	}

	resp.HasData = true
	resp.Model = model.String
	resp.InputTokens = input
	resp.CacheReadTokens = cacheRead
	resp.CacheWriteTokens = cacheWrite
	resp.OutputTokens = output
	if footprintInput > 0 {
		// FIR-1856: Codex/gpt runtimes record the last turn's footprint. It is
		// the prompt the model last read (input already includes cached for
		// OpenAI), so it is the window occupancy directly — not a sum of turns.
		resp.ContextTokens, resp.MaxContextTokens, resp.UsedPercent, resp.CacheSharePercent =
			computeContextFootprint(footprintInput, footprintCacheRead, model.String)
	} else {
		resp.ContextTokens, resp.MaxContextTokens, resp.UsedPercent, resp.CacheSharePercent =
			computeContextUsage(input, cacheRead, cacheWrite, model.String)
	}
	writeJSON(w, http.StatusOK, resp)
}

// computeContextFootprint is the FIR-1856 path for runtimes that report a
// last-turn footprint (Codex/gpt). `footprintInput` is the size of the prompt
// the model last read — for OpenAI accounting it already includes the cached
// tokens, so it IS the window occupancy and must not have cacheRead added on
// top (that is the double-count that made Codex read 1955k/272k and pin at
// 100%). `footprintCacheRead` is the cached subset, used only for the display.
func computeContextFootprint(footprintInput, footprintCacheRead int64, model string) (contextTokens, maxContext int64, usedPercent, cacheSharePercent int) {
	contextTokens = footprintInput
	maxContext = contextWindowForModel(model)
	if maxContext > 0 {
		usedPercent = clampPercent(int(contextTokens * 100 / maxContext))
	}
	if contextTokens > 0 {
		cacheSharePercent = clampPercent(int(footprintCacheRead * 100 / contextTokens))
	}
	return contextTokens, maxContext, usedPercent, cacheSharePercent
}

// computeContextUsage is the pure core of the measurement: from a run's raw
// token components it derives the whole-prompt context size, the model window,
// fullness percent and cache share. The prompt the model actually read is
// input + cache_read + cache_write (fresh + cache hits + cache creation) — NOT
// `input` alone, which is a tiny slice once prompt caching is warm. Counting
// `input` only is what made the indicator read ~0% on a session that was
// ~40% full (FIR-1839 1D).
func computeContextUsage(input, cacheRead, cacheWrite int64, model string) (contextTokens, maxContext int64, usedPercent, cacheSharePercent int) {
	contextTokens = input + cacheRead + cacheWrite
	maxContext = contextWindowForModel(model)
	if maxContext > 0 {
		usedPercent = clampPercent(int(contextTokens * 100 / maxContext))
	}
	if contextTokens > 0 {
		cacheSharePercent = clampPercent(int(cacheRead * 100 / contextTokens))
	}
	return contextTokens, maxContext, usedPercent, cacheSharePercent
}

func clampPercent(p int) int {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// activeSessionWindow returns the active session's id and timeline window. The
// active session is the first non-done marker, else the latest. When the issue
// has no session markers it is the implicit single "Session 1" covering the
// whole timeline: id "default", start = zero time, no end.
func (h *Handler) activeSessionWindow(ctx context.Context, issueID pgtype.UUID) (sessionID string, start time.Time, end time.Time, hasEnd bool, err error) {
	rows, err := h.pool.Query(ctx, `
		SELECT id, status, created_at
		FROM cerebro_session
		WHERE issue_id = $1
		ORDER BY created_at ASC, position ASC, id ASC`, issueID)
	if err != nil {
		return "", time.Time{}, time.Time{}, false, err
	}
	defer rows.Close()

	type marker struct {
		id      string
		status  string
		created time.Time
	}
	var markers []marker
	for rows.Next() {
		var id pgtype.UUID
		var status string
		var created time.Time
		if err := rows.Scan(&id, &status, &created); err != nil {
			return "", time.Time{}, time.Time{}, false, err
		}
		markers = append(markers, marker{id: util.UUIDToString(id), status: status, created: created.UTC()})
	}
	if err := rows.Err(); err != nil {
		return "", time.Time{}, time.Time{}, false, err
	}

	// No markers → implicit Session 1 = whole timeline.
	if len(markers) == 0 {
		return "default", time.Time{}, time.Time{}, false, nil
	}

	activeIdx := len(markers) - 1 // default: latest
	for i, m := range markers {
		if m.status != "done" {
			activeIdx = i
			break
		}
	}
	active := markers[activeIdx]
	if activeIdx+1 < len(markers) {
		return active.id, active.created, markers[activeIdx+1].created, true, nil
	}
	return active.id, active.created, time.Time{}, false, nil
}
