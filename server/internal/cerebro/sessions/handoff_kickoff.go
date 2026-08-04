// CEREBRO: carry the handoff brief in the kickoff comment itself (FIR-4500).
// The fresh run of a --start-new Handoff was told to go look the brief up
// (`multica issue session list`), which costs a tool call — and a tool call costs
// a full re-read of the run's context. The brief is small and already in hand
// when the kickoff comment is written, so write it there: the daemon inlines the
// triggering comment into every start prompt, and a human reading the issue sees
// what was handed over instead of a bare "Fresh session" marker.
package sessions

import (
	"strings"
)

// kickoffWithBrief appends the carry-over brief to the fresh session's opening
// message. An empty brief leaves the kickoff untouched, so the default kickoff's
// pointer at `multica issue session list` remains the fallback.
func kickoffWithBrief(kickoff string, brief *handoffBrief) string {
	rendered := renderHandoffBrief(brief)
	if rendered == "" {
		return kickoff
	}
	return strings.TrimSpace(kickoff) + "\n\n" + rendered
}

func renderHandoffBrief(brief *handoffBrief) string {
	if brief == nil {
		return ""
	}
	var b strings.Builder
	if s := strings.TrimSpace(brief.Summary); s != "" {
		b.WriteString(s + "\n\n")
	}
	writeList(&b, "Done", brief.Done)
	writeList(&b, "Remaining", brief.Remaining)
	if brief.PlanRef != nil {
		if ref := strings.TrimSpace(*brief.PlanRef); ref != "" {
			b.WriteString("Plan: " + ref + "\n")
		}
	}
	body := strings.TrimSpace(b.String())
	if body == "" {
		return ""
	}
	return "## Carry-over brief from the closed session\n\n" + body
}

func writeList(b *strings.Builder, heading string, items []string) {
	items = compactStrings(items)
	if len(items) == 0 {
		return
	}
	b.WriteString("**" + heading + "**\n")
	for _, item := range items {
		b.WriteString("- " + item + "\n")
	}
	b.WriteString("\n")
}
