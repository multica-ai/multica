package execenv

import "fmt"

// BuildNewCommentsHint returns the comment-reading pointer for the WARM path —
// the agent ran on this issue before, so there is a since-anchor. The server
// count is scoped to the triggering thread and excludes the triggering comment
// itself because that body is already injected into the prompt. It ships only
// the COUNT and the cursor — never the comment bodies — so the server stays
// cheap and the agent pulls details on demand.
//
// Both the per-turn prompt (daemon.buildCommentPrompt) and the CLAUDE.md
// workflow (InjectRuntimeConfig) call this so the two surfaces cannot drift
// (hard requirement from PR #2816).
//
// Renders nothing on cold start (no prior run → newCommentsSince empty) or when
// there are no additional thread comments (newCommentCount <= 0) or required
// IDs are empty. In those cases the caller falls back to BuildResumedCommentsHint
// (when a prior session is active) or BuildColdCommentsHint.
func BuildNewCommentsHint(issueID, triggerCommentID, newCommentsSince string, newCommentCount int) string {
	if newCommentCount <= 0 || newCommentsSince == "" || issueID == "" || triggerCommentID == "" {
		return ""
	}
	hint := fmt.Sprintf(
		"%d other new comment(s) in this thread since your last run. "+
			"The triggering comment is already included above. "+
			"To inspect the raw thread delta (which may include the injected trigger and agent-authored rows), run: "+
			"`multica issue comment list %s --thread %s --since %s --output json`.\n\n",
		newCommentCount, issueID, triggerCommentID, newCommentsSince,
	)
	// --thread + --since is still only a delta: it covers new rows in the
	// triggering thread, not older pre-anchor context. Keep a bounded fallback
	// when the older conversation context is missing.
	hint += fmt.Sprintf(
		"If older thread context before %s is missing, pull the triggering conversation: "+
			"`multica issue comment list %s --thread %s --tail 30 --output json`.\n\n",
		newCommentsSince, issueID, triggerCommentID,
	)
	return hint
}

// BuildResumedCommentsHint returns the comment-reading pointer for the WARM
// no-delta path: the daemon is resuming a prior provider session and the
// triggering comment body has already been injected into the per-turn prompt.
// In that shape, reading the triggering thread's last 30 replies is duplicate
// context by default. Keep the bounded thread read as an explicit fallback for
// missing context instead of making it the first action.
func BuildResumedCommentsHint(issueID, triggerCommentID string) string {
	if issueID == "" || triggerCommentID == "" {
		return ""
	}
	return fmt.Sprintf(
		"You're resuming the prior session, and the triggering comment is already included above. "+
			"Do not re-read comment history by default. "+
			"Only if the resumed session is missing thread context, pull the triggering conversation: "+
			"`multica issue comment list %s --thread %s --tail 30 --output json`.\n\n",
		issueID, triggerCommentID,
	)
}

// BuildColdCommentsHint returns the comment-reading pointer for the COLD path —
// the agent has no prior run on this issue, so there is no since-anchor and
// BuildNewCommentsHint renders nothing. Instead of dumping the whole flat
// timeline (oldest-first, server cap 2000), point the agent at the triggering
// CONVERSATION: `--thread <trigger> --tail 30` returns that thread's root plus
// its 30 newest replies (root is always included, even at --tail 0) — the
// context the triggering comment actually needs. A `--recent 20` pointer is kept
// for cross-thread background the agent can pull on judgment.
//
// Both surfaces call this so the cold fallback cannot drift between them (same
// single-source rule as BuildNewCommentsHint, PR #2816). Returns "" when there
// is no triggering comment to thread from, so the caller can keep a final plain
// fallback.
func BuildColdCommentsHint(issueID, triggerCommentID string) string {
	if issueID == "" || triggerCommentID == "" {
		return ""
	}
	return fmt.Sprintf(
		"Read the triggering conversation first: "+
			"`multica issue comment list %s --thread %s --tail 30 --output json` "+
			"(that thread's root + its 30 newest replies). "+
			"Need cross-thread background? `multica issue comment list %s --recent 20 --output json`.\n\n",
		issueID, triggerCommentID, issueID,
	)
}

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
