package taskfailure

import (
	"testing"
	"time"
)

func TestQuotaResetAt(t *testing.T) {
	zone := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 9, 29, 23, 0, 0, 0, zone)
	cases := []struct {
		name, input string
		want        bool
	}{
		{"GLM five-hour window", "已达到 5 小时的使用上限。您的限额将在 2026-09-30 01:55:13 重置。", true},
		{"generic billing refusal", "API Error: 402 Payment Required", false},
		{"unknown reset", "quota exhausted; try again later", false},
		{"stale reset", "已达到 5 小时的使用上限。您的限额将在 2026-09-29 22:00:00 重置。", false},
		{"distant reset", "已达到 5 小时的使用上限。您的限额将在 2026-10-20 01:55:13 重置。", false},
		{"malformed reset", "已达到 5 小时的使用上限。您的限额将在 2026-09-31 01:55:13 重置。", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := QuotaResetAt(tc.input, now, zone)
			if ok != tc.want {
				t.Fatalf("ok = %v, want %v", ok, tc.want)
			}
			if tc.want && !got.Equal(time.Date(2026, 9, 29, 17, 55, 13, 0, time.UTC)) {
				t.Errorf("reset = %s, want 2026-09-29T17:55:13Z", got)
			}
		})
	}
}
