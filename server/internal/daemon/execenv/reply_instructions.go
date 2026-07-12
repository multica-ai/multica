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
func BuildNewCommentsHint(issueID, triggerCommentID, triggerThreadID, newCommentsSince string, newCommentCount int) string {
	if newCommentCount <= 0 || newCommentsSince == "" || issueID == "" {
		return ""
	}
	threadID := activeThreadID(triggerThreadID, triggerCommentID)
	// When we know the triggering thread, steer the agent to read THAT thread
	// first rather than blindly pulling every new comment issue-wide. The
	// issue-wide --since catch-up is demoted to an only-if-needed fallback.
	if threadID != "" {
		return fmt.Sprintf(
			"%d new comment(s) on this issue since your last run — don't read them all blindly. "+
				"Start with the thread your triggering comment is in: "+
				"`multica issue comment list %s --thread %s --since %s --output json` "+
				"(swap `--since` for `--tail 30` if you need the full thread, not just the delta). "+
				"Only if you need context from the other threads, catch up issue-wide: "+
				"`multica issue comment list %s --since %s --output json`.\n\n",
			newCommentCount, issueID, threadID, newCommentsSince, issueID, newCommentsSince,
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
func BuildResumedCommentsHint(issueID, triggerCommentID, triggerThreadID string) string {
	threadID := activeThreadID(triggerThreadID, triggerCommentID)
	if issueID == "" || threadID == "" {
		return ""
	}
	return fmt.Sprintf(
		"You're resuming the prior session, and the triggering comment is already included above. "+
			"No other new comments on this issue since your last run. "+
			"Use the active thread anchor `%s` and triggering comment ID `%s`. "+
			"If your reply depends on thread context, do not rely only on resumed session memory — "+
			"first pull the triggering conversation with: "+
			"`multica issue comment list %s --thread %s --tail 30 --output json`.\n\n",
		threadID, triggerCommentID, issueID, threadID,
	)
}

// BuildColdCommentsHint returns the comment-reading pointer for the COLD path —
// the agent has no prior run on this issue, so there is no since-anchor and
// BuildNewCommentsHint renders nothing. Instead of dumping the whole flat
// timeline (oldest-first, server cap 2000), point the agent at the triggering
// CONVERSATION: `--thread <trigger> --tail 30` returns that thread's root plus
// its 30 newest replies (root is always included, even at --tail 0) — the
// context the triggering comment actually needs. A `--recent 10` pointer is kept
// for cross-thread background the agent can pull on judgment.
//
// Both surfaces call this so the cold fallback cannot drift between them (same
// single-source rule as BuildNewCommentsHint, PR #2816). Returns "" when there
// is no triggering comment to thread from, so the caller can keep a final plain
// fallback.
func BuildColdCommentsHint(issueID, triggerCommentID, triggerThreadID string) string {
	threadID := activeThreadID(triggerThreadID, triggerCommentID)
	if issueID == "" || threadID == "" {
		return ""
	}
	return fmt.Sprintf(
		"Read the triggering conversation first: "+
			"`multica issue comment list %s --thread %s --tail 30 --output json` "+
			"(that thread's root + its 30 newest replies). "+
			"Need cross-thread background? `multica issue comment list %s --recent 10 --output json` "+
			"(resolved threads come back folded — `--full` to expand).\n\n",
		issueID, threadID, issueID,
	)
}

func activeThreadID(triggerThreadID, triggerCommentID string) string {
	if triggerThreadID != "" {
		return triggerThreadID
	}
	return triggerCommentID
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
// The template is platform-agnostic AND provider-agnostic — the failure it
// guards against lives at the shell layer, so it cannot be scoped to one
// provider or one OS:
//
//   - Inline `--content "..."` lets the shell rewrite the body BEFORE the CLI
//     receives it: a backtick-wrapped token becomes a failed command
//     substitution that is silently deleted, the stored comment no longer
//     matches what the model intended, and a model that notices the mismatch
//     can retry forever (MUL-2904 / OKK-497). It also lets Codex emit literal
//     `\n` escapes inside `--content` (MUL-1467).
//   - `--content-stdin` with a HEREDOC has TWO failure modes the model cannot
//     see:
//     1. On Windows, PowerShell 5.1's `$OutputEncoding` defaults to
//     ASCIIEncoding when piping to native commands and drops non-ASCII as
//     `?` before the bytes reach `multica.exe` (#2198 Chinese, #2236
//     Chinese, #2376 Cyrillic).
//     2. On any host, when the model emits a multi-flag command (e.g.
//     `multica issue create --title ... --assignee-id ... --project ...`)
//     the bash heredoc/flag boundary is fragile: a `BODY \` "terminator
//     with trailing token" is not recognised as the heredoc end, so flag
//     lines after it are swallowed into the description; or a clean
//     terminator turns the trailing `--assignee ...` line into a separate
//     shell statement that fails while the create already succeeded with
//     no assignee. Both paths exit 0 with silently dropped flags. Github
//     issue #4182 documents two confirmed cases (OXY-78, OXY-76).
//
// The single safe path is therefore: write the body to a UTF-8 file with
// the file-write tool, post with `--content-file`, then remove the file.
// All flags live on one shell-token line; the body never touches the shell;
// no heredoc boundary exists for flags to leak across. This converges with
// the long-standing Windows path so the cross-platform template is one shape.
//
// provider is retained for caller symmetry and future per-provider tweaks; the
// guardrail itself is intentionally identical across providers and hosts.
func BuildCommentReplyInstructions(provider, issueID, triggerCommentID string) string {
	if triggerCommentID == "" {
		return ""
	}
	return buildCommentReplyInstructionsSlim(provider, issueID, triggerCommentID)
}

// buildCommentReplyInstructionsSlim is the compressed reply-instructions
// block used by BuildCommentReplyInstructions. It was introduced in
// MUL-3560 as the slim alternative to a legacy verbose form; the
// `runtime_brief_slim` flag has since been retired (MUL-4297) and this is
// now the only form.
//
// The slim block carries only the trigger-specific cookbook (the exact
// `--parent` UUID, the file path, the cleanup line) plus the two
// behavioural rules tests pin ("do NOT reuse --parent" and "do not rely
// on `\n` escapes"). The detailed shell-hazard rationale lives in the
// canonical `## Comment Formatting` section the same brief carries, so
// repeating it inline at every comment-triggered step 7 would be
// duplication, not signal.
func buildCommentReplyInstructionsSlim(provider, issueID, triggerCommentID string) string {
	if runtimeGOOS == "windows" {
		return fmt.Sprintf(
			"If you decide to reply, post it as a comment — always use the trigger comment ID below, "+
				"do NOT reuse --parent values from previous turns in this session.\n\n"+
				"On Windows, write the reply body to a UTF-8 file with your file-write tool first, then post with `--content-file`. "+
				"Do NOT pipe via `--content-stdin` — PowerShell 5.1's `$OutputEncoding` defaults to ASCIIEncoding when piping to native commands and silently drops non-ASCII (Chinese, Japanese, Cyrillic, accents, emoji) as `?` before bytes reach `multica.exe`. "+
				"See ## Comment Formatting above for the full rule:\n\n"+
				"    multica issue comment add %s --parent %s --content-file ./reply.md\n"+
				"    Remove-Item ./reply.md\n\n"+
				"Do NOT write literal `\\n` escapes to simulate line breaks; the file preserves real newlines.\n",
			issueID, triggerCommentID,
		)
	}
	return fmt.Sprintf(
		"If you decide to reply, post it as a comment — always use the trigger comment ID below, "+
			"do NOT reuse --parent values from previous turns in this session.\n\n"+
			"Write the reply body to a UTF-8 file with your file-write tool first, then post it with `--content-file` "+
			"(see ## Comment Formatting above for why inline `--content` and `--content-stdin` HEREDOCs are unsafe — MUL-2904 / #4182):\n\n"+
			"    multica issue comment add %s --parent %s --content-file ./reply.md\n"+
			"    rm ./reply.md\n\n"+
			"Do NOT write literal `\\n` escapes to simulate line breaks; the file preserves real newlines.\n",
		issueID, triggerCommentID,
	)
}
