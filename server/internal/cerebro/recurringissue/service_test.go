package recurringissue

import (
	"testing"
	"time"
)

func d(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestNextDueDate(t *testing.T) {
	tests := []struct {
		name      string
		base      string
		freq      string
		interval  int
		weekdays  []int
		daysAfter int
		want      string
	}{
		{"daily +1", "2026-06-17", FreqDaily, 1, nil, 0, "2026-06-18"},
		{"daily every 3", "2026-06-17", FreqDaily, 3, nil, 0, "2026-06-20"},
		{"weekly +1 no weekday", "2026-06-17", FreqWeekly, 1, nil, 0, "2026-06-24"},
		{"monthly +1", "2026-06-17", FreqMonthly, 1, nil, 0, "2026-07-17"},
		{"monthly +2", "2026-01-31", FreqMonthly, 2, nil, 0, "2026-03-31"},
		{"yearly +1", "2026-06-17", FreqYearly, 1, nil, 0, "2027-06-17"},
		{"days_after 10", "2026-06-17", FreqDaysAfter, 1, nil, 10, "2026-06-27"},
		// 2026-06-17 is a Wednesday (ISO 3). every 1 week → 2026-06-24 (Wed),
		// snap forward to Monday(1) → 2026-06-29.
		{"weekly snap to monday", "2026-06-17", FreqWeekly, 1, []int{1}, 0, "2026-06-29"},
		// custom = weekly; snap to Friday(5) from 2026-06-24(Wed) → 2026-06-26.
		{"custom every 1 week friday", "2026-06-17", FreqCustom, 1, []int{5}, 0, "2026-06-26"},
		{"interval zero clamps to 1", "2026-06-17", FreqDaily, 0, nil, 0, "2026-06-18"},
		// every_weekday: next working day, weekends skipped, interval/weekdays ignored.
		// 2026-06-17 is Wednesday → next weekday is Thursday 2026-06-18.
		{"every_weekday from wednesday", "2026-06-17", FreqEveryWeekday, 1, nil, 0, "2026-06-18"},
		// 2026-06-19 is Friday → skip Sat/Sun → Monday 2026-06-22.
		{"every_weekday friday skips weekend", "2026-06-19", FreqEveryWeekday, 1, nil, 0, "2026-06-22"},
		// 2026-06-20 is Saturday → next weekday is Monday 2026-06-22.
		{"every_weekday from saturday", "2026-06-20", FreqEveryWeekday, 1, nil, 0, "2026-06-22"},
		// interval is ignored for every_weekday → still just the next weekday.
		{"every_weekday ignores interval", "2026-06-17", FreqEveryWeekday, 3, []int{1, 5}, 0, "2026-06-18"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NextDueDate(d(tc.base), tc.freq, tc.interval, tc.weekdays, tc.daysAfter)
			if got.Format("2006-01-02") != tc.want {
				t.Fatalf("NextDueDate(%s, %s, %d, %v, %d) = %s, want %s",
					tc.base, tc.freq, tc.interval, tc.weekdays, tc.daysAfter,
					got.Format("2006-01-02"), tc.want)
			}
		})
	}
}

func TestValidateFrequency(t *testing.T) {
	for _, f := range ValidFrequencies {
		if err := ValidateFrequency(f); err != nil {
			t.Errorf("ValidateFrequency(%q) unexpected error: %v", f, err)
		}
	}
	if err := ValidateFrequency("hourly"); err == nil {
		t.Error("ValidateFrequency(hourly) should error")
	}
}

func TestNormalizeWeekdays(t *testing.T) {
	got := normalizeWeekdays([]int{3, 1, 3, 9, 0, 7})
	want := []int{1, 3, 7}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
