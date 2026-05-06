package daemon

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// BuildPrompt constructs the task prompt for an agent CLI.
// Keep this minimal — detailed instructions live in CLAUDE.md / AGENTS.md
// injected by execenv.InjectRuntimeConfig.
func BuildPrompt(task Task) string {
	if task.ChatSessionID != "" {
		return buildChatPrompt(task)
	}
	if task.TriggerCommentID != "" {
		return buildCommentPrompt(task)
	}
	if task.AutopilotRunID != "" {
		return buildAutopilotPrompt(task)
	}
	if task.QuickCreatePrompt != "" {
		return buildQuickCreatePrompt(task)
	}
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", task.IssueID)
	return b.String()
}

// buildQuickCreatePrompt constructs a prompt for quick-create tasks. The
// user typed a single natural-language sentence in the create-issue modal;
// the agent's job is to translate it into one `multica issue create` CLI
// invocation, using its judgment to decide whether fetching referenced URLs
// would produce a better issue. No issue exists yet, so the agent must NOT
// call `multica issue get` or attempt to comment — there's nothing to read
// or reply to.
func buildQuickCreatePrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a quick-create assistant for a Multica workspace.\n\n")
	b.WriteString("A user captured the following input via the quick-create modal. There is NO existing issue. Your job is to create a well-formed issue from this input with a single `multica issue create` command.\n\n")
	fmt.Fprintf(&b, "User input:\n> %s\n\n", task.QuickCreatePrompt)

	b.WriteString("Field rules:\n\n")

	// title
	b.WriteString("- **title**: required. A concise but semantically rich summary. If the input references external resources (PRs, issues, URLs), use your judgment on whether fetching the resource would produce a meaningfully better title — e.g. \"review PR #123\" → \"Review PR #123: Refactor auth module to OAuth2\". Strip filler words but preserve key semantic information.\n\n")

	// description — the core optimization
	b.WriteString("- **description**: The description will be the primary context for the executing agent. Your goal is **high fidelity** — the executing agent should understand the user's intent as well as if they had read the original input themselves.\n\n")
	b.WriteString("  **Structure the description as follows:**\n\n")
	b.WriteString("  1. **User request** — Faithfully restate what the user wants done, in their own terms. Preserve the user's phrasing, tone, and scope. Do NOT paraphrase into generic language (e.g. don't turn \"把这个按钮改成红色\" into \"UI improvement needed\"). Do NOT add implementation plans, acceptance criteria, design decisions, or constraints the user did not express.\n\n")
	b.WriteString("  2. **Context** (only if the input contains URLs, references, or image attachments) — Fetch referenced resources (PRs, issues, docs) and summarize the relevant factual content. Keep summaries to verifiable facts only (e.g. \"PR #45 changes the auth middleware to use JWT\" not \"this suggests we should also update the tests\"). Preserve any image URLs or markdown image references inline.\n\n")
	b.WriteString("  Omit the Context section entirely if there are no external references to enrich.\n\n")
	b.WriteString("  **Hard rules for description:**\n")
	b.WriteString("  - NEVER invent requirements, implementation details, acceptance criteria, or scope that the user did not express.\n")
	b.WriteString("  - NEVER reduce the user's multi-sentence input to a single vague sentence. If the user wrote three sentences, the description should carry at least that much information.\n")
	b.WriteString("  - Preserve specific names, identifiers, file paths, code snippets, and technical terms from the input verbatim.\n")
	b.WriteString("  - Never echo the title in the description.\n\n")

	// priority
	b.WriteString("- **priority**: one of `urgent`, `high`, `medium`, `low`, or omit. Map P0/P1 → urgent/high; \"asap\" → urgent. If unspecified, omit.\n\n")

	// assignee
	b.WriteString("- **assignee**:\n")
	b.WriteString("    - When the user names someone (\"assign to X\" / \"@X\"), call `multica workspace members --output json` (and `multica agent list --output json` if it might be an agent) and find the matching entity by display name. On a clean unambiguous match, prefer `--assignee-id <uuid>` using the `user_id` (member) or `id` (agent) from that JSON — UUID matching is exact and robust to name collisions in workspaces with overlapping names. `--assignee <name>` (fuzzy) is acceptable as a fallback when names are unambiguous. On no match or ambiguous match, do NOT pass either flag — instead append a final line to the description: `Unrecognized assignee: X`.\n")
	agentID := ""
	agentName := ""
	if task.Agent != nil {
		agentID = task.Agent.ID
		agentName = task.Agent.Name
	}
	if agentID != "" {
		fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to YOURSELF: pass `--assignee-id %q` (your agent UUID). The picker agent is the expected owner because the user opened quick-create with you selected — never leave the issue unassigned. Use the UUID flag, not `--assignee <name>`, so the assignment is unambiguous even when other agents share part of your name.\n\n", agentID)
	} else if agentName != "" {
		fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to YOURSELF: pass `--assignee %q`. The picker agent is the expected owner because the user opened quick-create with you selected — never leave the issue unassigned.\n\n", agentName)
	} else {
		b.WriteString("    - When the user did NOT name an assignee, default to YOURSELF (the picker agent): pass `--assignee-id <your agent UUID>` (preferred) or `--assignee <your agent name>`. Never leave the issue unassigned.\n\n")
	}

	// fields to omit
	b.WriteString("- **project**: omit. The platform will route the issue to the workspace default.\n")
	b.WriteString("- **status**: omit (defaults to `todo`).\n")
	b.WriteString("- **attachments**: do NOT pass `--attachment`. The flag only accepts LOCAL file paths. Any image URL in the user input is already markdown — keep it inline in `--description` instead.\n\n")

	// output format
	b.WriteString("Output format:\n")
	b.WriteString("- Run exactly one `multica issue create` invocation. Do not retry for any reason — even on non-zero exit. The issue may already exist; another attempt would create a duplicate.\n")
	b.WriteString("- After success, print exactly one line: `Created MUL-<n>: <title>` and exit. No commentary, no follow-up tool calls.\n")
	b.WriteString("- Do NOT call `multica issue get` or `multica issue comment add` — there is no issue to query or comment on.\n")
	b.WriteString("- On CLI error, exit with the error as the only output. The platform writes a failure notification automatically.\n")
	return b.String()
}

// buildCommentPrompt constructs a prompt for comment-triggered tasks.
// The triggering comment content is embedded directly so the agent cannot
// miss it, even when stale output files exist in a reused workdir.
// The reply instructions (including the current TriggerCommentID as --parent)
// are re-emitted on every turn so resumed sessions cannot carry forward a
// previous turn's --parent UUID.
func buildCommentPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	if task.TriggerCommentContent != "" {
		authorLabel := "A user"
		if task.TriggerAuthorType == "agent" {
			name := task.TriggerAuthorName
			if name == "" {
				name = "another agent"
			}
			authorLabel = fmt.Sprintf("Another agent (%s)", name)
		}
		fmt.Fprintf(&b, "[NEW COMMENT] %s just left a new comment. Focus on THIS comment — do not confuse it with previous ones:\n\n", authorLabel)
		fmt.Fprintf(&b, "> %s\n\n", task.TriggerCommentContent)
		if task.TriggerAuthorType == "agent" {
			b.WriteString("⚠️ The triggering comment was posted by another agent. Decide whether a reply is warranted. If you produced actual work this turn (investigated, fixed something, answered a real question), post the result as a normal reply — that is NOT a noise comment, and the standard rule that final results must be delivered via comment still applies. If the triggering comment was a pure acknowledgment, thanks, or sign-off AND you produced no work this turn, do NOT reply — and do NOT post a comment saying 'No reply needed' or similar. Simply exit with no output. Silence is the preferred way to end agent-to-agent threads. If you do reply, do not @mention the other agent as a sign-off (that re-triggers them and starts a loop).\n\n")
		}
	}
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then decide how to proceed.\n\n", task.IssueID)
	b.WriteString(execenv.BuildCommentReplyInstructions(task.IssueID, task.TriggerCommentID))
	return b.String()
}

// buildChatPrompt constructs a prompt for interactive chat tasks.
func buildChatPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a chat assistant for a Multica workspace.\n")
	b.WriteString("A user is chatting with you directly. Respond to their message.\n\n")
	fmt.Fprintf(&b, "User message:\n%s\n", task.ChatMessage)
	return b.String()
}

// buildAutopilotPrompt constructs a prompt for run_only autopilot tasks.
func buildAutopilotPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	b.WriteString("This task was triggered by an Autopilot in run-only mode. There is no assigned Multica issue for this run.\n\n")
	fmt.Fprintf(&b, "Autopilot run ID: %s\n", task.AutopilotRunID)
	if task.AutopilotID != "" {
		fmt.Fprintf(&b, "Autopilot ID: %s\n", task.AutopilotID)
	}
	if task.AutopilotTitle != "" {
		fmt.Fprintf(&b, "Autopilot title: %s\n", task.AutopilotTitle)
	}
	if task.AutopilotSource != "" {
		fmt.Fprintf(&b, "Trigger source: %s\n", task.AutopilotSource)
	}
	if strings.TrimSpace(string(task.AutopilotTriggerPayload)) != "" {
		fmt.Fprintf(&b, "Trigger payload:\n%s\n", strings.TrimSpace(string(task.AutopilotTriggerPayload)))
	}
	b.WriteString("\nAutopilot instructions:\n")
	if strings.TrimSpace(task.AutopilotDescription) != "" {
		b.WriteString(task.AutopilotDescription)
		b.WriteString("\n\n")
	} else if task.AutopilotTitle != "" {
		fmt.Fprintf(&b, "%s\n\n", task.AutopilotTitle)
	} else {
		b.WriteString("No additional autopilot instructions were provided. Inspect the autopilot configuration before proceeding.\n\n")
	}
	if task.AutopilotID != "" {
		fmt.Fprintf(&b, "Start by running `multica autopilot get %s --output json` if you need the full autopilot configuration, then complete the instructions above.\n", task.AutopilotID)
	} else {
		b.WriteString("Complete the instructions above.\n")
	}
	b.WriteString("Do not run `multica issue get`; this run does not have an issue ID.\n")
	return b.String()
}
