package execenv

import (
	"fmt"
	"strings"
)

// cerebroSessionModeBrief renders the Settings-managed policy lines that follow
// a Mode's instruction in the agent brief: what the session may write, which
// tools and data sources it may use, and which approval policy applies.
//
// FIR-3111 introduced these lines inline in renderIssueContext. FIR-4047 moved
// them here so the upstream file keeps a single marked call-site instead of a
// 20-line inline patch, and so the write policy could be split into two scopes
// without growing that patch further.
//
// The write lines are the reason this exists. One AllowsWrite switch rendered
// "Writes are disabled. Do not edit code or data and do not make external
// mutations." underneath Plan Mode's own instruction to "save a concrete plan".
// The agent received both and obeyed the prohibition, so Plan Mode could not
// produce a plan. Saving a plan or a note is now its own scope, separate from
// writing code and data.
func cerebroSessionModeBrief(ctx TaskContextForEnv) string {
	var b strings.Builder

	switch {
	case ctx.SessionModeAllowsWrite:
		b.WriteString("Writes are enabled, subject to workspace permissions and approvals.\n\n")
	case ctx.SessionModePlanWrite:
		b.WriteString("You may save plans, notes and artifacts for this session. Do not edit code or data and do not make external mutations.\n\n")
	default:
		b.WriteString("Writes are disabled. Do not edit code or data, do not save plans or notes, and do not make external mutations.\n\n")
	}

	if len(ctx.SessionModeAllowedTools) > 0 {
		fmt.Fprintf(&b, "Allowed tools: %s. Do not use tools outside this list.\n\n", strings.Join(ctx.SessionModeAllowedTools, ", "))
	}
	if len(ctx.SessionModeDataSources) > 0 {
		fmt.Fprintf(&b, "Approved data sources: %s.\n\n", strings.Join(ctx.SessionModeDataSources, ", "))
	}
	switch ctx.SessionModeApprovalPolicy {
	case "require":
		b.WriteString("Approval is required before any mutation.\n\n")
	case "deny_external":
		b.WriteString("External mutations are denied.\n\n")
	}

	// Extra skills are loaded into the brief by the handler, so naming them again
	// here told the agent to "run" skills it had already been given. The Workflow
	// line is gone for the same reason it was never actionable: it named an ID the
	// agent had no way to start, and Modes do not drive Workflow selection.
	if len(ctx.SessionModeExtraSkillIDs) > 0 {
		fmt.Fprintf(&b, "This Mode adds %d extra skill(s) to the list above. Apply them as you would any bound skill.\n\n", len(ctx.SessionModeExtraSkillIDs))
	}

	return b.String()
}
