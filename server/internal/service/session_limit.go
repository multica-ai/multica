package service

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// sessionLimitResetRe matches the Claude session limit reset time pattern. The
// trailing timezone is captured and may be "UTC" or any IANA name in
// Region/City form (e.g. "Europe/Paris", "America/Argentina/Buenos_Aires").
// Examples:
//
//	"You've hit your session limit · resets 5:10pm (UTC)"
//	"You've hit your session limit · resets 10:10pm (Europe/Paris)"
//	"You've hit your session limit · resets 12:00am (America/New_York)"
//	"You've hit your session limit · resets 12pm (UTC)"
//	"You've hit your session limit · resets 2pm (Europe/Paris)"
var sessionLimitResetRe = regexp.MustCompile(`(?i)resets?\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)\s*\(?([A-Za-z]+(?:/[A-Za-z0-9_+-]+)*)\)?`)

// ParseSessionLimitResetTime extracts the reset time from a Claude session
// limit message. The wall-clock time is interpreted in the timezone named by
// the message (UTC or an IANA Region/City zone) and returned as the absolute
// UTC time of its next occurrence. Returns (zero, false) when the message does
// not contain a parseable reset time or names a timezone the system cannot
// resolve.
//
// The message only carries a wall-clock time (e.g. "5:10pm (Europe/Paris)")
// without a date, so we compute the next occurrence relative to now in that
// zone. If the parsed time is in the past today, we assume it refers to
// tomorrow (the reset hasn't happened yet in the current cycle).
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

	// IANA names are case-sensitive; the regex preserves the original casing of
	// the captured zone, so it is passed to LoadLocation as written. An
	// unresolvable zone makes the reset time unparseable.
	loc, err := time.LoadLocation(matches[4])
	if err != nil {
		return time.Time{}, false
	}

	now := time.Now().In(loc)
	reset := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)
	if reset.Before(now) {
		reset = reset.AddDate(0, 0, 1)
	}
	return reset.UTC(), true
}
