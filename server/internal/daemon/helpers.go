package daemon

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func envOrDefault(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func durationFromEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	d, err := parseFlexDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid duration %q: %w", key, value, err)
	}
	return d, nil
}

// dayUnit matches a decimal number (with optional leading digits) followed by
// `d` (days), so both "5d" and "1.5d" are captured whole and expanded to hours.
var dayUnit = regexp.MustCompile(`(\d*\.\d+|\d+)d`)

// parseFlexDuration accepts the standard Go time.ParseDuration syntax plus a
// `d` (day) suffix, which the stdlib rejects. "5d" → 120h, "1d12h" → 36h,
// "0.5d" → 12h. Overflow or malformed numbers propagate as errors.
func parseFlexDuration(value string) (time.Duration, error) {
	var convErr error
	expanded := dayUnit.ReplaceAllStringFunc(value, func(match string) string {
		days, err := strconv.ParseFloat(match[:len(match)-1], 64)
		if err != nil {
			convErr = err
			return match
		}
		// time.ParseDuration handles fractional hours natively, and rejects
		// overflow on its own.
		return strconv.FormatFloat(days*24, 'f', -1, 64) + "h"
	})
	if convErr != nil {
		return 0, convErr
	}
	return time.ParseDuration(expanded)
}

func intFromEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: invalid integer %q: %w", key, value, err)
	}
	return n, nil
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
