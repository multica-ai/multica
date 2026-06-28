package execenv

// taskKind labels the dispatch path that the slim runtime brief should
// follow for a given TaskContextForEnv. Used only by
// `buildMetaSkillContentSlim` (MUL-3560 slim brief, flag-gated via
// `runtime_brief_slim`). The legacy `buildMetaSkillContent` body in
// runtime_config.go does not consult this type — it re-derives the same
// branches inline from raw ctx fields, the way it has shipped to
// production for the last two years.
//
// Five kinds, mutually exclusive in practice. classifyTask documents the
// tiebreak rule that applies if a future caller accidentally violates the
// mutex.
type taskKind int

const (
	// kindCommentTriggered: a NEW comment on an issue triggered this run.
	kindCommentTriggered taskKind = iota
	// kindAssignmentTriggered: an assignee was set / changed on an issue
	// and the daemon fired a fresh run for the new assignee.
	kindAssignmentTriggered
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
// chat → quick-create → autopilot run-only → comment-triggered →
// assignment-triggered.
func classifyTask(ctx TaskContextForEnv) taskKind {
	switch {
	case ctx.ChatSessionID != "":
		return kindChat
	case ctx.QuickCreatePrompt != "":
		return kindQuickCreate
	case ctx.AutopilotRunID != "":
		return kindAutopilotRunOnly
	case ctx.TriggerCommentID != "":
		return kindCommentTriggered
	default:
		return kindAssignmentTriggered
	}
}

// hasIssueContext returns true for the kinds that operate on a real Multica
// issue and therefore can read / pin issue-scoped state. The slim
// dispatcher gates these three sections on this predicate:
//
//   - Project Context
//   - Issue Metadata
//   - Sub-issue Creation
//
// All three are meaningless on the issue-less kinds (chat / quick-create /
// autopilot run-only) and would either render an empty body or steer the
// agent into a guaranteed-failed CLI call. Note this is a kind-based
// predicate, not a check on ctx.IssueID — comment- / assignment-triggered
// kinds always carry an issue id by construction (the daemon refuses to
// dispatch them otherwise), and the other three kinds never do.
func (k taskKind) hasIssueContext() bool {
	switch k {
	case kindCommentTriggered, kindAssignmentTriggered:
		return true
	default:
		return false
	}
}
