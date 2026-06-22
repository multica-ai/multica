package handler

// CEREBRO-PATCH(session-scoped-context): FIR-1874 thread = session engine. This
// is the "a thread = a fresh context window" motor, not the surface.
//
// FIR-1874 makes a session BE a comment thread (root comment + replies), with
// open/closed = the thread root's resolved_at — no time-window markers anymore.
// This file makes that mean something to a running agent: when the
// cerebro_comment_chapters flag is ON and a run was triggered by a comment, the
// bundled `issue context` it gets at run-start is scoped to that comment's
// THREAD. The agent sees only its own thread's slice, never the whole issue
// history — which is exactly what "fresh context per session" means now that a
// session is a thread.
//
// Ships dark: default OFF, scopes nothing for human callers (no task token) and
// nothing for a run with no triggering comment (cold start = whole issue). All
// scoping is additive and reversible by the flag alone.

import (
	"context"
	"errors"
	"log/slog"

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
// personal > unlocked workspace > default. It defaults OFF: the engine ships
// dark and only scopes context where the flag is explicitly turned on.
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

// threadScope identifies the thread (session) a run is scoped to: the root
// comment of the triggering comment's thread. A comment belongs to the thread
// iff it IS the root or its parent IS the root (threads are two levels deep).
type threadScope struct {
	RootID pgtype.UUID
}

// contains reports whether a comment belongs to this thread scope.
func (s threadScope) contains(c db.Comment) bool {
	if uuidEqual(c.ID, s.RootID) {
		return true
	}
	return c.ParentID.Valid && uuidEqual(c.ParentID, s.RootID)
}

// uuidEqual compares two pgtype.UUIDs by value (both must be valid).
func uuidEqual(a, b pgtype.UUID) bool {
	return a.Valid && b.Valid && a.Bytes == b.Bytes
}

// sessionScopeForContext derives the thread (session) scope for the current
// task-scoped request, or ok=false when no scoping applies (human call, or a run
// with no triggering comment). It walks the trigger comment to its thread root.
func (h *Handler) sessionScopeForContext(ctx context.Context, issue db.Issue) (threadScope, bool) {
	_ = issue
	ts := middleware.TaskScopeFromContext(ctx)
	if ts.TaskID == "" {
		return threadScope{}, false // human call — never scope
	}
	taskUUID, err := util.ParseUUID(ts.TaskID)
	if err != nil {
		return threadScope{}, false
	}
	task, err := h.Queries.GetAgentTask(ctx, taskUUID)
	if err != nil || !task.TriggerCommentID.Valid {
		return threadScope{}, false // cold start / no triggering comment → whole issue
	}
	trigger, err := h.Queries.GetComment(ctx, task.TriggerCommentID)
	if err != nil {
		return threadScope{}, false
	}
	// The thread root is the trigger's parent (if it is a reply) or itself.
	root := trigger.ID
	if trigger.ParentID.Valid {
		root = trigger.ParentID
	}
	return threadScope{RootID: root}, true
}

// scopeContextComments filters a comment slice to the triggering comment's
// thread. A run only ever sees its own thread (root + replies) — the literal
// definition of a fresh context window now that a session is a thread. Returns
// the input unchanged when the slice is empty.
func scopeContextComments(comments []db.Comment, scope threadScope) []db.Comment {
	if len(comments) == 0 {
		return comments
	}
	out := comments[:0]
	for _, c := range comments {
		if scope.contains(c) {
			out = append(out, c)
		}
	}
	return out
}
