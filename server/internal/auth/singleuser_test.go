package auth

import "testing"

func TestSingleUserMode(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"", false},
		{"false", false},
		{"0", false},
		{"no", false},
		{"off", false},
		{"random", false},
		{"true", true},
		{"True", true},
		{"TRUE", true},
		{"1", true},
		{"yes", true},
		{"on", true},
		{"  true  ", true},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			t.Setenv("MULTICA_SINGLE_USER", tc.raw)
			if got := SingleUserMode(); got != tc.want {
				t.Fatalf("SingleUserMode(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
