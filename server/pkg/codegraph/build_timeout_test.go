package codegraph

import (
	"testing"
	"time"
)

func TestCodeGraphBuildTimeout(t *testing.T) {
	for _, tc := range []struct {
		input   string
		seconds int
	}{
		{"", 660}, {"2400", 2400}, {" 2400 ", 2400},
		{"120", 120}, {"86400", 86400}, {"119", 660},
		{"86401", 660}, {"0", 660}, {"-1", 660},
		{"2.4e3", 660}, {"2400s", 660}, {"999999999999999999999", 660},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Setenv("MULTICA_CODEGRAPH_BUILD_TIMEOUT_SECONDS", tc.input)
			if got := BuildTimeout(); got != time.Duration(tc.seconds)*time.Second {
				t.Fatalf("timeout = %s; want %d seconds", got, tc.seconds)
			}
		})
	}
}
