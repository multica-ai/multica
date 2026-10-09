package service

import "testing"

func TestOneAttemptBudget(t *testing.T) {
	cases := []struct {
		name  string
		flags []bool
		valid bool
		want  int32
	}{
		{"legacy default", nil, false, 0},
		{"explicit false", []bool{false}, false, 0},
		{"single attempt", []bool{true}, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := oneAttemptBudget(tc.flags)
			if got.Valid != tc.valid || (got.Valid && got.Int32 != tc.want) {
				t.Fatalf("got %+v, wanted valid=%v limit=%d", got, tc.valid, tc.want)
			}
		})
	}
}
