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
//
// FIR-1931: the very first run on an issue is fired at issue creation and has NO
// triggering comment (trigger_comment_id IS NULL), so it matches no thread and
// its context is invisible — leaving session 1 stuck at 0 even though that
// cold-start prompt is exactly the context session 1 inherits. The oldest
// session (session 1) therefore ADOPTS runs with no trigger comment, carrying
// that cold-start footprint into session 1 instead of losing it.

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
	// Approximate is true when no last-turn footprint existed and we fell back to
	// the cumulative task_usage sum, which over-counts a warm-cache run. The bar
	// must then prefix "~" and the token figure is clamped to the window so a
	// heavy issue never displays an impossible figure like 6986k / 1000k tokens.
	Approximate bool `json:"approximate"`
	// FIR-1960: surface compaction tracking in the lightweight measurement, not
	// only the development-curve sheet. Compactions counts the sharp context drops
	// in this session's run history (a compaction or fresh turn after a fairly-full
	// run); LastRunCompaction is true when the most recent run was itself such a
	// drop — the window was just compacted. Both are read from
	// cerebro_task_context_footprint_history and stay zero/false for sessions that
	// predate footprint history.
	Compactions       int  `json:"compactions"`
	LastRunCompaction bool `json:"last_run_compaction"`
	// FIR-2279: when the most recent run in this session finished. The prompt
	// cache Anthropic keeps warm lives ~5 minutes past the last call, so the
	// context bar counts down from this toward a "cache cold" state — resuming
	// after it means the next run re-pays the full cache-write instead of a cheap
	// read. Nil (omitted) when there is no run with usage yet; an older client
	// simply shows no timer. RFC3339 UTC.
	LastActivityAt *string `json:"last_activity_at,omitempty"`
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

	// FIR-1931: is the resolved session the OLDEST thread on the issue (session
	// 1)? Only session 1 adopts the cold-start run that has no trigger comment.
	var isFirst bool
	err = h.pool.QueryRow(r.Context(), `
		SELECT $2::uuid = (
			SELECT id FROM comment
			WHERE issue_id = $1 AND parent_id IS NULL AND type = 'comment'
			ORDER BY created_at ASC, id ASC
			LIMIT 1)`, issue.ID, rootID).Scan(&isFirst)
	if err != nil && err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "failed to resolve first session")
		return
	}

	u, found2, err := h.latestRunUsage(r.Context(), issue.ID, rootID, isFirst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read context usage")
		return
	}
	if !found2 {
		writeJSON(w, http.StatusOK, resp) // no run with usage yet — has_data stays false
		return
	}

	resp.HasData = true
	resp.Model = u.model
	resp.InputTokens = u.input
	resp.CacheReadTokens = u.cacheRead
	resp.CacheWriteTokens = u.cacheWrite
	resp.OutputTokens = u.output
	resp.ContextTokens, resp.MaxContextTokens, resp.UsedPercent, resp.CacheSharePercent, resp.Approximate = u.derive()

	// FIR-2279: expose when the latest run finished so the bar can show the
	// prompt-cache warm/cold countdown. Only set when we actually have a
	// timestamp — otherwise the field is omitted and no timer renders.
	if u.lastActivityAt.Valid {
		s := u.lastActivityAt.Time.UTC().Format(time.RFC3339)
		resp.LastActivityAt = &s
	}

	// FIR-1960: track compactions inside the measurement itself. Best-effort — a
	// failure here must never blank the bar, so a query error just leaves the
	// counts at zero rather than failing the whole response.
	if comps, lastComp, cerr := h.sessionCompactionStats(r.Context(), issue.ID, rootID, isFirst); cerr == nil {
		resp.Compactions = comps
		resp.LastRunCompaction = lastComp
	}
	writeJSON(w, http.StatusOK, resp)
}

// runUsage holds one run's token components: the cumulative task_usage sums plus
// the last-turn footprint (zero when the runtime reported none).
type runUsage struct {
	input, cacheRead, cacheWrite, output int64
	footprintInput, footprintCacheRead   int64
	model                                string
	// FIR-2279: when the latest run in the session finished (completed_at, else
	// started_at, else created_at) — the anchor for the prompt-cache countdown.
	lastActivityAt pgtype.Timestamptz
}

// derive turns a run's components into the displayed context figures: prefer the
// exact last-turn footprint, else fall back to the cumulative sum (approximate,
// clamped to the window — FIR-1931 Fix C).
func (u runUsage) derive() (contextTokens, maxContext int64, usedPercent, cacheSharePercent int, approximate bool) {
	if u.footprintInput > 0 {
		return computeContextFootprint(u.footprintInput, u.footprintCacheRead, u.model)
	}
	return computeContextUsage(u.input, u.cacheRead, u.cacheWrite, u.model)
}

// latestRunUsage returns the most recent run with recorded usage that belongs to
// the session rooted at rootID (its trigger comment is the root or any reply
// beneath it, at any depth). cf.* is the last-turn footprint (the size of the
// prompt the model last read); when present it is the authoritative numerator,
// and the task_usage SUMs are the fallback for runtimes that report no footprint.
//
// Thread membership is depth-independent: a session is the root thread and EVERY
// comment beneath it, at any nesting. An earlier check only matched the root +
// its DIRECT replies (`parent_id = $2`), which silently dropped a run whose
// trigger sat at depth ≥2 — and the parent-must-equal-trigger rule
// (handler/comment.go) lands an agent's reply at depth 2 the moment it is
// triggered by a depth-1 comment, so a follow-up triggered there fell out of the
// session. The recursive CTE walks the whole thread subtree so the "latest run in
// this session" is correct regardless of how deep the back-and-forth nested
// (FIR-1931). The isFirst branch additionally lets the oldest session adopt a
// cold-start run with no trigger comment; since it is the oldest run, ORDER BY
// created_at DESC means any real session-1 run still wins — the orphan only
// surfaces when session 1 has no run of its own. found=false when the session has
// no run with usage yet.
func (h *Handler) latestRunUsage(ctx context.Context, issueID, rootID pgtype.UUID, isFirst bool) (runUsage, bool, error) {
	const q = `
		WITH RECURSIVE thread(id) AS (
			SELECT $2::uuid
			UNION ALL
			SELECT c.id FROM comment c JOIN thread ON c.parent_id = thread.id
		)
		SELECT COALESCE(SUM(tu.input_tokens), 0),
		       COALESCE(SUM(tu.cache_read_tokens), 0),
		       COALESCE(SUM(tu.cache_write_tokens), 0),
		       COALESCE(SUM(tu.output_tokens), 0),
		       MAX(tu.model),
		       COALESCE(MAX(cf.input_tokens), 0),
		       COALESCE(MAX(cf.cache_read_tokens), 0),
		       COALESCE(t.completed_at, t.started_at, t.created_at)
		FROM agent_task_queue t
		JOIN task_usage tu ON tu.task_id = t.id
		LEFT JOIN cerebro_task_context_footprint cf ON cf.task_id = t.id
		WHERE t.issue_id = $1
		  AND ( t.trigger_comment_id IN (SELECT id FROM thread)
		        OR ($3::bool AND t.trigger_comment_id IS NULL) )
		GROUP BY t.id, t.created_at
		ORDER BY t.created_at DESC
		LIMIT 1`

	var u runUsage
	var model pgtype.Text
	err := h.pool.QueryRow(ctx, q, issueID, rootID, isFirst).
		Scan(&u.input, &u.cacheRead, &u.cacheWrite, &u.output, &model, &u.footprintInput, &u.footprintCacheRead, &u.lastActivityAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return runUsage{}, false, nil
		}
		return runUsage{}, false, err
	}
	u.model = model.String
	return u, true, nil
}

// computeContextFootprint is the FIR-1856 path for runtimes that report a
// last-turn footprint (Codex/gpt). `footprintInput` is the size of the prompt
// the model last read — for OpenAI accounting it already includes the cached
// tokens, so it IS the window occupancy and must not have cacheRead added on
// top. `footprintCacheRead` is the cached subset, used only for the display.
func computeContextFootprint(footprintInput, footprintCacheRead int64, model string) (contextTokens, maxContext int64, usedPercent, cacheSharePercent int, approximate bool) {
	contextTokens = footprintInput
	maxContext = contextWindowForModel(model)
	if maxContext > 0 {
		usedPercent = clampPercent(int(contextTokens * 100 / maxContext))
	}
	if contextTokens > 0 {
		cacheSharePercent = clampPercent(int(footprintCacheRead * 100 / contextTokens))
	}
	// An exact last-turn measure — never approximate.
	return contextTokens, maxContext, usedPercent, cacheSharePercent, false
}

// computeContextUsage is the pure core of the measurement: from a run's raw
// token components it derives the whole-prompt context size, the model window,
// fullness percent and cache share. The prompt the model actually read is
// input + cache_read + cache_write — NOT `input` alone, which is a tiny slice
// once prompt caching is warm.
func computeContextUsage(input, cacheRead, cacheWrite int64, model string) (contextTokens, maxContext int64, usedPercent, cacheSharePercent int, approximate bool) {
	raw := input + cacheRead + cacheWrite
	maxContext = contextWindowForModel(model)
	// The cumulative task_usage sum over a whole run is never an exact last-turn
	// measure; on a long warm-cache run it sums cache_read across every turn and
	// can exceed the window many times over. Mark it approximate so the bar
	// prefixes "~", and clamp the displayed token figure to the window so a heavy
	// issue never shows an impossible count like 6986k / 1000k tokens.
	approximate = true
	contextTokens = raw
	if maxContext > 0 && contextTokens > maxContext {
		contextTokens = maxContext
	}
	if maxContext > 0 {
		usedPercent = clampPercent(int(raw * 100 / maxContext))
	}
	if raw > 0 {
		cacheSharePercent = clampPercent(int(cacheRead * 100 / raw))
	}
	return contextTokens, maxContext, usedPercent, cacheSharePercent, approximate
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
