package execenv

// taskKind labels the dispatch path that the runtime brief should
// follow for a given TaskContextForEnv. Used by
// `buildMetaSkillContentSlim` (MUL-3560 brief; the `runtime_brief_slim`
// flag that once gated it against a legacy verbose brief was retired in
// MUL-4297, so this is now the only brief).
//
// Four kinds, mutually exclusive in practice. classifyTask documents the
// tiebreak rule that applies if a future caller accidentally violates the
// mutex.
type taskKind int

const (
	// kindIssue: this run operates on a real Multica issue. It deliberately
	// does NOT distinguish comment-triggered from assignment-triggered runs.
	//
	// Those were two kinds until MUL-5377. Splitting them made the rendered
	// brief — which Claude Code loads into messages[0], ahead of the entire
	// conversation — differ between the first (on-assign) run and every
	// later (comment) run on the same resumed session, which invalidated the
	// prompt cache for the whole history on every resume. Which trigger
	// fired THIS turn is per-turn state and now travels in the per-turn user
	// message (daemon.BuildPrompt), which is appended after the cached
	// prefix. See runtime_config_sections.go:writeWorkflowIssue.
	kindIssue taskKind = iota
	// kindAutopilotRunOnly: an autopilot fired in run-only mode (no
	// issue created or attached).
	kindAutopilotRunOnly
	// kindQuickCreate: one-shot "create an issue from a natural-language
	// prompt" task.
	kindQuickCreate
	// kindChat: interactive chat session, no issue.
	kindChat
)

// classifyTask maps a TaskContextForEnv to the single taskKind the slim
// brief should be assembled for. Precedence (documented for the tiebreak
// case, although the daemon never sets two specific-kind flags at once):
// chat → quick-create → autopilot run-only → issue.
//
// Deliberately does not read ctx.TriggerCommentID: the classification must
// not vary across runs of the same resumed session, or the brief's bytes
// change and the prompt cache is lost from messages[0] onward (MUL-5377).
func classifyTask(ctx TaskContextForEnv) taskKind {
	switch {
	case ctx.ChatSessionID != "":
		return kindChat
	case ctx.QuickCreatePrompt != "":
		return kindQuickCreate
	case ctx.AutopilotRunID != "":
		return kindAutopilotRunOnly
	default:
		return kindIssue
	}
}

// hasIssueContext returns true for the kinds that operate on a real Multica
// issue and therefore can read / pin issue-scoped state. The slim
// dispatcher gates these two sections on this predicate:
//
//   - Issue Metadata
//   - Sub-issue Creation
//
// Both are meaningless on the issue-less kinds (chat / quick-create /
// autopilot run-only) and would either render an empty body or steer the
// agent into a guaranteed-failed CLI call. Note this is a kind-based
// predicate, not a check on ctx.IssueID — kindIssue always carries an issue
// id by construction (the daemon refuses to dispatch it otherwise), and the
// other three kinds never do.
func (k taskKind) hasIssueContext() bool {
	return k == kindIssue
}
