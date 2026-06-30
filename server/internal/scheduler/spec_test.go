package scheduler

import (
	"testing"
	"time"
)

func TestFloorPlanUTC(t *testing.T) {
	cadence := 5 * time.Minute

	cases := []struct {
		name  string
		input time.Time
		want  time.Time
	}{
		{
			name:  "exact bucket boundary",
			input: time.Date(2026, 6, 3, 8, 15, 0, 0, time.UTC),
			want:  time.Date(2026, 6, 3, 8, 15, 0, 0, time.UTC),
		},
		{
			name:  "mid-bucket truncates down",
			input: time.Date(2026, 6, 3, 8, 17, 42, 0, time.UTC),
			want:  time.Date(2026, 6, 3, 8, 15, 0, 0, time.UTC),
		},
		{
			name:  "non-utc input normalises to utc bucket",
			input: time.Date(2026, 6, 3, 17, 17, 42, 0, time.FixedZone("CST", 9*3600)),
			want:  time.Date(2026, 6, 3, 8, 15, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FloorPlan(tc.input, cadence)
			if !got.Equal(tc.want) {
				t.Fatalf("FloorPlan(%s, %s) = %s; want %s",
					tc.input, cadence, got, tc.want)
			}
			if got.Location() != time.UTC {
				t.Fatalf("FloorPlan must return UTC, got %s", got.Location())
			}
		})
	}
}
