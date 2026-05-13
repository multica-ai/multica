// Package ratelimit provides a provider-error parser that extracts an
// unpause timestamp from a free-form 429/rate-limit error string.
// Extracted from cerebro/runtime so both cerebro/runtime and
// cerebro/account can import it without creating a cycle.
package ratelimit

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultBackoff is used when the error text clearly indicates a
// rate-limit response but contains no parseable reset time.
const DefaultBackoff = 5 * time.Minute

var rateLimitDetectorRe = regexp.MustCompile(
	`rate[ -]?limit(?:ed|ing)?|ratelimit|limiting requests|\b429\b|` +
		`quota exceeded|insufficient_quota|monthly usage limit|` +
		`org's monthly usage|out of tokens|401 invalid authentication`,
)

// ParseReset extracts a runtime-unpause timestamp from a free-form
// provider error string. Returns (resetAt, true) on a successful parse.
//
// Supported shapes (in priority order):
//
//  1. Unix epoch seconds
//  2. ISO-8601 timestamp
//  3. Wall-clock time
//  4. Relative duration
//  5. Fallback — text matches a rate-limit-shaped error → now + DefaultBackoff.
func ParseReset(errText string, now time.Time) (time.Time, bool) {
	if strings.TrimSpace(errText) == "" {
		return time.Time{}, false
	}
	lower := strings.ToLower(errText)

	if t, ok := parseEpochSeconds(lower, now); ok {
		return t, true
	}
	if t, ok := parseISO8601(errText); ok {
		return t, true
	}
	if t, ok := parseWallClock(lower, now); ok {
		return t, true
	}
	if t, ok := parseRelativeDuration(lower, now); ok {
		return t, true
	}
	if rateLimitDetectorRe.MatchString(lower) {
		return now.Add(DefaultBackoff), true
	}
	return time.Time{}, false
}

var epochContextRe = regexp.MustCompile(`(?:reset|retry|limit|rate)[^0-9]*\b(1[6-9]\d{8}|2\d{9})\b`)

func parseEpochSeconds(lower string, now time.Time) (time.Time, bool) {
	m := epochContextRe.FindStringSubmatch(lower)
	if m == nil {
		return time.Time{}, false
	}
	secs, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	t := time.Unix(secs, 0).UTC()
	if t.Before(now.Add(-1*time.Minute)) || t.After(now.Add(7*24*time.Hour)) {
		return time.Time{}, false
	}
	return t, true
}

var isoRe = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2}))\b`)

func parseISO8601(errText string) (time.Time, bool) {
	m := isoRe.FindStringSubmatch(errText)
	if m == nil {
		return time.Time{}, false
	}
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04Z07:00",
		"2006-01-02T15:04:05Z07:00",
	} {
		if t, err := time.Parse(layout, m[1]); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

var wallClockRe = regexp.MustCompile(
	`(?:reset|resets|available|try again|retry)\s*(?:at\s*)?` +
		`(\d{1,2})(?::(\d{2}))?\s*(am|pm)?` +
		`\s*(?:\(?\s*(utc|gmt)\s*\)?)?`,
)

func parseWallClock(lower string, now time.Time) (time.Time, bool) {
	m := wallClockRe.FindStringSubmatch(lower)
	if m == nil {
		return time.Time{}, false
	}
	hour, err := strconv.Atoi(m[1])
	if err != nil || hour < 0 || hour > 23 {
		return time.Time{}, false
	}
	min := 0
	if m[2] != "" {
		v, err := strconv.Atoi(m[2])
		if err != nil || v < 0 || v > 59 {
			return time.Time{}, false
		}
		min = v
	}
	switch strings.ToLower(m[3]) {
	case "pm":
		if hour < 12 {
			hour += 12
		}
	case "am":
		if hour == 12 {
			hour = 0
		}
	}
	if hour > 23 {
		return time.Time{}, false
	}
	loc := time.UTC
	if m[4] == "" {
		loc = time.Local
	}
	candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, loc)
	if !candidate.After(now) {
		candidate = candidate.Add(24 * time.Hour)
	}
	if candidate.Sub(now) > 24*time.Hour {
		return time.Time{}, false
	}
	return candidate.UTC(), true
}

var durationRe = regexp.MustCompile(
	`(?:try again|retry after|retry-after|wait|in)\s*` +
		`(\d+)\s*` +
		`(s|sec|secs|second|seconds|m|min|mins|minute|minutes|h|hr|hrs|hour|hours)\b`,
)

func parseRelativeDuration(lower string, now time.Time) (time.Time, bool) {
	m := durationRe.FindStringSubmatch(lower)
	if m == nil {
		return time.Time{}, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return time.Time{}, false
	}
	var unit time.Duration
	switch m[2] {
	case "s", "sec", "secs", "second", "seconds":
		unit = time.Second
	case "m", "min", "mins", "minute", "minutes":
		unit = time.Minute
	case "h", "hr", "hrs", "hour", "hours":
		unit = time.Hour
	default:
		return time.Time{}, false
	}
	d := time.Duration(n) * unit
	if d > 24*time.Hour {
		return time.Time{}, false
	}
	return now.Add(d), true
}
