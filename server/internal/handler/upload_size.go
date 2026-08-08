package handler

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	DefaultMaxUploadSizeBytes      int64 = 100 << 20
	MaxConfigurableUploadSizeBytes int64 = 1 << 30
	multipartMemoryBytes           int64 = 8 << 20
	multipartEnvelopeBytes         int64 = 1 << 20
)

// ParseMaxUploadSize parses an integer byte count or a binary size suffix.
// MB/GB are treated as MiB/GiB for compatibility with common self-hosted
// configuration conventions. Values above 1 GiB are rejected so a typo
// cannot turn a public multipart endpoint into an unbounded resource sink.
func ParseMaxUploadSize(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return DefaultMaxUploadSizeBytes, nil
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
	case "g", "gb", "gib":
		multiplier = 1 << 30
	default:
		return 0, fmt.Errorf("unsupported size suffix %q", suffix)
	}

	if n > MaxConfigurableUploadSizeBytes/multiplier {
		return 0, fmt.Errorf("must not exceed 1 GiB")
	}
	return n * multiplier, nil
}

func (h *Handler) maxUploadSizeBytes() int64 {
	if h.cfg.MaxUploadSizeBytes > 0 {
		return h.cfg.MaxUploadSizeBytes
	}
	return DefaultMaxUploadSizeBytes
}
