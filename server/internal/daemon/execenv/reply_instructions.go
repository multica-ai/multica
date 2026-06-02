package execenv

import "fmt"

// BuildNewCommentsHint returns the comment-reading pointer for the WARM path —
// the agent ran on this issue before, so there is a since-anchor. The server
// count is ISSUE-WIDE (every thread, not just the triggering one) and excludes
// the triggering comment itself because that body is already injected into the
// prompt. It ships only the COUNT and the cursor — never the comment bodies —
// so the server stays cheap and the agent pulls details on demand.
//
// The agent is told the full issue-wide volume but steered to read the
// triggering (parent) thread FIRST instead of blindly catching up on every
// thread. The issue-wide `--since` catch-up is kept as an explicit
// "only if you need it" fallback.
//
// Both the per-turn prompt (daemon.buildCommentPrompt) and the CLAUDE.md
// workflow (InjectRuntimeConfig) call this so the two surfaces cannot drift
// (hard requirement from PR #2816).
//
// Renders nothing on cold start (no prior run → newCommentsSince empty) or when
// there are no new comments (newCommentCount <= 0) or issueID is empty. In those
// cases the caller falls back to BuildResumedCommentsHint (when a prior session
// is active) or BuildColdCommentsHint.
func BuildNewCommentsHint(issueID, triggerCommentID, newCommentsSince string, newCommentCount int) string {
	if newCommentCount <= 0 || newCommentsSince == "" || issueID == "" {
		return ""
	}
	// When we know the triggering thread, steer the agent to read THAT thread
	// first rather than blindly pulling every new comment issue-wide. The
	// issue-wide --since catch-up is demoted to an only-if-needed fallback.
	if triggerCommentID != "" {
		return fmt.Sprintf(
			"%d new comment(s) on this issue since your last run — don't read them all blindly. "+
				"Start with the thread your triggering comment is in: "+
				"`multica issue comment list %s --thread %s --since %s --output json` "+
				"(swap `--since` for `--tail 30` if you need the full thread, not just the delta). "+
				"Only if you need context from the other threads, catch up issue-wide: "+
				"`multica issue comment list %s --since %s --output json`.\n\n",
			newCommentCount, issueID, triggerCommentID, newCommentsSince, issueID, newCommentsSince,
		)
	}
	// Defensive: comment triggers always carry a trigger id, but if one is
	// missing there is no thread to anchor on, so fall back to the plain
	// issue-wide catch-up.
	return fmt.Sprintf(
		"%d new comment(s) on this issue since your last run. Catch up: "+
			"`multica issue comment list %s --since %s --output json`.\n\n",
		newCommentCount, issueID, newCommentsSince,
	)
}

// BuildResumedCommentsHint returns the comment-reading pointer for the WARM
// no-delta path: the daemon is resuming a prior provider session and the
// triggering comment body has already been injected into the per-turn prompt.
// newCommentCount == 0 here means no new comments arrived issue-wide since the
// last run (beyond the injected trigger and the agent's own replies). Keep the
// read bounded and conditional, but make it explicit that context-dependent
// replies should refresh the triggering conversation rather than trusting
// resumed memory alone.
func BuildResumedCommentsHint(issueID, triggerCommentID string) string {
	if issueID == "" || triggerCommentID == "" {
		return ""
	}
	return fmt.Sprintf(
		"You're resuming the prior session, and the triggering comment is already included above. "+
			"No other new comments on this issue since your last run. "+
			"Use the triggering comment ID / thread anchor: `%s`. "+
			"If your reply depends on thread context, do not rely only on resumed session memory — "+
			"first pull the triggering conversation with: "+
			"`multica issue comment list %s --thread %s --tail 30 --output json`.\n\n",
		triggerCommentID, issueID, triggerCommentID,
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
// The template is platform-aware but provider-agnostic — the failure it
// guards against lives at the shell layer, so it cannot be scoped to one
// provider (MUL-2904):
//
//   - Windows + any provider → write a UTF-8 file, post with `--content-file`.
//     This is the only path that survives Windows shells (PowerShell 5.1
//     defaults to ASCIIEncoding when piping to native commands and drops
//     non-ASCII as `?`; cmd.exe is at the mercy of `chcp`). The original
//     reports — #2198 (Chinese), #2236 (Chinese), #2376 (Cyrillic, observed
//     on a non-Codex agent) — all match this signature.
//   - Linux/macOS + any provider → `--content-stdin` with a QUOTED HEREDOC
//     (`<<'COMMENT'`). The quoted delimiter stops the shell from expanding
//     backticks, `$()`, or `$VAR` inside the body. Inlining `--content "..."`
//     instead lets the shell rewrite the body BEFORE the CLI receives it: a
//     backtick-wrapped token becomes a failed command substitution that is
//     silently deleted, the stored comment no longer matches what the model
//     intended, and a model that notices the mismatch can retry forever
//     (MUL-2904 / OKK-497). It also sidesteps Codex's habit of emitting
//     literal `\n` escapes inside `--content` (MUL-1467).
//
// provider is retained for caller symmetry and future per-provider tweaks; the
// guardrail itself is intentionally identical across providers.
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
	// Linux/macOS, any provider: `--content-stdin` with a quoted HEREDOC. The
	// quoted delimiter (`<<'COMMENT'`) is what makes this safe — it stops the
	// shell from running backtick / `$()` substitution or `$VAR` expansion on
	// the body. Inlining `--content "..."` is what triggered the MUL-2904
	// duplicate-comment loop, so it is banned for every provider here, not just
	// Codex.
	return fmt.Sprintf(
		"If you decide to reply, post it as a comment — always use the trigger comment ID below, "+
			"do NOT reuse --parent values from previous turns in this session.\n\n"+
			"Always use `--content-stdin` with a HEREDOC for agent-authored issue comments, even when the reply is a single line. "+
			"Do NOT use inline `--content`; the shell rewrites unescaped backticks, `$()`, `$VAR`, or quotes in the body before the CLI receives them, and it is easy to lose formatting or compress a structured reply into one line.\n\n"+
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
