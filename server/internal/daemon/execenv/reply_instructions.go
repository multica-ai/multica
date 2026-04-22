package execenv

import "fmt"

// BuildCommentReplyInstructions returns the canonical block telling an agent
// how to post its reply for a comment-triggered task. Both the per-turn
// prompt (daemon.buildCommentPrompt) and the CLAUDE.md workflow
// (InjectRuntimeConfig) call this so the trigger comment ID and the
// --parent value cannot drift between surfaces.
//
// The explicit "do not reuse --parent from previous turns" wording exists
// because resumed Claude sessions keep prior turns' tool calls in context
// and will otherwise copy the old --parent UUID forward.
func BuildCommentReplyInstructions(issueID, triggerCommentID string) string {
	if triggerCommentID == "" {
		return ""
	}
	return fmt.Sprintf(
		"Reply by running exactly this command — always use the trigger comment ID below, "+
			"do NOT reuse --parent values from previous turns in this session:\n\n"+
			"    multica issue comment add %s --parent %s --content \"...\"\n",
		issueID, triggerCommentID,
	)
}
