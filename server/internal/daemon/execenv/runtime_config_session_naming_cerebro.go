package execenv

// CEREBRO-PATCH(runtime-config-session-naming): FIR-4801 — contextual runtime
// guidance for naming every new agent-involved top-level issue thread. The
// existing session APIs already support human- and agent-selected names; this
// helper closes the behavioral gap without adding a parallel naming path.

import (
	"fmt"
	"strings"
)

func cerebroSessionNamingRule(ctx TaskContextForEnv) string {
	if ctx.IssueID == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Session naming for new threads\n\n")
	if ctx.TriggerCommentID != "" && ctx.TriggerThreadID != "" && ctx.TriggerCommentID == ctx.TriggerThreadID && ctx.WakeupPrompt == "" {
		b.WriteString("This run was triggered by a new top-level thread. Before doing any other work, give this session a short, human-readable name that describes the thread's concrete purpose. Call `rename_session` with:\n")
		fmt.Fprintf(&b, "- `issue_id`: `%s`\n", ctx.IssueID)
		fmt.Fprintf(&b, "- `root_comment_id`: `%s`\n", ctx.TriggerThreadID)
		b.WriteString("- `name`: the descriptive name you choose\n\n")
		b.WriteString("If `rename_session` is unavailable, run `multica issue session rename <issue-id> <root-comment-id> --name \"<descriptive name>\"`. Do not use a generic name such as `Session 1`, `Plan`, `Build 1`, or `Review 1`.\n\n")
	}
	b.WriteString("Whenever you create a new top-level comment (no `--parent`), use the returned comment ID as the thread root and immediately name the new session with `rename_session` or `multica issue session rename`.\n\n")
	return b.String()
}
