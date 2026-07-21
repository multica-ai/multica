package agent

import "testing"

func TestHandoffSupported(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{"at minimum", MinHandoffCLIVersion, true},
		{"above minimum", "0.4.0", true},
		{"below minimum", "0.3.26", false},
		{"far below", "0.2.21", false},
		{"empty (unreported)", "", false},
		{"unparsable", "garbage", false},
		{"dev git-describe build", "v0.3.0-5-gabc1234", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HandoffSupported(tc.version); got != tc.want {
				t.Fatalf("HandoffSupported(%q) = %v, want %v", tc.version, got, tc.want)
			}
		})
	}
}
