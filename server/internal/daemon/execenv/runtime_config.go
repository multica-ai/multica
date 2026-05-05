package execenv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// primaryProjectRepoURL returns the URL of the first `github_repo` resource
// in the list — by convention, the project's primary / default repo. The
// daemon receives resources already ordered by the server's `position ASC,
// created_at ASC`, so "first" is deterministic. Returns "" when the project
// has no github_repo resources (e.g. it only attaches docs or notion pages).
func primaryProjectRepoURL(resources []ProjectResourceForEnv) string {
	for _, r := range resources {
		if r.ResourceType != "github_repo" {
			continue
		}
		var payload struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(r.ResourceRef, &payload); err != nil {
			continue
		}
		if payload.URL != "" {
			return payload.URL
		}
	}
	return ""
}

// formatMemoryKindLabel renders a memory_artifact.kind as a short, human-
// readable badge for the meta-skill heading. Unknown kinds fall back to the
// raw string so future kinds (added on the server side) are still legible
// without a daemon update.
func formatMemoryKindLabel(kind string) string {
	switch kind {
	case "wiki_page":
		return "Wiki"
	case "agent_note":
		return "Agent note"
	case "runbook":
		return "Runbook"
	case "decision":
		return "Decision"
	default:
		return kind
	}
}

// formatProjectResource renders a single resource as a human-readable bullet.
// Unknown resource types fall back to a JSON-encoded ref so the agent can
// still read what the user attached. New resource types should add a case
// here AND in the API validator (handler/project_resource.go).
func formatProjectResource(r ProjectResourceForEnv) string {
	label := r.Label
	switch r.ResourceType {
	case "github_repo":
		var payload struct {
			URL               string `json:"url"`
			DefaultBranchHint string `json:"default_branch_hint,omitempty"`
		}
		_ = json.Unmarshal(r.ResourceRef, &payload)
		out := fmt.Sprintf("**GitHub repo**: %s", payload.URL)
		if payload.DefaultBranchHint != "" {
			out += fmt.Sprintf(" (default branch: `%s`)", payload.DefaultBranchHint)
		}
		if label != "" {
			out += " — " + label
		}
		return out
	default:
		ref := string(r.ResourceRef)
		if ref == "" {
			ref = "{}"
		}
		out := fmt.Sprintf("**%s**: `%s`", r.ResourceType, ref)
		if label != "" {
			out += " — " + label
		}
		return out
	}
}

// InjectRuntimeConfig writes the meta skill content into the runtime-specific
// config file so the agent discovers its environment through its native mechanism.
//
// For Claude:   writes {workDir}/CLAUDE.md  (skills discovered natively from .claude/skills/)
// For Codex:    writes {workDir}/AGENTS.md  (skills discovered natively via CODEX_HOME)
// For Copilot:  writes {workDir}/AGENTS.md  (skills discovered natively from .github/skills/)
// For OpenCode: writes {workDir}/AGENTS.md  (skills discovered natively from .config/opencode/skills/)
// For OpenClaw: writes {workDir}/AGENTS.md  (skills discovered natively from .openclaw/skills/)
// For Hermes:   writes {workDir}/AGENTS.md  (skills fall back to .agent_context/skills/; AGENTS.md points there)
// For Gemini:   writes {workDir}/GEMINI.md  (discovered natively by the Gemini CLI)
// For Pi:       writes {workDir}/AGENTS.md  (skills discovered natively from .pi/skills/)
// For Cursor:   writes {workDir}/AGENTS.md  (skills discovered natively from .cursor/skills/)
// For Kimi:     writes {workDir}/AGENTS.md  (Kimi Code CLI reads AGENTS.md natively; skills auto-discovered from project skills dirs)
// For Kiro:     writes {workDir}/AGENTS.md  (Kiro CLI reads AGENTS.md natively; skills auto-discovered from project skills dirs)
func InjectRuntimeConfig(workDir, provider string, ctx TaskContextForEnv) error {
	content := buildMetaSkillContent(provider, ctx)

	switch provider {
	case "claude":
		return os.WriteFile(filepath.Join(workDir, "CLAUDE.md"), []byte(content), 0o644)
	case "codex", "copilot", "opencode", "openclaw", "hermes", "pi", "cursor", "kimi", "kiro":
		return os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte(content), 0o644)
	case "gemini":
		return os.WriteFile(filepath.Join(workDir, "GEMINI.md"), []byte(content), 0o644)
	default:
		// Unknown provider — skip config injection, prompt-only mode.
		return nil
	}
}

// buildMetaSkillContent generates the meta skill markdown that teaches the agent
// about the Multica runtime environment and available CLI tools.
func buildMetaSkillContent(provider string, ctx TaskContextForEnv) string {
	var b strings.Builder

	b.WriteString("# Multica Agent Runtime\n\n")
	b.WriteString("You are a coding agent in the Multica platform. Use the `multica` CLI to interact with the platform.\n\n")

	// Always emit agent identity so the agent knows who it is, even when
	// dispatched via @mention on an issue assigned to a different agent.
	if ctx.AgentName != "" || ctx.AgentID != "" {
		b.WriteString("## Agent Identity\n\n")
		if ctx.AgentName != "" {
			fmt.Fprintf(&b, "**You are: %s**", ctx.AgentName)
			if ctx.AgentID != "" {
				fmt.Fprintf(&b, " (ID: `%s`)", ctx.AgentID)
			}
			b.WriteString("\n\n")
		}
		if ctx.AgentInstructions != "" {
			b.WriteString(ctx.AgentInstructions)
			b.WriteString("\n\n")
		}
	} else if ctx.AgentInstructions != "" {
		b.WriteString("## Agent Identity\n\n")
		b.WriteString(ctx.AgentInstructions)
		b.WriteString("\n\n")
	}

	// Inject peer agents — every other non-archived agent in the workspace.
	// Orchestrator-style agents (the picker, Hermes, etc.) need to know who
	// the other agents are so they can route work by name (`multica issue
	// assign --to <name>` or `--assignee <name>` on create) instead of self-
	// assigning because they don't realise other agents exist. The block is
	// agent-name-agnostic — it lists peers as data, never hardcodes which
	// peer to pick; the agent's own instructions decide the routing policy.
	if len(ctx.PeerAgents) > 0 {
		b.WriteString("## Peer Agents in this Workspace\n\n")
		b.WriteString("Other agents are available in this workspace. When delegating work (creating issues for others, reassigning, picking an assignee), refer to them by name. Do not assume you are the only agent — if a task is outside your role, route it to a peer instead of self-assigning.\n\n")
		for _, p := range ctx.PeerAgents {
			fmt.Fprintf(&b, "- **%s** (id: `%s`)", p.Name, p.ID)
			if trimmed := strings.TrimSpace(p.Instructions); trimmed != "" {
				// First non-empty line of the peer's instructions —
				// usually their role/persona one-liner.
				if idx := strings.IndexByte(trimmed, '\n'); idx > 0 {
					trimmed = trimmed[:idx]
				}
				fmt.Fprintf(&b, " — %s", trimmed)
			}
			b.WriteString("\n")
		}
		b.WriteString("\nUse `multica issue assign <issue-id> --to <name>` to reassign, or `--assignee <name>` on `multica issue create` to dispatch a new issue to a peer.\n\n")
	}

	b.WriteString("## Available Commands\n\n")
	b.WriteString("**Always use `--output json` for all read commands** to get structured data with full IDs.\n\n")
	b.WriteString("### Read\n")
	b.WriteString("- `multica issue get <id> --output json` — Get full issue details (title, description, status, priority, assignee)\n")
	b.WriteString("- `multica issue list [--status X] [--priority X] [--assignee X] [--limit N] [--offset N] --output json` — List issues in workspace (default limit: 50; JSON output includes `total`, `has_more` — use offset to paginate when `has_more` is true)\n")
	b.WriteString("- `multica issue comment list <issue-id> [--limit N] [--offset N] [--since <RFC3339>] --output json` — List comments on an issue (supports pagination; includes id, parent_id for threading)\n")
	b.WriteString("- `multica issue label list <issue-id> --output json` — List labels currently attached to an issue\n")
	b.WriteString("- `multica issue subscriber list <issue-id> --output json` — List members/agents subscribed to an issue\n")
	b.WriteString("- `multica label list --output json` — List all labels defined in the workspace (returns id + name + color)\n")
	b.WriteString("- `multica workspace get --output json` — Get workspace details and context\n")
	b.WriteString("- `multica workspace members [workspace-id] --output json` — List workspace members (user IDs, names, roles)\n")
	b.WriteString("- `multica agent list --output json` — List agents in workspace\n")
	b.WriteString("- `multica repo checkout <url> [--ref <branch-or-sha>]` — Check out a repository into the working directory (creates a git worktree with a dedicated branch; use `--ref` for review/QA on a specific branch, tag, or commit)\n")
	b.WriteString("- `multica issue runs <issue-id> --output json` — List all execution runs for an issue (status, timestamps, errors)\n")
	b.WriteString("- `multica issue run-messages <task-id> [--since <seq>] --output json` — List messages for a specific execution run (supports incremental fetch)\n")
	b.WriteString("- `multica attachment download <id> [-o <dir>]` — Download an attachment file locally by ID\n")
	b.WriteString("- `multica autopilot list [--status X] --output json` — List autopilots (scheduled/triggered agent automations) in the workspace\n")
	b.WriteString("- `multica autopilot get <id> --output json` — Get autopilot details including triggers\n")
	b.WriteString("- `multica autopilot runs <id> [--limit N] --output json` — List execution history for an autopilot\n")
	b.WriteString("- `multica memory list [--kind X] [--limit N] --output json` — List memory artifacts (wiki/runbook/decision/agent_note) in the workspace. The runtime context above already includes artifacts anchored to the current task; use this to browse the wider library.\n")
	b.WriteString("- `multica memory get <id> --output json` — Get a memory artifact's full content. Use the IDs from the `## Memory` section above to fetch updated copies if you suspect the embedded content is stale.\n")
	b.WriteString("- `multica memory search --q \"...\" [--kind X] --output json` — Full-text search memory artifacts via tsvector. Use this to find runbooks/decisions on a topic before duplicating one.\n")
	b.WriteString("- `multica memory by-anchor <type> <id> --output json` — List artifacts anchored to a specific entity (e.g. `multica memory by-anchor issue <issue-id>`). Anchor types: issue | project | agent | channel.\n\n")

	b.WriteString("### Write\n")
	b.WriteString("- `multica issue create --title \"...\" [--description \"...\"] [--priority X] [--status X] [--assignee X] [--parent <issue-id>] [--project <project-id>] [--due-date <RFC3339>] [--attachment <path>]` — Create a new issue. `--attachment` may be repeated to upload multiple files; labels and subscribers are not accepted here, attach them after create with the commands below.\n")
	b.WriteString("- `multica issue update <id> [--title X] [--description X] [--priority X] [--status X] [--assignee X] [--parent <issue-id>] [--project <project-id>] [--due-date <RFC3339>]` — Update one or more issue fields in a single call. Use `--parent \"\"` to clear the parent.\n")
	b.WriteString("- `multica issue status <id> <status>` — Shortcut for `issue update --status` when you only need to flip status (todo, in_progress, in_review, done, blocked, backlog, cancelled)\n")
	b.WriteString("- `multica issue assign <id> --to <name>` — Assign an issue to a member or agent by name (use `--unassign` to remove assignee)\n")
	b.WriteString("- `multica issue label add <issue-id> <label-id>` — Attach a label to an issue (look up the label id via `multica label list`)\n")
	b.WriteString("- `multica issue label remove <issue-id> <label-id>` — Detach a label from an issue\n")
	b.WriteString("- `multica issue subscriber add <issue-id> [--user <name>]` — Subscribe a member or agent to issue updates (defaults to the caller when `--user` is omitted)\n")
	b.WriteString("- `multica issue subscriber remove <issue-id> [--user <name>]` — Unsubscribe a member or agent\n")
	b.WriteString("- `multica issue comment add <issue-id> --content-stdin [--parent <comment-id>] [--attachment <path>]` — Post a comment. Agent-authored comments should always pipe content via stdin, even for short single-line replies. Use `--parent` to reply to a specific comment; `--attachment` may be repeated.\n")
	b.WriteString("  - **For comment content, you MUST pipe via stdin; this is mandatory for multi-line content (anything with line breaks, paragraphs, code blocks, backticks, or quotes).** Do not use inline `--content` and do not write `\\n` escapes. Use a HEREDOC instead:\n")
	b.WriteString("\n")
	b.WriteString("    ```\n")
	b.WriteString("    cat <<'COMMENT' | multica issue comment add <issue-id> --content-stdin\n")
	b.WriteString("    First paragraph.\n")
	b.WriteString("\n")
	b.WriteString("    Second paragraph with `code` and \"quotes\".\n")
	b.WriteString("    COMMENT\n")
	b.WriteString("    ```\n")
	b.WriteString("\n")
	b.WriteString("  - The same rule applies to `--description` on `multica issue create` and `multica issue update` — use `--description-stdin` and pipe a HEREDOC for any multi-line description; the inline `--description \"...\"` form is for short single-line text only.\n")
	b.WriteString("- `multica issue comment delete <comment-id>` — Delete a comment\n")
	b.WriteString("- `multica label create --name \"...\" --color \"#hex\"` — Define a new workspace label (use this only when the label you need does not exist yet; reuse existing labels via `multica label list` first)\n")
	b.WriteString("- `multica autopilot create --title \"...\" --agent <name> --mode create_issue [--description \"...\"]` — Create an autopilot\n")
	b.WriteString("- `multica autopilot update <id> [--title X] [--description X] [--status active|paused]` — Update an autopilot\n")
	b.WriteString("- `multica autopilot trigger <id>` — Manually trigger an autopilot to run once\n")
	b.WriteString("- `multica autopilot delete <id>` — Delete an autopilot\n")
	b.WriteString("- `multica memory create --kind <kind> --title \"...\" --content-file - [--anchor type:id] [--tags a,b,c] [--always-inject]` — Create a memory artifact. Use `--content-file -` to pipe markdown via stdin (HEREDOC pattern as for comments). Anchor it to the current issue/project/channel — or to your own agent id — so the next agent on a similar task picks it up via runtime injection. Use `--always-inject` only for workspace-wide rules (e.g. \"How we deploy\", \"Brand voice\") that every agent should see; the runtime caps these to 5 per workspace. Recommended kinds: `agent_note` for findings during work; `runbook` for procedures; `decision` for architectural choices; `wiki_page` for general knowledge.\n")
	b.WriteString("- `multica memory update <id> [--title X] [--content-file -] [--tags ...] [--anchor type:id|none] [--always-inject=true|false]` — Partial update; only fields you pass are changed. Pass `--anchor none` to clear the anchor; `--always-inject=false` to remove from always-on.\n")
	b.WriteString("- `multica memory archive <id>` — Soft-delete (reversible via `restore`). Prefer over `delete` so the history isn't lost.\n")
	b.WriteString("- `multica memory restore <id>` — Restore an archived artifact.\n")
	b.WriteString("- `multica memory delete <id>` — Hard-delete (irreversible — prefer `archive`).\n\n")
	b.WriteString("**When to write memory:** if you discover something non-obvious during a task — a footgun, a constraint, a workaround — write a short `agent_note` artifact anchored to the issue or project (or to your own agent id for cross-task notes). The next agent dispatched to similar work will read it through the `## Memory` section automatically. Keep notes terse — one or two paragraphs, focused on the surprise.\n\n")

	if provider == "codex" {
		b.WriteString("## Codex-Specific Comment Formatting\n\n")
		b.WriteString("Codex often follows the per-turn reply command literally. For issue comments, always use `--content-stdin` with a HEREDOC, even for short single-line replies. ")
		b.WriteString("Never use inline `--content` for agent-authored comments. Keep the same `--parent` value from the trigger comment when replying. ")
		b.WriteString("Do not compress a multi-paragraph answer into one line and do not rely on `\\n` escapes.\n\n")
	}

	// Inject available repositories section.
	if len(ctx.Repos) > 0 {
		b.WriteString("## Repositories\n\n")
		b.WriteString("The following code repositories are available in this workspace.\n")
		b.WriteString("Use `multica repo checkout <url>` to check out a repository into your working directory. Add `--ref <branch-or-sha>` when you need an exact branch, tag, or commit.\n\n")
		for _, repo := range ctx.Repos {
			fmt.Fprintf(&b, "- %s\n", repo.URL)
		}
		b.WriteString("\nThe checkout command creates a git worktree with a dedicated branch. You can check out one or more repos as needed, and can pass `--ref` for review/QA on a non-default branch or commit.\n\n")
	}

	// Inject project-scoped context (resources attached to the issue's project).
	// The full structured payload is also available at .multica/project/resources.json
	// so skills can consume it programmatically.
	if ctx.ProjectID != "" || len(ctx.ProjectResources) > 0 {
		b.WriteString("## Project Context\n\n")
		if ctx.ProjectTitle != "" {
			fmt.Fprintf(&b, "This issue belongs to **%s**.\n\n", ctx.ProjectTitle)
		}
		if len(ctx.ProjectResources) > 0 {
			// The first `github_repo` resource (lowest position) is the project's
			// primary / default repo. Surface it explicitly so the agent has a
			// deterministic answer to "which repo do I work in?" instead of having
			// to guess from the issue description. Falls back to no callout when
			// the project has no github_repo resources at all (e.g. only docs or
			// notion pages attached).
			if primaryRepo := primaryProjectRepoURL(ctx.ProjectResources); primaryRepo != "" {
				fmt.Fprintf(&b, "**Primary repo:** %s — when an issue under this project doesn't say otherwise, work in this repo. Use `multica repo checkout %s` to fetch it.\n\n", primaryRepo, primaryRepo)
			}
			b.WriteString("Project resources (also written to `.multica/project/resources.json`):\n\n")
			for _, r := range ctx.ProjectResources {
				fmt.Fprintf(&b, "- %s\n", formatProjectResource(r))
			}
			b.WriteString("\nResources are pointers — open them only when relevant to the task. ")
			b.WriteString("For `github_repo` resources, use `multica repo checkout <url>` to fetch the code. Add `--ref <branch-or-sha>` when a task or handoff names an exact revision.\n\n")
		} else {
			b.WriteString("This project has no resources attached yet.\n\n")
		}
	}

	// Inject memory artifacts anchored to this issue (and its parent project,
	// if any). The server-side claim handler caps the count and ordering so
	// the renderer here is purely presentation: group by anchor_type so the
	// agent can tell "about THIS issue" vs "about the surrounding project."
	//
	// Title/content render verbatim — the artifacts ARE the content the user
	// or another agent curated, so we don't paraphrase or compress. Each
	// artifact gets a stable ID footer so the agent can reference it back
	// (and we'll wire `multica memory get <id>` in a follow-up so the agent
	// can pull updated content if it suspects staleness).
	if len(ctx.MemoryArtifacts) > 0 {
		b.WriteString("## Memory\n\n")
		b.WriteString("Workspace knowledge anchored to this task. ")
		b.WriteString("Treat as authoritative context — these are runbooks, decisions, and notes the team has explicitly attached. ")
		b.WriteString("Read what's relevant; don't blindly act on every artifact.\n\n")

		// Group by anchor type. Issue and project anchors come from the
		// issue-claim path; agent anchors carry long-lived agent persona/
		// preferences and ride along on every task; channel anchors come
		// from the channel-mention claim path. Order matches the per-task
		// fetch order: most-specific anchor first.
		byAnchor := make(map[string][]MemoryArtifactForEnv)
		for _, a := range ctx.MemoryArtifacts {
			byAnchor[a.AnchorType] = append(byAnchor[a.AnchorType], a)
		}

		writeArtifacts := func(header string, list []MemoryArtifactForEnv) {
			if len(list) == 0 {
				return
			}
			fmt.Fprintf(&b, "### %s\n\n", header)
			for _, a := range list {
				fmt.Fprintf(&b, "#### %s — %s\n\n", formatMemoryKindLabel(a.Kind), a.Title)
				if len(a.Tags) > 0 {
					fmt.Fprintf(&b, "_Tags: %s_\n\n", strings.Join(a.Tags, ", "))
				}
				b.WriteString(strings.TrimSpace(a.Content))
				b.WriteString("\n\n")
				fmt.Fprintf(&b, "<sub>Memory artifact `%s` · updated %s</sub>\n\n", a.ID, a.UpdatedAt)
			}
		}

		// Render in fixed order. Anchor types not in the known set get
		// dropped into a generic "Other" bucket — better than silently
		// hiding them when a future anchor type is added on the server.
		// "always" is a synthetic anchor_type the server stamps on
		// always_inject_at_runtime artifacts so they get their own
		// heading rather than merging with anchored content.
		writeArtifacts("On this issue", byAnchor["issue"])
		writeArtifacts("On the project", byAnchor["project"])
		writeArtifacts("On this channel", byAnchor["channel"])
		writeArtifacts("Notes I've kept (agent-anchored)", byAnchor["agent"])
		writeArtifacts("Workspace knowledge (always-on)", byAnchor["always"])
		var unknown []MemoryArtifactForEnv
		for kind, list := range byAnchor {
			switch kind {
			case "issue", "project", "channel", "agent", "always":
				continue
			}
			unknown = append(unknown, list...)
		}
		writeArtifacts("Other", unknown)
	}

	b.WriteString("### Workflow\n\n")

	if ctx.ChatSessionID != "" {
		// Chat task: interactive assistant mode
		b.WriteString("**You are in chat mode.** A user is messaging you directly in a chat window.\n\n")
		b.WriteString("- Respond conversationally and helpfully to the user's message\n")
		b.WriteString("- You have full access to the `multica` CLI to look up issues, workspace info, members, agents, etc.\n")
		b.WriteString("- If asked about issues, use `multica issue list --output json` or `multica issue get <id> --output json`\n")
		b.WriteString("- If asked about the workspace, use `multica workspace get --output json`\n")
		b.WriteString("- If asked to perform actions (create issues, update status, etc.), use the appropriate CLI commands\n")
		b.WriteString("- If the task requires code changes, use `multica repo checkout <url>` to get the code first. Use `--ref <branch-or-sha>` when you need an exact revision\n")
		b.WriteString("- Keep responses concise and direct\n\n")
	} else if ctx.QuickCreatePrompt != "" {
		// Quick-create task: detailed field / output rules live in the
		// per-turn prompt (BuildPrompt → buildQuickCreatePrompt) so they
		// have a single source of truth. Quick-create is one-shot, so the
		// per-turn message is always present and the agent reads the rules
		// from there. We only keep the hard guardrails here so a provider
		// that doesn't propagate the user message into its working context
		// (or a resumed session) still avoids the assignment-task workflow
		// pointing at an empty issue id.
		b.WriteString("**This task was triggered by quick-create.** There is NO existing Multica issue. Follow the field and output rules in the user message you just received; ignore the default assignment-task workflow.\n\n")
		b.WriteString("Hard guardrails (apply even if the user message is missing):\n")
		b.WriteString("- Run exactly one `multica issue create` invocation, then exit.\n")
		b.WriteString("- Do NOT call `multica issue get`, `multica issue status`, or `multica issue comment add` for this task — there is no issue to query, transition, or comment on. The platform writes the user's success/failure inbox notification automatically based on whether `multica issue create` succeeded.\n")
		b.WriteString("- If the CLI returns an error, exit with that error as the only output. Do not retry.\n\n")
	} else if ctx.AutopilotRunID != "" {
		// Autopilot run_only task: no issue exists, so the agent must not
		// follow the assignment/comment workflow.
		b.WriteString("**This task was triggered by an Autopilot in run-only mode.** There is no assigned Multica issue for this run.\n\n")
		fmt.Fprintf(&b, "- Autopilot run ID: `%s`\n", ctx.AutopilotRunID)
		if ctx.AutopilotID != "" {
			fmt.Fprintf(&b, "- Autopilot ID: `%s`\n", ctx.AutopilotID)
		}
		if ctx.AutopilotTitle != "" {
			fmt.Fprintf(&b, "- Autopilot title: %s\n", ctx.AutopilotTitle)
		}
		if ctx.AutopilotSource != "" {
			fmt.Fprintf(&b, "- Trigger source: %s\n", ctx.AutopilotSource)
		}
		if ctx.AutopilotTriggerPayload != "" {
			fmt.Fprintf(&b, "- Trigger payload:\n\n```json\n%s\n```\n", ctx.AutopilotTriggerPayload)
		}
		if strings.TrimSpace(ctx.AutopilotDescription) != "" {
			b.WriteString("\nAutopilot instructions:\n\n")
			b.WriteString(ctx.AutopilotDescription)
			b.WriteString("\n\n")
		}
		if ctx.AutopilotID != "" {
			fmt.Fprintf(&b, "- Run `multica autopilot get %s --output json` if you need the full autopilot configuration\n", ctx.AutopilotID)
		}
		b.WriteString("- Complete the autopilot instructions directly\n")
		b.WriteString("- Do not run `multica issue get`, `multica issue comment add`, or `multica issue status` for this run unless the autopilot instructions explicitly tell you to create or update an issue\n\n")
	} else if ctx.TriggerCommentID != "" {
		// Comment-triggered: focus on reading and replying
		b.WriteString("**This task was triggered by a NEW comment.** Your primary job is to respond to THIS specific comment, even if you have handled similar requests before in this session.\n\n")
		fmt.Fprintf(&b, "1. Run `multica issue get %s --output json` to understand the issue context\n", ctx.IssueID)
		fmt.Fprintf(&b, "2. Run `multica issue comment list %s --output json` to read the conversation\n", ctx.IssueID)
		b.WriteString("   - If the output is very large or truncated, use pagination: `--limit 30` to get the latest 30 comments, or `--since <timestamp>` to fetch only recent ones\n")
		fmt.Fprintf(&b, "3. Find the triggering comment (ID: `%s`) and understand what is being asked — do NOT confuse it with previous comments\n", ctx.TriggerCommentID)
		b.WriteString("4. **Decide whether a reply is warranted.** If you produced actual work this turn (investigated, fixed, answered a real question), post the result via step 6 — that is a normal reply, not a noise comment. If the triggering comment was a pure acknowledgment / thanks / sign-off from another agent AND you produced no work this turn, do NOT post a reply — and do NOT post a comment saying 'No reply needed' or similar. Simply exit with no output. Silence is a valid and preferred way to end agent-to-agent conversations.\n")
		b.WriteString("5. If a reply IS warranted: do any requested work first, then **decide whether to include any `@mention` link.** The default is NO mention. Only mention when you are escalating to a human owner who is not yet involved, delegating a concrete new sub-task to another agent for the first time, or the user explicitly asked you to loop someone in. Never @mention the agent you are replying to as a thank-you or sign-off.\n")
		b.WriteString("6. **If you reply, post it as a comment — this step is mandatory when you reply.** Text in your terminal or run logs is NOT delivered to the user. ")
		b.WriteString(BuildCommentReplyInstructions(ctx.IssueID, ctx.TriggerCommentID))
		b.WriteString("7. Do NOT change the issue status unless the comment explicitly asks for it\n\n")
	} else {
		// Assignment-triggered: defer to agent Skills for workflow specifics.
		b.WriteString("You are responsible for managing the issue status throughout your work.\n\n")
		fmt.Fprintf(&b, "1. Run `multica issue get %s --output json` to understand your task\n", ctx.IssueID)
		fmt.Fprintf(&b, "2. Run `multica issue comment list %s --output json` to read the full comment history — this is mandatory, not optional. Earlier comments often carry context the issue body lacks (e.g. which repo to work in, the prior agent's findings, the reason the issue was reassigned to you). Skipping this step is the most common cause of agents acting on stale or incomplete instructions.\n", ctx.IssueID)
		fmt.Fprintf(&b, "   - If the output is very large or truncated, use pagination: `--limit 30` to get the latest 30 comments, or `--since <timestamp>` to fetch only recent ones\n")
		fmt.Fprintf(&b, "3. Run `multica issue status %s in_progress`\n", ctx.IssueID)
		b.WriteString("4. Follow your Skills and Agent Identity to complete the task (write code, investigate, etc.)\n")
		fmt.Fprintf(&b, "5. **Post your final results as a comment — this step is mandatory**: `multica issue comment add %s --content \"...\"`. Your results are only visible to the user if posted via this CLI call; text in your terminal or run logs is NOT delivered.\n", ctx.IssueID)
		fmt.Fprintf(&b, "6. When done, run `multica issue status %s in_review`\n", ctx.IssueID)
		fmt.Fprintf(&b, "7. If blocked, run `multica issue status %s blocked` and post a comment explaining why\n\n", ctx.IssueID)
	}

	if len(ctx.AgentSkills) > 0 {
		b.WriteString("## Skills\n\n")
		switch provider {
		case "claude":
			// Claude discovers skills natively from .claude/skills/ — just list names.
			b.WriteString("You have the following skills installed (discovered automatically):\n\n")
		case "codex", "copilot", "opencode", "openclaw", "pi", "cursor", "kimi", "kiro":
			// Codex, Copilot, OpenCode, OpenClaw, Pi, Cursor, Kimi, and Kiro discover skills natively from their respective paths — just list names.
			b.WriteString("You have the following skills installed (discovered automatically):\n\n")
		case "gemini", "hermes":
			// Gemini reads GEMINI.md directly; Hermes has no native skills discovery path
			// wired up in resolveSkillsDir, so both fall back to .agent_context/skills/.
			b.WriteString("Detailed skill instructions are in `.agent_context/skills/`. Each subdirectory contains a `SKILL.md`.\n\n")
		default:
			b.WriteString("Detailed skill instructions are in `.agent_context/skills/`. Each subdirectory contains a `SKILL.md`.\n\n")
		}
		for _, skill := range ctx.AgentSkills {
			fmt.Fprintf(&b, "- **%s**\n", skill.Name)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Mentions\n\n")
	b.WriteString("Mention links are **side-effecting actions**, not just formatting:\n\n")
	b.WriteString("- `[MUL-123](mention://issue/<issue-id>)` — clickable link to an issue (safe, no side effect)\n")
	b.WriteString("- `[@Name](mention://member/<user-id>)` — **sends a notification to a human**\n")
	b.WriteString("- `[@Name](mention://agent/<agent-id>)` — **enqueues a new run for that agent**\n\n")
	b.WriteString("### When NOT to use a mention link\n\n")
	b.WriteString("- Referring to someone in prose (e.g. \"GPT-Boy is right\") — write the plain name, no link.\n")
	b.WriteString("- **Replying to another agent that just spoke to you.** By default, do NOT put a `mention://agent/...` link anywhere in your reply. The platform already shows your comment to everyone on the issue; re-mentioning the other agent will make them run again, and if they reply with a mention back, you will be triggered again. That is a loop and it costs the user money.\n")
	b.WriteString("- Thanking, acknowledging, wrapping up, or signing off. These are exactly the moments where an accidental `@mention` causes the other agent to reply \"you're welcome\" and restart the loop. If the work is done, **end with no mention at all**.\n\n")
	b.WriteString("### When a mention IS appropriate\n\n")
	b.WriteString("- Escalating to a human owner who is not yet involved.\n")
	b.WriteString("- Delegating a concrete sub-task to another agent for the first time, with a clear request.\n")
	b.WriteString("- The user explicitly asked you to loop someone in.\n\n")
	b.WriteString("If you are unsure whether a mention is warranted, **don't mention**. Silence ends conversations; `@` restarts them.\n\n")
	b.WriteString("Use `multica issue list --output json` to look up issue IDs, and `multica workspace members --output json` for member IDs.\n\n")

	b.WriteString("## Attachments\n\n")
	b.WriteString("Issues and comments may include file attachments (images, documents, etc.).\n")
	b.WriteString("Use the download command to fetch attachment files locally:\n\n")
	b.WriteString("```\nmultica attachment download <attachment-id>\n```\n\n")
	b.WriteString("This downloads the file to the current directory and prints the local path. Use `-o <dir>` to save elsewhere.\n")
	b.WriteString("After downloading, you can read the file directly (e.g. view an image, read a document).\n\n")

	b.WriteString("## Important: Always Use the `multica` CLI\n\n")
	b.WriteString("All interactions with Multica platform resources — including issues, comments, attachments, images, files, and any other platform data — **must** go through the `multica` CLI. ")
	b.WriteString("Do NOT use `curl`, `wget`, or any other HTTP client to access Multica URLs or APIs directly. ")
	b.WriteString("Multica resource URLs require authenticated access that only the `multica` CLI can provide.\n\n")
	b.WriteString("If you need to perform an operation that is not covered by any existing `multica` command, ")
	b.WriteString("do NOT attempt to work around it. Instead, post a comment mentioning the workspace owner to request the missing functionality.\n\n")

	b.WriteString("## Output\n\n")
	switch {
	case ctx.AutopilotRunID != "":
		b.WriteString("This is a run-only autopilot task, so there may be no issue comment to post. Your final assistant output is captured automatically as the autopilot run result. Keep it concise and state the outcome.\n")
	case ctx.QuickCreatePrompt != "":
		b.WriteString("This is a quick-create task. There is NO existing issue to comment on. Your final stdout is captured automatically and the platform writes the user's success/failure inbox notification based on whether `multica issue create` succeeded.\n\n")
		b.WriteString("- Do NOT call `multica issue comment add` — the issue you just created has no conversation context for this run.\n")
		b.WriteString("- Print exactly one final line: `Created MUL-<n>: <title>` after a successful `multica issue create`.\n")
		b.WriteString("- On CLI failure, exit with the CLI error as the only output. The platform translates that into a `quick_create_failed` inbox item carrying the original prompt for the user.\n")
	default:
		b.WriteString("⚠️ **Final results MUST be delivered via `multica issue comment add`.** The user does NOT see your terminal output, assistant chat text, or run logs — only comments on the issue. A task that finishes without a result comment is invisible to the user, even if the work itself was correct.\n\n")
		b.WriteString("Keep comments concise and natural — state the outcome, not the process.\n")
		b.WriteString("Good: \"Fixed the login redirect. PR: https://...\"\n")
		b.WriteString("Bad: \"1. Read the issue 2. Found the bug in auth.go 3. Created branch 4. ...\"\n")
		b.WriteString("When referencing an issue in a comment, use the issue mention format `[MUL-123](mention://issue/<issue-id>)` so it renders as a clickable link. (Issue mentions have no side effect; only member/agent mentions do — see the Mentions section above.)\n")
	}

	return b.String()
}
