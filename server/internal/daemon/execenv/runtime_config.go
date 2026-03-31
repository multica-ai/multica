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
// For Claude: writes {workDir}/CLAUDE.md  (skills discovered natively from .claude/skills/)
// For Codex:  writes {workDir}/AGENTS.md  (skills discovered natively via CODEX_HOME)
func InjectRuntimeConfig(workDir, provider string, ctx TaskContextForEnv) error {
	content := buildMetaSkillContent(provider, ctx)

	switch provider {
	case "claude":
		return os.WriteFile(filepath.Join(workDir, "CLAUDE.md"), []byte(content), 0o644)
	case "codex":
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
	b.WriteString("- `multica issue comment list <issue-id> --output json` — List all comments on an issue (includes id, parent_id for threading)\n")
	b.WriteString("- `multica workspace get --output json` — Get workspace details and context\n")
	b.WriteString("- `multica agent list --output json` — List agents in workspace\n\n")

	b.WriteString("### Write\n")
	b.WriteString("- `multica issue comment add <issue-id> --content \"...\" [--parent <comment-id>]` — Post a comment (use --parent to reply to a specific comment)\n")
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

	b.WriteString("### Workflow\n")
	b.WriteString("You are responsible for managing the issue status throughout your work.\n\n")
	fmt.Fprintf(&b, "1. Run `multica issue get %s --output json` to understand your task\n", ctx.IssueID)
	fmt.Fprintf(&b, "2. Run `multica issue status %s in_progress`\n", ctx.IssueID)
	b.WriteString("3. Read comments for additional context or human instructions. When replying to a specific comment, use `--parent <comment-id>` to thread your response\n")
	b.WriteString("4. If the task requires code changes:\n")
	if len(ctx.Repos) > 0 {
		b.WriteString("   a. Run `multica repo checkout <url>` to check out the appropriate repository\n")
		b.WriteString("   b. `cd` into the checked-out directory\n")
		b.WriteString("   c. Implement the changes and commit\n")
	} else {
		b.WriteString("   a. Create a new branch\n")
		b.WriteString("   b. Implement the changes and commit\n")
	}
	b.WriteString("   c. Push the branch to the remote\n")
	b.WriteString("   d. Create a pull request (decide the target branch based on the repo's conventions)\n")
	fmt.Fprintf(&b, "   e. Post the PR link as a comment: `multica issue comment add %s --content \"PR: <url>\"`\n", ctx.IssueID)
	b.WriteString("5. If the task does not require code (e.g. research, documentation), post your findings as a comment\n")
	fmt.Fprintf(&b, "6. Run `multica issue status %s in_review`\n", ctx.IssueID)
	fmt.Fprintf(&b, "7. If blocked, run `multica issue status %s blocked` and post a comment explaining why\n\n", ctx.IssueID)

	if len(ctx.AgentSkills) > 0 {
		b.WriteString("## Skills\n\n")
		switch provider {
		case "claude":
			// Claude discovers skills natively from .claude/skills/ — just list names.
			b.WriteString("You have the following skills installed (discovered automatically):\n\n")
		case "codex":
			// Codex discovers skills natively via CODEX_HOME/skills/ — just list names.
			b.WriteString("You have the following skills installed (discovered automatically):\n\n")
		default:
			b.WriteString("Detailed skill instructions are in `.agent_context/skills/`. Each subdirectory contains a `SKILL.md`.\n\n")
		}
		for _, skill := range ctx.AgentSkills {
			fmt.Fprintf(&b, "- **%s**\n", skill.Name)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Output\n\n")
	b.WriteString("Keep comments concise and natural — state the outcome, not the process.\n")
	b.WriteString("Good: \"Fixed the login redirect. PR: https://...\"\n")
	b.WriteString("Bad: \"1. Read the issue 2. Found the bug in auth.go 3. Created branch 4. ...\"\n")

	return b.String()
}
