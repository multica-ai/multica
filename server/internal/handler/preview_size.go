package handler

const (
	DefaultMaxPreviewSizeBytes int64 = 2 << 20
	// Text previews buffer the complete body and render it in the browser.
	MaxConfigurablePreviewSizeBytes int64 = 16 << 20
)

func (h *Handler) maxPreviewSizeBytes() int64 {
	size := h.cfg.MaxPreviewSizeBytes
	if size <= 0 || size > MaxConfigurablePreviewSizeBytes {
		return DefaultMaxPreviewSizeBytes
	}
	return size
}
