package handler

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/cerebro/sessionmode"
)

func normalizeNewThreadSessionMode(raw *string, hasParent bool) (string, error) {
	if raw == nil {
		return "", nil
	}
	if hasParent {
		return "", errors.New("session_mode is only valid for a new thread")
	}
	normalizedRaw := strings.ToLower(strings.TrimSpace(*raw))
	mode, valid := sessionmode.Normalize(normalizedRaw)
	if !valid || normalizedRaw == "auto" || normalizedRaw == "default" {
		return "", errors.New("session_mode must be plan, build, research, or review")
	}
	return string(mode), nil
}

func (h *Handler) recordCommentSessionMode(ctx context.Context, tx pgx.Tx, issueID, rootCommentID, mode string) error {
	if mode == "" || h.CommentSessionMode == nil {
		return nil
	}
	return h.CommentSessionMode.RecordCommentSessionMode(ctx, tx, issueID, rootCommentID, mode)
}

// CommentSessionModeRecorder is the Cerebro-owned transaction seam that stores
// the selected Mode on a new root comment before any agent task is enqueued.
type CommentSessionModeRecorder interface {
	RecordCommentSessionMode(ctx context.Context, tx pgx.Tx, issueID, rootCommentID, mode string) error
}
