package agent

import "github.com/multica-ai/multica/server/pkg/redact"

func safeAgentCommandArgs(args []string) []string {
	safe := make([]string, len(args))
	for i, arg := range args {
		safe[i] = redact.Text(arg)
	}
	return safe
}
