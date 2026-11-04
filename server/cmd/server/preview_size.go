package main

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/internal/handler"
)

func configuredMaxPreviewSize(raw string) int64 {
	size, err := parseMaxPreviewSize(raw)
	if err != nil {
		slog.Warn("invalid MULTICA_MAX_PREVIEW_SIZE, using 2 MiB default", "error", err)
		return handler.DefaultMaxPreviewSizeBytes
	}
	return size
}

// parseMaxPreviewSize accepts positive integer bytes, KB/KiB, or MB/MiB.
// KB and MB use binary units, matching the existing attachment size limits.
func parseMaxPreviewSize(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return handler.DefaultMaxPreviewSizeBytes, nil
	}
	i := 0
	for i < len(value) && value[i] >= '0' && value[i] <= '9' {
		i++
	}
	n, err := strconv.ParseInt(value[:i], 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("preview size must be a positive integer")
	}
	multiplier := int64(1)
	switch strings.ToLower(strings.TrimSpace(value[i:])) {
	case "", "b":
	case "kb", "kib":
		multiplier = 1 << 10
	case "mb", "mib":
		multiplier = 1 << 20
	default:
		return 0, fmt.Errorf("preview size must use bytes, KB/KiB, or MB/MiB")
	}
	// Divide before multiplying to reject overflow as well as oversized values.
	if n > handler.MaxConfigurablePreviewSizeBytes/multiplier {
		return 0, fmt.Errorf("preview size must not exceed 16 MiB")
	}
	return n * multiplier, nil
}
