package execenv

// Fork (OPE-1943): channel-scenario sections of the agent brief.
//
// This file is Fork-only — upstream multica-ai/multica has no channel feature
// in runtime_config.go. Keeping every channel-specific helper, predicate, and
// copy string here (instead of inline in buildMetaSkillContent) keeps
// runtime_config.go byte-close to upstream, so upstream merges only ever
// conflict at the small `if isChannelTask(ctx)` hook sites, never inside
// channel copy. When editing channel agent behavior, edit this file; when
// merging upstream, this file needs no attention.

import (
	"fmt"
	"strings"
)

// isChannelTask reports whether this run was triggered by a channel-message
// mention. The chat / quick-create exclusions mirror the discriminator
// precedence of the Workflow chain in buildMetaSkillContent (chat >
// quick-create > channel > ...) so every hook site classifies the task
// identically.
func isChannelTask(ctx TaskContextForEnv) bool {
	return ctx.ChatSessionID == "" && ctx.QuickCreatePrompt == "" && ctx.ChannelID != ""
}

// Channel wording for the one-line spots where the shared brief scaffold
// (Mentions / Attachments / CLI reminder in buildMetaSkillContent) differs
// between channel and issue tasks. The surrounding sections stay inline in
// runtime_config.go in their upstream form; only these strings are swapped in
// at the hook sites.
const (
	// Mentions → "When NOT to use a mention link" loop-prevention bullet.
	channelMentionsReplyLine = "- **Replying to another agent that just spoke to you.** By default, do NOT put a `mention://agent/...` link anywhere in your reply. Everyone in the channel already sees your message; re-mentioning the other agent will make them run again, and if they reply with a mention back, you will be triggered again. That is a loop and it costs the user money.\n"

	// Attachments → carrier noun (channel messages instead of issues/comments).
	channelAttachmentsLine = "Channel messages may include file attachments (images, documents, etc.).\n"

	// "Always Use the multica CLI" → escalation verb (channel message instead
	// of issue comment).
	channelCliWorkaroundLine = "do NOT attempt to work around it. Instead, send a channel message mentioning the workspace owner to request the missing functionality.\n\n"
)

// writeChannelCommands emits the Available Commands menu for channel-origin
// tasks: the channel CLI surface plus repo checkout, with `issue create`
// explicitly gated so an ordinary channel question never spawns an issue.
func writeChannelCommands(b *strings.Builder) {
	b.WriteString("This is a channel-origin task. The commands below are the ones you will normally need; run `multica channel --help` or `multica <command> --help` for anything else.\n\n")
	b.WriteString("### Core\n")
	b.WriteString("- `multica channel context <channel-id> --message <message-id> --include-replies --recent 20 --output json` — Fetch channel info, members, the triggering message, its replies, and recent channel messages. This is your primary context source — read it before acting.\n")
	b.WriteString("- `multica channel message list <channel-id> --output json` — List recent top-level messages in the channel timeline.\n")
	b.WriteString("- `multica channel message send <channel-id> [--content \"...\" | --content-stdin | --content-file <path>]` — Post a top-level message to the channel. Use this for your final result so it appears in the channel timeline.\n")
	b.WriteString("- `multica channel message reply <channel-id> <message-id> [--content \"...\" | --content-stdin | --content-file <path>]` — Reply to a specific message (auto-creates a thread). Use this when the result should stay attached to the triggering message.\n")
	b.WriteString("- `multica channel member list <channel-id> --output json` — List the channel's members (people and agents) when you need to know who is in the conversation.\n")
	b.WriteString("- `multica repo checkout <url> [--ref <branch-or-sha>]` — Check out a repository into the working directory when the task needs code (creates a git worktree with a dedicated branch).\n")
	b.WriteString("- `multica issue create --title \"...\" [--description \"...\" | --description-stdin | --description-file <path>] [--priority X] [--status X] [--assignee X | --assignee-id <uuid>] [--project <project-id>]` — Create an issue **only if the channel conversation explicitly asks you to open one**. Do not create an issue for an ordinary channel question.\n\n")
}

// writeChannelReplyFormatting is the channel counterpart of the issue
// "Comment Formatting" guardrail: same shell-escape hazard (MUL-2904) and the
// same Windows encoding hazard (#2198/#2236/#2376), applied to the channel
// message/reply verbs instead of issue comments.
func writeChannelReplyFormatting(b *strings.Builder) {
	b.WriteString("## Channel Reply Formatting\n\n")
	b.WriteString("When you post a channel message or reply, write well-structured Markdown and keep it natural and concise — state the outcome, not the process.\n")
	if runtimeGOOS == "windows" {
		b.WriteString("On Windows, **always write the message body to a UTF-8 file with your file-write tool first, then post it with `--content-file <path>`** — do NOT pipe via `--content-stdin`. PowerShell 5.1's `$OutputEncoding` defaults to ASCIIEncoding when piping to a native command, silently dropping non-ASCII characters as `?` before they reach `multica.exe`. Never use inline `--content` for agent-authored bodies.\n\n")
	} else {
		b.WriteString("Use `--content-stdin` with a quoted HEREDOC (`<<'MSG'`) so the shell does not expand backticks, `$()`, or `$VAR` inside the body; `--content-file <path>` works too. Never inline `--content` for agent-authored bodies — unescaped backticks, `$()`, `$VAR`, or quotes are rewritten by the shell before the CLI receives them.\n\n")
	}
}

// writeChannelWorkflow emits the Workflow body for channel-mention tasks:
// work from the triggering message, pull channel context on demand, and stay
// off the issue CLI unless the work explicitly calls for an issue.
func writeChannelWorkflow(b *strings.Builder, ctx TaskContextForEnv) {
	b.WriteString("**This task was triggered by a channel message mention.** There is no assigned Multica issue for this run. Work from the triggering channel message and fetch channel context on demand.\n\n")
	fmt.Fprintf(b, "- Channel ID: `%s`\n", ctx.ChannelID)
	if ctx.ChannelName != "" {
		fmt.Fprintf(b, "- Channel: %s\n", ctx.ChannelName)
	}
	if ctx.ChannelMessageID != "" {
		fmt.Fprintf(b, "- Triggering message ID: `%s`\n", ctx.ChannelMessageID)
	}
	if ctx.ChannelThreadRootMsgID != "" {
		fmt.Fprintf(b, "- Thread root message ID: `%s` (reply here to keep your response in the same thread)\n", ctx.ChannelThreadRootMsgID)
	}
	b.WriteString("- Start by reading the triggering message in the user prompt, then run the channel context CLI if you need more context:\n")
	fmt.Fprintf(b, "  `multica channel context %s --message %s --include-replies --recent 20 --output json`\n", ctx.ChannelID, ctx.ChannelMessageID)
	b.WriteString("- Do NOT run `multica issue get`, `multica issue metadata list`, `multica issue comment list`, `multica issue comment add`, or `multica issue status` unless you explicitly decide to create or update an issue as part of the work.\n")
	if ctx.ChannelThreadRootMsgID != "" {
		fmt.Fprintf(b, "- To reply in the same thread, use `multica channel message reply %s %s` (the thread root message). Do NOT reply to the triggering message directly — that would create a nested thread.\n", ctx.ChannelID, ctx.ChannelThreadRootMsgID)
		b.WriteString("- To send a top-level channel message (outside the thread), use `multica channel message send`.\n\n")
	} else {
		b.WriteString("- If you need to reply in the channel, use `multica channel message reply` for the triggering message or `multica channel message send` for a top-level channel message.\n\n")
	}
}

// writeChannelOutput emits the Output body for channel-origin tasks: results
// go back to the channel (when a reply is useful), not to an issue comment.
func writeChannelOutput(b *strings.Builder, ctx TaskContextForEnv) {
	b.WriteString("This is a channel-origin task, not an Issue task. Your final answer should normally be posted back to the channel only when a reply is useful.\n\n")
	if ctx.ChannelThreadRootMsgID != "" {
		fmt.Fprintf(b, "- To reply in the same thread, use `multica channel message reply %s %s --content \"...\"`.\n", ctx.ChannelID, ctx.ChannelThreadRootMsgID)
		fmt.Fprintf(b, "- To send a top-level channel message (outside the thread), use `multica channel message send %s --content \"...\"`.\n", ctx.ChannelID)
	} else {
		b.WriteString("- To reply to the triggering message, use `multica channel message reply <channel-id> <message-id> --content \"...\"`.\n")
		b.WriteString("- To send a top-level channel message, use `multica channel message send <channel-id> --content \"...\"`.\n")
	}
	b.WriteString("- Do NOT call `multica issue comment add` for this task unless you explicitly created or selected a real issue that needs a comment.\n")
}
