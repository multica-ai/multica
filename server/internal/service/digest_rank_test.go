package service

import (
	"fmt"
	"testing"
	"time"
)

func TestRankSquads_AttentionOrderTiesAndQuiet(t *testing.T) {
	squads := []DigestSquad{
		{ID: "1", Name: "busy", Done: 3, InProgress: 2},
		{ID: "2", Name: "quiet-b"},
		{ID: "3", Name: "proposing", PendingProposalCount: 1},
		{ID: "4", Name: "blocked", Blocked: 2},
		{ID: "5", Name: "quiet-a", InProgress: 4},
		{ID: "6", Name: "also-blocked", Blocked: 2},
		{ID: "7", Name: "no-action", LeaderNoAction: 1},
	}
	full, omitted, quiet := rankSquads(squads)

	var order []string
	for _, sq := range full {
		order = append(order, sq.Name)
	}
	want := []string{"proposing", "also-blocked", "blocked", "no-action", "busy"}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if omitted != 0 {
		t.Fatalf("omitted = %d, want 0", omitted)
	}
	if quiet.Count != 2 || quiet.Squads[0].Name != "quiet-a" || quiet.Squads[0].InProgress != 4 || quiet.Squads[1].Name != "quiet-b" {
		t.Fatalf("quiet = %+v", quiet)
	}
}

func TestRankSquads_CutsAtTheCapAndCapsTheQuietList(t *testing.T) {
	var squads []DigestSquad
	for i := 0; i < DigestMaxSquads+5; i++ {
		squads = append(squads, DigestSquad{ID: fmt.Sprint(i), Name: fmt.Sprintf("loud-%02d", i), Blocked: 1})
	}
	for i := 0; i < DigestMaxQuiet+7; i++ {
		squads = append(squads, DigestSquad{ID: fmt.Sprint(1000 + i), Name: fmt.Sprintf("quiet-%03d", i)})
	}
	full, omitted, quiet := rankSquads(squads)
	if len(full) != DigestMaxSquads || omitted != 5 {
		t.Fatalf("full=%d omitted=%d, want %d and 5", len(full), omitted, DigestMaxSquads)
	}
	if quiet.Count != DigestMaxQuiet+7 || len(quiet.Squads) != DigestMaxQuiet {
		t.Fatalf("quiet count=%d listed=%d", quiet.Count, len(quiet.Squads))
	}
}

func TestParseDigestSince(t *testing.T) {
	now := time.Date(2026, 5, 10, 8, 0, 0, 0, time.UTC)
	if got, err := ParseDigestSince("", now); err != nil || !got.Equal(now.Add(-DigestDefaultWindow)) {
		t.Fatalf("default = %v, %v", got, err)
	}
	if _, err := ParseDigestSince("2026-05-09T08:00:00Z", now); err != nil {
		t.Fatalf("a valid timestamp was refused: %v", err)
	}
	for _, bad := range []string{"yesterday", "2026-05-11T08:00:00Z", "2026-01-01T00:00:00Z"} {
		if _, err := ParseDigestSince(bad, now); err == nil {
			t.Errorf("ParseDigestSince(%q) should fail", bad)
		}
	}
}
