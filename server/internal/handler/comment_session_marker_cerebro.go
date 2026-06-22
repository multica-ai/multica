// FIR-1787 point 5 / FIR-1741 Slice 2 — net-new cerebro file. Realises Jesper's
// decision A: "a new thread starts a new session." Sara's engine already makes a
// session a fresh context window (issue_context_session_scope_cerebro.go) and a
// timeline slice opened by a cerebro_session marker (PR #1657). What was missing
// is the trigger that opens such a marker when a user starts a new thread — until
// now sessions were only ever opened by the explicit Start fresh action, so a new
// root comment just fell into the latest session and inherited its context (the
// exact bug behind point 5: "ny tråd laver ikke en ny session").
//
// This hook runs AFTER the comment-create transaction commits, in its own
// transaction, so a session-open failure can never block or roll back the
// comment itself — the comment is primary; the session marker is best-effort and
// self-heals on the next thread. It is gated by the same cerebro_comment_chapters
// flag as the rest of the engine (off → no-op) and only fires for top-level
// comments (parent_id IS NULL) on real issue threads, never on channels.
package handler

import (
	"context"
	"log/slog"
	"time"

	cerebrosessions "github.com/multica-ai/multica/server/internal/cerebro/sessions"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// openSessionForNewThreadOnComment opens a new session for a freshly created root
// comment so the thread it starts becomes its own session (fresh context). No-op
// for replies, channels, when the flag is off, or for the issue's first thread
// (the implicit Session 1 already covers it — decided inside OpenSessionForNewThread).
func (h *Handler) openSessionForNewThreadOnComment(ctx context.Context, issue db.Issue, isRoot bool, commentAt time.Time, authorType, authorID string) {
	if !isRoot || issue.Kind == "channel" {
		return
	}
	requesterUserID := ""
	if authorType == "member" {
		requesterUserID = authorID
	}
	if !h.cerebroCommentChaptersEnabled(ctx, issue.WorkspaceID, requesterUserID) {
		return
	}

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		slog.Warn("new-thread session: begin tx failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	defer tx.Rollback(ctx)

	if err := cerebrosessions.OpenSessionForNewThread(ctx, tx, issue.ID, issue.CreatedAt.Time, commentAt); err != nil {
		slog.Warn("new-thread session: open failed", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		slog.Warn("new-thread session: commit failed", "issue_id", uuidToString(issue.ID), "error", err)
	}
}
