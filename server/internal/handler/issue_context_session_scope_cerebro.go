package handler

// CEREBRO-PATCH(session-scoped-context): FIR-1741 Model B engine. This is the
// "a new thread/session = a fresh context window" motor, not the surface.
//
// The surface (PR #1657) made a session a contiguous slice of the comment
// timeline, derived from session-start markers in cerebro_session — no
// per-comment field (migration 9097). This file makes that slice *mean*
// something to a running agent: when the cerebro_comment_chapters flag is ON and
// the issue has session markers, the bundled `issue context` read an agent gets
// at run-start is scoped to the session its triggering comment belongs to. The
// agent sees only that session's slice of the timeline, never the whole issue
// history — which is exactly what "fresh context per session" means. Because
// membership is derived from timeline order (there is no field to set), a run in
// a new session simply cannot inherit the previous session's context.
//
// Ships dark: default OFF, scopes nothing for human callers (no task token) and
// nothing for issues with no session markers (the implicit single "Session 1"
// that is the whole timeline). All scoping is therefore additive and reversible
// by the flag alone.

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const commentChaptersFlagKey = "cerebro_comment_chapters"

// cerebroCommentChaptersEnabled resolves the workspace flag with the same
// precedence as packages/cerebro-feature-flags/store.ts: locked workspace >
// personal > unlocked workspace > default. Unlike the child-notify flags this
// one defaults OFF: the engine ships dark and only scopes context where the flag
// is explicitly turned on, so deploy changes nothing until a workspace opts in.
func (h *Handler) cerebroCommentChaptersEnabled(ctx context.Context, workspaceID pgtype.UUID, requesterUserID string) bool {
	if h.CerebroQueries == nil || !workspaceID.Valid {
		return false
	}

	var wsEnabled, wsLocked, wsFound bool
	wsRows, err := h.CerebroQueries.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		slog.Warn("comment sessions flag: workspace flag lookup failed", "error", err)
		return false
	}
	for _, row := range wsRows {
		if row.FlagKey == commentChaptersFlagKey {
			wsEnabled, wsLocked, wsFound = row.Enabled, row.Locked, true
			break
		}
	}
	if wsFound && wsLocked {
		return wsEnabled
	}

	if requesterUserID != "" {
		if userID, parseErr := util.ParseUUID(requesterUserID); parseErr == nil {
			on, perr := h.CerebroQueries.GetCerebroFeatureFlag(ctx, cerebrodb.GetCerebroFeatureFlagParams{
				WorkspaceID: workspaceID,
				UserID:      userID,
				FlagKey:     commentChaptersFlagKey,
			})
			if perr == nil {
				return on
			}
			if !errors.Is(perr, pgx.ErrNoRows) {
				slog.Warn("comment sessions flag: personal flag lookup failed", "error", perr)
				return false
			}
		}
	}

	if wsFound {
		return wsEnabled
	}
	return false
}

// sessionWindow is the [Start, End) time window of one Model B session. End is
// only meaningful when HasEnd is true; the latest (open) session has no end and
// runs "to now", so HasEnd is false for it.
type sessionWindow struct {
	Start  time.Time
	End    time.Time
	HasEnd bool
}

// contains reports whether a timeline entry created at t belongs to this window.
// Lower bound inclusive, upper bound exclusive — the same half-open convention
// the surface uses in grouping.ts so the engine and the UI agree on membership.
func (w sessionWindow) contains(t time.Time) bool {
	if t.Before(w.Start) {
		return false
	}
	if w.HasEnd && !t.Before(w.End) {
		return false
	}
	return true
}

// sessionWindowForComment returns the timeline window of the session that the
// comment created at commentAt belongs to, given the issue's session-start
// times in any order. Model B membership: a comment belongs to the latest
// session whose start is at or before it; a comment earlier than the first
// marker falls into the earliest session (nothing is ever orphaned). Returns
// ok=false when there are no markers — that is the implicit single "Session 1"
// covering the whole timeline, where no scoping should happen.
func sessionWindowForComment(starts []time.Time, commentAt time.Time) (sessionWindow, bool) {
	if len(starts) == 0 {
		return sessionWindow{}, false
	}
	sorted := make([]time.Time, len(starts))
	copy(sorted, starts)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })

	idx := 0
	for i, s := range sorted {
		if !s.After(commentAt) { // s <= commentAt
			idx = i
		} else {
			break
		}
	}

	w := sessionWindow{Start: sorted[idx]}
	if idx+1 < len(sorted) {
		w.End = sorted[idx+1]
		w.HasEnd = true
	}
	return w, true
}

// sessionScopeForContext derives the session window for the current task-scoped
// request, or ok=false when no scoping applies (human call, no trigger comment,
// or an issue with no session markers). It is the bridge between the task that
// triggered the run and the Model B slice that run should see.
func (h *Handler) sessionScopeForContext(ctx context.Context, issue db.Issue) (sessionWindow, bool) {
	ts := middleware.TaskScopeFromContext(ctx)
	if ts.TaskID == "" {
		return sessionWindow{}, false // human call — never scope
	}
	taskUUID, err := util.ParseUUID(ts.TaskID)
	if err != nil {
		return sessionWindow{}, false
	}
	task, err := h.Queries.GetAgentTask(ctx, taskUUID)
	if err != nil || !task.TriggerCommentID.Valid {
		return sessionWindow{}, false // cold start / no triggering comment → whole issue
	}
	trigger, err := h.Queries.GetComment(ctx, task.TriggerCommentID)
	if err != nil || !trigger.CreatedAt.Valid {
		return sessionWindow{}, false
	}

	starts, err := h.sessionStartTimes(ctx, issue.ID)
	if err != nil {
		slog.Warn("session scope: start-times lookup failed", "issue_id", uuidToString(issue.ID), "error", err)
		return sessionWindow{}, false
	}
	return sessionWindowForComment(starts, trigger.CreatedAt.Time)
}

// sessionStartTimes returns the created_at of every session marker on the issue.
// Empty (no rows) means the implicit single Session 1 — the caller treats that
// as "no scoping".
func (h *Handler) sessionStartTimes(ctx context.Context, issueID pgtype.UUID) ([]time.Time, error) {
	rows, err := h.DB.Query(ctx, `SELECT created_at FROM cerebro_session WHERE issue_id = $1`, issueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// scopeContextComments filters a comment slice to the session window. Each
// comment is judged by its own created_at, so a run only ever sees what was
// written during its session — the literal definition of a fresh context
// window. Returns the input unchanged when the slice is empty.
func scopeContextComments(comments []db.Comment, w sessionWindow) []db.Comment {
	if len(comments) == 0 {
		return comments
	}
	out := comments[:0]
	for _, c := range comments {
		if c.CreatedAt.Valid && !w.contains(c.CreatedAt.Time.UTC()) {
			continue
		}
		out = append(out, c)
	}
	return out
}
