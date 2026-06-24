package sessions

// FIR-1931: the development curve over a session. The session sheet draws how the
// context window filled up across a session's runs (one point per agent run, from
// cerebro_task_context_footprint_history) and marks where it dropped — a fresh
// turn or a compaction.
//
// Honest scope: the footprint is recorded once per run (at completion), so each
// point is one run. A session is many runs, so plotting them over time is the
// development curve over the session. True per-turn-within-a-run granularity would
// require emitting every turn from each runtime backend — a larger, separate
// change. The curve is forward-only: history is collected from this deploy on, so
// sessions that ran before it have no points (the sheet shows an empty state).

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
)

type contextTimelinePoint struct {
	ObservedAt        string `json:"observed_at"`
	ContextTokens     int64  `json:"context_tokens"`
	MaxContextTokens  int64  `json:"max_context_tokens"`
	UsedPercent       int    `json:"used_percent"`
	CacheSharePercent int    `json:"cache_share_percent"`
	// IsCompaction marks a point where the context dropped sharply from the
	// previous run — a compaction or a fresh turn. Computed at read time.
	IsCompaction bool `json:"is_compaction"`
}

type contextTimelineResponse struct {
	SessionID string                 `json:"session_id"`
	HasData   bool                   `json:"has_data"`
	Points    []contextTimelinePoint `json:"points"`
}

// Compaction heuristic, mirroring how Claude Code detects a compaction: a sharp
// drop in context between two runs. A point is a compaction boundary if its
// footprint fell below this fraction of the previous run's AND the previous run
// was at least this full of the window. Tunable at read time (no migration).
const (
	compactionDropToFraction  = 0.60 // dropped below 60% of the previous run's footprint
	compactionPrevFullPercent = 50   // and the previous run filled > 50% of the window
)

// ContextTimeline serves GET
// /api/cerebro/issues/{issueId}/sessions/{sessionId}/context-timeline — the
// per-run footprint history for a session's thread, ordered oldest-first, with
// compaction boundaries flagged. has_data=false when the session has no recorded
// history yet (e.g. it ran before this feature deployed).
func (h *Handler) ContextTimeline(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssue(w, r)
	if !ok {
		return
	}
	rootID, ok := parseUUIDParam(w, r, "sessionId")
	if !ok {
		return
	}

	// Is this the oldest thread (session 1)? Only it adopts the cold-start run.
	var isFirst bool
	if err := h.pool.QueryRow(r.Context(), `
		SELECT $2::uuid = (
			SELECT id FROM comment
			WHERE issue_id = $1 AND parent_id IS NULL AND type = 'comment'
			ORDER BY created_at ASC, id ASC
			LIMIT 1)`, issue.ID, rootID).Scan(&isFirst); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve first session")
		return
	}

	points, err := h.sessionContextTimeline(r.Context(), issue.ID, rootID, isFirst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read context timeline")
		return
	}
	resp := contextTimelineResponse{
		SessionID: util.UUIDToString(rootID),
		HasData:   len(points) > 0,
		Points:    points,
	}
	if resp.Points == nil {
		resp.Points = []contextTimelinePoint{}
	}
	writeJSON(w, http.StatusOK, resp)
}

// sessionContextTimeline reads the footprint history for every run in the
// session's thread subtree (same membership as latestRunUsage), ordered by
// observation time, and computes window fullness + compaction flags per point.
func (h *Handler) sessionContextTimeline(ctx context.Context, issueID, rootID pgtype.UUID, isFirst bool) ([]contextTimelinePoint, error) {
	const q = `
		WITH RECURSIVE thread(id) AS (
			SELECT $2::uuid
			UNION ALL
			SELECT c.id FROM comment c JOIN thread ON c.parent_id = thread.id
		)
		SELECT h.model, h.input_tokens, h.cache_read_tokens, h.observed_at
		FROM agent_task_queue t
		JOIN cerebro_task_context_footprint_history h ON h.task_id = t.id
		WHERE t.issue_id = $1
		  AND ( t.trigger_comment_id IN (SELECT id FROM thread)
		        OR ($3::bool AND t.trigger_comment_id IS NULL) )
		ORDER BY h.observed_at ASC, h.id ASC`

	rows, err := h.pool.Query(ctx, q, issueID, rootID, isFirst)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []contextTimelinePoint
	var prevTokens, prevMax int64
	for rows.Next() {
		var model pgtype.Text
		var input, cacheRead int64
		var observedAt pgtype.Timestamptz
		if err := rows.Scan(&model, &input, &cacheRead, &observedAt); err != nil {
			return nil, err
		}
		// The footprint input is already the whole prompt the model read (includes
		// the cached subset), so it IS the window occupancy — same as
		// computeContextFootprint.
		maxCtx := contextWindowForModel(model.String)
		p := contextTimelinePoint{
			ContextTokens:    input,
			MaxContextTokens: maxCtx,
		}
		if observedAt.Valid {
			p.ObservedAt = observedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		if maxCtx > 0 {
			p.UsedPercent = clampPercent(int(input * 100 / maxCtx))
		}
		if input > 0 {
			p.CacheSharePercent = clampPercent(int(cacheRead * 100 / input))
		}

		// Compaction: a sharp drop from the previous run that was itself fairly full.
		if prevTokens > 0 && prevMax > 0 {
			prevUsed := clampPercent(int(prevTokens * 100 / prevMax))
			if float64(input) < compactionDropToFraction*float64(prevTokens) &&
				prevUsed > compactionPrevFullPercent {
				p.IsCompaction = true
			}
		}
		prevTokens, prevMax = input, maxCtx

		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return points, nil
}
