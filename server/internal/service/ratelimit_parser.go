package service

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultRateLimitBackoff is used when the error text clearly indicates a
// rate-limit response but contains no parseable reset time. Five minutes
// matches the typical "transient throttle" window observed across providers
// (Anthropic's "temporarily limiting requests" message in particular). Long
// enough to actually back off, short enough that a missed parse doesn't
// leave the runtime paused indefinitely.
const DefaultRateLimitBackoff = 5 * time.Minute

// rateLimitDetectorRe matches phrases that confidently indicate a rate-limit
// response. Used only as a fallback when no explicit reset time was found —
// the precise parsers (epoch / ISO / wall-clock / relative) take priority,
// so a hint-bearing error like "Rate limited. Try again in 30s" still gets
// the exact 30s value rather than the 5-minute default.
//
// Conservative on purpose: a false positive auto-pauses the runtime for 5
// minutes which is annoying. We require an unambiguous signal — the literal
// phrase "rate limit(ed/ing)" / "ratelimit" / "limiting requests", or HTTP
// 429, or "quota exceeded".
var rateLimitDetectorRe = regexp.MustCompile(
	`rate[ -]?limit(?:ed|ing)?|ratelimit|limiting requests|\b429\b|quota exceeded`,
)

// ParseRateLimitReset extracts a runtime-unpause timestamp from a free-form
// provider error string. Returns (resetAt, true) on a successful parse.
//
// Supported shapes (in priority order):
//
//  1. Unix epoch seconds — "X-RateLimit-Reset: 1714850000" or a bare 10-digit
//     integer adjacent to "reset"/"retry"/"limit" context.
//  2. ISO-8601 timestamp — "2026-05-04T18:50:00Z" anywhere in the text.
//  3. Wall-clock time — "resets 6:50pm (UTC)", "resets at 18:50". The clock
//     time is resolved against the provided timezone (UTC if "(UTC)" appears,
//     local otherwise) and rolled forward to the next future occurrence.
//  4. Relative duration — "try again in 5 minutes", "retry after 30s",
//     "wait 2 hours". Returns now + duration.
//  5. Fallback — text matches a rate-limit-shaped error (see
//     rateLimitDetectorRe) with no parseable hint → now + DefaultRateLimitBackoff.
//
// `now` is injected for determinism in tests and to anchor relative durations.
// The parser is intentionally permissive — false positives are cheap (we
// pause for a bit, sweeper unpauses), false negatives just fall back to a
// manual unpause.
func ParseRateLimitReset(errText string, now time.Time) (time.Time, bool) {
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
		return now.Add(DefaultRateLimitBackoff), true
	}
	return time.Time{}, false
}

// 10-digit integer anywhere in a window where "reset"/"retry"/"limit"/
// "rate" appears. Bounded to plausible epoch range (2020-01-01 .. 2100).
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
	// Sanity-check: reset time should be in the future-ish (<=24h) and not
	// further out than a week. Outside that window, the integer probably
	// wasn't a reset epoch despite the keyword nearby.
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

// "resets 6:50pm (utc)", "resets at 18:50", "available at 9am utc", etc.
// Capture groups: 1=hour, 2=minute (optional, defaults to 00), 3=am/pm
// (optional), 4=tz hint (optional, only "utc" recognised here).
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
		// No tz hint — fall back to local. Most providers state UTC explicitly,
		// but Anthropic's web UI quotes the user's local zone.
		loc = time.Local
	}
	candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, loc)
	// Roll forward if the wall-clock time has already passed today.
	if !candidate.After(now) {
		candidate = candidate.Add(24 * time.Hour)
	}
	// Cap horizon — a "reset" parsed >12h out is almost certainly a misread.
	if candidate.Sub(now) > 24*time.Hour {
		return time.Time{}, false
	}
	return candidate.UTC(), true
}

// "try again in 5 minutes", "retry after 30s", "wait 2 hours", "in 90 seconds"
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
	// Cap at 24h to match the wall-clock cap; a multi-day "wait" is almost
	// certainly off the rails.
	if d > 24*time.Hour {
		return time.Time{}, false
	}
	return now.Add(d), true
}
