package service

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestAutopilotSquadAttribution(t *testing.T) {
	id := pgtype.UUID{Valid: true}
	copy(id.Bytes[:], []byte("01234567890123456789012345678901"))

	tests := []struct {
		name string
		ap   db.Autopilot
		want pgtype.UUID
	}{
		{"agent assignee returns zero", db.Autopilot{AssigneeType: "agent", AssigneeID: id}, pgtype.UUID{}},
		{"squad assignee returns squad id", db.Autopilot{AssigneeType: "squad", AssigneeID: id}, id},
		{"squad with invalid id returns zero", db.Autopilot{AssigneeType: "squad", AssigneeID: pgtype.UUID{}}, pgtype.UUID{}},
		{"unset type defaults to non-squad", db.Autopilot{AssigneeID: id}, pgtype.UUID{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := autopilotSquadAttribution(tc.ap)
			if got.Valid != tc.want.Valid {
				t.Fatalf("Valid mismatch: got %v want %v", got.Valid, tc.want.Valid)
			}
			if got.Valid && got.Bytes != tc.want.Bytes {
				t.Fatalf("Bytes mismatch")
			}
		})
	}
}

func TestFormatAdmissionReason(t *testing.T) {
	tests := []struct {
		name string
		ap   db.Autopilot
		raw  string
		want string
	}{
		{"agent archived", db.Autopilot{AssigneeType: "agent"}, "agent is archived", "assignee agent is archived"},
		{"squad archived", db.Autopilot{AssigneeType: "squad"}, "agent is archived", "squad leader agent is archived"},
		{"agent no runtime", db.Autopilot{AssigneeType: "agent"}, "agent has no runtime bound", "assignee agent has no runtime bound"},
		{"squad no runtime", db.Autopilot{AssigneeType: "squad"}, "agent has no runtime bound", "squad leader agent has no runtime bound"},
		{"runtime offline retains MUL-1899 suffix", db.Autopilot{AssigneeType: "agent"}, "agent runtime is offline", "agent runtime is offline at dispatch time"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatAdmissionReason(tc.ap, tc.raw); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

// errDispatchSkipped must be distinguishable via errors.As from a wrapped
// fmt.Errorf, otherwise DispatchAutopilot's failure-vs-skip switch will treat
// it as a generic failure and the manual-trigger handler will 500. Locks in
// the contract that fixed the post-admission race (PR #2888 review fix #2).
func TestErrDispatchSkippedUnwraps(t *testing.T) {
	base := &errDispatchSkipped{reason: "squad leader agent is archived"}
	wrapped := fmt.Errorf("dispatch run_only: %w", base)

	var got *errDispatchSkipped
	if !errors.As(wrapped, &got) {
		t.Fatalf("errors.As did not match errDispatchSkipped through fmt.Errorf wrap")
	}
	if got.reason != base.reason {
		t.Fatalf("reason mismatch: got %q want %q", got.reason, base.reason)
	}

	// pgx.ErrNoRows must NOT pass through the same gate — otherwise transient
	// "row not found" errors that should fail-open via shouldSkipDispatch
	// would be swallowed silently as skips at the dispatch level.
	if errors.As(pgx.ErrNoRows, &got) {
		t.Fatal("pgx.ErrNoRows wrongly satisfied errDispatchSkipped")
	}
}

func TestResolveAutopilotLeaderSentinels(t *testing.T) {
	// Sanity-check the sentinel exported via errors.Is so callers can branch
	// on "archived" without string-matching the failure reason.
	if !errors.Is(errSquadArchived, errSquadArchived) {
		t.Fatal("errSquadArchived must satisfy errors.Is itself")
	}
	wrapped := fmt.Errorf("wrap: %w", errSquadArchived)
	if !errors.Is(wrapped, errSquadArchived) {
		t.Fatal("errSquadArchived must unwrap through fmt.Errorf")
	}
}
