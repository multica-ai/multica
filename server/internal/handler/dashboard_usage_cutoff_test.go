package handler

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestUsageHourCutoffRoundsUpToTheHour(t *testing.T) {
	at := func(s string) pgtype.Timestamptz {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			t.Fatal(err)
		}
		return pgtype.Timestamptz{Time: v, Valid: true}
	}
	for _, tc := range []struct{ in, want string }{
		{"2026-03-01T00:00:00Z", "2026-03-01T00:00:00Z"},           // whole hour: unchanged
		{"2026-02-28T18:30:00Z", "2026-02-28T19:00:00Z"},           // Asia/Kolkata midnight
		{"2026-02-28T18:15:00Z", "2026-02-28T19:00:00Z"},           // Asia/Kathmandu midnight
		{"2026-03-01T00:00:00.000001Z", "2026-03-01T01:00:00Z"},    // just past
		{"2026-03-01T05:30:00+05:30", "2026-03-01T00:00:00Z"},      // offset form of a whole UTC hour
		{"2026-03-01T09:45:00+09:30", "2026-03-01T01:00:00+00:00"}, // Australia/Darwin
	} {
		got := usageHourCutoff(at(tc.in))
		if want := at(tc.want); !got.Time.Equal(want.Time) || !got.Valid {
			t.Errorf("usageHourCutoff(%s) = %s, want %s", tc.in, got.Time.UTC(), want.Time.UTC())
		}
	}
}
