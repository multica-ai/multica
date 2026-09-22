package taskfailure

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// QuotaResetHorizon caps how far ahead a parsed reset time may sit. A parse
// that lands further out than this is treated as unparseable: the caller then
// falls back to its own backoff instead of parking a task for a week on the
// strength of one ambiguous string.
const QuotaResetHorizon = 7 * 24 * time.Hour

// quotaResetRe matches the reset clause the Claude runtime appends to a quota
// refusal: "You've hit your session limit - resets 12:50pm (UTC)" and
// "You've hit your weekly limit - resets 2pm (UTC)". Minutes and the zone are
// optional in the wording, so both are optional here.
var quotaResetRe = regexp.MustCompile(`(?i)resets?\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*(?:\(\s*([A-Za-z0-9_+\-/]+)\s*\))?`)

// ParseQuotaResetAt extracts the moment a provider quota window reopens from a
// free-form failure message, resolved against now.
//
// It deliberately refuses everything it cannot pin down exactly:
//
//   - no reset clause at all;
//   - a zone that is not UTC/GMT/Z (the message carries a zone abbreviation,
//     not an offset, and guessing one would park the task at the wrong
//     minute);
//   - a wall-clock time that cannot exist (hour > 23, minute > 59);
//   - a result further out than QuotaResetHorizon.
//
// The wall clock has no date, so the answer is the next occurrence of that
// time strictly after now. That under-shoots a weekly window - "resets 2pm"
// on a weekly limit is the next 2pm, not the one seven days out - which is
// the safe direction: the retry simply fails again and the caller's backoff
// widens, whereas over-shooting would strand the task.
func ParseQuotaResetAt(message string, now time.Time) (time.Time, bool) {
	m := quotaResetRe.FindStringSubmatch(message)
	if m == nil {
		return time.Time{}, false
	}
	hour, err := strconv.Atoi(m[1])
	if err != nil {
		return time.Time{}, false
	}
	minute := 0
	if m[2] != "" {
		if minute, err = strconv.Atoi(m[2]); err != nil {
			return time.Time{}, false
		}
	}
	switch strings.ToLower(m[3]) {
	case "am":
		if hour < 1 || hour > 12 {
			return time.Time{}, false
		}
		if hour == 12 {
			hour = 0
		}
	case "pm":
		if hour < 1 || hour > 12 {
			return time.Time{}, false
		}
		if hour != 12 {
			hour += 12
		}
	default:
		// 24-hour wording, e.g. "resets 14:00 (UTC)".
		if hour > 23 {
			return time.Time{}, false
		}
	}
	if minute > 59 {
		return time.Time{}, false
	}
	switch strings.ToUpper(m[4]) {
	case "", "UTC", "GMT", "Z":
		// An absent zone is read as UTC: every observed message carries
		// "(UTC)", and the runtime reports in UTC.
	default:
		return time.Time{}, false
	}

	now = now.UTC()
	reset := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if !reset.After(now) {
		reset = reset.Add(24 * time.Hour)
	}
	if reset.Sub(now) > QuotaResetHorizon {
		return time.Time{}, false
	}
	return reset, true
}
