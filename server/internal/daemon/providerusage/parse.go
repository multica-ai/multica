package providerusage

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

const maxAuthFileBytes = 1 << 20

func readRegularFile(path string) ([]byte, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxAuthFileBytes {
		return nil, os.ErrInvalid
	}
	return os.ReadFile(path)
}

func parseFlexibleTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			utc := parsed.UTC()
			return utc, true
		}
	}
	return time.Time{}, false
}

func parseResetValue(v any) *time.Time {
	switch value := v.(type) {
	case string:
		parsed, ok := parseFlexibleTime(value)
		if !ok {
			return nil
		}
		return &parsed
	case float64:
		if value <= 0 || math.IsNaN(value) || math.IsInf(value, 0) {
			return nil
		}
		seconds := value
		if seconds > 10_000_000_000 {
			seconds = seconds / 1000
		}
		parsed := time.UnixMilli(int64(seconds * 1000)).UTC()
		return &parsed
	default:
		return nil
	}
}

func asFloat(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, false
		}
		return value, true
	case json.Number:
		parsed, err := value.Float64()
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func asMap(v any) map[string]any {
	mapped, _ := v.(map[string]any)
	return mapped
}

func asSlice(v any) []any {
	items, _ := v.([]any)
	return items
}

func usablePercent(value float64) (float64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1000 {
		return 0, false
	}
	return math.Round(value*10000) / 10000, true
}

func trimPlan(name string) string {
	name = strings.TrimSpace(name)
	if len(name) <= 64 {
		return name
	}
	return name[:64]
}

func decodeObject(body []byte) (map[string]any, bool) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil || root == nil {
		return nil, false
	}
	return root, true
}
