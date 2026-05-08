package execenv

// multicaCLISkillName is the public-facing name of the built-in CLI reference
// skill. Both the on-disk directory (sanitized) and the rendered Skills section
// in CLAUDE.md / AGENTS.md / GEMINI.md derive from this constant.
const multicaCLISkillName = "Multica CLI Reference"

// builtinMulticaCLISkill returns the SKILL.md bundle for the multica CLI
// reference. The bundle is installed into every task's skills directory so the
// agent can lazy-load the full flag-level manual on demand instead of paying
// the ~1.5k-token cost of inlining it into the runtime config (see MUL-1821).
//
// The frontmatter `name` matches the sanitized directory; the `description`
// is what providers like Claude Code use to decide when to autoload the skill.
func builtinMulticaCLISkill() SkillContextForEnv {
	return SkillContextForEnv{
		Name:    multicaCLISkillName,
		Content: multicaCLISkillContent,
	}
}

const multicaCLISkillContent = `---
name: multica-cli
description: Full reference for the Multica CLI. Load this when you need a flag, subcommand, or behavior detail that is not in the CLAUDE.md / AGENTS.md quick reference — for example autopilot management, label/subscriber writes, run-message inspection, project-resource queries, or any flag combination beyond the common path.
---

# Multica CLI Reference

This skill is the authoritative reference for the ` + "`multica`" + ` CLI. The runtime config (CLAUDE.md / AGENTS.md / GEMINI.md) only lists the high-frequency subset; everything else is here. Use this skill instead of guessing flags, and prefer ` + "`multica <command> --help`" + ` when you need to verify a flag at runtime.

**Always use ` + "`--output json`" + ` for all read commands** to get structured data with full IDs.

## Read

- ` + "`multica issue get <id> --output json`" + ` — Get full issue details (title, description, status, priority, assignee).
- ` + "`multica issue list [--status X] [--priority X] [--assignee X | --assignee-id <uuid>] [--limit N] [--offset N] --output json`" + ` — List issues in workspace. Default limit: 50; JSON output includes ` + "`total`" + ` and ` + "`has_more`" + ` — use ` + "`--offset`" + ` to paginate while ` + "`has_more`" + ` is true. Prefer ` + "`--assignee-id <uuid>`" + ` when scripting from ` + "`multica workspace members --output json`" + ` / ` + "`multica agent list --output json`" + `.
- ` + "`multica issue comment list <issue-id> [--limit N] [--offset N] [--since <RFC3339>] --output json`" + ` — List comments on an issue. Supports pagination; includes ` + "`id`" + ` and ` + "`parent_id`" + ` for threading.
- ` + "`multica issue label list <issue-id> --output json`" + ` — List labels currently attached to an issue.
- ` + "`multica issue subscriber list <issue-id> --output json`" + ` — List members/agents subscribed to an issue.
- ` + "`multica label list --output json`" + ` — List all labels defined in the workspace (returns id + name + color).
- ` + "`multica workspace get --output json`" + ` — Get workspace details and context.
- ` + "`multica workspace members [workspace-id] --output json`" + ` — List workspace members (user IDs, names, roles).
- ` + "`multica agent list --output json`" + ` — List agents in workspace.
- ` + "`multica repo checkout <url> [--ref <branch-or-sha>]`" + ` — Check out a repository into the working directory. Creates a git worktree with a dedicated branch; use ` + "`--ref`" + ` for review/QA on a specific branch, tag, or commit.
- ` + "`multica issue runs <issue-id> --output json`" + ` — List all execution runs for an issue (status, timestamps, errors).
- ` + "`multica issue run-messages <task-id> [--since <seq>] --output json`" + ` — List messages for a specific execution run. Supports incremental fetch with ` + "`--since`" + `.
- ` + "`multica attachment download <id> [-o <dir>]`" + ` — Download an attachment file locally by ID. Prints the local path; use ` + "`-o`" + ` to save elsewhere.
- ` + "`multica autopilot list [--status X] --output json`" + ` — List autopilots (scheduled / triggered agent automations) in the workspace.
- ` + "`multica autopilot get <id> --output json`" + ` — Get autopilot details including triggers.
- ` + "`multica autopilot runs <id> [--limit N] --output json`" + ` — List execution history for an autopilot.
- ` + "`multica project get <id> --output json`" + ` — Get project details. Includes ` + "`resource_count`" + `; the resources themselves live at the sub-collection below.
- ` + "`multica project resource list <project-id> --output json`" + ` — List resources (e.g. ` + "`github_repo`" + `) attached to a project. Use this when ` + "`resource_count > 0`" + ` and you need the actual refs.

## Write

- ` + "`multica issue create --title \"...\" [--description \"...\" | --description-stdin] [--priority X] [--status X] [--assignee X | --assignee-id <uuid>] [--parent <issue-id>] [--project <project-id>] [--due-date <RFC3339>] [--attachment <path>]`" + ` — Create a new issue. ` + "`--attachment`" + ` may be repeated to upload multiple files. Labels and subscribers are not accepted here; attach them after create with the commands below.
- ` + "`multica issue update <id> [--title X] [--description X | --description-stdin] [--priority X] [--status X] [--assignee X | --assignee-id <uuid>] [--parent <issue-id>] [--project <project-id>] [--due-date <RFC3339>]`" + ` — Update one or more issue fields in a single call. Use ` + "`--parent \"\"`" + ` to clear the parent.
- ` + "`multica issue status <id> <status>`" + ` — Shortcut for ` + "`issue update --status`" + ` when you only need to flip status (` + "`todo`" + `, ` + "`in_progress`" + `, ` + "`in_review`" + `, ` + "`done`" + `, ` + "`blocked`" + `, ` + "`backlog`" + `, ` + "`cancelled`" + `).
- ` + "`multica issue assign <id> --to <name>|--to-id <uuid>`" + ` — Assign an issue to a member or agent. ` + "`--to <name>`" + ` does fuzzy name matching; pass ` + "`--to-id <uuid>`" + ` (mutually exclusive with ` + "`--to`" + `) to assign by canonical UUID, e.g. when names overlap. Use ` + "`--unassign`" + ` to clear the assignee.
- ` + "`multica issue label add <issue-id> <label-id>`" + ` — Attach a label to an issue (look up the label id via ` + "`multica label list`" + `).
- ` + "`multica issue label remove <issue-id> <label-id>`" + ` — Detach a label from an issue.
- ` + "`multica issue subscriber add <issue-id> [--user <name>|--user-id <uuid>]`" + ` — Subscribe a member or agent to issue updates (defaults to the caller when neither flag is set; the two flags are mutually exclusive).
- ` + "`multica issue subscriber remove <issue-id> [--user <name>|--user-id <uuid>]`" + ` — Unsubscribe a member or agent.
- ` + "`multica issue comment add <issue-id> --content-stdin [--parent <comment-id>] [--attachment <path>]`" + ` — Post a comment. Agent-authored comments should always pipe content via stdin, even for short single-line replies. Use ` + "`--parent`" + ` to reply to a specific comment; ` + "`--attachment`" + ` may be repeated.
- ` + "`multica issue comment delete <comment-id>`" + ` — Delete a comment.
- ` + "`multica label create --name \"...\" --color \"#hex\"`" + ` — Define a new workspace label. Use this only when the label you need does not exist yet; reuse existing labels via ` + "`multica label list`" + ` first.
- ` + "`multica autopilot create --title \"...\" --agent <name> --mode create_issue [--description \"...\"]`" + ` — Create an autopilot.
- ` + "`multica autopilot update <id> [--title X] [--description X] [--status active|paused]`" + ` — Update an autopilot.
- ` + "`multica autopilot trigger <id>`" + ` — Manually trigger an autopilot to run once.
- ` + "`multica autopilot delete <id>`" + ` — Delete an autopilot.

## Multi-line content rule (MUL-1467)

For ` + "`multica issue comment add`" + ` and the ` + "`--description`" + ` flag on ` + "`multica issue create`" + ` / ` + "`multica issue update`" + `, you MUST pipe via stdin (` + "`--content-stdin`" + ` / ` + "`--description-stdin`" + `) for any content that contains line breaks, paragraphs, code blocks, backticks, or quotes. Inline ` + "`--content \"...\\n\\n...\"`" + ` does not work because bash does not expand backslash escapes inside double quotes — agents using that form ended up with literal four-character ` + "`\\n`" + ` sequences in stored comments.

Use a HEREDOC instead:

` + "```" + `
cat <<'COMMENT' | multica issue comment add <issue-id> --content-stdin
First paragraph.

Second paragraph with ` + "`code`" + ` and "quotes".
COMMENT
` + "```" + `

The same shape works for ` + "`--description-stdin`" + ` on ` + "`issue create`" + ` / ` + "`issue update`" + `.
`
