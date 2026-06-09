package service

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// sessionLimitResetRe matches the Claude session limit reset time pattern.
// Examples:
//
//	"You've hit your session limit · resets 5:10pm (UTC)"
//	"You've hit your session limit · resets 10:10pm (UTC)"
//	"You've hit your session limit · resets 12:00am (UTC)"
var sessionLimitResetRe = regexp.MustCompile(`(?i)resets?\s+(\d{1,2}):(\d{2})\s*(am|pm)\s*\(?UTC\)?`)

// ParseSessionLimitResetTime extracts the reset time from a Claude session
// limit message. Returns the absolute UTC time of the next occurrence of the
// given wall-clock time. Returns (zero, false) when the message does not
// contain a parseable reset time.
//
// The message only carries a wall-clock time (e.g. "5:10pm (UTC)") without a
// date, so we compute the next occurrence relative to now. If the parsed time
// is in the past today, we assume it refers to tomorrow (the reset hasn't
// happened yet in the current cycle).
func ParseSessionLimitResetTime(message string) (time.Time, bool) {
	matches := sessionLimitResetRe.FindStringSubmatch(message)
	if matches == nil {
		return time.Time{}, false
	}

	// The regex guarantees matches[1] and matches[2] are digit runs, so the
	// conversion cannot fail; the error is discarded by construction.
	hour, _ := strconv.Atoi(matches[1])
	minute, _ := strconv.Atoi(matches[2])
	ampm := strings.ToLower(matches[3])

	if hour < 1 || hour > 12 || minute > 59 {
		return time.Time{}, false
	}

	if ampm == "pm" && hour != 12 {
		hour += 12
	} else if ampm == "am" && hour == 12 {
		hour = 0
	}

	now := time.Now().UTC()
	reset := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if reset.Before(now) {
		reset = reset.AddDate(0, 0, 1)
	}
	return reset, true
}
