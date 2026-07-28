package execenv

import (
	"strings"
	"testing"
)

// FIR-3659: the Workpad protocol section is rendered only when the workspace's
// cerebro_workpad flag is on (carried in via TaskContextForEnv.WorkpadBriefEnabled).
func TestCerebroWorkpadBrief(t *testing.T) {
	if got := cerebroWorkpadBrief(false); got != "" {
		t.Fatalf("flag off: want empty brief, got %q", got)
	}
	got := cerebroWorkpadBrief(true)
	if got == "" {
		t.Fatal("flag on: want a non-empty Workpad brief, got empty")
	}
	for _, want := range []string{"## Workpad", "plan", "--kind plan", "- [ ]", "- [x]"} {
		if !strings.Contains(got, want) {
			t.Errorf("flag on: brief missing %q\nbrief:\n%s", want, got)
		}
	}
}
