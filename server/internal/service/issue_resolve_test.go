package service

import (
	"errors"
	"testing"
)

// TestProjectTeamAmbiguousError pins the contract handlers depend on when a
// Team is omitted for a multi-team Project: the error matches the sentinel via
// errors.Is and its message names the candidate Team keys in order.
func TestProjectTeamAmbiguousError(t *testing.T) {
	err := error(&ProjectTeamAmbiguousError{TeamKeys: []string{"ENG", "GROWTH"}})

	if !errors.Is(err, ErrProjectTeamAmbiguous) {
		t.Fatalf("expected errors.Is(err, ErrProjectTeamAmbiguous) to be true")
	}
	// The other typed team errors must not match, so handlers can branch on them
	// independently.
	if errors.Is(err, ErrTeamNotFound) || errors.Is(err, ErrTeamArchived) {
		t.Fatalf("ambiguous error must not match unrelated team sentinels")
	}

	const want = "project has multiple teams (ENG, GROWTH); specify team_id"
	if got := err.Error(); got != want {
		t.Fatalf("message mismatch:\n got %q\nwant %q", got, want)
	}

	var amb *ProjectTeamAmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("expected errors.As to recover *ProjectTeamAmbiguousError")
	}
	if len(amb.TeamKeys) != 2 || amb.TeamKeys[0] != "ENG" || amb.TeamKeys[1] != "GROWTH" {
		t.Fatalf("recovered team keys mismatch: %v", amb.TeamKeys)
	}
}
