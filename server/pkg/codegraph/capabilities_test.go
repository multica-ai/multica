package codegraph

import "testing"

func TestSupportsLocalTools(t *testing.T) {
	for _, tc := range []struct {
		mode, provider string
		want           bool
	}{
		{"local", "hermes", true}, {"local", "codex", true},
		{"cloud", "hermes", false}, {"cloud", "ag-ui", false},
		{"local", "ag-ui", false}, {"local", "unknown", false},
	} {
		if got := SupportsLocalTools(tc.mode, tc.provider); got != tc.want {
			t.Errorf("SupportsLocalTools(%q, %q) = %v, want %v", tc.mode, tc.provider, got, tc.want)
		}
	}
}
