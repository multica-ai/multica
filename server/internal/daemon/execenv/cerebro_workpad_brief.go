package execenv

import (
	"strings"
)

// cerebro_workpad_brief.go (FIR-3659) renders the Workpad protocol section of
// the runtime brief. The Workpad is the issue's PLAN: a single `plan` note
// coupled to the issue, whose checklist the UI renders as a panel at the bottom
// of the issue (above the composer) the moment the plan exists. This brief tells
// the working agent to create that one plan note before starting and keep its
// checklist updated for the life of the issue — it is guidance, not a gate.
//
// Gated by the cerebro_workpad workspace feature flag (resolved server-side at
// claim time and carried into the brief via TaskContextForEnv.WorkpadBriefEnabled).

func cerebroWorkpadBrief(enabled bool) string {
	if !enabled {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Workpad — the plan for this issue\n\n")
	b.WriteString("**The Workpad is the issue's plan, shown as a checklist at the bottom of the issue.** It is backed by a single `plan` note coupled to the issue, and it appears automatically the moment that plan exists — no one has to paste anything into the description. On an issue you own:\n\n")
	b.WriteString("1. Before you start, create ONE plan for the issue: `multica artifact create --kind plan --issue <issue-id> --title \"Plan\" --body-file <path>`, where the body is a markdown checklist — one `- [ ]` line per step.\n")
	b.WriteString("2. As you work, keep that SAME plan updated: flip a step to `- [x]` the moment it is done (`multica artifact update <plan-id> --body-file <path>`), and add new `- [ ]` lines if the plan grows. Update the plan BEFORE posting any progress comment.\n")
	b.WriteString("3. There is exactly ONE plan per issue — never create a second (the server rejects it); update the existing plan. Never paste the checklist into the issue description; the Workpad renders the plan note for you.\n\n")
	b.WriteString("The plan note is versioned automatically, so its history is preserved every time you update it.\n\n")
	return b.String()
}
