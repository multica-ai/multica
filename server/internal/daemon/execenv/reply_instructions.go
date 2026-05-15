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
//
// The template is provider- and platform-aware:
//
//   - Windows + any provider → write a UTF-8 file, post with `--content-file`.
//     This is the only path that survives Windows shells (PowerShell 5.1
//     defaults to ASCIIEncoding when piping to native commands and drops
//     non-ASCII as `?`; cmd.exe is at the mercy of `chcp`). The original
//     reports — #2198 (Chinese), #2236 (Chinese), #2376 (Cyrillic, observed
//     on a non-Codex agent) — all match this signature.
//   - Linux/macOS + Codex → stdin/HEREDOC. Codex tends to emit literal `\n`
//     escapes inside `--content "..."` and produce broken multi-line stored
//     comments (MUL-1467); stdin sidesteps that.
//   - Linux/macOS + non-Codex → lightweight inline `--content "..."`.
//     The CLI's `util.UnescapeBackslashEscapes` decodes `\n` server-side,
//     so escaped multi-line works correctly. This is the pre-#1795 default,
//     restored after we found #1795 / #1851 had expanded a Codex-specific
//     fix into a global mandate that broke Windows non-ASCII for every
//     provider.
func BuildCommentReplyInstructions(provider, issueID, triggerCommentID string) string {
	if triggerCommentID == "" {
		return ""
	}
	if runtimeGOOS == "windows" {
		return fmt.Sprintf(
			"If you decide to reply, post it as a comment — always use the trigger comment ID below, "+
				"do NOT reuse --parent values from previous turns in this session.\n\n"+
				"On Windows, write the reply body to a UTF-8 file with your file-write tool, then post it with `--content-file`. "+
				"Do NOT pipe via `--content-stdin` — Windows PowerShell 5.1's `$OutputEncoding` defaults to ASCIIEncoding when piping to native commands and silently drops non-ASCII (Chinese, Japanese, Cyrillic, accents, emoji) as `?` before the bytes reach `multica.exe`. "+
				"Do NOT use inline `--content`; it is easy to lose formatting or accidentally compress a structured reply into one line.\n\n"+
				"Use this form, preserving the same issue ID and --parent value:\n\n"+
				"    # 1. Write the reply body to a UTF-8 file (e.g. reply.md) with your file-write tool.\n"+
				"    # 2. Then run:\n"+
				"    multica issue comment add %s --parent %s --content-file ./reply.md\n\n"+
				"Do NOT write literal `\\n` escapes to simulate line breaks; the file preserves real newlines.\n",
			issueID, triggerCommentID,
		)
	}
	if provider == "codex" {
		return fmt.Sprintf(
			"If you decide to reply, post it as a comment — always use the trigger comment ID below, "+
				"do NOT reuse --parent values from previous turns in this session.\n\n"+
				"Always use `--content-stdin` with a HEREDOC for agent-authored issue comments, even when the reply is a single line. "+
				"Do NOT use inline `--content`; it is easy to lose formatting or accidentally compress a structured reply into one line.\n\n"+
				"Use this form, preserving the same issue ID and --parent value:\n\n"+
				"    cat <<'COMMENT' | multica issue comment add %s --parent %s --content-stdin\n"+
				"    First paragraph.\n"+
				"\n"+
				"    Second paragraph.\n"+
				"    COMMENT\n\n"+
				"Do NOT write literal `\\n` escapes to simulate line breaks; the HEREDOC preserves real newlines.\n",
			issueID, triggerCommentID,
		)
	}
	// Non-Codex providers on Linux/macOS: lightweight inline template, no
	// platform branch. Pre-#1795 default, restored after we found that
	// #1795 / #1851 had expanded a Codex-specific fix into a global mandate
	// that broke Windows non-ASCII for every provider. The CLI decodes
	// `\n` etc. server-side, so escaped multi-line is fine; for richer
	// formatting the agent can still reach for `--content-stdin` (works
	// on Linux/macOS) or `--content-file <path>` (works on every platform),
	// both listed in Available Commands above.
	return fmt.Sprintf(
		"If you decide to reply, post it as a comment — always use the trigger comment ID below, "+
			"do NOT reuse --parent values from previous turns in this session.\n\n"+
			"Use this form, preserving the same issue ID and --parent value:\n\n"+
			"    multica issue comment add %s --parent %s --content \"...\"\n\n"+
			"For multi-line bodies, code blocks, or content with quotes/backticks, prefer `--content-stdin` "+
			"(pipe a HEREDOC) or `--content-file <path>` (read a UTF-8 file). See Available Commands above for the full menu.\n",
		issueID, triggerCommentID,
	)
}
