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
	// Channel-mention check goes FIRST: a channel-mention task has neither
	// IssueID nor ChatSessionID, but it shouldn't fall through to the
	// quick-create or default-issue branches. The hydrator on the server
	// only populates ChannelID when context.type == "channel_mention".
	if task.ChannelID != "" {
		return buildChannelMentionPrompt(task)
	}
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
	b.WriteString("    - When the user names someone (\"assign to X\" / \"@X\"), call `multica workspace members --output json` and find the matching member by display name (case-insensitive substring match is fine). On a clean match, pass `--assignee <name>`. On no match or ambiguous match, do NOT pass `--assignee` — instead append a final line to the description: `Unrecognized assignee: X`.\n")
	agentName := ""
	if task.Agent != nil {
		agentName = task.Agent.Name
	}
	if agentName != "" {
		fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to YOURSELF: pass `--assignee %q`. The picker agent is the expected owner because the user opened quick-create with you selected — never leave the issue unassigned.\n\n", agentName)
	} else {
		b.WriteString("    - When the user did NOT name an assignee, default to YOURSELF (the picker agent): pass `--assignee <your agent name>`. Never leave the issue unassigned.\n\n")
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

// buildChannelMentionPrompt constructs a prompt for tasks triggered by an
// @-mention in a channel message. The agent is acting as a conversational
// participant — there is NO issue, NO branch, NO commit; the agent's
// reply is posted back as a `channel_message` from its identity.
//
// The triggering message is embedded inline so the agent doesn't have to
// re-fetch it; recent message history is fetchable via
// `multica channel history` (CLI extension TBD; for now the agent has the
// triggering message and channel name only).
func buildChannelMentionPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a chat participant in a Multica workspace channel.\n\n")
	if task.ChannelName != "" {
		fmt.Fprintf(&b, "**Channel:** #%s\n", task.ChannelName)
	}
	if task.ChannelID != "" {
		fmt.Fprintf(&b, "**Channel ID:** `%s`\n", task.ChannelID)
	}
	if task.Agent != nil && task.Agent.Name != "" {
		fmt.Fprintf(&b, "**You are:** %s\n", task.Agent.Name)
	}
	if task.TriggerAuthorName != "" {
		who := task.TriggerAuthorName
		if task.TriggerAuthorType == "agent" {
			who += " (agent)"
		}
		fmt.Fprintf(&b, "**Mentioned by:** %s\n", who)
	}
	b.WriteString("\n")
	// Include recent channel history directly in the user prompt — not just
	// in CLAUDE.md — because resumed Claude Code sessions tend to trust
	// their conversation memory over re-read context files. With history in
	// the per-turn prompt the model sees a fresh snapshot it can't miss.
	if len(task.ChannelHistory) > 0 {
		b.WriteString("Recent channel history (oldest first; triggering message is below this section):\n\n")
		for _, m := range task.ChannelHistory {
			who := m.AuthorName
			if m.AuthorType == "agent" {
				who += " (agent)"
			}
			ts := m.CreatedAt
			if len(ts) >= 19 {
				ts = ts[:19] + "Z"
			}
			fmt.Fprintf(&b, "[%s] %s:\n", ts, who)
			b.WriteString("> ")
			b.WriteString(strings.ReplaceAll(m.Content, "\n", "\n> "))
			b.WriteString("\n\n")
		}
	}
	if task.ChannelMessageContent != "" {
		b.WriteString("Triggering message:\n\n> ")
		b.WriteString(strings.ReplaceAll(task.ChannelMessageContent, "\n", "\n> "))
		b.WriteString("\n\n")
	}
	b.WriteString("Respond conversationally as if you were a teammate in the channel. ")
	b.WriteString("Your reply will be posted as a single `channel_message` from your agent identity ")
	b.WriteString("once you finish — there is no separate `multica` command to call. Just produce ")
	b.WriteString("your reply as your final output and exit.\n\n")
	b.WriteString("Constraints:\n")
	b.WriteString("- This is NOT an issue task. Do not call `multica issue ...` commands.\n")
	b.WriteString("- Do not @-mention other agents in your reply unless absolutely necessary — repeated cross-agent mentions can produce notification storms.\n")
	b.WriteString("- Keep replies concise. Markdown (code blocks, lists, links) renders correctly in the channel.\n")
	b.WriteString("- If the request is ambiguous, ask one clarifying question rather than guessing.\n")
	b.WriteString("- When the user asks about earlier messages in this channel, treat the Recent channel history above as the source of truth — your conversation memory only covers prior @-mentions of you, not other people's chatter.\n")
	if task.ChannelID != "" {
		fmt.Fprintf(&b, "- If the user references a message older than the window above, run `multica channel history %s --before <oldest-timestamp-you-have> --output json` to fetch more.\n", task.ChannelID)
	}
	return b.String()
}
