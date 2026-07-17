package sessions

// FIR-1931: the development curve over a session. The session sheet draws how the
// context window filled across calls in a session from model_usage_event and
// marks provider-explicit or inferred drops. Legacy per-run footprint history
// remains the fallback for tasks that predate canonical events.
//
// Canonical points are call-level where the adapter exposes that granularity.
// Aggregate-only adapters and historical tasks remain explicitly coarser.

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
		), canonical AS (
			SELECT m.model, m.context_tokens, m.cache_read_tokens, m.context_window_tokens,
			       m.observed_at, m.compaction_kind <> '' AS explicit_compaction,
			       m.sequence, m.created_at
			FROM agent_task_queue t
			JOIN model_usage_event m ON m.task_id = t.id
			WHERE t.issue_id = $1
			  AND m.context_tokens > 0
			  AND ( t.trigger_comment_id IN (SELECT id FROM thread)
			        OR ($3::bool AND t.trigger_comment_id IS NULL) )
		), legacy AS (
			SELECT h.model, h.input_tokens AS context_tokens, h.cache_read_tokens,
			       0::bigint AS context_window_tokens, h.observed_at,
			       false AS explicit_compaction, 0::bigint AS sequence, h.observed_at AS created_at
			FROM agent_task_queue t
			JOIN cerebro_task_context_footprint_history h ON h.task_id = t.id
			WHERE t.issue_id = $1
			  AND ( t.trigger_comment_id IN (SELECT id FROM thread)
			        OR ($3::bool AND t.trigger_comment_id IS NULL) )
			  AND NOT EXISTS (
				SELECT 1 FROM model_usage_event m
				WHERE m.task_id = t.id AND m.context_tokens > 0
			  )
		)
		SELECT model, context_tokens, cache_read_tokens, context_window_tokens,
		       observed_at, explicit_compaction
		FROM (
			SELECT * FROM canonical
			UNION ALL
			SELECT * FROM legacy
		) points
		ORDER BY observed_at ASC, sequence ASC, created_at ASC`

	rows, err := h.pool.Query(ctx, q, issueID, rootID, isFirst)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []contextTimelinePoint
	var prevTokens, prevMax int64
	for rows.Next() {
		var model pgtype.Text
		var input, cacheRead, reportedWindow int64
		var observedAt pgtype.Timestamptz
		var explicitCompaction bool
		if err := rows.Scan(&model, &input, &cacheRead, &reportedWindow, &observedAt, &explicitCompaction); err != nil {
			return nil, err
		}
		// The footprint input is already the whole prompt the model read (includes
		// the cached subset), so it IS the window occupancy — same as
		// computeContextFootprint.
		maxCtx := reportedWindow
		if maxCtx <= 0 {
			maxCtx = contextWindowForModel(model.String)
		}
		p := contextTimelinePoint{
			ContextTokens:    input,
			MaxContextTokens: maxCtx,
			IsCompaction:     explicitCompaction,
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
		if !p.IsCompaction && prevTokens > 0 && prevMax > 0 {
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

// sessionCompactionStats summarises the session's compaction boundaries for the
// lightweight context measurement (the bar), reusing the development-curve points
// so the heuristic lives in exactly one place. count is how many runs were a
// sharp drop from a fuller previous run; latestIsCompaction is true when the most
// recent run was itself such a drop — i.e. the window was just compacted.
func (h *Handler) sessionCompactionStats(ctx context.Context, issueID, rootID pgtype.UUID, isFirst bool) (count int, latestIsCompaction bool, err error) {
	points, err := h.sessionContextTimeline(ctx, issueID, rootID, isFirst)
	if err != nil {
		return 0, false, err
	}
	for _, p := range points {
		if p.IsCompaction {
			count++
		}
	}
	if n := len(points); n > 0 {
		latestIsCompaction = points[n-1].IsCompaction
	}
	return count, latestIsCompaction, nil
}
