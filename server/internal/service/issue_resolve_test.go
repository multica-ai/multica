package service

import (
	"errors"
	"testing"
)

// TestProjectSpaceAmbiguousError pins the contract handlers depend on when a
// Space is omitted for a multi-space Project: the error matches the sentinel via
// errors.Is and its message names the candidate Space keys in order.
func TestProjectSpaceAmbiguousError(t *testing.T) {
	err := error(&ProjectSpaceAmbiguousError{SpaceKeys: []string{"ENG", "GROWTH"}})

	if !errors.Is(err, ErrProjectSpaceAmbiguous) {
		t.Fatalf("expected errors.Is(err, ErrProjectSpaceAmbiguous) to be true")
	}
	// The other typed space errors must not match, so handlers can branch on them
	// independently.
	if errors.Is(err, ErrSpaceNotFound) || errors.Is(err, ErrSpaceArchived) {
		t.Fatalf("ambiguous error must not match unrelated space sentinels")
	}

	const want = "project has multiple spaces (ENG, GROWTH); specify space_id"
	if got := err.Error(); got != want {
		t.Fatalf("message mismatch:\n got %q\nwant %q", got, want)
	}

	var amb *ProjectSpaceAmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("expected errors.As to recover *ProjectSpaceAmbiguousError")
	}
	if len(amb.SpaceKeys) != 2 || amb.SpaceKeys[0] != "ENG" || amb.SpaceKeys[1] != "GROWTH" {
		t.Fatalf("recovered space keys mismatch: %v", amb.SpaceKeys)
	}
}
