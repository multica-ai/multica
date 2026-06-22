package sessions

// FIR-1874 thread = session — context-window measurement (engine "motor").
//
// The surface tells you a session (= a thread) exists; the engine
// (issue_context_session_scope in the handler package) makes a run in a thread
// receive only that thread's slice. This file answers the question the quiet
// indicator needs: "how full is this session's context window right now, and how
// much of it was cache?" — from ground truth (task_usage), never a guess.
//
// Definition: the fullness of a session's context window is the WHOLE prompt
// footprint of the most recent agent run TRIGGERED INSIDE that thread (its
// trigger comment is the thread root or a reply to it). That is literally how
// many tokens the model last had to read — and the prompt is input + cache_read
// + cache_write (fresh + cache-hits + cache-creation), NOT just the fresh
// `input_tokens`. With prompt caching warm, almost the entire prompt is served
// from cache, so counting `input_tokens` alone reported ~0% even when the window
// was ~40% full (FIR-1839 1D). cache_share = cache_read / context.

import (
	"context"
	"net/http"

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
// FIR-1874: a session is a thread, so the optional session_id query param is the
// THREAD ROOT comment id. Empty resolves the ACTIVE session = the latest open
// (unresolved) thread, else the latest thread. has_data=false when no run with
// recorded usage has happened in that thread yet — the caller renders nothing.
func (h *Handler) ContextUsage(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}

	requested := r.URL.Query().Get("session_id")
	rootID, sessionID, found, err := h.resolveThreadRoot(r.Context(), issue.ID, requested)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve active session")
		return
	}
	resp := contextUsageResponse{SessionID: sessionID}
	if !found {
		writeJSON(w, http.StatusOK, resp) // no thread yet — has_data stays false
		return
	}

	// Latest run TRIGGERED inside this thread (trigger comment is the root or a
	// reply to it) that has recorded token usage. cf.* is the last-turn footprint
	// (the size of the prompt the model last read); when present it is the
	// authoritative numerator. The task_usage SUMs are the fallback for runtimes
	// that report no footprint yet.
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
		  AND t.trigger_comment_id IS NOT NULL
		  AND (t.trigger_comment_id = $2
		       OR t.trigger_comment_id IN (SELECT id FROM comment WHERE parent_id = $2))
		GROUP BY t.id, t.created_at
		ORDER BY t.created_at DESC
		LIMIT 1`

	var input, cacheRead, cacheWrite, output int64
	var footprintInput, footprintCacheRead int64
	var model pgtype.Text
	err = h.pool.QueryRow(r.Context(), q, issue.ID, rootID).Scan(&input, &cacheRead, &cacheWrite, &output, &model, &footprintInput, &footprintCacheRead)
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
// top. `footprintCacheRead` is the cached subset, used only for the display.
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
// input + cache_read + cache_write — NOT `input` alone, which is a tiny slice
// once prompt caching is warm.
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

// resolveThreadRoot turns the optional session_id query value into a thread root
// comment id. A valid UUID is taken as the requested thread root. Empty/"default"
// resolves the ACTIVE session: the latest open (unresolved) thread root, else the
// latest thread root. found=false when the issue has no comment threads yet.
func (h *Handler) resolveThreadRoot(ctx context.Context, issueID pgtype.UUID, requested string) (rootID pgtype.UUID, sessionID string, found bool, err error) {
	if requested != "" && requested != "default" {
		if id, perr := util.ParseUUID(requested); perr == nil {
			var exists bool
			if err = h.pool.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM comment WHERE id = $1 AND issue_id = $2 AND parent_id IS NULL)`,
				id, issueID).Scan(&exists); err != nil {
				return pgtype.UUID{}, "", false, err
			}
			if exists {
				return id, requested, true, nil
			}
		}
	}

	// Active session = latest open thread, else latest thread.
	var id pgtype.UUID
	err = h.pool.QueryRow(ctx, `
		SELECT id FROM comment
		WHERE issue_id = $1 AND parent_id IS NULL AND type = 'comment'
		ORDER BY (resolved_at IS NULL) DESC, created_at DESC, id DESC
		LIMIT 1`, issueID).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return pgtype.UUID{}, "", false, nil
		}
		return pgtype.UUID{}, "", false, err
	}
	return id, util.UUIDToString(id), true, nil
}
