package execenv

import (
	"strings"
	"testing"
)

// FIR-3377: the Cerebro Feature Flags section must appear in the Available
// Commands block of every agent brief so agents can look up which cerebro
// features are active instead of guessing. Locks both the section header and
// the two read-only commands it advertises.
func TestBriefIncludesCerebroFeatureSection(t *testing.T) {
	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID: "00000000-0000-0000-0000-000000000001",
	})
	if !strings.Contains(out, "### Cerebro Feature Flags") {
		t.Fatalf("expected Cerebro Feature Flags section in the agent brief")
	}
	if !strings.Contains(out, "multica feature list") {
		t.Fatalf("expected `multica feature list` to appear in the brief")
	}
	if !strings.Contains(out, "multica feature get") {
		t.Fatalf("expected `multica feature get` to appear in the brief")
	}
}
