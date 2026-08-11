package handler

import "testing"

func TestAuthoritativeCostTicksPresence(t *testing.T) {
	t.Parallel()

	if got := authoritativeCostTicks(0, false); got.Valid {
		t.Fatalf("legacy zero must stay NULL, got %#v", got)
	}
	if got := authoritativeCostTicks(123, false); !got.Valid || got.Int64 != 123 {
		t.Fatalf("legacy positive = %#v", got)
	}
	gotZero := authoritativeCostTicks(0, true)
	if !gotZero.Valid || gotZero.Int64 != 0 {
		t.Fatalf("authoritative zero = %#v", gotZero)
	}
	if got := authoritativeCostTicks(-1, true); got.Valid {
		t.Fatalf("negative present must be rejected, got %#v", got)
	}
}
