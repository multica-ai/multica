package daemon

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// BuildPrompt constructs the task prompt for an agent CLI.
// Keep this minimal — detailed instructions live in CLAUDE.md / AGENTS.md
// injected by execenv.InjectRuntimeConfig.
func BuildPrompt(task Task) string {
	return BuildPromptWithRunMode(task, protocol.ResolveTaskRunMode(task.Context))
}

func BuildPromptWithRunMode(task Task, runMode string) string {
	if task.ChatSessionID != "" {
		return buildChatPrompt(task)
	}
	if task.TriggerCommentID != "" {
		return buildCommentPrompt(task, runMode)
	}
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	writeLanguageInstruction(&b)
	writeRunModeInstruction(&b, runMode)
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", task.IssueID)
	return b.String()
}

// buildCommentPrompt constructs a prompt for comment-triggered tasks.
// The triggering comment content is embedded directly so the agent cannot
// miss it, even when stale output files exist in a reused workdir.
// The reply instructions (including the current TriggerCommentID as --parent)
// are re-emitted on every turn so resumed sessions cannot carry forward a
// previous turn's --parent UUID.
func buildCommentPrompt(task Task, runMode string) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	writeLanguageInstruction(&b)
	writeRunModeInstruction(&b, runMode)
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	if task.TriggerCommentContent != "" {
		b.WriteString("[NEW COMMENT] A user just left a new comment that triggered this task. You MUST respond to THIS comment, not any previous ones:\n\n")
		fmt.Fprintf(&b, "> %s\n\n", task.TriggerCommentContent)
	}
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n\n", task.IssueID)
	b.WriteString(execenv.BuildCommentReplyInstructions(task.IssueID, task.TriggerCommentID))
	return b.String()
}

// buildChatPrompt constructs a prompt for interactive chat tasks.
func buildChatPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a chat assistant for a Multica workspace.\n")
	b.WriteString("A user is chatting with you directly. Respond to their message.\n\n")
	writeLanguageInstruction(&b)
	fmt.Fprintf(&b, "User message:\n%s\n", task.ChatMessage)
	return b.String()
}

func writeLanguageInstruction(b *strings.Builder) {
	b.WriteString("Use the same language as the user's request for all visible natural-language output, unless the user explicitly asks for another language.\n")
	b.WriteString("This includes plans, progress notes, revision explanations, execution updates, and final replies.\n")
	b.WriteString("Do not switch languages mid-run for user-facing text. Keep commands, file paths, code, and error literals unchanged when needed.\n\n")
}

func writeRunModeInstruction(b *strings.Builder, runMode string) {
	if runMode != protocol.TaskRunModePlan {
		return
	}
	b.WriteString("Run mode: PLAN ONLY.\n")
	b.WriteString("Do not modify files, run destructive commands, or perform implementation in this run. Produce a clear, actionable plan for the user's request, call out risks or unknowns, and wait for a later user confirmation before execution. Keep the plan in the user's language.\n\n")
}
