package daemon

import (
	"fmt"
	"strings"
)

// BuildPrompt constructs the task prompt for an agent CLI.
// Keep this minimal — detailed instructions live in CLAUDE.md / AGENTS.md
// injected by execenv.InjectRuntimeConfig.
func BuildPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")

	if task.IsAgentflow() {
		af := task.Agentflow
		fmt.Fprintf(&b, "This task was triggered by Agentflow: **%s**\n\n", af.Title)
		if af.Description != nil && *af.Description != "" {
			b.WriteString("## Instructions\n\n")
			b.WriteString(*af.Description)
			b.WriteString("\n\n")
		}
		b.WriteString("Execute the instructions above. If the work requires tracking, create an issue using `multica issue create`.\n")
	} else {
		issueID := task.EffectiveIssueID()
		fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", issueID)
		fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", issueID)
	}

	return b.String()
}
