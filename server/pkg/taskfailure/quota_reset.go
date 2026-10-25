package taskfailure

import (
	"regexp"
	"time"
)

// glmQuotaWindowReset matches the reset notice emitted by the GLM CLI. Its
// timestamp has no timezone, so it must be interpreted on the daemon host
// where the CLI produced it, never on the server.
var glmQuotaWindowReset = regexp.MustCompile(`已达到\s*5\s*小时的使用上限[\s\S]*?限额将在\s*(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})\s*重置`)

// QuotaResetAt returns a trusted, bounded quota-window reset hint. It does not
// infer a reset from generic 402, billing, credits or rate-limit errors.
func QuotaResetAt(errText string, now time.Time, resetZone *time.Location) (time.Time, bool) {
	if resetZone == nil {
		return time.Time{}, false
	}
	match := glmQuotaWindowReset.FindStringSubmatch(errText)
	if len(match) != 2 {
		return time.Time{}, false
	}
	reset, err := time.ParseInLocation("2006-01-02 15:04:05", match[1], resetZone)
	if err != nil || reset.Before(now.Add(time.Minute)) || reset.After(now.Add(8*24*time.Hour)) {
		return time.Time{}, false
	}
	return reset.UTC(), true
}
