package execenv

// CEREBRO-PATCH(user-profile-prompt): JEH-304 — render the compiled user
// communication profile into the runtime brief. Extracted to a cerebro sibling
// file after upstream syncs deleted the inline render block (FIR-2743); only
// the single marked call in runtime_config.go remains upstream.

import "strings"

// userProfilePromptSection renders the `## User Communication Profile` brief
// section. The user has explicitly configured how they want the agent to talk
// to them, so the section instructs the model to weight it over defaults.
// Returns "" when no profile is set so the caller writes nothing.
func userProfilePromptSection(prompt string) string {
	if strings.TrimSpace(prompt) == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("## User Communication Profile\n\n")
	b.WriteString("The user invoking this task has set explicit communication preferences. ")
	b.WriteString("Apply these to every response and tool message you produce. ")
	b.WriteString("These rules win over any default style you'd normally use.\n\n")
	b.WriteString("```\n")
	b.WriteString(prompt)
	b.WriteString("\n```\n\n")
	return b.String()
}
