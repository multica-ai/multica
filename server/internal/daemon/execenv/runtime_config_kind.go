package execenv

// taskKind labels the dispatch path that `buildMetaSkillContent` should follow
// for a given TaskContextForEnv. Today the brief contains several conditional
// sections gated by ad-hoc `ctx.ChatSessionID != ""` / `hasIssueContext` /
// `isAssignmentTriggered` checks scattered through `buildMetaSkillContent`.
// Centralising the classification into a single enum + helper gives every
// section a named axis to switch on (instead of re-deriving it from four
// pointers in each call site) and lets follow-up work apply strict per-kind
// gating without scattering more `if`s.
//
// This file is the structural prep for MUL-3560 PR 0.5 — see Eve's design
// reply on that issue:
//
//   - kind classification: this file
//   - per-section extraction + kind-driven dispatch in buildMetaSkillContent:
//     runtime_config.go / runtime_config_sections.go
//
// The follow-up PR (0.6) will start removing sections that a given kind does
// not need (e.g. Mentions / Comment Formatting / Issue Metadata / Sub-issue
// out of quick-create); this PR keeps brief output byte-for-byte identical to
// the pre-refactor builder for every existing fixture so the refactor risk is
// isolated from the content-gating risk.
type taskKind int

const (
	// kindCommentTriggered: a NEW comment on an issue triggered this run.
	// `ctx.TriggerCommentID != ""` AND none of the chat / quick-create /
	// autopilot fields are set. By far the most common kind.
	kindCommentTriggered taskKind = iota

	// kindAssignmentTriggered: an assignee was set / changed on an issue
	// and the daemon fired a fresh run for the new assignee. No trigger
	// comment, no chat / quick-create / autopilot context.
	kindAssignmentTriggered

	// kindAutopilotRunOnly: an autopilot fired in run-only mode (no issue
	// is created or attached to this run; `ctx.AutopilotRunID != ""`).
	kindAutopilotRunOnly

	// kindQuickCreate: one-shot "create an issue from a natural-language
	// prompt" task (`ctx.QuickCreatePrompt != ""`). There is no existing
	// issue; the agent runs `multica issue create` exactly once and exits.
	kindQuickCreate

	// kindChat: interactive chat session (`ctx.ChatSessionID != ""`); no
	// issue, no autopilot, no quick-create prompt.
	kindChat
)

// classifyTask maps a TaskContextForEnv to the single taskKind that the brief
// should be assembled for. The ordering of the checks is the established
// precedence rule from the pre-refactor `buildMetaSkillContent`:
//
//  1. chat wins (ChatSessionID is the most-specific flag and runtime owners
//     gate everything else off "not a chat" in the old code),
//  2. then quick-create,
//  3. then autopilot run-only,
//  4. then comment-triggered,
//  5. otherwise assignment-triggered.
//
// All five kinds are mutually exclusive at the call site that builds
// TaskContextForEnv — the daemon never sets two of ChatSessionID /
// QuickCreatePrompt / AutopilotRunID at once, and a comment trigger always
// implies an existing issue (TriggerCommentID is empty for on-assign). The
// precedence rule above is documented here only so a future caller that
// breaks the mutex by accident still falls into a deterministic kind instead
// of silently picking up the wrong workflow.
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
// issue and therefore can read / pin issue-scoped state (Issue Metadata,
// Sub-issue Creation, Project Context). Equivalent to the pre-refactor
// `ctx.ChatSessionID == "" && ctx.QuickCreatePrompt == "" && ctx.AutopilotRunID == ""`
// — extracted so the predicate has a name and every call site agrees on its
// meaning.
func (k taskKind) hasIssueContext() bool {
	switch k {
	case kindCommentTriggered, kindAssignmentTriggered:
		return true
	default:
		return false
	}
}
