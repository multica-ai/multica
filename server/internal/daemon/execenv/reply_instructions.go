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
		"If you decide to reply, post it by piping the comment body through stdin. Always use the trigger comment ID below, "+
			"do NOT reuse --parent values from previous turns in this session:\n\n"+
			"```sh\n"+
			"cat <<'COMMENT' | multica issue comment add %s --parent %s --content-stdin\n"+
			"...\n"+
			"COMMENT\n"+
			"```\n\n"+
			"Do not use `--content \"...\\n...\"`; shell-escaped `\\n` is stored literally and renders as backslash-n text.\n",
		issueID, triggerCommentID,
	)
}
