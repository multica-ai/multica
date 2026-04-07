package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InjectRuntimeConfig writes the meta skill content into the runtime-specific
// config file so the agent discovers its environment through its native mechanism.
//
// For Claude:   writes {workDir}/CLAUDE.md  (skills discovered natively from .claude/skills/)
// For Codex:    writes {workDir}/AGENTS.md  (skills discovered natively via CODEX_HOME)
// For OpenCode: writes {workDir}/AGENTS.md  (skills discovered natively from .config/opencode/skills/)
// For OpenClaw: writes {workDir}/AGENTS.md  (skills discovered natively from .openclaw/skills/)
func InjectRuntimeConfig(workDir, provider string, ctx TaskContextForEnv) error {
	content := buildMetaSkillContent(provider, ctx)

	switch provider {
	case "claude":
		return os.WriteFile(filepath.Join(workDir, "CLAUDE.md"), []byte(content), 0o644)
	case "codex", "opencode", "openclaw":
		return os.WriteFile(filepath.Join(workDir, "AGENTS.md"), []byte(content), 0o644)
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

	// Inject agent identity instructions before workflow commands.
	if ctx.AgentInstructions != "" {
		b.WriteString("## Agent Identity\n\n")
		b.WriteString(ctx.AgentInstructions)
		b.WriteString("\n\n")
	}

	b.WriteString("## Available Commands\n\n")
	b.WriteString("**Always use `--output json` for all read commands** to get structured data with full IDs.\n\n")
	b.WriteString("### Read\n")
	b.WriteString("- `multica issue get <id> --output json` — Get full issue details (title, description, status, priority, assignee)\n")
	b.WriteString("- `multica issue list [--status X] [--priority X] [--assignee X] --output json` — List issues in workspace\n")
	b.WriteString("- `multica issue comment list <issue-id> [--limit N] [--offset N] [--since <RFC3339>] --output json` — List comments on an issue (supports pagination; includes id, parent_id for threading)\n")
	b.WriteString("- `multica workspace get --output json` — Get workspace details and context\n")
	b.WriteString("- `multica workspace members [workspace-id] --output json` — List workspace members (user IDs, names, roles)\n")
	b.WriteString("- `multica agent list --output json` — List agents in workspace\n")
	b.WriteString("- `multica repo checkout <url>` — Check out a repository into the working directory (creates a git worktree with a dedicated branch)\n")
	b.WriteString("- `multica issue runs <issue-id> --output json` — List all execution runs for an issue (status, timestamps, errors)\n")
	b.WriteString("- `multica issue run-messages <task-id> [--since <seq>] --output json` — List messages for a specific execution run (supports incremental fetch)\n")
	b.WriteString("- `multica attachment download <id> [-o <dir>]` — Download an attachment file locally by ID\n\n")

	b.WriteString("### Write\n")
	b.WriteString("- `multica issue create --title \"...\" [--description \"...\"] [--priority X] [--assignee X] [--parent <issue-id>] [--status X]` — Create a new issue\n")
	b.WriteString("- `multica issue assign <id> --to <name>` — Assign an issue to a member or agent by name (use --unassign to remove assignee)\n")
	b.WriteString("- `multica issue comment add <issue-id> --content \"...\" [--parent <comment-id>]` — Post a comment (use --parent to reply to a specific comment)\n")
	b.WriteString("- `multica issue comment delete <comment-id>` — Delete a comment\n")
	b.WriteString("- `multica issue status <id> <status>` — Update issue status (todo, in_progress, in_review, done, blocked)\n")
	b.WriteString("- `multica issue update <id> [--title X] [--description X] [--priority X]` — Update issue fields\n\n")

	// Inject available repositories section.
	if len(ctx.Repos) > 0 {
		b.WriteString("## Repositories\n\n")
		b.WriteString("The following code repositories are available in this workspace.\n")
		b.WriteString("Use `multica repo checkout <url>` to check out a repository into your working directory.\n\n")
		b.WriteString("| URL | Description |\n")
		b.WriteString("|-----|-------------|\n")
		for _, repo := range ctx.Repos {
			desc := repo.Description
			if desc == "" {
				desc = "—"
			}
			fmt.Fprintf(&b, "| %s | %s |\n", repo.URL, desc)
		}
		b.WriteString("\nThe checkout command creates a git worktree with a dedicated branch. You can check out one or more repos as needed.\n\n")
	}

	b.WriteString("### Workflow\n\n")

	if ctx.IsAgentflow {
		// Agentflow-triggered: execute the prompt template
		fmt.Fprintf(&b, "**This task was triggered by Agentflow: %s**\n\n", ctx.AgentflowTitle)
		if ctx.AgentflowDescription != "" {
			b.WriteString("## Instructions\n\n")
			b.WriteString(ctx.AgentflowDescription)
			b.WriteString("\n\n")
		}
		b.WriteString("Execute the instructions above.\n")
		b.WriteString("- If the work requires tracking, create an issue using `multica issue create --title \"...\" --description \"...\"`\n")
		b.WriteString("- If the work involves code changes, use `multica repo checkout <url>` to check out a repository\n")
		b.WriteString("- Post results as appropriate (comments on related issues, new issues, etc.)\n\n")
	} else if ctx.TriggerCommentID != "" {
		// Comment-triggered: focus on reading and replying
		b.WriteString("**This task was triggered by a comment.** Your primary job is to respond.\n\n")
		fmt.Fprintf(&b, "1. Run `multica issue get %s --output json` to understand the issue context\n", ctx.IssueID)
		fmt.Fprintf(&b, "2. Run `multica issue comment list %s --output json` to read the conversation\n", ctx.IssueID)
		b.WriteString("   - If the output is very large or truncated, use pagination: `--limit 30` to get the latest 30 comments, or `--since <timestamp>` to fetch only recent ones\n")
		fmt.Fprintf(&b, "3. Find the triggering comment (ID: `%s`) and understand what is being asked\n", ctx.TriggerCommentID)
		fmt.Fprintf(&b, "4. Reply: `multica issue comment add %s --parent %s --content \"...\"`\n", ctx.IssueID, ctx.TriggerCommentID)
		b.WriteString("5. If the comment requests code changes or further work, do the work first, then reply with your results\n")
		b.WriteString("6. Do NOT change the issue status unless the comment explicitly asks for it\n\n")
	} else {
		// Assignment-triggered: full workflow
		b.WriteString("You are responsible for managing the issue status throughout your work.\n\n")
		fmt.Fprintf(&b, "1. Run `multica issue get %s --output json` to understand your task\n", ctx.IssueID)
		fmt.Fprintf(&b, "2. Run `multica issue status %s in_progress`\n", ctx.IssueID)
		b.WriteString("3. Read comments for additional context or human instructions\n")
		b.WriteString("4. If the task requires code changes:\n")
		if len(ctx.Repos) > 0 {
			b.WriteString("   a. Run `multica repo checkout <url>` to check out the appropriate repository\n")
			b.WriteString("   b. `cd` into the checked-out directory\n")
			b.WriteString("   c. Implement the changes and commit\n")
			b.WriteString("   d. Push the branch to the remote\n")
			b.WriteString("   e. Create a pull request (decide the target branch based on the repo's conventions)\n")
			fmt.Fprintf(&b, "   f. Post the PR link as a comment: `multica issue comment add %s --content \"PR: <url>\"`\n", ctx.IssueID)
		} else {
			b.WriteString("   a. Create a new branch\n")
			b.WriteString("   b. Implement the changes and commit\n")
			b.WriteString("   c. Push the branch to the remote\n")
			b.WriteString("   d. Create a pull request (decide the target branch based on the repo's conventions)\n")
			fmt.Fprintf(&b, "   e. Post the PR link as a comment: `multica issue comment add %s --content \"PR: <url>\"`\n", ctx.IssueID)
		}
		b.WriteString("5. If the task does not require code (e.g. research, documentation), post your findings as a comment\n")
		fmt.Fprintf(&b, "6. Run `multica issue status %s in_review`\n", ctx.IssueID)
		fmt.Fprintf(&b, "7. If blocked, run `multica issue status %s blocked` and post a comment explaining why\n\n", ctx.IssueID)
	}

	if len(ctx.AgentSkills) > 0 {
		b.WriteString("## Skills\n\n")
		switch provider {
		case "claude":
			// Claude discovers skills natively from .claude/skills/ — just list names.
			b.WriteString("You have the following skills installed (discovered automatically):\n\n")
		case "codex", "opencode", "openclaw":
			// Codex, OpenCode, and OpenClaw discover skills natively from their respective paths — just list names.
			b.WriteString("You have the following skills installed (discovered automatically):\n\n")
		default:
			b.WriteString("Detailed skill instructions are in `.agent_context/skills/`. Each subdirectory contains a `SKILL.md`.\n\n")
		}
		for _, skill := range ctx.AgentSkills {
			fmt.Fprintf(&b, "- **%s**\n", skill.Name)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Mentions\n\n")
	b.WriteString("When referencing issues or people in comments, use the mention format so they render as interactive links:\n\n")
	b.WriteString("- **Issue**: `[MUL-123](mention://issue/<issue-id>)` — renders as a clickable link to the issue\n")
	b.WriteString("- **Member**: `[@Name](mention://member/<user-id>)` — renders as a styled mention and sends a notification\n")
	b.WriteString("- **Agent**: `[@Name](mention://agent/<agent-id>)` — renders as a styled mention\n\n")
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
	b.WriteString("Keep comments concise and natural — state the outcome, not the process.\n")
	b.WriteString("Good: \"Fixed the login redirect. PR: https://...\"\n")
	b.WriteString("Bad: \"1. Read the issue 2. Found the bug in auth.go 3. Created branch 4. ...\"\n")

	return b.String()
}
