package sessions

import (
	"strings"
	"testing"
)

func TestKickoffWithBrief(t *testing.T) {
	t.Parallel()

	plan := "  " // whitespace-only refs must not render a heading
	got := kickoffWithBrief(defaultHandoffKickoff, &handoffBrief{
		Summary:   "Shipped the snapshot fix.",
		Done:      []string{"PR 2896 opened", "  "},
		Remaining: []string{"Merge when CI is green"},
		PlanRef:   &plan,
	})

	for _, want := range []string{
		"## Carry-over brief from the closed session",
		"Shipped the snapshot fix.",
		"- PR 2896 opened",
		"- Merge when CI is green",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("kickoff missing %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Plan:") {
		t.Fatalf("blank plan_ref must not render a Plan line, got:\n%s", got)
	}
	if !strings.HasPrefix(got, "🔄 Fresh session (handoff)") {
		t.Fatalf("kickoff must keep its opening line, got:\n%s", got)
	}
}

func TestKickoffWithBrief_Empty(t *testing.T) {
	t.Parallel()

	for name, brief := range map[string]*handoffBrief{
		"nil":   nil,
		"blank": {Summary: "   "},
	} {
		if got := kickoffWithBrief(defaultHandoffKickoff, brief); got != defaultHandoffKickoff {
			t.Fatalf("%s brief must leave the kickoff untouched, got:\n%s", name, got)
		}
	}
}
