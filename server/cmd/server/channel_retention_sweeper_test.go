package main

import (
	"testing"
	"time"
)

// TestNextRetentionRunAt covers the day-boundary cases for the sweep
// scheduler. The function is pure so we don't need a real clock.
func TestNextRetentionRunAt(t *testing.T) {
	loc := time.UTC
	cases := []struct {
		name string
		now  time.Time
		hour int
		want time.Time
	}{
		{
			name: "before today's hour mark",
			now:  time.Date(2026, 4, 30, 1, 0, 0, 0, loc),
			hour: 3,
			want: time.Date(2026, 4, 30, 3, 0, 0, 0, loc),
		},
		{
			name: "exactly at the hour mark counts as already-fired",
			now:  time.Date(2026, 4, 30, 3, 0, 0, 0, loc),
			hour: 3,
			want: time.Date(2026, 5, 1, 3, 0, 0, 0, loc),
		},
		{
			name: "after today's hour mark rolls to tomorrow",
			now:  time.Date(2026, 4, 30, 5, 30, 0, 0, loc),
			hour: 3,
			want: time.Date(2026, 5, 1, 3, 0, 0, 0, loc),
		},
		{
			name: "month rollover",
			now:  time.Date(2026, 4, 30, 23, 59, 0, 0, loc),
			hour: 3,
			want: time.Date(2026, 5, 1, 3, 0, 0, 0, loc),
		},
		{
			name: "hour 0 (midnight)",
			now:  time.Date(2026, 4, 30, 12, 0, 0, 0, loc),
			hour: 0,
			want: time.Date(2026, 5, 1, 0, 0, 0, 0, loc),
		},
		{
			name: "hour 23 (just before midnight)",
			now:  time.Date(2026, 4, 30, 12, 0, 0, 0, loc),
			hour: 23,
			want: time.Date(2026, 4, 30, 23, 0, 0, 0, loc),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextRetentionRunAt(tc.now, tc.hour)
			if !got.Equal(tc.want) {
				t.Fatalf("nextRetentionRunAt(%v, %d) = %v, want %v", tc.now, tc.hour, got, tc.want)
			}
		})
	}
}
