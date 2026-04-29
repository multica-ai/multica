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
		"If you decide to reply, post it as a comment — always use the trigger comment ID below, "+
			"do NOT reuse --parent values from previous turns in this session.\n\n"+
			"For a short single-line reply, use:\n\n"+
			"    multica issue comment add %s --parent %s --content \"...\"\n\n"+
			"For multi-line content (paragraphs, bullets, code blocks, backticks, quotes, or anything where formatting matters), "+
			"do NOT squeeze it into `--content` and do NOT write `\\n` escapes. Pipe via stdin instead, preserving the same issue ID and --parent value:\n\n"+
			"    cat <<'COMMENT' | multica issue comment add %s --parent %s --content-stdin\n"+
			"    First paragraph.\n"+
			"\n"+
			"    Second paragraph.\n"+
			"    COMMENT\n",
		issueID, triggerCommentID, issueID, triggerCommentID,
	)
}
