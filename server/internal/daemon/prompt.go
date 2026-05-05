package daemon

// CEREBRO-PATCH(daemon-prompt): cerebro modification of upstream file

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
	if task.TriggerCommentID != "" {
		return buildCommentPrompt(task)
	}
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", task.IssueID)
	return b.String()
}

// buildCommentPrompt constructs a prompt for comment-triggered tasks.
// The triggering comment content is embedded directly so the agent cannot
// miss it, even when stale output files exist in a reused workdir.
func buildCommentPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	if task.TriggerCommentContent != "" {
		b.WriteString("[NEW COMMENT] A user just left a new comment that triggered this task. You MUST respond to THIS comment, not any previous ones:\n\n")
		fmt.Fprintf(&b, "> %s\n\n", task.TriggerCommentContent)
	}
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", task.IssueID)
	return b.String()
}

// buildChatPrompt constructs a prompt for interactive chat tasks.
// ChatMessages holds every user message newer than the last assistant reply
// in chronological order — multiple entries appear when the user sent
// follow-ups while the prior agent turn was still running.
func buildChatPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a chat assistant for a Multica workspace.\n")
	if len(task.ChatMessages) > 1 {
		b.WriteString("A user is chatting with you directly. They sent multiple messages while you were responding — address all of them.\n\n")
		b.WriteString("User messages (oldest first):\n")
		for i, msg := range task.ChatMessages {
			fmt.Fprintf(&b, "%d. %s\n", i+1, msg)
		}
	} else {
		b.WriteString("A user is chatting with you directly. Respond to their message.\n\n")
		var msg string
		if len(task.ChatMessages) == 1 {
			msg = task.ChatMessages[0]
		}
		fmt.Fprintf(&b, "User message:\n%s\n", msg)
	}
	return b.String()
}
