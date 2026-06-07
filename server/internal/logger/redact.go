package logger

import (
	"fmt"
	"strings"
)

var sensitiveKeys = map[string]struct{}{
	"authorization": {},
	"cookie":        {},
	"email":         {},
	"password":      {},
	"secret":        {},
	"token":         {},
}

// RedactValue removes sensitive data from structured values before logging.
func RedactValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(value))
		for k, item := range value {
			if isSensitiveKey(k) {
				redacted[k] = "[REDACTED]"
				continue
			}
			redacted[k] = RedactValue(item)
		}
		return redacted
	case []any:
		redacted := make([]any, 0, len(value))
		for _, item := range value {
			redacted = append(redacted, RedactValue(item))
		}
		return redacted
	case string:
		if looksSensitiveString(value) {
			return "[REDACTED]"
		}
		return value
	default:
		return value
	}
}

// RedactString hides obvious secrets in string payloads used for debug logs.
func RedactString(value string) string {
	if looksSensitiveString(value) {
		return "[REDACTED]"
	}
	return value
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for candidate := range sensitiveKeys {
		if normalized == candidate || strings.Contains(normalized, candidate) {
			return true
		}
	}
	return false
}

func looksSensitiveString(value string) bool {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "bearer ") || strings.Contains(lower, "mul_") {
		return true
	}
	if strings.Count(value, "@") == 1 && strings.Contains(value, ".") {
		return true
	}
	return false
}

func truncateString(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return fmt.Sprintf("%s...(truncated %d bytes)", value[:limit], len(value)-limit)
}
