package attachmentpreview

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

const (
	DefaultMaxSizeBytes int64 = 2 << 20
	// Text previews buffer the complete body and render it in the browser.
	MaxSizeBytes int64 = 16 << 20
)

func ConfiguredMaxSizeBytes(raw string) int64 {
	size, err := parseMaxSizeBytes(raw)
	if err != nil {
		slog.Warn("invalid MULTICA_MAX_PREVIEW_SIZE, using 2 MiB default", "error", err)
		return DefaultMaxSizeBytes
	}
	return size
}

// parseMaxSizeBytes accepts positive integer bytes, KB/KiB, or MB/MiB.
// KB and MB use binary units, matching the existing attachment size limits.
func parseMaxSizeBytes(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return DefaultMaxSizeBytes, nil
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
	// Clamp before multiplying so even extreme valid values cannot overflow.
	if n > MaxSizeBytes/multiplier {
		return MaxSizeBytes, nil
	}
	return n * multiplier, nil
}

func EffectiveMaxSizeBytes(size int64) int64 {
	if size <= 0 {
		return DefaultMaxSizeBytes
	}
	return min(size, MaxSizeBytes)
}
