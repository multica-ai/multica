package daemon

import (
	"fmt"
	"strings"
)

// BuildPrompt constructs the task prompt for an agent CLI.
// Keep this minimal — detailed instructions live in CLAUDE.md / AGENTS.md
// injected by execenv.InjectRuntimeConfig.
func BuildPrompt(task Task) string {
	return BuildPromptWithStartDir(task, "")
}

// BuildPromptWithStartDir is like BuildPrompt but mentions a pre-checked-out
// starting directory so the agent knows it doesn't need to run `multica repo
// checkout` for the primary repo.
func BuildPromptWithStartDir(task Task, startDir string) string {
	if task.ChatSessionID != "" {
		return buildChatPrompt(task)
	}
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	if startDir != "" {
		fmt.Fprintf(&b, "Your starting directory is already checked out at: %s\n", startDir)
		b.WriteString("This is a fresh git worktree on a dedicated agent branch; the user's working tree is untouched.\n\n")
	}
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", task.IssueID)
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
