package daemon

import (
	"context"
	"log/slog"
	"mime"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
)

const maxClaudeImageAttachmentBytes = 5 << 20

func (d *Daemon) loadClaudeImageAttachments(ctx context.Context, task Task, taskLog *slog.Logger) []agent.ImageAttachment {
	if len(task.ChatMessageAttachments) == 0 {
		return nil
	}
	images := make([]agent.ImageAttachment, 0, len(task.ChatMessageAttachments))
	for _, att := range task.ChatMessageAttachments {
		contentType := normalizeImageContentType(att.ContentType)
		if !isClaudeVisionContentType(contentType) {
			continue
		}
		if att.SizeBytes > maxClaudeImageAttachmentBytes {
			taskLog.Warn("skipping oversized image attachment for claude vision",
				"attachment_id", att.ID,
				"filename", att.Filename,
				"content_type", att.ContentType,
				"size_bytes", att.SizeBytes,
			)
			continue
		}
		info, err := d.client.GetAttachment(ctx, att.ID)
		if err != nil {
			taskLog.Warn("failed to fetch image attachment metadata for claude vision",
				"attachment_id", att.ID,
				"error", err,
			)
			continue
		}
		downloadURL := info.DownloadURL
		if downloadURL == "" {
			downloadURL = info.URL
		}
		data, err := d.client.DownloadFile(ctx, downloadURL)
		if err != nil {
			taskLog.Warn("failed to download image attachment for claude vision",
				"attachment_id", att.ID,
				"error", err,
			)
			continue
		}
		if len(data) > maxClaudeImageAttachmentBytes {
			taskLog.Warn("skipping downloaded oversized image attachment for claude vision",
				"attachment_id", att.ID,
				"filename", att.Filename,
				"content_type", att.ContentType,
				"size_bytes", len(data),
			)
			continue
		}
		images = append(images, agent.ImageAttachment{
			ID:          att.ID,
			Filename:    att.Filename,
			ContentType: contentType,
			Data:        data,
		})
	}
	return images
}

func normalizeImageContentType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = contentType
	}
	return strings.ToLower(strings.TrimSpace(mediaType))
}

func isClaudeVisionContentType(contentType string) bool {
	switch normalizeImageContentType(contentType) {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}
