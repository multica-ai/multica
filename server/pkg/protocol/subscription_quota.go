package protocol

import "time"

const (
	SubscriptionQuotaComplete           = "complete"
	SubscriptionQuotaUnavailable        = "unavailable"
	SubscriptionQuotaFailed             = "failed"
	DaemonCapabilitySubscriptionQuotaV1 = "subscription-quota-v1"
	SubscriptionQuotaUnsupported        = "unsupported"
	SubscriptionQuotaRequestTTL         = time.Minute
	PendingWorkKindQuota                = "subscription_quota"
)

type SubscriptionQuotaWindow struct {
	UsedPercent  *float64 `json:"usedPercent"`
	DurationMins *int     `json:"windowDurationMins,omitempty"`
	ResetsAt     *int64   `json:"resetsAt,omitempty"`
}

type SubscriptionQuotaSnapshot struct {
	Exhausted     bool                     `json:"exhausted,omitempty"`
	LimitResetsAt *int64                   `json:"limitResetsAt,omitempty"`
	ObservedAt    int64                    `json:"observedAt"`
	Primary       *SubscriptionQuotaWindow `json:"primary,omitempty"`
	Secondary     *SubscriptionQuotaWindow `json:"secondary,omitempty"`
}

func ObservesTaskSubscriptionQuota(provider string) bool {
	return provider == "claude" || provider == "dsh"
}

type SubscriptionQuotaReport struct {
	Status   string                     `json:"status"`
	Snapshot *SubscriptionQuotaSnapshot `json:"snapshot,omitempty"`
}
