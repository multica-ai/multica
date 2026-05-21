package handler

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestParseSinceParam locks in the natural-calendar-day semantic that the
// workspace dashboard's `1d` / `7d` / `30d` / `90d` selectors depend on:
//
//   - `days=N` returns UTC start-of-today minus (N-1) full days, so the
//     window covers N calendar days (today + N-1 prior).
//   - `days=1` therefore means "today only" — not the trailing 24h that the
//     previous wall-clock implementation produced (which leaked yesterday
//     into the pre-aggregated `byAgent` / `runTime` endpoints).
func TestParseSinceParam(t *testing.T) {
	now := time.Now().UTC()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		path string
		want time.Time
	}{
		{name: "default (no days)", path: "/x", want: startOfToday.AddDate(0, 0, -29)},      // defaultDays=30
		{name: "1d = today only", path: "/x?days=1", want: startOfToday},                    // critical: PR 2837 fix
		{name: "7d", path: "/x?days=7", want: startOfToday.AddDate(0, 0, -6)},
		{name: "30d", path: "/x?days=30", want: startOfToday.AddDate(0, 0, -29)},
		{name: "90d", path: "/x?days=90", want: startOfToday.AddDate(0, 0, -89)},
		{name: "invalid falls back to default", path: "/x?days=abc", want: startOfToday.AddDate(0, 0, -29)},
		{name: "zero falls back to default", path: "/x?days=0", want: startOfToday.AddDate(0, 0, -29)},
		{name: "over cap falls back to default", path: "/x?days=400", want: startOfToday.AddDate(0, 0, -29)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			got := parseSinceParam(req, 30)
			if !got.Valid {
				t.Fatalf("expected Valid timestamptz, got invalid")
			}
			if !got.Time.Equal(tc.want) {
				t.Errorf("days param in %q: got %s, want %s", tc.path, got.Time.Format(time.RFC3339), tc.want.Format(time.RFC3339))
			}
		})
	}
}
