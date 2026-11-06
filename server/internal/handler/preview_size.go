package handler

import "github.com/multica-ai/multica/server/internal/attachmentpreview"

func (h *Handler) maxPreviewSizeBytes() int64 {
	return attachmentpreview.EffectiveMaxSizeBytes(h.cfg.MaxPreviewSizeBytes)
}
