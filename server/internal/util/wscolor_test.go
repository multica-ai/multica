package util

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestWorkspaceColorDeterministic(t *testing.T) {
	id := ParseUUID("11111111-1111-4111-8111-111111111111")
	got := WorkspaceColor(id)
	if got != WorkspaceColor(id) {
		t.Fatalf("WorkspaceColor must be stable for the same UUID")
	}
	if got == "" {
		t.Fatalf("WorkspaceColor returned empty for a valid UUID")
	}
}

func TestWorkspaceColorReturnsPaletteValue(t *testing.T) {
	got := WorkspaceColor(ParseUUID("11111111-1111-4111-8111-111111111111"))
	in := false
	for _, c := range workspaceColorPalette {
		if c == got {
			in = true
			break
		}
	}
	if !in {
		t.Fatalf("WorkspaceColor returned %q, not in palette", got)
	}
}

func TestWorkspaceColorInvalidUUID(t *testing.T) {
	var invalid pgtype.UUID
	if got := WorkspaceColor(invalid); got != "" {
		t.Fatalf("WorkspaceColor for invalid UUID = %q, want \"\"", got)
	}
}

func TestWorkspaceColorDistribution(t *testing.T) {
	// Distinct UUIDs should not all collapse to one color. We don't assert a
	// strict distribution — only that more than one bucket is hit across a
	// small but varied sample, which catches a broken hash.
	ids := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
		"44444444-4444-4444-8444-444444444444",
		"55555555-5555-4555-8555-555555555555",
		"66666666-6666-4666-8666-666666666666",
		"77777777-7777-4777-8777-777777777777",
		"88888888-8888-4888-8888-888888888888",
	}
	seen := map[string]struct{}{}
	for _, raw := range ids {
		seen[WorkspaceColor(ParseUUID(raw))] = struct{}{}
	}
	if len(seen) < 2 {
		t.Fatalf("expected at least 2 distinct colors, got %d", len(seen))
	}
}
