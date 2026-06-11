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

// rateLimitDetectorRe decides whether a free-form provider error means "the
// agent cannot work right now" — rate limits, quota caps, AND expired/invalid
// auth. All three keep failing identically until a reset passes or a human
// swaps the credential, so all three must auto-pause the runtime rather than
// bounce-loop a failed run every 5 minutes (FIR-2611). The auth/credential and
// capacity patterns on the last three lines were the classifier gap that left
// ~360 such failures mis-labelled as generic agent_error.
var rateLimitDetectorRe = regexp.MustCompile(
	`rate[ -]?limit(?:ed|ing)?|ratelimit|limiting requests|\b429\b|` +
		`quota exceeded|insufficient_quota|monthly usage limit|` +
		`org's monthly usage|out of tokens|out of extra usage|401 invalid authentication|` +
		`hit your usage limit|usage limit reached|usage limit has been reached|reached your usage limit|` +
		`refresh token (?:was )?(?:already used|revoked|expired)|token (?:was )?(?:already used|revoked)|` +
		`invalid api key|incorrect api key|invalid x-api-key|` +
		`not logged in|please (?:run )?/?login|please run codex login|run codex login|` +
		`exhausted your (?:current )?capacity|capacity exhausted`,
)

// ParseReset extracts a runtime-unpause timestamp from a free-form
// provider error string. Returns (resetAt, true) on a successful parse.
//
// Supported shapes (in priority order):
//
//  1. Unix epoch seconds
//  2. ISO-8601 timestamp
//  3. Absolute month-day with explicit year (Codex: "Try again at May 30th, 2026 10:12 PM")
//  4. Absolute month-day, year inferred (e.g. "resets May 16 at 3pm (Europe/Copenhagen)")
//  5. Wall-clock time (same-day/next-day)
//  6. Relative duration
//  7. Fallback — text matches a rate-limit-shaped error → now + DefaultBackoff.
func ParseReset(errText string, now time.Time) (time.Time, bool) {
	resetAt, hasReset, pauseWorthy := ClassifyReset(errText, now)
	if !pauseWorthy {
		return time.Time{}, false
	}
	if hasReset {
		return resetAt, true
	}
	return now.Add(DefaultBackoff), true
}

// ClassifyReset is the three-valued core behind ParseReset. It separates "is
// this a pause-worthy error?" from "did it carry a parseable reset time?" so
// callers that apply their own backoff (FIR-2476's growing auto-pause backoff)
// can tell a concrete provider reset time apart from the no-hint fallback case.
//
// Returns:
//   - (resetAt, true,  true)  — a concrete reset time was parsed; pause until it.
//   - (zero,    false, true)  — rate-limit-shaped error with no parseable reset
//     time; the caller decides the backoff (ParseReset uses DefaultBackoff).
//   - (zero,    false, false) — not a pause-worthy error.
func ClassifyReset(errText string, now time.Time) (time.Time, bool, bool) {
	if strings.TrimSpace(errText) == "" {
		return time.Time{}, false, false
	}
	lower := strings.ToLower(errText)

	if t, ok := parseEpochSeconds(lower, now); ok {
		return t, true, true
	}
	if t, ok := parseISO8601(errText); ok {
		return t, true, true
	}
	if t, ok := parseAbsoluteDateTime(errText, now); ok {
		return t, true, true
	}
	if t, ok := parseAbsoluteMonthDay(errText, now); ok {
		return t, true, true
	}
	if t, ok := parseWallClock(errText, now); ok {
		return t, true, true
	}
	if t, ok := parseRelativeDuration(lower, now); ok {
		return t, true, true
	}
	if rateLimitDetectorRe.MatchString(lower) {
		return time.Time{}, false, true
	}
	return time.Time{}, false, false
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

// wallClockRe matches "resets 12:40pm (Europe/Copenhagen)" / "resets 6:50pm
// (UTC)" / "Resets at 18:50 UTC" style hints (a time-of-day with no date).
// Uses (?i) so the keywords and am/pm match case-insensitively while group 4
// keeps its original casing for time.LoadLocation (IANA zone names are
// case-sensitive). Group 4 captures an optional trailing timezone —
// parenthesised or bare — which may be an IANA name (Europe/Copenhagen) or a
// UTC/GMT alias; locationFromHint resolves it.
var wallClockRe = regexp.MustCompile(
	`(?i)(?:reset|resets|available|try again|retry)\s*(?:at\s*)?` +
		`(\d{1,2})(?::(\d{2}))?\s*(am|pm)?` +
		`(?:\s*\(?\s*([A-Za-z][A-Za-z0-9/_.+\-]*)\s*\)?)?`,
)

func parseWallClock(text string, now time.Time) (time.Time, bool) {
	m := wallClockRe.FindStringSubmatch(text)
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
	loc := locationFromHint(m[4])
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

// codexDateTimeRe matches the Codex CLI usage-limit reset string, which carries
// a full month-name date WITH an explicit year, e.g.
//
//	"You've hit your usage limit. Try again at May 30th, 2026 10:12 PM"
//
// The year makes this unambiguous, so unlike absMonthDayRe (which infers the
// year) this branch never has to guess. Connectors are kept flexible to absorb
// phrasing drift: the prefix may be "try again"/"retry"/"resets"/"available"
// optionally followed by "at"/"on"; the day may carry an ordinal suffix
// (30th/1st/2nd/3rd); the year may be followed by a comma and/or an "at"; and
// an optional trailing timezone (parenthesized IANA name or UTC/GMT) is parsed
// when present, defaulting to time.Local otherwise (Codex prints reset times in
// the runtime machine's local zone).
var codexDateTimeRe = regexp.MustCompile(
	`(?i)(?:resets?|available|try again|retry)\s+(?:on\s+|at\s+)?` +
		`(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)` +
		`\s+(\d{1,2})(?:st|nd|rd|th)?,?\s+(\d{4})` +
		`,?\s+(?:at\s+)?(\d{1,2})(?::(\d{2}))?(?::\d{2})?\s*(am|pm)?` +
		`(?:\s*\(?\s*([A-Za-z][A-Za-z0-9/_.+\-]*)\s*\)?)?`,
)

// parseAbsoluteDateTime handles the Codex full-date reset string described on
// codexDateTimeRe. The explicit year is used verbatim — no year inference and
// no future-rollover guess. A parsed time more than 400 days out is rejected as
// a misparse.
func parseAbsoluteDateTime(errText string, now time.Time) (time.Time, bool) {
	m := codexDateTimeRe.FindStringSubmatch(errText)
	if m == nil {
		return time.Time{}, false
	}
	month, ok := monthNames[strings.ToLower(m[1])]
	if !ok {
		return time.Time{}, false
	}
	day, err := strconv.Atoi(m[2])
	if err != nil || day < 1 || day > 31 {
		return time.Time{}, false
	}
	year, err := strconv.Atoi(m[3])
	if err != nil || year < 2000 || year > 2100 {
		return time.Time{}, false
	}
	hour, err := strconv.Atoi(m[4])
	if err != nil || hour < 0 || hour > 23 {
		return time.Time{}, false
	}
	min := 0
	if m[5] != "" {
		v, err := strconv.Atoi(m[5])
		if err != nil || v < 0 || v > 59 {
			return time.Time{}, false
		}
		min = v
	}
	switch strings.ToLower(m[6]) {
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
	loc := locationFromHint(m[7])
	candidate := time.Date(year, month, day, hour, min, 0, 0, loc)
	if candidate.Sub(now) > 400*24*time.Hour {
		return time.Time{}, false
	}
	return candidate.UTC(), true
}

// locationFromHint resolves an optional timezone token to a *time.Location.
// Tries an IANA name (e.g. "Europe/Copenhagen"), then the UTC/GMT aliases, then
// falls back to time.Local — matching how parseWallClock / parseAbsoluteMonthDay
// treat a missing or unrecognised zone.
func locationFromHint(tz string) *time.Location {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return time.Local
	}
	if loaded, err := time.LoadLocation(tz); err == nil {
		return loaded
	}
	switch strings.ToLower(tz) {
	case "utc", "gmt":
		return time.UTC
	}
	return time.Local
}

// absMonthDayRe matches "resets May 16 at 3pm (Europe/Copenhagen)" style reset
// strings. Uses (?i) so month names and am/pm are case-insensitive while
// preserving original casing for the IANA timezone capture group.
var absMonthDayRe = regexp.MustCompile(
	`(?i)(?:resets?|available)\s+` +
		`(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)` +
		`\s+(\d{1,2})\s+at\s+` +
		`(\d{1,2})(?::(\d{2}))?\s*(am|pm)?` +
		`(?:\s*\(\s*([^)]+?)\s*\))?`,
)

var monthNames = map[string]time.Month{
	"jan": time.January, "january": time.January,
	"feb": time.February, "february": time.February,
	"mar": time.March, "march": time.March,
	"apr": time.April, "april": time.April,
	"may": time.May,
	"jun": time.June, "june": time.June,
	"jul": time.July, "july": time.July,
	"aug": time.August, "august": time.August,
	"sep": time.September, "sept": time.September, "september": time.September,
	"oct": time.October, "october": time.October,
	"nov": time.November, "november": time.November,
	"dec": time.December, "december": time.December,
}

// parseAbsoluteMonthDay handles errors like "resets May 16 at 3pm (Europe/Copenhagen)".
// Preserves original errText (not lowercased) so the IANA timezone name keeps
// its casing for time.LoadLocation. Falls back to time.Local when the timezone
// is absent or unrecognised.
func parseAbsoluteMonthDay(errText string, now time.Time) (time.Time, bool) {
	m := absMonthDayRe.FindStringSubmatch(errText)
	if m == nil {
		return time.Time{}, false
	}
	month, ok := monthNames[strings.ToLower(m[1])]
	if !ok {
		return time.Time{}, false
	}
	day, err := strconv.Atoi(m[2])
	if err != nil || day < 1 || day > 31 {
		return time.Time{}, false
	}
	hour, err := strconv.Atoi(m[3])
	if err != nil || hour < 0 || hour > 23 {
		return time.Time{}, false
	}
	min := 0
	if m[4] != "" {
		v, err := strconv.Atoi(m[4])
		if err != nil || v < 0 || v > 59 {
			return time.Time{}, false
		}
		min = v
	}
	switch strings.ToLower(m[5]) {
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
	loc := time.Local
	if m[6] != "" {
		if loaded, err := time.LoadLocation(strings.TrimSpace(m[6])); err == nil {
			loc = loaded
		}
	}
	year := now.Year()
	candidate := time.Date(year, month, day, hour, min, 0, 0, loc)
	if !candidate.After(now) {
		candidate = time.Date(year+1, month, day, hour, min, 0, 0, loc)
	}
	if candidate.Sub(now) > 400*24*time.Hour {
		return time.Time{}, false
	}
	return candidate.UTC(), true
}
