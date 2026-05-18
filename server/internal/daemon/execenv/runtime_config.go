package execenv

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// runtimeGOOS is the host-platform string used by buildMetaSkillContent and
// BuildCommentReplyInstructions to emit Windows-specific guidance. Defaults
// to runtime.GOOS; tests override it to exercise the cross-platform branches
// deterministically without having to run on every target OS.
var runtimeGOOS = runtime.GOOS

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
// For OpenCode: writes {workDir}/AGENTS.md  (skills discovered natively from .opencode/skills/)
// For OpenClaw: writes {workDir}/AGENTS.md  (skills discovered natively from {workDir}/skills/ via per-task openclaw-config.json that pins agents.defaults.workspace)
// For Hermes:   writes {workDir}/AGENTS.md  (skills fall back to .agent_context/skills/; AGENTS.md points there)
// For Gemini:   writes {workDir}/GEMINI.md  (discovered natively by the Gemini CLI)
// For Pi:       writes {workDir}/AGENTS.md  (skills discovered natively from .pi/skills/)
// For Cursor:   writes {workDir}/AGENTS.md  (skills discovered natively from .cursor/skills/)
// For Kimi:     writes {workDir}/AGENTS.md  (Kimi Code CLI reads AGENTS.md natively; skills auto-discovered from project skills dirs)
// For Kiro:     writes {workDir}/AGENTS.md  (Kiro CLI reads AGENTS.md natively; skills auto-discovered from project skills dirs)
func InjectRuntimeConfig(workDir, provider string, ctx TaskContextForEnv) (string, error) {
	content := buildMetaSkillContent(provider, ctx)

	switch provider {
	case "claude":
		return content, os.WriteFile(filepath.Join(workDir, "CLAUDE.md"), []byte(content), 0o644)
	case "codex", "copilot", "opencode", "openclaw", "hermes", "pi", "cursor", "kimi", "kiro":
		return content, os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte(content), 0o644)
	case "gemini":
		return content, os.WriteFile(filepath.Join(workDir, "GEMINI.md"), []byte(content), 0o644)
	default:
		// Unknown provider — skip config injection, prompt-only mode.
		return content, nil
	}
}

// buildMetaSkillContent generates the meta skill markdown that teaches the agent
// about the Multica runtime environment and available CLI tools.
func buildMetaSkillContent(provider string, ctx TaskContextForEnv) string {
	var b strings.Builder
	caps := deriveRuntimeCapabilities(ctx)

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

	writeAvailableCommands(&b, caps)

	if provider == "codex" {
		b.WriteString("## Codex-Specific Comment Formatting\n\n")
		if runtimeGOOS == "windows" {
			b.WriteString("Codex often follows the per-turn reply command literally. On Windows, **always write the comment body to a UTF-8 file with your file-write tool first, then post it with `--content-file <path>`** — do NOT pipe via `--content-stdin`. PowerShell 5.1's `$OutputEncoding` defaults to ASCIIEncoding when piping to a native command, silently dropping non-ASCII characters as `?` before they reach `multica.exe`. Never use inline `--content` for agent-authored comments. ")
			b.WriteString("Keep the same `--parent` value from the trigger comment when replying. ")
			b.WriteString("Do not compress a multi-paragraph answer into one line and do not rely on `\\n` escapes.\n\n")
		} else {
			b.WriteString("Codex often follows the per-turn reply command literally. For issue comments, always use `--content-stdin` with a HEREDOC, even for short single-line replies. ")
			b.WriteString("Never use inline `--content` for agent-authored comments. Keep the same `--parent` value from the trigger comment when replying. ")
			b.WriteString("Do not compress a multi-paragraph answer into one line and do not rely on `\\n` escapes.\n\n")
		}
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

	writeProjectContext(&b, ctx, caps)

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
		fmt.Fprintf(&b, "2. Run `multica issue comment list %s --output json` to read the conversation (returns all comments, capped server-side at 2000)\n", ctx.IssueID)
		b.WriteString("   - For incremental polling, use `--since <RFC3339-timestamp>` to fetch only comments newer than a known cursor\n")
		fmt.Fprintf(&b, "3. Find the triggering comment (ID: `%s`) and understand what is being asked — do NOT confuse it with previous comments\n", ctx.TriggerCommentID)
		if ctx.IsSquadLeader {
			b.WriteString("4. **Decide whether a reply is warranted.** If you produced actual work this turn (investigated, fixed, answered a real question), post the result via step 6 — that is a normal reply, not a noise comment. If the triggering comment was a pure acknowledgment / thanks / sign-off from another agent AND you produced no work this turn, do NOT post a reply — and do NOT post a comment saying 'No reply needed' or similar. Simply exit with no output. Silence is a valid and preferred way to end agent-to-agent conversations.\n")
			fmt.Fprintf(&b, "   - **Squad leader rule:** If your evaluation outcome is `no_action`, call `multica squad activity %s no_action --reason \"...\"` and then EXIT IMMEDIATELY. DO NOT post any comment whose only purpose is to announce that you are taking no action, exiting silently, or acknowledging another agent. A comment like \"No action needed\" or \"Exiting silently\" is noise — the `squad activity` call already records your decision in the timeline.\n", ctx.IssueID)
		} else {
			b.WriteString("4. **Decide whether a reply is warranted.** If you produced actual work this turn (investigated, fixed, answered a real question), post the result via step 6 — that is a normal reply, not a noise comment. If the triggering comment was a pure acknowledgment / thanks / sign-off from another agent AND you produced no work this turn, do NOT post a reply — and do NOT post a comment saying 'No reply needed' or similar. Simply exit with no output. Silence is a valid and preferred way to end agent-to-agent conversations.\n")
		}
		b.WriteString("5. If a reply IS warranted: do any requested work first, then **decide whether to include any `@mention` link.** The default is NO mention. Only mention when you are escalating to a human owner who is not yet involved, delegating a concrete new sub-task to another agent for the first time, or the user explicitly asked you to loop someone in. Never @mention the agent you are replying to as a thank-you or sign-off.\n")
		b.WriteString("6. **If you reply, post it as a comment — this step is mandatory when you reply.** Text in your terminal or run logs is NOT delivered to the user. ")
		b.WriteString(BuildCommentReplyInstructions(provider, ctx.IssueID, ctx.TriggerCommentID))
		b.WriteString("7. Do NOT change the issue status unless the comment explicitly asks for it\n\n")
	} else {
		// Assignment-triggered: defer to agent Skills for workflow specifics.
		b.WriteString("You are responsible for managing the issue status throughout your work.\n\n")
		fmt.Fprintf(&b, "1. Run `multica issue get %s --output json` to understand your task\n", ctx.IssueID)
		fmt.Fprintf(&b, "2. Run `multica issue comment list %s --output json` to read the full comment history (returns all comments, capped server-side at 2000) — this is mandatory, not optional. Earlier comments often carry context the issue body lacks (e.g. which repo to work in, the prior agent's findings, the reason the issue was reassigned to you). Skipping this step is the most common cause of agents acting on stale or incomplete instructions.\n", ctx.IssueID)
		fmt.Fprintf(&b, "3. Run `multica issue status %s in_progress`\n", ctx.IssueID)
		b.WriteString("4. Follow your Skills and Agent Identity to complete the task (write code, investigate, etc.)\n")
		if ctx.IsSquadLeader {
			fmt.Fprintf(&b, "5. **Post your final results as a comment** (unless your outcome is `no_action` — in that case, calling `multica squad activity %s no_action --reason \"...\"` alone is sufficient; you MUST exit without posting any comment. DO NOT post a comment announcing no_action or saying you are exiting silently): `multica issue comment add %s --content \"...\"`. Your results are only visible to the user if posted via this CLI call; text in your terminal or run logs is NOT delivered.\n", ctx.IssueID, ctx.IssueID)
		} else {
			fmt.Fprintf(&b, "5. **Post your final results as a comment — this step is mandatory**: `multica issue comment add %s --content \"...\"`. Your results are only visible to the user if posted via this CLI call; text in your terminal or run logs is NOT delivered.\n", ctx.IssueID)
		}
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
			// Codex, Copilot, OpenCode, OpenClaw, Pi, Cursor, Kimi, and Kiro discover skills
			// natively from their respective paths. For OpenClaw, the daemon also writes a
			// per-task openclaw-config.json (exported via OPENCLAW_CONFIG_PATH) that pins
			// agents.defaults.workspace to the task workdir so the CLI's scanner picks up
			// {workDir}/skills/.
			b.WriteString("You have the following skills installed (discovered automatically):\n\n")
		case "gemini", "hermes":
			// Gemini reads GEMINI.md directly. Hermes has no native skills discovery
			// path wired up in resolveSkillsDir; both fall back to referencing the
			// files explicitly under .agent_context/skills/.
			b.WriteString("Detailed skill instructions are in `.agent_context/skills/`. Each subdirectory contains a `SKILL.md`.\n\n")
		default:
			b.WriteString("Detailed skill instructions are in `.agent_context/skills/`. Each subdirectory contains a `SKILL.md`.\n\n")
		}
		for _, skill := range ctx.AgentSkills {
			fmt.Fprintf(&b, "- **%s**\n", skill.Name)
		}
		b.WriteString("\n")
	}

	writeMentionRules(&b, caps)
	writeAttachmentRules(&b, caps)

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
		b.WriteString("- Print exactly one final line: `Created <identifier-or-id>: <title>` after a successful `multica issue create`. Use the created issue's `identifier` from JSON output when available; otherwise use its `id`. Do not assume any workspace issue prefix such as `MUL-`; workspaces can use custom prefixes.\n")
		b.WriteString("- On CLI failure, exit with the CLI error as the only output. The platform translates that into a `quick_create_failed` inbox item carrying the original prompt for the user.\n")
	default:
		if ctx.IsSquadLeader {
			b.WriteString("⚠️ **Final results MUST be delivered via `multica issue comment add`** — unless your outcome is `no_action`. When you evaluate a trigger and decide no action is needed, calling `multica squad activity <issue-id> no_action --reason \"...\"` alone is sufficient; you MUST exit without posting any comment. DO NOT post a comment that announces no_action, acknowledges another agent, or says you are exiting silently — such comments are noise. For all other outcomes (`action`, `failed`), a comment is still mandatory.\n\n")
		} else {
			b.WriteString("⚠️ **Final results MUST be delivered via `multica issue comment add`.** The user does NOT see your terminal output, assistant chat text, or run logs — only comments on the issue. A task that finishes without a result comment is invisible to the user, even if the work itself was correct.\n\n")
		}
		b.WriteString("Keep comments concise and natural — state the outcome, not the process.\n")
		b.WriteString("Good: \"Fixed the login redirect. PR: https://...\"\n")
		b.WriteString("Bad: \"1. Read the issue 2. Found the bug in auth.go 3. Created branch 4. ...\"\n")
		b.WriteString("When referencing an issue in a comment, use the issue mention format `[MUL-123](mention://issue/<issue-id>)` so it renders as a clickable link. (Issue mentions have no side effect; only member/agent mentions do — see the Mentions section above.)\n")
	}

	return b.String()
}
