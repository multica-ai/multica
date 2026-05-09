package daemon

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// BuildPrompt constructs the task prompt for an agent CLI.
// Keep this minimal — detailed instructions live in CLAUDE.md / AGENTS.md
// injected by execenv.InjectRuntimeConfig. The provider string is threaded
// through to comment-triggered tasks' per-turn reply template; that template
// is provider-agnostic now (Linux/macOS → quoted-HEREDOC stdin, Windows →
// file) because the shell-layer corruption it guards against is not specific
// to any one provider (MUL-2904).
func BuildPrompt(task Task, provider string) string {
	if task.ChatSessionID != "" {
		return buildChatPrompt(task)
	}
	if task.TriggerCommentID != "" {
		return buildCommentPrompt(task, provider)
	}
	if task.AutopilotRunID != "" {
		return buildAutopilotPrompt(task)
	}
	if task.QuickCreatePrompt != "" {
		return buildQuickCreatePrompt(task)
	}
	if task.ChannelMessageID != "" {
		return buildChannelMessagePrompt(task)
	}
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", task.IssueID)
	fmt.Fprintf(&b, "For comment history, follow the rule in your runtime workflow file (assignment-triggered tasks treat the read as mandatory). `multica issue comment list %s --output json` returns all comments for the issue (server caps at 2000). On long-running issues use `--recent 20 --output json` to read the 20 most recently active threads, then page older threads via the stderr `Next thread cursor: ...` line and the matching `--before` / `--before-id` until you have enough history. `--since <RFC3339>` is still available for incremental polling and may combine with `--recent`.\n", task.IssueID)
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
	b.WriteString("  1. **User request** — Faithfully restate what the user wants in their own words. Preserve specific names, identifiers, file paths, code snippets, and technical terms verbatim. Strip non-spec material before writing it (this is removal, not paraphrasing): verbal routing wrappers about creating the issue or routing it (e.g. \"create an issue\", \"分配给 X\", \"让 @X 处理\") and pure conversational fillers (e.g. \"对吧？\"). When in doubt, keep it.\n\n")
	b.WriteString("     CC exception: `multica issue create` has no `--subscriber` flag, and the platform auto-subscribes members whose `[@Name](mention://member/<uuid>)` link appears in the description. When the user wrote \"cc @Y\", strip the verbal \"cc\" wrapper from the User request body and append a final `CC: <mention link(s)>` line to the description so the cc routing still fires.\n\n")
	b.WriteString("  2. **Context** — include ONLY when the input cited external resources AND you successfully fetched them AND they produced verifiable facts worth recording. Summarize facts only (e.g. \"PR #45 changes auth to JWT\"), not interpretation or unsolicited reference implementations. If you have nothing factual to add, omit the section entirely — never use it as an apology log for resources you could not fetch.\n\n")
	b.WriteString("  Hard rules: never invent requirements, implementation details, or acceptance criteria the user did not express; never reduce multi-sentence input to a single vague sentence; never echo the title.\n\n")

	// priority
	b.WriteString("- **priority**: one of `urgent`, `high`, `medium`, `low`, or omit. Map P0/P1 → urgent/high; \"asap\" → urgent. If unspecified, omit.\n\n")

	// assignee
	b.WriteString("- **assignee**:\n")
	b.WriteString("    - When the user names someone (\"assign to X\" / \"@X\"), call `multica workspace member list --output json`, `multica agent list --output json`, and `multica squad list --output json` and find the matching entity by display name. Squads are first-class assignees too — a squad name (e.g. \"Super Human\") routes work to the squad leader, who then delegates. On a clean unambiguous match, prefer `--assignee-id <uuid>` using the `user_id` (member) or `id` (agent or squad) from that JSON — UUID matching is exact and robust to name collisions in workspaces with overlapping names. `--assignee <name>` (fuzzy) is acceptable as a fallback when names are unambiguous. On no match or ambiguous match, do NOT pass either flag — instead append a final line to the description: `Unrecognized assignee: X`.\n")
	b.WriteString("    - Treat bare @-routing as an assignee directive even when the user did not write the English word \"assign\". This includes Chinese imperatives like `让 @独立团 review 这个 PR`, `给 @X 处理`, or `交给 @X`; strip the leading `@`/`＠` before matching display names. Do not keep that routing wrapper or `@Name` in the description unless it is a true CC-style notification rather than ownership. If the matched entity is a squad, pass the squad's `id` as `--assignee-id`, not the leader agent's id.\n")
	agentID := ""
	agentName := ""
	if task.Agent != nil {
		agentID = task.Agent.ID
		agentName = task.Agent.Name
	}
	switch {
	case task.SquadID != "":
		// The user opened quick-create with a SQUAD selected. The task
		// runs on the squad's leader agent, but the squad is the expected
		// owner — assigning to the leader would mask the squad's
		// delegation flow. Always point the default at the squad UUID.
		if task.SquadName != "" {
			fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to the picker SQUAD %q: pass `--assignee-id %q` (the squad's UUID). The user opened quick-create with the squad selected; you (the leader agent) are running on the squad's behalf, so the squad — not you — is the expected owner. Never leave the issue unassigned, and do not assign it to your own agent UUID.\n\n", task.SquadName, task.SquadID)
		} else {
			fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to the picker SQUAD: pass `--assignee-id %q` (the squad's UUID). The user opened quick-create with the squad selected; you (the leader agent) are running on the squad's behalf, so the squad — not you — is the expected owner. Never leave the issue unassigned, and do not assign it to your own agent UUID.\n\n", task.SquadID)
		}
	case agentID != "":
		fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to YOURSELF: pass `--assignee-id %q` (your agent UUID). The picker agent is the expected owner because the user opened quick-create with you selected — never leave the issue unassigned. Use the UUID flag, not `--assignee <name>`, so the assignment is unambiguous even when other agents share part of your name.\n\n", agentID)
	case agentName != "":
		fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to YOURSELF: pass `--assignee %q`. The picker agent is the expected owner because the user opened quick-create with you selected — never leave the issue unassigned.\n\n", agentName)
	default:
		b.WriteString("    - When the user did NOT name an assignee, default to YOURSELF (the picker agent): pass `--assignee-id <your agent UUID>` (preferred) or `--assignee <your agent name>`. Never leave the issue unassigned.\n\n")
	}

	// project — pinned by the modal when the user picked one, otherwise
	// omitted so the platform routes to the workspace default. Always pass
	// the UUID (never a name) so the issue lands in the right project even
	// when several share a title.
	if task.ProjectID != "" {
		if task.ProjectTitle != "" {
			fmt.Fprintf(&b, "- **project**: required for this run. Pass `--project %q` so the new issue lands in project %q (the user picked it in the quick-create modal). Do not infer a different project from the prompt text — the modal selection is authoritative.\n", task.ProjectID, task.ProjectTitle)
		} else {
			fmt.Fprintf(&b, "- **project**: required for this run. Pass `--project %q` so the new issue lands in the project the user picked in the quick-create modal. Do not infer a different project from the prompt text — the modal selection is authoritative.\n", task.ProjectID)
		}
	} else {
		b.WriteString("- **project**: omit. The platform will route the issue to the workspace default.\n")
	}
	// parent — pinned by the modal when the user opened it from "Add sub
	// issue" on an existing issue. Pass the UUID (never the identifier) so
	// the create lands the sub-issue under the right parent even when the
	// workspace prefix changes; the identifier is included in the prose
	// purely as human-readable context for the agent.
	if task.ParentIssueID != "" {
		if task.ParentIssueIdentifier != "" {
			fmt.Fprintf(&b, "- **parent**: required for this run. Pass `--parent %q` so the new issue is filed as a sub-issue of %s (the user opened quick-create from that issue's \"Add sub issue\" entry). Do not infer a different parent from the prompt text — the modal entry point is authoritative.\n", task.ParentIssueID, task.ParentIssueIdentifier)
		} else {
			fmt.Fprintf(&b, "- **parent**: required for this run. Pass `--parent %q` so the new issue is filed as a sub-issue of the parent the user picked in the quick-create modal. Do not infer a different parent from the prompt text — the modal entry point is authoritative.\n", task.ParentIssueID)
		}
	}
	b.WriteString("- **status**: omit (defaults to `todo`).\n")
	b.WriteString("- **attachments**: do NOT pass `--attachment`. The flag only accepts LOCAL file paths. Any image URL in the user input is already markdown — keep it inline in `--description` instead.\n\n")

	// output format
	b.WriteString("Output format:\n")
	b.WriteString("- Run exactly one `multica issue create --output json` invocation. Do not retry for any reason — even on non-zero exit. The issue may already exist; another attempt would create a duplicate.\n")
	b.WriteString("- Parse the JSON response to read the created issue's `identifier` (preferred) or `id` (fallback). Do not scrape human output and do not assume any workspace issue prefix such as `MUL-`; workspaces can use custom prefixes.\n")
	b.WriteString("- After success, print exactly one line: `Created <identifier-or-id>: <title>` and exit. No commentary, no follow-up tool calls.\n")
	b.WriteString("- Do NOT call `multica issue get` or `multica issue comment add` — there is no issue to query or comment on.\n")
	b.WriteString("- On CLI error or JSON parse error, exit with the error as the only output. The platform writes a failure notification automatically.\n")
	return b.String()
}

// buildCommentPrompt constructs a prompt for comment-triggered tasks.
// The triggering comment content is embedded directly so the agent cannot
// miss it, even when stale output files exist in a reused workdir.
// The reply instructions (including the current TriggerCommentID as --parent)
// are re-emitted on every turn so resumed sessions cannot carry forward a
// previous turn's --parent UUID.
func buildCommentPrompt(task Task, provider string) string {
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
		if task.Agent != nil && strings.Contains(task.Agent.Instructions, "## Squad Operating Protocol") {
			fmt.Fprintf(&b, "⚠️ **Squad leader no_action rule:** If you decide no action is needed, call `multica squad activity %s no_action --reason \"...\"` and EXIT. DO NOT post any comment — not even one that says \"no action needed\" or \"exiting silently\". The squad activity call records your decision; a comment is redundant noise.\n\n", task.IssueID)
		}
	}
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then decide how to proceed.\n\n", task.IssueID)
	// Comment-reading pointer. Warm path with new comments: issue-wide
	// since-delta count, but steer the agent to read the triggering thread
	// first. Warm resumed path with no new comments: the trigger is already
	// injected, so don't force a duplicate thread read. Cold path: read the
	// triggering thread, not the flat timeline. Final fallback (no trigger id,
	// shouldn't happen here): plain read.
	if hint := execenv.BuildNewCommentsHint(task.IssueID, task.TriggerCommentID, task.TriggerThreadID, task.NewCommentsSince, task.NewCommentCount); hint != "" {
		b.WriteString(hint)
	} else if task.PriorSessionID != "" {
		b.WriteString(execenv.BuildResumedCommentsHint(task.IssueID, task.TriggerCommentID, task.TriggerThreadID))
	} else if cold := execenv.BuildColdCommentsHint(task.IssueID, task.TriggerCommentID, task.TriggerThreadID); cold != "" {
		b.WriteString(cold)
	} else {
		fmt.Fprintf(&b, "Read the discussion: `multica issue comment list %s --output json` (long issue? use `--recent 20`).\n\n", task.IssueID)
	}
	b.WriteString(execenv.BuildCommentReplyInstructions(provider, task.IssueID, task.TriggerCommentID))
	return b.String()
}

// buildChatPrompt constructs a prompt for interactive chat tasks.
func buildChatPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a chat assistant for a Multica workspace.\n")
	b.WriteString("A user is chatting with you directly. Respond to their message.\n\n")
	if task.Agent != nil && len(task.Agent.Skills) > 0 {
		refs := ExtractSlashSkills(task.ChatMessage)
		if len(refs) > 0 {
			agentSkills := make(map[string]string, len(task.Agent.Skills))
			for _, s := range task.Agent.Skills {
				agentSkills[s.ID] = s.Name
			}

			selected := make([]string, 0, len(refs))
			seen := make(map[string]struct{}, len(refs))
			for _, ref := range refs {
				name, ok := agentSkills[ref.ID]
				if !ok {
					continue
				}
				if _, ok := seen[ref.ID]; ok {
					continue
				}
				seen[ref.ID] = struct{}{}
				selected = append(selected, name)
			}

			if len(selected) > 0 {
				b.WriteString("Explicitly selected skills:\n")
				for _, name := range selected {
					fmt.Fprintf(&b, "- %s\n", name)
				}
				b.WriteString("\n")
			}
		}
	}
	fmt.Fprintf(&b, "User message:\n%s\n", task.ChatMessage)
	// List attachments by id + filename so the agent can fetch them via
	// the CLI. We deliberately do NOT inline the URL: chat attachments
	// live behind a signed CDN with a short TTL, so by the time the agent
	// has finished thinking the URL embedded in the markdown body may
	// have expired. `multica attachment download <id>` re-signs at click
	// time and is the only reliable path.
	if len(task.ChatMessageAttachments) > 0 {
		b.WriteString("\nAttachments on this message:\n")
		for _, a := range task.ChatMessageAttachments {
			if a.ContentType != "" {
				fmt.Fprintf(&b, "- id=%s filename=%q content_type=%s\n", a.ID, a.Filename, a.ContentType)
			} else {
				fmt.Fprintf(&b, "- id=%s filename=%q\n", a.ID, a.Filename)
			}
		}
		b.WriteString("Use `multica attachment download <id>` to fetch each file locally before referring to it.\n")
	}
	return b.String()
}

// buildChannelMessagePrompt constructs a prompt for channel message tasks.
// The server pre-injects recent channel messages (ChannelRecentMessages) so the
// agent can understand the conversation without an extra API call. The prompt
// renders those messages inline and tells the agent to reply via the CLI.
func buildChannelMessagePrompt(task Task) string {
	var b strings.Builder

	// ── Identity block ────────────────────────────────────────────────────
	// Always tell the agent who it is. Without this, references like
	// "assign to yourself" or "you just created X" are ambiguous.
	agentName := ""
	agentID := task.AgentID
	if task.Agent != nil {
		agentName = task.Agent.Name
		if task.Agent.Instructions != "" {
			fmt.Fprintf(&b, "%s\n\n", task.Agent.Instructions)
		}
	}
	b.WriteString("You are participating in a team channel discussion.\n\n")
	b.WriteString("## Your identity\n\n")
	if agentName != "" {
		fmt.Fprintf(&b, "- Your name: **%s**\n", agentName)
	}
	if agentID != "" {
		fmt.Fprintf(&b, "- Your agent ID (use as `--assignee-id` when assigning to yourself): `%s`\n", agentID)
	}
	b.WriteString("\n")

	// ── Conversation history ──────────────────────────────────────────────
	// The server has already fetched the recent messages; we render them
	// here so the agent does not need to call `multica channel messages`.
	// Messages sent by YOU are marked "(you, previously)" — treat them as
	// your own prior actions and build on them rather than re-doing them.
	if len(task.ChannelRecentMessages) > 0 {
		b.WriteString("## Recent conversation\n\n")
		for _, m := range task.ChannelRecentMessages {
			ts := ""
			if m.CreatedAt != "" {
				ts = " [" + m.CreatedAt + "]"
			}
			fmt.Fprintf(&b, "**%s**%s:\n> %s\n\n", m.AuthorName, ts, m.Content)
		}
	} else {
		// Fallback: no messages were pre-loaded (e.g. channel was empty).
		b.WriteString("(No previous messages in this channel.)\n\n")
	}

	// ── Triggering message (highlighted) ─────────────────────────────────
	authorLabel := "A user"
	if task.ChannelAuthorName != "" {
		authorLabel = task.ChannelAuthorName
	}
	if task.ChannelContent != "" {
		fmt.Fprintf(&b, "## New message you must respond to\n\n**%s** sent:\n\n> %s\n\n", authorLabel, task.ChannelContent)
	}

	// ── Channel mode constraint ───────────────────────────────────────────
	// Channels are for EXPLORATION and DISCUSSION only.
	// Substantive execution (writing code, producing PRDs, reviewing PRs,
	// running tests, making architectural decisions) MUST NOT happen here.
	b.WriteString("## Channel rules — READ CAREFULLY\n\n")
	b.WriteString("This channel is for **exploration and discussion only**. Your role here is:\n\n")
	b.WriteString("✅ **Allowed in channel:**\n")
	b.WriteString("- Answer questions, explain concepts, share knowledge\n")
	b.WriteString("- Search for and summarise existing information (`multica issue list`, `multica issue get`, etc.)\n")
	b.WriteString("- Discuss approaches, trade-offs, and plans\n")
	b.WriteString("- Create issues and assign them to the appropriate agent for execution\n")
	b.WriteString("- Report back what an issue produced (fetch the issue/comments and summarise)\n\n")
	b.WriteString("🚫 **NEVER do the following directly in a channel task:**\n")
	b.WriteString("- Write, edit, or review code\n")
	b.WriteString("- Author or refine a PRD / spec / design document\n")
	b.WriteString("- Make architectural decisions or produce deliverables\n")
	b.WriteString("- Run tests or execute long-running processes\n\n")
	b.WriteString("**If the request requires any of the above:**\n")
	b.WriteString("1. STOP — do not attempt the work yourself.\n")
	b.WriteString("2. Create an issue with `multica issue create` describing the task clearly.\n")
	b.WriteString("3. Assign the issue to the agent best suited for the job using `--assignee <agent-name>` or `--assignee-id <uuid>`.\n")
	b.WriteString("4. Post a channel message confirming the issue was created (include the issue number/ID).\n")
	b.WriteString("5. If the user later asks for the result, fetch the issue and its comments with `multica issue get <id> --output json` and `multica issue comment list <id>`, then summarise what was produced.\n\n")

	// ── Instructions ─────────────────────────────────────────────────────
	b.WriteString("## Steps for this turn\n\n")
	b.WriteString("1. Read the conversation carefully, including your own previous messages (marked \"you, previously\").\n")
	b.WriteString("2. Decide: is this a question/exploration request, or a request to DO something substantive?\n")
	b.WriteString("   - **Question/exploration** → answer directly, search if needed, reply with findings.\n")
	b.WriteString("   - **Substantive task** → create an issue, assign to the right agent, confirm in channel.\n")
	fmt.Fprintf(&b, "3. Post your reply with: `multica channel send %s --content \"<your response>\"`\n\n", task.ChannelID)
	b.WriteString("Additional rules:\n")
	b.WriteString("- When the user says \"assign to yourself\" or similar, use YOUR agent ID above with `--assignee-id`.\n")
	b.WriteString("- Reuse IDs from the conversation history instead of re-fetching when you already know them.\n")
	b.WriteString("- Keep replies concise. If you created or updated something, confirm with the issue number.\n")
	b.WriteString("- If you have nothing meaningful to add, exit without sending.\n")
	b.WriteString("- For more channel history: `multica channel messages " + task.ChannelID + " --limit 50 --output json`\n")
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
