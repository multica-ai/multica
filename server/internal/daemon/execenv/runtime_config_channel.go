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
	b.WriteString("- `multica channel context <channel-id> --message <message-id> --include-replies --recent 20 --output json` — Fetch channel info, members, the triggering message, its replies, and recent channel messages. Use this only when you need conversation history beyond the triggering message.\n")
	b.WriteString("- `multica channel message list <channel-id> --output json` — List recent top-level messages in the channel timeline.\n")
	b.WriteString("- `multica channel message send <channel-id> --content \"...\"` — Post a top-level message on the channel timeline. **Default when the user @-mentioned you from the main message area.**\n")
	b.WriteString("- `multica channel message reply <channel-id> <message-id> --content \"...\"` — Reply inside a message's thread (auto-creates a thread when the target is a top-level message). **Default when the user @-mentioned you from the replies panel** — use the thread root message ID, not the triggering reply ID.\n")
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
	b.WriteString("Use `--content \"...\"` to pass the message body inline. For multi-line messages, use your shell's quoting (e.g. `--content $'line1\\nline2'` on bash/zsh). Keep messages concise.\n\n")
}

// writeChannelWorkflow emits the Workflow body for channel-mention tasks:
// work from the triggering message, pull channel context on demand, and stay
// off the issue CLI unless the work explicitly calls for an issue.
func writeChannelWorkflow(b *strings.Builder, ctx TaskContextForEnv) {
	b.WriteString("**This task was triggered by a channel message mention.** There is no assigned Multica issue for this run. The triggering message content in the prompt IS the user's request — act on it directly. Only fetch channel context if you need surrounding conversation history.\n\n")
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
	b.WriteString("- The triggering message in the prompt is your primary input. Run the channel context CLI only when you need conversation history:\n")
	fmt.Fprintf(b, "  `multica channel context %s --message %s --include-replies --recent 20 --output json`\n", ctx.ChannelID, ctx.ChannelMessageID)
	b.WriteString("- Do NOT run `multica issue get`, `multica issue metadata list`, `multica issue comment list`, `multica issue comment add`, or `multica issue status` unless you explicitly decide to create or update an issue as part of the work.\n")
	if ctx.ChannelThreadRootMsgID != "" {
		fmt.Fprintf(b, "- **Default (replies panel):** reply in the same thread: `multica channel message reply %s %s --content \"...\"` (the second ID is the thread root). Do NOT reply to the triggering message directly — that would create a nested thread.\n", ctx.ChannelID, ctx.ChannelThreadRootMsgID)
		fmt.Fprintf(b, "- Use `multica channel message send %s --content \"...\"` only when the triggering message or recent conversation explicitly asks for a main-timeline answer.\n\n", ctx.ChannelID)
	} else {
		fmt.Fprintf(b, "- **Default (main message area):** post on the channel timeline: `multica channel message send %s --content \"...\"`. Do NOT use `message reply` unless the triggering message or recent conversation explicitly asks you to answer in a thread instead.\n\n", ctx.ChannelID)
	}
	b.WriteString("- If the triggering message or channel context you fetch (`--include-replies --recent N`) states a different delivery preference, follow that instead of the defaults above.\n\n")
}

// writeChannelOutput emits the Output body for channel-origin tasks: results
// go back to the channel (when a reply is useful), not to an issue comment.
func writeChannelOutput(b *strings.Builder, ctx TaskContextForEnv) {
	b.WriteString("This is a channel-origin task, not an Issue task. When a visible reply is useful, post it where the user invoked you — main timeline for a main-area @-mention, the same thread for a replies-panel @-mention — unless recent conversation explicitly states otherwise.\n\n")
	if ctx.ChannelThreadRootMsgID != "" {
		fmt.Fprintf(b, "- Default: reply in the same thread: `multica channel message reply %s %s --content \"...\"`.\n", ctx.ChannelID, ctx.ChannelThreadRootMsgID)
		fmt.Fprintf(b, "- Use `multica channel message send %s --content \"...\"` only when explicitly asked for a main-timeline answer.\n", ctx.ChannelID)
	} else {
		fmt.Fprintf(b, "- Default: post on the main timeline: `multica channel message send %s --content \"...\"`.\n", ctx.ChannelID)
		b.WriteString("- Use `multica channel message reply` only when explicitly asked to answer in a thread.\n")
	}
	b.WriteString("- Do NOT call `multica issue comment add` for this task unless you explicitly created or selected a real issue that needs a comment.\n")
}
