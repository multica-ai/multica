package chapters

import "testing"

func TestResolveStartMode(t *testing.T) {
	cases := []struct {
		in     string
		want   string
		wantOK bool
	}{
		{"", startModeHandoff, true},        // default carries the handoff forward
		{"handoff", startModeHandoff, true}, // explicit carry-over
		{"blank", startModeBlank, true},     // explicit blank session
		{"Handoff", "", false},              // case-sensitive: unknown
		{"none", "", false},                 // unknown value is rejected, not silently defaulted
		{"reset", "", false},                // unknown value is rejected
	}
	for _, c := range cases {
		got, ok := resolveStartMode(c.in)
		if ok != c.wantOK || got != c.want {
			t.Errorf("resolveStartMode(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}
