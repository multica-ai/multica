package service

import (
	"errors"
	"strings"
	"testing"
)

func TestGuardrailDefaults(t *testing.T) {
	want := map[GuardrailKind]int{
		GuardrailAddressees:      20,
		GuardrailOpenRequests:    8,
		GuardrailChallenges:      8,
		GuardrailDirectives:      5,
		GuardrailDelegationDepth: 2,
	}
	for kind, max := range want {
		if got := GuardrailMax(kind); got != max {
			t.Errorf("GuardrailMax(%s) = %d, want %d", kind, got, max)
		}
	}
	if got := GuardrailMax("nope"); got != 0 {
		t.Errorf("GuardrailMax(unknown) = %d, want 0", got)
	}
}

func TestCheckGuardrailBoundaries(t *testing.T) {
	cases := []struct {
		kind     GuardrailKind
		proposed int
		wantErr  bool
	}{
		{GuardrailAddressees, 20, false},
		{GuardrailAddressees, 21, true},
		{GuardrailOpenRequests, 8, false},
		{GuardrailOpenRequests, 9, true},
		{GuardrailChallenges, 8, false},
		{GuardrailChallenges, 9, true},
		{GuardrailDirectives, 1, false},
		{GuardrailDirectives, 5, false},
		{GuardrailDirectives, 6, true},
		{GuardrailDelegationDepth, 2, false},
		{GuardrailDelegationDepth, 3, true},
		{GuardrailKind("unknown"), 1, true},
	}
	for _, tc := range cases {
		err := CheckGuardrail(tc.kind, tc.proposed)
		if (err != nil) != tc.wantErr {
			t.Errorf("CheckGuardrail(%s, %d) error = %v, wantErr %v", tc.kind, tc.proposed, err, tc.wantErr)
		}
	}
}

func TestCheckGuardrailErrorIsPlainAndTyped(t *testing.T) {
	err := CheckGuardrail(GuardrailDirectives, 6)
	var ge *GuardrailError
	if !errors.As(err, &ge) {
		t.Fatalf("expected *GuardrailError, got %T", err)
	}
	if ge.Kind != GuardrailDirectives || ge.Max != 5 || ge.Count != 6 {
		t.Errorf("unexpected fields: %+v", ge)
	}
	if msg := err.Error(); !strings.Contains(msg, "at most 5") {
		t.Errorf("message should state the limit, got %q", msg)
	}
}
