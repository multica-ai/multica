package handler

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	// DefaultMaxPreviewSizeBytes preserves the existing inline text-preview
	// limit when MULTICA_MAX_PREVIEW_SIZE is unset.
	DefaultMaxPreviewSizeBytes int64 = 2 << 20
	// MaxConfigurablePreviewSizeBytes bounds the memory and client rendering
	// work caused by one preview request. Larger files remain downloadable.
	MaxConfigurablePreviewSizeBytes int64 = 16 << 20
)

// ParseMaxPreviewSize parses an integer byte count or a binary size suffix.
// MB is treated as MiB for compatibility with common self-hosted
// configuration conventions. Values above 16 MiB are rejected because the
// preview path buffers the bounded body before returning it to the client.
func ParseMaxPreviewSize(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return DefaultMaxPreviewSizeBytes, nil
	}

	i := 0
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("must start with a positive integer")
	}
	n, err := strconv.ParseInt(value[:i], 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("must be a positive integer")
	}

	suffix := strings.ToLower(strings.TrimSpace(value[i:]))
	multiplier := int64(1)
	switch suffix {
	case "", "b":
	case "k", "kb", "kib":
		multiplier = 1 << 10
	case "m", "mb", "mib":
		multiplier = 1 << 20
	default:
		return 0, fmt.Errorf("unsupported size suffix %q", suffix)
	}

	if n > MaxConfigurablePreviewSizeBytes/multiplier {
		return 0, fmt.Errorf("must not exceed 16 MiB")
	}
	return n * multiplier, nil
}

func normalizeMaxPreviewSize(size int64) int64 {
	if size <= 0 || size > MaxConfigurablePreviewSizeBytes {
		return DefaultMaxPreviewSizeBytes
	}
	return size
}

func (h *Handler) maxPreviewSizeBytes() int64 {
	return normalizeMaxPreviewSize(h.cfg.MaxPreviewSizeBytes)
}
