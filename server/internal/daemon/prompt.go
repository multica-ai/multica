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
	fmt.Fprintf(&b, "If you need comment history, `multica issue comment list %s` returns the latest 50 by default — pass --limit or --since to scope older windows. Long issues can have thousands of comments; do not fetch everything blindly.\n", task.IssueID)
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
	b.WriteString("- **description**: The description is the executing agent's primary context. Aim for high fidelity — they should grasp the user's intent as if they had read the raw input themselves. Use a two-section structure:\n\n")
	b.WriteString("  1. **User request** — Faithfully restate what the user wants in their own words. Preserve specific names, identifiers, file paths, code snippets, and technical terms verbatim. Strip non-spec material before writing it (this is removal, not paraphrasing): verbal routing wrappers about creating the issue (e.g. \"create an issue\", \"分配给 X\") and pure conversational fillers (e.g. \"对吧？\"). When in doubt, keep it.\n\n")
	b.WriteString("     CC exception: `multica issue create` has no `--subscriber` flag, and the platform auto-subscribes members whose `[@Name](mention://member/<uuid>)` link appears in the description. When the user wrote \"cc @Y\", strip the verbal \"cc\" wrapper from the User request body and append a final `CC: <mention link(s)>` line to the description so the cc routing still fires.\n\n")
	b.WriteString("  2. **Context** — include ONLY when the input cited external resources AND you successfully fetched them AND they produced verifiable facts worth recording. Summarize facts only (e.g. \"PR #45 changes auth to JWT\"), not interpretation or unsolicited reference implementations. If you have nothing factual to add, omit the section entirely — never use it as an apology log for resources you could not fetch.\n\n")
	b.WriteString("  Hard rules: never invent requirements, implementation details, or acceptance criteria the user did not express; never reduce multi-sentence input to a single vague sentence; never echo the title.\n\n")

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
	// Three regimes for the "user didn't name an assignee" default:
	//
	//  - Workspace has peer agents: orchestrator-style pickers (Hermes,
	//    routing patterns, etc.) need to be allowed to delegate by name
	//    instead of forced to self-assign. The Agent Identity / Peer
	//    Agents sections in CLAUDE.md already tell the agent who exists
	//    and what their role is; we just stop the prompt from overriding
	//    the persona's routing logic. Coding-agent personas will still
	//    self-assign (their instructions tell them so); orchestrator
	//    personas will route. The decision lives in the persona.
	//
	//  - Single-agent workspace WITH agentID: the picker is the only
	//    agent; self-assign by canonical UUID so the assignment is
	//    unambiguous even if names overlap.
	//
	//  - Single-agent workspace, name only: legacy fallback.
	hasPeers := len(task.PeerAgents) > 0
	if agentName != "" && hasPeers {
		fmt.Fprintf(&b, "    - When the user did NOT name an assignee, decide based on the work AND your role described in the Agent Identity section. If your persona delegates this kind of task to a peer (peers are listed in the \"Peer Agents in this Workspace\" section above, with each peer's role one-liner), pass `--assignee \"<peer name>\"`. Otherwise keep it yourself: pass `--assignee %q`. Never leave the issue unassigned. Pick exactly one assignee.\n\n", agentName)
	} else if agentID != "" {
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
		switch {
		case task.IsOrchestratorWake:
			// The orchestrator pattern — this agent is configured as the
			// workspace's orchestrator and was woken because a peer agent
			// posted on this issue. Different default than the generic
			// agent-to-agent case: the orchestrator is SUPPOSED to react.
			// "Silence is the preferred way to end agent-to-agent threads"
			// would defeat the whole point.
			b.WriteString("**You are this workspace's orchestrator.** A peer agent just posted on an issue — review it, decide on the next step, and act. Common moves:\n\n")
			b.WriteString("- **Acknowledge + change status**: e.g. when a peer reports work complete, update the issue status (`multica issue status <id> in_review` or `done`) and post a brief acknowledgment.\n")
			b.WriteString("- **Reassign**: if the comment indicates the work belongs to a different agent (e.g. \"this needs the reviewer\"), `multica issue assign <id> --to <peer-name>` and post a one-liner explaining the handoff.\n")
			b.WriteString("- **Ping a human**: when a peer asks for clarification or flags a blocker that needs human input, @mention the appropriate workspace member in your reply so they get notified.\n")
			b.WriteString("- **Drive forward**: if the work is mid-flight and you have context the peer is missing, post a directive comment on the same thread.\n\n")
			b.WriteString("Do NOT do nothing. Do NOT post \"acknowledged\" with no follow-up — silence isn't your job here. If you genuinely have no action to take, post a one-liner explaining why (the user will read it).\n\n")
		case task.TriggerAuthorType == "agent":
			b.WriteString("⚠️ The triggering comment was posted by another agent. Decide whether a reply is warranted. If you produced actual work this turn (investigated, fixed something, answered a real question), post the result as a normal reply — that is NOT a noise comment, and the standard rule that final results must be delivered via comment still applies. If the triggering comment was a pure acknowledgment, thanks, or sign-off AND you produced no work this turn, do NOT reply — and do NOT post a comment saying 'No reply needed' or similar. Simply exit with no output. Silence is the preferred way to end agent-to-agent threads. If you do reply, do not @mention the other agent as a sign-off (that re-triggers them and starts a loop).\n\n")
		}
	}
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then decide how to proceed.\n\n", task.IssueID)
	fmt.Fprintf(&b, "If you need comment history, `multica issue comment list %s` returns the latest 50 by default — pass --limit or --since to scope older windows. Long issues can have thousands of comments; do not fetch everything blindly.\n\n", task.IssueID)
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
