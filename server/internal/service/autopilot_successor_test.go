package service

import "testing"

func TestSuccessorMatchesStatus(t *testing.T) {
	cases := []struct {
		onStatus       string
		terminalStatus string
		want           bool
	}{
		{"completed", "completed", true},
		{"completed", "failed", false},
		{"completed", "skipped", false},
		{"failed", "completed", false},
		{"failed", "failed", true},
		{"failed", "skipped", false},
		{"both", "completed", true},
		{"both", "failed", true},
		{"both", "skipped", false},
		{"unknown", "completed", false},
		{"", "completed", false},
	}
	for _, c := range cases {
		got := successorMatchesStatus(c.onStatus, c.terminalStatus)
		if got != c.want {
			t.Fatalf("successorMatchesStatus(%q, %q) = %v, want %v", c.onStatus, c.terminalStatus, got, c.want)
		}
	}
}
