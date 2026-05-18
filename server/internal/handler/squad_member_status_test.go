package handler

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestDeriveSquadMemberStatus(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	online := pgtype.Text{String: "online", Valid: true}
	offline := pgtype.Text{String: "offline", Valid: true}
	missing := pgtype.Text{}

	tsAgo := func(d time.Duration) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: now.Add(-d), Valid: true}
	}
	tsNone := pgtype.Timestamptz{}

	cases := []struct {
		name          string
		runtimeStatus pgtype.Text
		lastSeen      pgtype.Timestamptz
		hasActiveTask bool
		want          string
	}{
		{"active wins over offline runtime", offline, tsAgo(time.Hour), true, "working"},
		{"active wins over missing runtime", missing, tsNone, true, "working"},
		{"online runtime, no task", online, tsAgo(2 * time.Second), false, "idle"},
		{"offline runtime, recent heartbeat", offline, tsAgo(2 * time.Minute), false, "unstable"},
		{"offline runtime, stale heartbeat", offline, tsAgo(2 * time.Hour), false, "offline"},
		{"offline runtime, no heartbeat", offline, tsNone, false, "offline"},
		{"no runtime row", missing, tsNone, false, "offline"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveSquadMemberStatus(tc.runtimeStatus, tc.lastSeen, tc.hasActiveTask, now)
			if got != tc.want {
				t.Fatalf("deriveSquadMemberStatus = %q, want %q", got, tc.want)
			}
		})
	}
}
