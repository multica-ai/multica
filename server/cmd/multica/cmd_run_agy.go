package main

import (
	"fmt"
	"strings"
)

type agyLocalRunProvider struct{}

func (agyLocalRunProvider) Name() string { return "agy" }

func (agyLocalRunProvider) Run(args []string, cwd string, env localCLIEnv, reporter *localRunReporter, usageReporter *localRunUsageReporter) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("missing agy command")
	}
	if err := validateAgyLocalRunArgs(args[1:]); err != nil {
		return 1, err
	}
	childArgs := agyLocalRunChildArgs(args)
	return runProviderPTY(childArgs, cwd, env, "")
}

// validateAgyLocalRunArgs rejects flags that multica manages automatically.
func validateAgyLocalRunArgs(args []string) error {
	for _, arg := range args {
		// -i / --prompt-interactive would re-enter interactive mode; the PTY
		// already provides an interactive session, so passing it is redundant
		// and could confuse the CLI's argument parsing.
		if arg == "-i" || arg == "--prompt-interactive" {
			return fmt.Errorf("multica run already starts agy interactively; remove %s from the command", arg)
		}
	}
	return nil
}

func agyLocalRunChildArgs(args []string) []string {
	return append([]string{args[0]}, args[1:]...)
}

// agyLocalRunSystemPrompt is reserved for future use when agy supports
// injecting runtime context (similar to Claude's --append-system-prompt).
// Today the Antigravity CLI has no equivalent flag; instructions are delivered
// via AGENTS.md in the task workdir.
func agyLocalRunSystemPrompt(issueID string) string {
	issueID = strings.TrimSpace(issueID)
	if issueID == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("Multica local run context:\n")
	fmt.Fprintf(&b, "Bound Multica issue ID: %s\n\n", issueID)
	fmt.Fprintf(&b, "- Get issue details: multica issue get %s --output json\n", issueID)
	fmt.Fprintf(&b, "- Get issue comments: multica issue comment list %s --output json\n", issueID)
	return b.String()
}
