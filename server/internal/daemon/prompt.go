package daemon

import (
	"fmt"
	"strings"
)

// BuildPrompt constructs the task prompt for an agent CLI.
// Keep this minimal — detailed instructions live in CLAUDE.md / AGENTS.md
// injected by execenv.InjectRuntimeConfig.
func BuildPrompt(task Task) string {
	if task.ChatSessionID != "" {
		return buildChatPrompt(task)
	}
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n", task.IssueID)
	if task.Issue != nil {
		fmt.Fprintf(&b, "Issue: %s", task.Issue.Title)
		if task.Issue.Identifier != "" {
			fmt.Fprintf(&b, " (%s)", task.Issue.Identifier)
		}
		b.WriteString("\n")
	}
	b.WriteString("\nStart by reading `.agent_context/issue_context.md` in your workdir. It contains the issue details Multica injected before launch, so do not depend on `multica issue get` just to understand the task.\n")
	b.WriteString("Use the `multica` CLI only for follow-up platform reads or writeback when it is available.\n")
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
