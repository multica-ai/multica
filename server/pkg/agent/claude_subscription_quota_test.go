package agent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestClaudeSubscriptionQuotaEvent(t *testing.T) {
	var msg claudeSDKMessage
	input := `{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","rateLimitType":"five_hour",
		"unifiedWindows":{"five_hour":{"utilization":0,"resetsAt":1800000000},"seven_day":{"utilization":0.45}}}}`
	if err := json.Unmarshal([]byte(input), &msg); err != nil {
		t.Fatal(err)
	}
	quota := msg.RateLimitInfo.subscriptionQuota(time.Now())
	if quota == nil || quota.Primary == nil || quota.Secondary == nil ||
		*quota.Primary.UsedPercent != 0 || *quota.Primary.DurationMins != 300 ||
		*quota.Secondary.UsedPercent != 45 || *quota.Secondary.DurationMins != 10080 {
		t.Fatalf("unexpected quota: %+v", quota)
	}

	var event claudeQuotaEvent
	if err := json.Unmarshal([]byte(`{"status":"rejected","rateLimitType":"seven_day","resetsAt":1800000000}`), &event); err != nil {
		t.Fatal(err)
	}
	quota = event.subscriptionQuota(time.Now())
	if quota == nil || !quota.Exhausted || quota.Primary != nil || quota.Secondary != nil ||
		quota.LimitResetsAt == nil || *quota.LimitResetsAt != 1800000000 {
		t.Fatalf("expected exhaustion without inferred usage: %+v", quota)
	}

	event.RateLimitType = "overage"
	if quota := event.subscriptionQuota(time.Now()); quota != nil {
		t.Fatalf("unexpected subscription quota for overage: %+v", quota)
	}
}
