package execenv

import (
	"fmt"
	"strings"
)

type taskKind string

const (
	taskKindAssignment  taskKind = "assignment"
	taskKindComment     taskKind = "comment"
	taskKindChat        taskKind = "chat"
	taskKindAutopilot   taskKind = "autopilot"
	taskKindQuickCreate taskKind = "quick_create"
)

type capabilityFlag uint64

const (
	capabilityAttachments capabilityFlag = 1 << iota
	capabilityFullMentions
	capabilityLabelsSubscribers
	capabilityAutopilot
	capabilityProjectResources
	capabilitySquadDelegation
)

type runtimeCapabilities struct {
	kind  taskKind
	flags capabilityFlag
}

func (c runtimeCapabilities) Has(flag capabilityFlag) bool {
	return c.flags&flag != 0
}

func deriveRuntimeCapabilities(ctx TaskContextForEnv) runtimeCapabilities {
	caps := runtimeCapabilities{kind: taskKindForContext(ctx)}
	text := capabilityIntentText(ctx)
	allowTextDrivenBlocks := caps.kind != taskKindQuickCreate

	if ctx.HasIssueOrCommentAttachments || len(ctx.ChatMessageAttachments) > 0 || (allowTextDrivenBlocks && containsAny(text, attachmentIntentKeywords)) {
		caps.flags |= capabilityAttachments
	}
	if containsSideEffectMentionLink(text) || containsAny(text, mentionIntentKeywords) || ctx.IsSquadLeader {
		caps.flags |= capabilityFullMentions
	}
	if allowTextDrivenBlocks && containsAny(text, labelSubscriberIntentKeywords) {
		caps.flags |= capabilityLabelsSubscribers
	}
	if ctx.AutopilotRunID != "" || (allowTextDrivenBlocks && containsAny(text, autopilotIntentKeywords)) {
		caps.flags |= capabilityAutopilot
	}
	if len(ctx.ProjectResources) > 0 || (allowTextDrivenBlocks && containsAny(text, projectResourceIntentKeywords)) {
		caps.flags |= capabilityProjectResources
	}
	if ctx.IsSquadLeader || containsAny(text, squadDelegationIntentKeywords) {
		caps.flags |= capabilitySquadDelegation
	}

	return caps
}

func taskKindForContext(ctx TaskContextForEnv) taskKind {
	switch {
	case ctx.ChatSessionID != "":
		return taskKindChat
	case ctx.QuickCreatePrompt != "":
		return taskKindQuickCreate
	case ctx.AutopilotRunID != "":
		return taskKindAutopilot
	case ctx.TriggerCommentID != "":
		return taskKindComment
	default:
		return taskKindAssignment
	}
}

func capabilityIntentText(ctx TaskContextForEnv) string {
	parts := []string{
		ctx.IssueTitle,
		ctx.IssueDescription,
		ctx.TriggerCommentContent,
		ctx.ChatMessage,
		ctx.QuickCreatePrompt,
		ctx.AutopilotTitle,
		ctx.AutopilotDescription,
		ctx.AutopilotTriggerPayload,
	}
	return strings.ToLower(strings.Join(parts, "\n"))
}

func containsAny(text string, needles []string) bool {
	if text == "" {
		return false
	}
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func containsSideEffectMentionLink(text string) bool {
	return strings.Contains(text, "mention://member/") || strings.Contains(text, "mention://agent/")
}

var attachmentIntentKeywords = []string{
	"attachment",
	"attachments",
	"attached",
	"uploaded",
	"uploaded file",
	"screenshot",
	"file attached",
	"附件",
	"上传",
	"截图",
}

var mentionIntentKeywords = []string{
	"loop someone in",
	"mention link",
	"member mention",
	"agent mention",
	"@mention",
	"notify a member",
	"notify an agent",
	"notify @",
	"cc @",
	"提及链接",
	"mention 链接",
	"通知某人",
	"拉人进来",
	"抄送 @",
}

var labelSubscriberIntentKeywords = []string{
	" label ",
	" labels ",
	"issue label",
	"label this",
	"relabel",
	"subscriber",
	"subscribers",
	"subscribe",
	"unsubscribe",
	"watcher",
	"watchers",
	"标签",
	"订阅",
	"取消订阅",
}

var autopilotIntentKeywords = []string{
	"autopilot",
	"multica autopilot",
	"autopilot run",
	"autopilot runs",
	"autopilot trigger",
	"autopilot webhook",
	"scheduled autopilot",
	"create autopilot",
	"update autopilot",
	"delete autopilot",
	"list autopilots",
	"创建 autopilot",
	"更新 autopilot",
	"删除 autopilot",
	"autopilot 定时",
	"autopilot 触发",
}

var projectResourceIntentKeywords = []string{
	"project resource",
	"project resources",
	"resource_count",
	"resources.json",
	".multica/project",
	"项目资源",
	"项目仓库",
	"项目 repo",
}

var squadDelegationIntentKeywords = []string{
	"squad delegation",
	"squad leader",
	"delegate to squad",
	"squad roster",
	"squad member",
	"squad members",
	"小队",
	"班组",
	"小队委派",
	"小队转派",
	"小队成员名单",
}

func writeAvailableCommands(b *strings.Builder, caps runtimeCapabilities) {
	b.WriteString("## Available Commands\n\n")
	b.WriteString("**Use `--output json` for structured data.** Human table output now prints routable issue keys (for example `MUL-123`) and short UUID prefixes for workspace resources; use `--full-id` on list commands when you need canonical UUIDs.\n\n")
	b.WriteString("The default brief includes the commands needed for the core agent loop and common issue create/update tasks. For everything else, run `multica --help`, `multica <command> --help`, or `multica <command> <subcommand> --help`; prefer `--output json` when the command supports it.\n\n")

	b.WriteString("### Core\n")
	b.WriteString("- `multica issue get <id> --output json` — Get full issue details.\n")
	if caps.kind == taskKindChat {
		b.WriteString("- `multica issue list [--status X] [--priority X] [--assignee X | --assignee-id <uuid>] [--limit N] [--offset N] [--full-id] [--output json]` — List issues in workspace; use offset pagination when JSON output reports `has_more`.\n")
	}
	b.WriteString("- `multica issue comment list <issue-id> [--since <RFC3339>] --output json` — List comments on an issue; use `--since` for incremental polling.\n")
	b.WriteString("- `multica issue create --title \"...\" [--description \"...\" | --description-stdin | --description-file <path>] [--priority X] [--status X] [--assignee X | --assignee-id <uuid>] [--parent <issue-id>] [--project <project-id>] [--due-date <RFC3339>] [--attachment <path>]` — Create a new issue; `--attachment` may be repeated.\n")
	b.WriteString("- `multica issue update <id> [--title X] [--description X | --description-stdin | --description-file <path>] [--priority X] [--status X] [--assignee X | --assignee-id <uuid>] [--parent <issue-id>] [--project <project-id>] [--due-date <RFC3339>]` — Update issue fields; use `--parent \"\"` to clear parent.\n")
	b.WriteString("- `multica repo checkout <url> [--ref <branch-or-sha>]` — Check out a repository into the working directory (creates a git worktree with a dedicated branch; use `--ref` for review/QA on a specific branch, tag, or commit)\n")
	b.WriteString("- `multica issue status <id> <status>` — Shortcut for `issue update --status` when you only need to flip status (todo, in_progress, in_review, done, blocked, backlog, cancelled)\n")
	b.WriteString("- `multica issue comment add <issue-id> [--content \"...\" | --content-stdin | --content-file <path>] [--parent <comment-id>] [--attachment <path>]` — Post a comment. Pick the input mode that preserves your content; run `multica issue comment add --help` for details.\n")
	b.WriteString("\n")

	if caps.Has(capabilityFullMentions) || caps.Has(capabilityLabelsSubscribers) || caps.Has(capabilityAutopilot) || caps.Has(capabilityProjectResources) || caps.Has(capabilitySquadDelegation) || caps.kind == taskKindChat {
		b.WriteString("### Additional Read\n")
	}
	if caps.kind == taskKindChat {
		b.WriteString("- `multica workspace get --output json` — Get workspace details and context\n")
	}
	if caps.Has(capabilityFullMentions) {
		b.WriteString("- `multica workspace members [workspace-id] --output json` — List workspace members (user IDs, names, roles)\n")
		b.WriteString("- `multica agent list --output json` — List agents in workspace\n")
	}
	if caps.Has(capabilityLabelsSubscribers) {
		b.WriteString("- `multica issue label list <issue-id> --output json` — List labels currently attached to an issue\n")
		b.WriteString("- `multica issue subscriber list <issue-id> --output json` — List members/agents subscribed to an issue\n")
		b.WriteString("- `multica label list --output json` — List all labels defined in the workspace (returns id + name + color)\n")
	}
	if caps.Has(capabilitySquadDelegation) {
		b.WriteString("- `multica squad list --output json` — List squads in workspace\n")
		b.WriteString("- `multica squad get <squad-id> --output json` — Get squad details including leader and instructions\n")
		b.WriteString("- `multica squad member list <squad-id> --output json` — List members of a squad\n")
	}
	if caps.Has(capabilityAttachments) {
		b.WriteString("- `multica attachment download <id> [-o <dir>]` — Download an attachment file locally by ID\n")
	}
	if caps.Has(capabilityAutopilot) {
		b.WriteString("- `multica autopilot list [--status X] [--full-id] [--output json]` — List autopilots (scheduled/triggered agent automations) in the workspace; copied short IDs are accepted by autopilot subcommands when unique\n")
		b.WriteString("- `multica autopilot get <id> --output json` — Get autopilot details including triggers\n")
		b.WriteString("- `multica autopilot runs <id> [--limit N] --output json` — List execution history for an autopilot\n")
	}
	if caps.Has(capabilityProjectResources) {
		b.WriteString("- `multica project get <id> --output json` — Get project details. Includes `resource_count`; the resources themselves live at the sub-collection below.\n")
		b.WriteString("- `multica project resource list <project-id> --output json` — List resources (e.g. github_repo) attached to a project. Use this when `resource_count > 0` and you need the actual refs.\n")
	}
	if caps.Has(capabilityFullMentions) || caps.Has(capabilityLabelsSubscribers) || caps.Has(capabilityAutopilot) || caps.Has(capabilityProjectResources) || caps.Has(capabilitySquadDelegation) || caps.kind == taskKindChat {
		b.WriteString("\n")
	}

	if caps.Has(capabilityFullMentions) || caps.Has(capabilityLabelsSubscribers) || caps.Has(capabilityAutopilot) || caps.Has(capabilitySquadDelegation) {
		b.WriteString("### Additional Write\n")
	}
	if caps.Has(capabilityFullMentions) || caps.Has(capabilitySquadDelegation) {
		b.WriteString("- `multica issue assign <id> --to <name>|--to-id <uuid>` — Assign an issue to a member, agent, or squad. Use `--unassign` to clear the assignee.\n")
	}
	if caps.Has(capabilityLabelsSubscribers) {
		b.WriteString("- Note: `multica issue create` does not accept labels or subscribers directly; attach them after creation with the commands below.\n")
		b.WriteString("- `multica issue label add <issue-id> <label-id>` — Attach a label to an issue (look up the label id via `multica label list`)\n")
		b.WriteString("- `multica issue label remove <issue-id> <label-id>` — Detach a label from an issue\n")
		b.WriteString("- `multica issue subscriber add <issue-id> [--user <name>|--user-id <uuid>]` — Subscribe a member or agent to issue updates (defaults to the caller when neither flag is set; the two flags are mutually exclusive)\n")
		b.WriteString("- `multica issue subscriber remove <issue-id> [--user <name>|--user-id <uuid>]` — Unsubscribe a member or agent\n")
	}
	if caps.Has(capabilityLabelsSubscribers) {
		b.WriteString("- `multica label create --name \"...\" --color \"#hex\"` — Define a new workspace label (use this only when the label you need does not exist yet; reuse existing labels via `multica label list` first)\n")
	}
	if caps.Has(capabilityAutopilot) {
		b.WriteString("- `multica autopilot create --title \"...\" --agent <name> --mode create_issue|run_only [--description \"...\"]` — Create an autopilot\n")
		b.WriteString("- `multica autopilot update <id> [--title X] [--description X] [--status active|paused] [--mode create_issue|run_only]` — Update an autopilot\n")
		b.WriteString("- `multica autopilot trigger <id>` — Manually trigger an autopilot to run once\n")
		b.WriteString("- `multica autopilot delete <id>` — Delete an autopilot\n")
	}
	if caps.Has(capabilitySquadDelegation) {
		b.WriteString("- `multica squad activity <issue-id> action|no_action|failed [--reason \"...\"] [--output json]` — Record a squad leader evaluation decision in the issue timeline\n")
	}
	if caps.Has(capabilityFullMentions) || caps.Has(capabilityLabelsSubscribers) || caps.Has(capabilityAutopilot) || caps.Has(capabilitySquadDelegation) {
		b.WriteString("\n")
	}
}

func writeProjectContext(b *strings.Builder, ctx TaskContextForEnv, caps runtimeCapabilities) {
	if !caps.Has(capabilityProjectResources) || (ctx.ProjectID == "" && len(ctx.ProjectResources) == 0) {
		return
	}

	b.WriteString("## Project Context\n\n")
	if ctx.ProjectTitle != "" {
		fmt.Fprintf(b, "This issue belongs to **%s**.\n\n", ctx.ProjectTitle)
	}
	if len(ctx.ProjectResources) > 0 {
		b.WriteString("Project resources (also written to `.multica/project/resources.json`):\n\n")
		for _, r := range ctx.ProjectResources {
			fmt.Fprintf(b, "- %s\n", formatProjectResource(r))
		}
		b.WriteString("\nResources are pointers — open them only when relevant to the task. ")
		b.WriteString("For `github_repo` resources, use `multica repo checkout <url>` to fetch the code. Add `--ref <branch-or-sha>` when a task or handoff names an exact revision.\n\n")
	} else {
		b.WriteString("This project has no resources attached yet.\n\n")
	}
}

func writeMentionRules(b *strings.Builder, caps runtimeCapabilities) {
	b.WriteString("## Mentions\n\n")
	if !caps.Has(capabilityFullMentions) {
		b.WriteString("Do not create member or agent mention links unless the task explicitly requires notifying or delegating to someone; plain names are safe.\n\n")
		return
	}

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
}

func writeAttachmentRules(b *strings.Builder, caps runtimeCapabilities) {
	if !caps.Has(capabilityAttachments) {
		return
	}

	b.WriteString("## Attachments\n\n")
	b.WriteString("Issues and comments may include file attachments (images, documents, etc.).\n")
	b.WriteString("Use the download command to fetch attachment files locally:\n\n")
	b.WriteString("```\nmultica attachment download <attachment-id>\n```\n\n")
	b.WriteString("This downloads the file to the current directory and prints the local path. Use `-o <dir>` to save elsewhere.\n")
	b.WriteString("After downloading, you can read the file directly (e.g. view an image, read a document).\n\n")
}
