package execenv

import (
	"strings"
)

// cerebro_workpad_brief.go (FIR-3659) renders the Workpad protocol section of
// the runtime brief. The workpad convention: an issue's description opens with
// a prepended `## Workpad` checklist that the working agent creates before
// flipping the issue to in_progress and keeps updated for the life of the
// issue. Enforcement is server-side (before.issue.status_change hook policies
// backed by the issue_has_workpad eval), so this brief section is guidance,
// not the gate — it exists so agents do the right thing on the first attempt
// instead of learning the rule from a 422.
//
// Gated by the cerebro_workpad workspace feature flag (resolved server-side at
// claim time and carried into the brief via TaskContextForEnv.WorkpadBriefEnabled)
// so the rollout order can be: brief on (agents learn the convention) → policies
// on (the gate enforces it).

func cerebroWorkpadBrief(enabled bool) string {
	if !enabled {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Workpad — plan checklist in the issue description\n\n")
	b.WriteString("**The issue description IS your workpad.** Before you start building on an issue you own:\n\n")
	b.WriteString("1. Read the current description (`multica issue get <id> --output json`). If it has no `## Workpad` section, prepend one — a `- [ ]` checklist of your plan, one stage per line — above ALL existing content, followed by a `---` divider. Never alter the content below the divider.\n")
	b.WriteString("2. The moment a stage is done, flip its line to `- [x]` and write the FULL description back with `multica issue update <id> --description-file <path>` (resend every other line unchanged; never inline `--description`).\n")
	b.WriteString("3. Update the workpad BEFORE posting any progress comment — a progress claim with a stale workpad is a protocol violation.\n")
	b.WriteString("4. Plan changed? Add new `- [ ]` lines; never delete or rewrite completed ones.\n\n")
	b.WriteString("An issue without a workpad cannot be moved to `in_progress` by an agent when the workspace's workpad gate is enforced — create the workpad first.\n\n")
	return b.String()
}
