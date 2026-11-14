package agent

import (
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

type claudeQuotaWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    *int64   `json:"resetsAt"`
}

type claudeQuotaEvent struct {
	Status         string                       `json:"status"`
	RateLimitType  string                       `json:"rateLimitType"`
	UnifiedWindows map[string]claudeQuotaWindow `json:"unifiedWindows"`
	claudeQuotaWindow
}

func (e *claudeQuotaEvent) subscriptionQuota(now time.Time) *protocol.SubscriptionQuotaSnapshot {
	if e == nil {
		return nil
	}
	switch e.Status {
	case "allowed", "allowed_warning", "rejected":
	default:
		return nil
	}
	window := func(key string, duration int) *protocol.SubscriptionQuotaWindow {
		data := e.UnifiedWindows[key]
		if data.Utilization == nil && e.RateLimitType == key {
			data = e.claudeQuotaWindow
		}
		if data.Utilization == nil || *data.Utilization < 0 || *data.Utilization > 1 {
			return nil
		}
		percent := *data.Utilization * 100
		return &protocol.SubscriptionQuotaWindow{UsedPercent: &percent, DurationMins: &duration, ResetsAt: data.ResetsAt}
	}
	snapshot := &protocol.SubscriptionQuotaSnapshot{
		ObservedAt: now.Unix(),
		Primary:    window("five_hour", 5*60), Secondary: window("seven_day", 7*24*60),
	}
	if e.Status == "rejected" && (e.RateLimitType == "five_hour" || e.RateLimitType == "seven_day") {
		snapshot.Exhausted = true
		snapshot.LimitResetsAt = e.ResetsAt
	}
	if snapshot.Primary == nil && snapshot.Secondary == nil && !snapshot.Exhausted {
		return nil
	}
	return snapshot
}
