package execenv

import "fmt"

// BuildNewCommentsHint supplies issue-wide catch-up facts and exact read commands
// for a resumed provider session. The count excludes the injected trigger and
// the agent's own comments; the unfiltered since read may return more rows.
//
// The issue-wide since read supplies the server-computed delta and satisfies the
// workflow's scan. This hint does not make that scan optional. Keep per-run state
// in the user message rather than the stable brief.
//
// The since read has a 2000-comment cap. Use tail for bounded thread reads because
// thread+since alone may omit the root. Empty inputs render nothing, allowing
// the caller to select resumed-empty or cold-start guidance instead.
func BuildNewCommentsHint(issueID, triggerCommentID, triggerThreadID, newCommentsSince string, newCommentCount int) string {
	if newCommentCount <= 0 || newCommentsSince == "" || issueID == "" {
		return ""
	}
	threadID := activeThreadID(triggerThreadID, triggerCommentID)
	// One command, not two: the server already computed this delta, so the
	// agent reads it instead of rediscovering it with a scan plus expansions
	// (MUL-7344). The optional second command is a THREAD read, offered only
	// for the reply itself — it is never the wide read.
	delta := fmt.Sprintf(
		"Comment delta: %d new comment(s) on this issue since your last run, across all threads. "+
			"Read exactly those comments with "+
			"`multica issue comment list %s --since %s --compact --output json` "+
			"(every comment created after that anchor, in every thread — it also returns the triggering comment and your own replies, "+
			"which the count above excludes, so expect more rows than that number).",
		newCommentCount, issueID, newCommentsSince,
	)
	// Defensive: comment triggers always carry a trigger id, but if one is
	// missing there is no thread to anchor the optional full-thread read on.
	if threadID == "" {
		return delta + "\n\n"
	}
	return fmt.Sprintf(
		"%s Triggering thread in full, if resumed memory is not enough for the reply: "+
			"`multica issue comment list %s --thread %s --tail 30 --compact --output json`.\n\n",
		delta, issueID, threadID,
	)
}

// BuildResumedCommentsHint returns the comment-reading pointer for the WARM
// path where the server COMPUTED an issue-wide delta this claim and it came
// back empty: the daemon is resuming a prior provider session, the triggering
// comment body has already been injected into the per-turn prompt, and no
// other comment arrived since the last run (beyond that trigger and the
// agent's own replies).
//
// That zero is server-computed and issue-wide, so it IS the answer the scan in
// workflow step 2 exists to produce; the hint says so, which is the one way a
// per-turn message may satisfy the scan without a call. Re-reading the
// triggering thread in full stays the agent's own call: step 2 leaves "which
// threads to expand" to judgment, and this is that judgment, not a decision
// about whether the wide read happens (MUL-6984).
//
// The caller must reach this ONLY on Task.NewCommentsDeltaKnown. A zero count
// on its own does not mean the delta is empty — a failed anchor read, a failed
// count read, a cold start and an old server all produce the same zero, and
// none of them looked at the issue. Claiming the scan is answered from one of
// those would delete the very fallback the mandatory scan exists to be; that
// path renders BuildResumedUnknownDeltaCommentsHint instead.
func BuildResumedCommentsHint(issueID, triggerCommentID, triggerThreadID string) string {
	threadID := activeThreadID(triggerThreadID, triggerCommentID)
	if issueID == "" || threadID == "" {
		return ""
	}
	// No standalone anchor-restating sentence here (MUL-5721 OPT-1): the read
	// command below already carries the thread anchor, and the trigger comment
	// id reaches the agent as the reply cookbook's `--parent` value.
	return fmt.Sprintf(
		"Comment delta: no other new comments on this issue since the previous run, "+
			"excluding the triggering comment and your own replies. "+
			"Triggering thread in full, if resumed memory is not enough for the reply: "+
			"`multica issue comment list %s --thread %s --tail 30 --compact --output json`.\n\n",
		issueID, threadID,
	)
}

// BuildResumedUnknownDeltaCommentsHint returns the comment-reading pointer for
// a resumed run whose issue-wide delta this claim does NOT carry: the count
// query or its anchor lookup failed, or the server predates the delta fields.
//
// The hint offers the thread read without implying anything about the rest
// of the issue: nothing here looked. Workflow step 2's scan is mandatory
// by default and only an affirmative server report may waive it, so this hint
// hands the scan over as a command instead of waiving it (MUL-6984).
func BuildResumedUnknownDeltaCommentsHint(issueID, triggerCommentID, triggerThreadID string) string {
	threadID := activeThreadID(triggerThreadID, triggerCommentID)
	if issueID == "" {
		return ""
	}
	if threadID == "" {
		return fmt.Sprintf(
			"Comment delta: unknown. Read the issue's comment history with "+
				"`multica issue comment list %s --roots-only --summary --compact --output json`.\n\n",
			issueID,
		)
	}
	return fmt.Sprintf(
		"Comment delta: unknown. Read the issue's comment history with "+
			"`multica issue comment list %s --roots-only --summary --compact --output json`, "+
			"and expand what its `last_activity_at` shows has moved. "+
			"Triggering thread in full, if resumed memory is not enough for the reply: "+
			"`multica issue comment list %s --thread %s --tail 30 --compact --output json`.\n\n",
		issueID, issueID, threadID,
	)
}

// BuildColdCommentsHint returns the comment-reading pointer for a run whose
// latest turn on this issue did not come back — its first run, a resume the
// daemon had to drop, an explicitly fresh rerun, or a MUL-5305 older-fallback
// session, where the server hands back an OLDER session with the continuity
// gap flagged and the daemon does resume it. The hint therefore states nothing
// about the provider session: "fresh" is false for the fallback case, and no
// decision depends on it — every one of these runs reconstructs from the issue
// record with the same two reads (MUL-6984 review). Instead of dumping the
// whole flat timeline (oldest-first, server cap 2000), point the agent at the
// triggering CONVERSATION: `--thread <trigger> --tail 30` returns that thread's
// root plus its 30 newest replies (root is always included, even at --tail 0)
// — the context the triggering comment actually needs. The scan workflow step 2
// requires is handed over as the wide read; the hint deliberately does NOT
// name `--recent`, whose saturation trap and pagination live once in the
// brief's `## Available Commands` (MUL-5372). Per-turn hints name only the
// reads they actually want the agent to run, and state no modality: the
// earlier "Need cross-thread background?" framing invited the agent to judge
// a need it had no data to judge (MUL-6984).
//
// Since MUL-5377 the per-turn prompt is the only caller (same as
// BuildNewCommentsHint). Returns "" when there is no triggering comment to
// thread from, so the caller can keep a final plain fallback.
func BuildColdCommentsHint(issueID, triggerCommentID, triggerThreadID string) string {
	threadID := activeThreadID(triggerThreadID, triggerCommentID)
	if issueID == "" || threadID == "" {
		return ""
	}
	// The roots scan is phrased as a flag swap on the thread command above, not
	// a second full command: the duplicate restated the issue UUID for no
	// routing value (MUL-5721 OPT-1).
	return fmt.Sprintf(
		"Triggering thread: "+
			"`multica issue comment list %s --thread %s --tail 30 --compact --output json` "+
			"(that thread's root + its 30 newest replies). "+
			"Scan the other threads with the same command using `--roots-only --summary` in place of `--thread ... --tail 30`.\n\n",
		issueID, threadID,
	)
}

func activeThreadID(triggerThreadID, triggerCommentID string) string {
	if triggerThreadID != "" {
		return triggerThreadID
	}
	return triggerCommentID
}

// BuildCommentReplyInstructions formats this turn's reply target and file-based
// posting command. Resumed sessions must not reuse a previous --parent value.
//
// Write UTF-8 with --content-file so shell substitution, pipeline encoding, and
// heredoc boundaries cannot alter the body or swallow trailing flags. Cleanup
// must depend on a successful post. The template is shared across providers.
func BuildCommentReplyInstructions(provider, issueID, triggerCommentID string, squadLeader bool) string {
	if triggerCommentID == "" {
		return ""
	}
	return buildCommentReplyInstructionsSlim(provider, issueID, triggerCommentID, squadLeader)
}

// buildCommentReplyInstructionsSlim is the compressed reply-instructions
// block used by BuildCommentReplyInstructions. It was introduced in
// MUL-3560 as the slim alternative to a legacy verbose form; the
// `runtime_brief_slim` flag has since been retired (MUL-4297) and this is
// now the only form.
//
// The slim block identifies this turn's reply target and the file-based
// posting command, including cleanup only after success. The detailed
// shell-hazard rationale lives in the
// canonical `## Comment Formatting` section the same brief carries, so
// repeating it inline at every comment-triggered step 7 would be
// duplication, not signal.
func buildCommentReplyInstructionsSlim(provider, issueID, triggerCommentID string, squadLeader bool) string {
	// The squad leader's `no_action` exit (recorded via `squad activity`) is
	// the one path where posting no comment is correct — the imperative must
	// carry its own carve-out so a later line never contradicts the
	// no_action rule injected above it (MUL-5442 #6493 review).
	lead := "Follow the existing workspace instructions and access rules. Reply to the comment identified below."
	if squadLeader {
		lead = "Follow the existing workspace instructions and access rules. Unless your outcome is `no_action`, reply to the comment identified below."
	}
	// Cleanup is GATED on the post succeeding, never a separate statement.
	// Agents hand the whole snippet to one shell call, and an unconditional
	// cleanup line runs — and SUCCEEDS — after a failed post, so the call's
	// exit status becomes the cleanup's 0. Under `--output table` a failed
	// and a successful post both write nothing to stdout, which leaves the
	// exit status as the only machine-checkable signal; masking it lets an
	// agent end its turn believing an unposted result was delivered. The
	// gate also keeps ./reply.md on disk after a failure, so the retry does
	// not have to regenerate the body.
	if runtimeGOOS == "windows" {
		// The daemon knows the host OS, not the shell behind the agent's
		// command tool: Codex runs PowerShell on Windows, Claude Code runs
		// Git Bash for its Bash tool and (by default on claude.ai accounts)
		// a PowerShell tool the user can turn off with
		// CLAUDE_CODE_USE_POWERSHELL_TOOL=0 (#8627). A PowerShell-only
		// cookbook pasted into Git Bash fails at the `if (` line AFTER the
		// post succeeded, so the agent sees a failure and may re-post. Offer
		// one variant per shell and let the agent pick.
		//
		// PowerShell 5.1 has no `&&` (it landed in PowerShell 7), so that
		// variant checks $LASTEXITCODE — which carries the exit code of the
		// last NATIVE command — and propagates it instead.
		return fmt.Sprintf(
			lead+"\n\n"+
				"Write the reply into a UTF-8 file and use `--content-file`; PowerShell stdin may lose non-ASCII text. See `## Comment Formatting` for details. Choose the commands for your shell:\n\n"+
				"PowerShell:\n\n"+
				"    multica issue comment add %[1]s --parent %[2]s --content-file ./reply.md --output table\n"+
				"    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }\n"+
				"    Remove-Item ./reply.md\n\n"+
				"Git Bash:\n\n"+
				"    multica issue comment add %[1]s --parent %[2]s --content-file ./reply.md --output table && rm ./reply.md\n\n"+
				"The file preserves actual newlines. Cleanup runs only after a successful post.\n",
			issueID, triggerCommentID,
		)
	}
	return fmt.Sprintf(
		lead+"\n\n"+
			"Write the reply into a UTF-8 file, then use:\n\n"+
			"    multica issue comment add %s --parent %s --content-file ./reply.md --output table && rm ./reply.md\n\n"+
			"The file preserves actual newlines. Cleanup runs only after a successful post.\n",
		issueID, triggerCommentID,
	)
}

// ThreadReplyTarget is one root-thread group a coalesced run must answer.
// ThreadID labels the conversation (its root comment id); ParentID is the exact
// `--parent` the agent must pass so its reply lands inside that thread.
type ThreadReplyTarget struct {
	ThreadID string
	ParentID string
}

// BuildMultiThreadCommentReplyInstructions is the reply fan-out block for a run
// whose coalesced comments span MORE THAN ONE root thread (MUL-4348). It
// deliberately overrides the general "post exactly one comment per run"
// guidance for this specific run: three unrelated questions raised in three
// separate threads must land as three in-thread answers, not one merged blob
// posted under a single thread (or as a stray root comment).
//
// The grouping is computed server-side, so same-thread follow-ups never reach
// here — they collapse to a single target upstream and take the ordinary
// single-parent path. That is why the agent is told, unconditionally, to post
// exactly one reply per listed thread and never more than one reply in the same
// thread: the "multiple @mentions in one thread" case is already consolidated
// before this instruction is emitted, so a per-thread fan-out cannot split it.
//
// The block carries only what is multi-thread-SPECIFIC: the fan-out
// announcement + override, the posting order, the per-thread `--parent`
// targets, and the one mechanical delta (a DISTINCT body file per thread).
// The posting mechanism itself — body file → `--content-file` → cleanup, the
// inline/`--content-stdin` bans, the `\n`-escape rule, and the OS-specific
// cleanup command — lives once in the brief's `## Comment Formatting`; this
// block used to restate all of it plus two example command pairs, triple-
// writing the same cookbook for ~1KB extra per multi-thread turn (MUL-5825).
// Dropping the embedded commands also removes the only OS-dependent text, so
// the block no longer branches on runtimeGOOS: `## Comment Formatting` keeps
// the OS split.
//
// Returns "" for fewer than two targets; callers keep the single-parent path.
func BuildMultiThreadCommentReplyInstructions(issueID string, targets []ThreadReplyTarget, squadLeader bool) string {
	if issueID == "" || len(targets) < 2 {
		return ""
	}

	targetLines := ""
	for i, tgt := range targets {
		targetLines += fmt.Sprintf("%d. thread %s → reply with `--parent %s`\n", i+1, tgt.ThreadID, tgt.ParentID)
	}

	// Same carve-out as the single-thread cookbook (MUL-5442 #6493 review):
	// the leader's no_action exit must not be contradicted by this later
	// imperative, and the scope sentence must govern the ENTIRE fan-out
	// block — every obligation below (the per-thread reply count, the
	// posting order, the per-thread file rule) sits under the "Otherwise" —
	// not just the first verb. Ordinary agents keep the unconditional form.
	lead := "Reply scope: %d distinct threads. Post one reply per thread"
	if squadLeader {
		lead = "Reply scope: %d distinct threads. For a `no_action` outcome, post no replies and exit via `multica squad activity` as your leader rules direct. Otherwise, post one reply per thread"
	}
	return fmt.Sprintf(
		lead+" — %d in total. Each thread receives its own reply, using the target listed for this run.\n\n"+
			"Reply targets, in posting order — oldest thread first, the newest (triggering) thread last:\n"+
			"%s\n"+
			"Follow `## Comment Formatting`, with a separate body file per thread (./reply-1.md, ./reply-2.md, …).\n",
		len(targets), len(targets), targetLines,
	)
}
