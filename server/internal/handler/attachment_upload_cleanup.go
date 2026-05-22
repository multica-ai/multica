package handler

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/storage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const defaultAttachmentUploadCleanupInterval = 15 * time.Minute

func (h *Handler) CleanupExpiredAttachmentUploadSessions(ctx context.Context, limit int32) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	sessions, err := h.Queries.ListExpiredAttachmentUploadSessions(ctx, limit)
	if err != nil {
		return 0, err
	}
	if len(sessions) == 0 {
		return 0, nil
	}
	multipartStore, _ := h.Storage.(storage.MultipartUploadStorage)
	cleaned := 0
	for _, session := range sessions {
		if multipartStore != nil {
			if err := multipartStore.AbortMultipartUpload(ctx, session.ObjectKey, session.UploadID); err != nil {
				slog.Warn("failed to abort expired multipart upload", "session_id", uuidToString(session.ID), "error", err)
				continue
			}
		}
		if err := h.Queries.MarkAttachmentUploadSessionExpired(ctx, db.MarkAttachmentUploadSessionExpiredParams{
			ID:          session.ID,
			WorkspaceID: session.WorkspaceID,
		}); err != nil {
			slog.Warn("failed to mark expired attachment upload session", "session_id", uuidToString(session.ID), "error", err)
			continue
		}
		cleaned++
	}
	return cleaned, nil
}

func (h *Handler) RunAttachmentUploadCleanup(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = defaultAttachmentUploadCleanupInterval
	}
	if h.Storage == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleaned, err := h.CleanupExpiredAttachmentUploadSessions(ctx, 100)
			if err != nil {
				slog.Warn("attachment upload cleanup failed", "error", err)
				continue
			}
			if cleaned > 0 {
				slog.Info("attachment upload cleanup completed", "cleaned", cleaned)
			}
		}
	}
}
