package execenv

import (
	"fmt"
	"strings"
)

// cerebroSessionModeBrief renders the Settings-managed policy lines that follow
// a Mode's instruction in the agent brief: what the session may write, which
// tools and data sources it may use, which approval policy applies, and which
// evaluations run when the session completes.
//
// FIR-3111 introduced these lines inline in renderIssueContext. FIR-4047 moved
// them here so the upstream file keeps a single marked call-site instead of a
// 20-line inline patch.
//
// Every line here is brief text and nothing more — it persuades the agent, it
// does not gate it. Real gating lives in the tool-policy chain, so a line must
// never assert a prohibition the chain is not enforcing. The write lines are
// deliberately silent about plans and notes: Plan Mode's own instruction tells
// the agent to save a plan, and an added "do not save plans" line made the two
// contradict each other (FIR-4047).
func cerebroSessionModeBrief(ctx TaskContextForEnv) string {
	var b strings.Builder

	if ctx.SessionModeAllowsWrite {
		b.WriteString("Writes are enabled, subject to workspace permissions and approvals.\n\n")
	} else {
		b.WriteString("Writes are disabled. Do not edit code or data and do not make external mutations.\n\n")
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

	// The evaluations belong to the Mode, not to the agent: the server runs them
	// against this issue when the session completes. Saying so is honest — the
	// agent is told what will be checked, not asked to run the checks itself.
	if len(ctx.SessionModeEvalIDs) > 0 {
		fmt.Fprintf(&b, "%d evaluation(s) configured on this Mode run against this issue when the session completes. Their results are recorded in the eval history.\n\n", len(ctx.SessionModeEvalIDs))
	}

	return b.String()
}
