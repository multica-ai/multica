package taskfailure

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// QuotaResetKind preserves whether a provider gave a precise reset instant or
// only a calendar date. A date must be probed before the pool opens.
type QuotaResetKind string

const (
	QuotaResetUnknown  QuotaResetKind = "unknown"
	QuotaResetExact    QuotaResetKind = "exact"
	QuotaResetDateOnly QuotaResetKind = "date_only"
)

// QuotaResetHint contains no raw provider text or credential material.
type QuotaResetHint struct {
	Kind     QuotaResetKind
	At       time.Time // UTC, populated only for exact resets.
	Date     string    // YYYY-MM-DD in Timezone, populated only for date-only resets.
	Timezone string
}

var (
	quotaResetMarker = regexp.MustCompile(`(?i)\bresets?\s+(?:at\s+|on\s+)?`)
	quotaISOInstant  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`)
	quotaISODate     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\b`)
	quotaNamedDate   = regexp.MustCompile(`(?i)^([A-Za-z]{3,9})\s+(\d{1,2})(?:,?\s+(\d{4}))?(?:\s+at\s+(\d{1,2})(?::(\d{2}))?\s*(am|pm)?)?(?:\s*\(([^)]+)\))?`)
)

// ParseQuotaResetHint extracts a reset only from an already-classified quota
// failure. A missing or ambiguous reset stays unknown; callers must never
// derive a weekly/monthly deadline from the failure timestamp alone. The
// account location is required for provider messages without an offset.
func ParseQuotaResetHint(reason Reason, message string, observedAt time.Time, accountLocation *time.Location) QuotaResetHint {
	unknown := QuotaResetHint{Kind: QuotaResetUnknown}
	if reason != ReasonAgentProviderQuotaLimit || accountLocation == nil || observedAt.IsZero() {
		return unknown
	}
	marker := quotaResetMarker.FindStringIndex(message)
	if marker == nil {
		return unknown
	}
	tail := strings.TrimSpace(message[marker[1]:])
	if match := quotaISOInstant.FindString(tail); match != "" {
		at, err := time.Parse(time.RFC3339Nano, match)
		if err == nil && at.After(observedAt) {
			return QuotaResetHint{Kind: QuotaResetExact, At: at.UTC(), Timezone: at.Location().String()}
		}
		return unknown
	}
	if match := quotaISODate.FindString(tail); match != "" {
		remainder := strings.TrimSpace(tail[len(match):])
		if remainder != "" && ((remainder[0] >= 'A' && remainder[0] <= 'Z') ||
			(remainder[0] >= 'a' && remainder[0] <= 'z') || remainder[0] == '(') {
			return unknown
		}
		date, err := time.ParseInLocation("2006-01-02", match, accountLocation)
		if err == nil && !beforeLocalDate(date, observedAt.In(accountLocation)) {
			return QuotaResetHint{Kind: QuotaResetDateOnly, Date: match, Timezone: accountLocation.String()}
		}
		return unknown
	}
	parts := quotaNamedDate.FindStringSubmatch(tail)
	if parts == nil {
		return unknown
	}
	// Reject a malformed time or zone suffix instead of accepting only its
	// date prefix and accidentally releasing a pool at midnight.
	remainder := strings.TrimSpace(tail[len(parts[0]):])
	if remainder != "" && ((remainder[0] >= 'A' && remainder[0] <= 'Z') ||
		(remainder[0] >= 'a' && remainder[0] <= 'z') || remainder[0] == '(') {
		return unknown
	}
	month, err := time.Parse("Jan", strings.ToUpper(parts[1][:1])+strings.ToLower(parts[1][1:3]))
	if err != nil {
		return unknown
	}
	day, _ := strconv.Atoi(parts[2])
	location := accountLocation
	if parts[7] != "" {
		location, err = time.LoadLocation(parts[7])
		if err != nil {
			return unknown
		}
	}
	year := observedAt.In(location).Year()
	if parts[3] != "" {
		year, err = strconv.Atoi(parts[3])
		if err != nil {
			return unknown
		}
	}
	date := time.Date(year, month.Month(), day, 0, 0, 0, 0, location)
	if date.Month() != month.Month() || date.Day() != day {
		return unknown
	}
	if parts[3] == "" && beforeLocalDate(date, observedAt.In(location)) {
		// Only a nearby New Year rollover is unambiguous without a year.
		candidate := time.Date(year+1, month.Month(), day, 0, 0, 0, 0, location)
		if candidate.Sub(observedAt) > 35*24*time.Hour {
			return unknown
		}
		date = candidate
	}
	if parts[3] == "" && date.Sub(observedAt) > 35*24*time.Hour {
		return unknown
	}
	if beforeLocalDate(date, observedAt.In(location)) {
		return unknown
	}
	if parts[4] == "" {
		return QuotaResetHint{Kind: QuotaResetDateOnly, Date: date.Format("2006-01-02"), Timezone: location.String()}
	}
	hour, _ := strconv.Atoi(parts[4])
	minute := 0
	if parts[5] != "" {
		minute, _ = strconv.Atoi(parts[5])
	}
	if parts[6] != "" {
		if hour < 1 || hour > 12 {
			return unknown
		}
		if strings.EqualFold(parts[6], "am") {
			hour %= 12
		} else {
			hour = hour%12 + 12
		}
	}
	if hour > 23 || minute > 59 {
		return unknown
	}
	at := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, location)
	if at.Year() != date.Year() || at.Month() != date.Month() || at.Day() != date.Day() || at.Hour() != hour || at.Minute() != minute || !at.After(observedAt) {
		return unknown
	}
	return QuotaResetHint{Kind: QuotaResetExact, At: at.UTC(), Timezone: location.String()}
}

func beforeLocalDate(date, observed time.Time) bool {
	y, m, d := observed.Date()
	return date.Before(time.Date(y, m, d, 0, 0, 0, 0, observed.Location()))
}
