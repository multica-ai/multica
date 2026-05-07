package util

import "testing"

func TestParseUUID_Valid(t *testing.T) {
	u, err := ParseUUID("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if !u.Valid {
		t.Fatalf("expected u.Valid = true")
	}
}

func TestParseUUID_InvalidReturnsError(t *testing.T) {
	cases := []string{"", "not-a-uuid", "MUL-123", "12345"}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			u, err := ParseUUID(s)
			if err == nil {
				t.Fatalf("expected error for %q, got nil (u.Valid=%v)", s, u.Valid)
			}
			if u.Valid {
				// Critical invariant: invalid input must NOT yield a valid UUID.
				// Returning a valid zero-UUID was the root cause of #1661.
				t.Fatalf("expected u.Valid = false for %q, got true", s)
			}
		})
	}
}

func TestMustParseUUID_PanicsOnInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected MustParseUUID to panic on invalid input")
		}
	}()
	MustParseUUID("not-a-uuid")
}

func TestMustParseUUID_RoundTrip(t *testing.T) {
	const s = "550e8400-e29b-41d4-a716-446655440000"
	u := MustParseUUID(s)
	if got := UUIDToString(u); got != s {
		t.Fatalf("round-trip mismatch: got %q want %q", got, s)
	}
}
