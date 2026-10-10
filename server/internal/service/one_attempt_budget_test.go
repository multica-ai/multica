package service

import "testing"

func TestOneAttemptBudget(t *testing.T) {
	tests := []struct {
		name  string
		flags []bool
		want  bool
	}{
		{"legacy default", nil, false},
		{"explicit false", []bool{false}, false},
		{"single attempt", []bool{true}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := oneAttemptBudget(tc.flags); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
