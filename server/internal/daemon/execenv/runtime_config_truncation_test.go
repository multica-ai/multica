package execenv

import (
	"strings"
	"testing"
)

func TestCriticalBriefSectionsSurviveAntigravityCap(t *testing.T) {
	t.Parallel()

	filler := strings.Repeat("User-authored instruction line.\n", 400)
	out := buildMetaSkillContent("antigravity", TaskContextForEnv{
		IssueID:            "issue-1",
		ProjectID:          "proj-1",
		DocsEnabled:        true,
		AgentName:          "Eve",
		AgentID:            "eve-1",
		AgentInstructions:  filler,
		WorkspaceContext:   filler,
		ProjectDescription: filler,
	})

	if !strings.Contains(out, "the runtime has cut this file") {
		t.Fatal("brief is missing the truncation guard")
	}
	if !strings.Contains(out, briefCompleteMarker) {
		t.Fatal("full brief is missing the closing marker")
	}

	prefix := out
	if len(prefix) > antigravityRuleFileCap {
		prefix = prefix[:antigravityRuleFileCap]
	}
	for _, want := range []string{
		"the runtime has cut this file",
		"## Important: Always Use the `multica` CLI",
		"## Mentions",
		"## Output",
		"### Workflow",
	} {
		if strings.Contains(prefix, want) {
			continue
		}
		t.Errorf("first %d bytes of a large brief are missing %q (brief is %d bytes)", antigravityRuleFileCap, want, len(out))
	}
	if strings.HasSuffix(strings.TrimSpace(prefix), briefCompleteMarker) {
		t.Fatal("truncated prefix still ends with the closing marker; enlarge the user-controlled filler")
	}

	for _, later := range []string{"## Agent Identity", "## Workspace Context", "## Project Context", "## Available Commands"} {
		crit := headingIndex(out, "## Output")
		other := strings.Index(out, later)
		if crit < 0 || other < 0 || other < crit {
			t.Errorf("%q must appear after ## Output (output=%d, other=%d)", later, crit, other)
		}
	}
}

func TestBriefClosingMarkerIsLastNonEmptyLine(t *testing.T) {
	t.Parallel()
	out := strings.TrimSpace(buildMetaSkillContent("claude", TaskContextForEnv{IssueID: "issue-1"}))
	if !strings.HasSuffix(out, briefCompleteMarker) {
		t.Fatalf("brief should end with %q, last 80 bytes: %q", briefCompleteMarker, out[max(0, len(out)-80):])
	}
}
